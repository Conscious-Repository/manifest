package server

// Construction steward requests (P8): the agent pane's "ask the agent". The
// owner's instruction becomes one native delivery (same Accept/Claim/runner/
// Finish path as research steps) carrying a retained packet — the problem
// summary, the open alternative's exact revision and the command schema —
// with only the bounded construction tool scope. The reply may only be
// typed command envelopes; they run under a short-lived capability bound to
// this problem, as the agent, so they can edit drafts and propose decisions
// and nothing else. Without a satisfied preflight the route answers 503.

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"manifest/agentchat"
	"manifest/construction"
)

type constructionStewardRequest struct {
	ID        string                    `json:"id"`
	ProblemID string                    `json:"problemId"`
	Agent     string                    `json:"agent"`
	State     string                    `json:"state"` // pending | completed | failed
	Summary   string                    `json:"summary,omitempty"`
	Error     string                    `json:"error,omitempty"`
	Results   []constructionAgentResult `json:"results"`
	Native    construction.NativeRef    `json:"native"`
	capID     string
	subject   construction.SubjectRef
}

type constructionStewards struct {
	mu   sync.Mutex
	reqs map[string]*constructionStewardRequest // by request id
}

func (t *constructionStewards) live(capID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.reqs {
		if r.capID == capID && r.State == "pending" {
			return true
		}
	}
	return false
}

func (t *constructionStewards) get(id string) (constructionStewardRequest, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.reqs[id]
	if !ok {
		return constructionStewardRequest{}, false
	}
	return *r, true
}

func (t *constructionStewards) update(id string, fn func(r *constructionStewardRequest)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r := t.reqs[id]; r != nil {
		fn(r)
	}
}

func (s *Server) registerConstructionStewardRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/preflight", s.handleConstructionPreflight)
	mux.HandleFunc("POST "+p+"/problems/{id}/agent/requests", s.handleConstructionStewardRequest)
	mux.HandleFunc("GET "+p+"/problems/{id}/agent/requests/{req}", s.handleConstructionStewardStatus)
}

type constructionStewardBody struct {
	SchemaVersion int    `json:"schemaVersion"`
	RequestID     string `json:"requestId"`
	Text          string `json:"text"`
	AssemblyID    string `json:"assemblyId"`
}

func (s *Server) handleConstructionStewardRequest(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	body, ok := readConstructionBody(w, r, 64<<10)
	if !ok {
		return
	}
	var in constructionStewardBody
	if err := construction.DecodeRequest(body, &in); err != nil {
		constructionError(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if in.SchemaVersion != 1 || !construction.ValidRequestID(in.RequestID) || text == "" || len(text) > 4000 || !construction.ValidID("asm", in.AssemblyID) {
		constructionError(w, construction.Invalid("schemaVersion 1, requestId, text (≤4000) and assemblyId are required"))
		return
	}
	pid := r.PathValue("id")
	if prior, ok := s.construction.stewards.get(in.RequestID); ok {
		if prior.ProblemID != pid || prior.subject != sub {
			constructionError(w, construction.Conflict("request ID was already used for different content", nil))
			return
		}
		constructionJSON(w, map[string]any{"request": prior})
		return
	}
	if ready, notes := s.constructionNativeReady(); !ready {
		constructionError(w, construction.Unavailable(strings.Join(notes, "; ")))
		return
	}
	st, err := s.construction.store.Load(sub, pid)
	if err != nil {
		constructionError(w, err)
		return
	}
	a := st.Assemblies[in.AssemblyID]
	if a == nil {
		constructionError(w, construction.NotFound("no such assembly in this problem"))
		return
	}
	agent := constructionAgentName(st.Problem.Steward.Agent)
	capa, err := s.grantConstructionAgent(sub, pid, agent, "", 15*time.Minute)
	if err != nil {
		constructionError(w, err)
		return
	}
	packet, err := construction.Canonical(map[string]any{
		"kind":             "construction-steward-packet/1",
		"problemId":        pid,
		"title":            st.Problem.Title,
		"owner":            text,
		"assemblyId":       a.ID,
		"assemblyRevision": st.Revision("assembly:" + a.ID),
		"problemRevision":  st.Revision("problem"),
		"assembly":         a,
		"operations":       construction.OperationNames(),
		"notice":           construction.NonApprovalNotice,
		"instructions": "Do what the owner asked in \"owner\" by proposing typed commands. Reply with ONLY a JSON object {\"summary\": \"…\", \"commands\": [ {\"schemaVersion\":1, \"requestId\":\"<new 8–128 char id>\", \"problemId\":…, \"assemblyId\":…, \"expectedAssemblyRevision\":…, \"operations\":[{\"op\":…}]} ]}. " +
			"Use only the listed operations; you may edit draft or proposed alternatives and propose decisions, never approve or reject one. Commands you send are validated and may be refused.",
	})
	if err != nil {
		constructionError(w, err)
		return
	}
	hash, err := s.construction.store.RetainPacket(sub, pid, "pkt-"+in.RequestID, packet, actor)
	if err != nil {
		constructionError(w, err)
		return
	}
	conv, err := s.agentChat.store.CreateOnce(agent, "", "Construction steward "+strings.TrimPrefix(pid, "cp-")[:12], "", "cxsteward-"+strings.TrimPrefix(pid, "cp-"))
	if err != nil {
		constructionError(w, construction.Unavailable(err.Error()))
		return
	}
	desc := agentConversation("hermes", agent, conv, "private", "").Key
	if _, err := s.construction.store.AttachConversation(sub, pid, construction.ConversationRef{Agent: agent, Conversation: desc, Session: conv, Purpose: "steward"}, "cxsteward-"+strings.TrimPrefix(pid, "cp-")); err != nil {
		constructionError(w, err)
		return
	}
	req := &constructionStewardRequest{ID: in.RequestID, ProblemID: pid, Agent: agent, State: "pending", Results: []constructionAgentResult{}, capID: capa.ID, subject: sub,
		Native: construction.NativeRef{Agent: agent, Session: conv, Conversation: desc, RequestID: "cxs-" + in.RequestID, PacketHash: hash, ToolScope: s.construction.opts.AgentToolsets, State: "dispatched"}}
	t := &s.construction.stewards
	t.mu.Lock()
	if t.reqs == nil {
		t.reqs = map[string]*constructionStewardRequest{}
	}
	t.reqs[in.RequestID] = req
	t.mu.Unlock()
	recipient := agentchat.Recipient{Agent: agent, Profile: "", RequestedModel: ""}
	mctx := &agentchat.MessageContext{Conversation: desc, Agent: agent, Recipient: &recipient, Construction: &agentchat.ConstructionContext{
		Subject: sub.Kind + ":" + sub.ID, ProblemID: pid, Stage: "steward", PacketHash: hash, Capability: capa.ID, ToolScope: s.construction.opts.AgentToolsets}}
	if _, err := s.agentChat.store.Accept(agent, conv, req.Native.RequestID, text, mctx); err != nil {
		t.update(in.RequestID, func(r *constructionStewardRequest) { r.State, r.Error = "failed", err.Error() })
		s.revokeConstructionAgent(capa.ID)
		constructionError(w, construction.Unavailable(err.Error()))
		return
	}
	s.startAgentChatDelivery(agent, conv)
	s.construction.runs.wg.Add(1)
	go func() {
		defer s.construction.runs.wg.Done()
		defer s.revokeConstructionAgent(capa.ID)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		res, err := s.awaitConstructionDelivery(ctx, agent, conv, req.Native.RequestID, req.Native)
		if err != nil {
			t.update(in.RequestID, func(r *constructionStewardRequest) { r.State, r.Error = "failed", err.Error() })
			return
		}
		reply, err := parseConstructionAgentReply(res.Reply)
		if err != nil {
			t.update(in.RequestID, func(r *constructionStewardRequest) { r.State, r.Error, r.Native = "failed", err.Error(), res.Ref })
			return
		}
		results := s.applyConstructionAgentReply(capa.ID, reply)
		t.update(in.RequestID, func(r *constructionStewardRequest) {
			r.State, r.Summary, r.Results, r.Native = "completed", reply.Summary, results, res.Ref
			for _, x := range results {
				if x.Status != 200 {
					r.Error = "a command was refused: " + x.Error
				}
			}
		})
	}()
	got, _ := t.get(in.RequestID)
	constructionJSON(w, map[string]any{"request": got})
}

func (s *Server) handleConstructionStewardStatus(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	req, ok := s.construction.stewards.get(r.PathValue("req"))
	if !ok || req.ProblemID != r.PathValue("id") || req.subject != sub {
		constructionError(w, construction.NotFound("no such agent request in this process (after a restart, see the native conversation)"))
		return
	}
	constructionJSON(w, map[string]any{"request": req})
}
