package server

// Construction agent tool (P7.2): a bounded, revocable capability that lets
// one agent submit typed construction commands for one problem — through
// the same ParseCommand → ExecuteCommand path as the owner's inspector, under
// an agent actor that names the capability. Agents may edit draft/proposed
// assemblies, views, annotations and evidence links, and propose decisions;
// owner-only operations (accept/reject a decision, steward, problem facts,
// catalog and evidence entry) are refused with 403 and change nothing. The
// capability is bound to its subject and problem: a command naming any other
// problem is refused, and no request field can switch the subject. It is
// in-process only — no HTTP route accepts a capability — so a native agent's
// structured reply reaches it only through the server.

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"manifest/construction"
)

type constructionAgentCap struct {
	ID        string
	Subject   construction.SubjectRef
	ProblemID string
	Agent     string
	Run       string
	Expires   time.Time
	Revoked   bool
}

type constructionAgentTools struct {
	mu   sync.Mutex
	caps map[string]*constructionAgentCap
}

// grantConstructionAgent mints a capability for one agent on one problem.
func (s *Server) grantConstructionAgent(sub construction.SubjectRef, pid, agent, run string, ttl time.Duration) (*constructionAgentCap, error) {
	if s.construction == nil {
		return nil, construction.Unavailable("construction is not enabled")
	}
	if agent != "alfred" && agent != "zeck" {
		return nil, construction.Invalid("agent must be alfred or zeck")
	}
	if _, err := s.construction.store.ReadHead(sub, pid); err != nil {
		return nil, err
	}
	c := &constructionAgentCap{ID: "cxcap-" + newConstructionNonce()[:32], Subject: sub, ProblemID: pid, Agent: agent, Run: run, Expires: time.Now().Add(ttl)}
	t := &s.construction.agents
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.caps == nil {
		t.caps = map[string]*constructionAgentCap{}
	}
	now := time.Now()
	for id, old := range t.caps {
		if old.Revoked || now.After(old.Expires) {
			delete(t.caps, id)
		}
	}
	t.caps[c.ID] = c
	return c, nil
}

func (s *Server) revokeConstructionAgent(id string) {
	if s.construction == nil {
		return
	}
	t := &s.construction.agents
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.caps[id]; c != nil {
		c.Revoked = true
	}
}

func (s *Server) constructionAgentCap(id string) (*constructionAgentCap, error) {
	t := &s.construction.agents
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.caps[id]
	if c == nil || c.Revoked || time.Now().After(c.Expires) {
		return nil, construction.Forbidden("the agent capability is unknown, revoked or expired")
	}
	cp := *c
	return &cp, nil
}

// constructionAgentCommand executes one typed command as the capability's
// agent. The subject and problem come from the capability, never the body.
func (s *Server) constructionAgentCommand(capID string, body []byte) (*construction.State, *construction.Receipt, error) {
	c, err := s.constructionAgentCap(capID)
	if err != nil {
		return nil, nil, err
	}
	if len(body) > construction.MaxCommandBytes {
		return nil, nil, construction.ErrTooLarge
	}
	pc, err := construction.ParseCommand(body)
	if err != nil {
		return nil, nil, err
	}
	if pc.Command.ProblemID != c.ProblemID {
		return nil, nil, construction.Forbidden("the agent capability is bound to problem " + c.ProblemID)
	}
	actor := construction.AgentActor(c.Agent, c.ID, c.Run)
	return s.construction.store.ExecuteCommand(c.Subject, pc, actor, &construction.ApplyContext{ResolveScope: s.constructionScopeResolver(c.Subject)})
}

// constructionAgentReply is the only shape a steward reply may take: a
// summary and typed command envelopes. Free text, scripts or any other field
// are refused.
type constructionAgentReply struct {
	Summary  string            `json:"summary"`
	Commands []json.RawMessage `json:"commands"`
}

func parseConstructionAgentReply(reply string) (*constructionAgentReply, error) {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 && j < len(s)-1 {
		s = s[:j+1]
	}
	if len(s) > construction.MaxCommandBytes {
		return nil, construction.ErrTooLarge
	}
	var out constructionAgentReply
	d := json.NewDecoder(bytes.NewReader([]byte(s)))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return nil, construction.Invalid("the agent reply is not the command schema: " + err.Error())
	}
	if len(out.Commands) > 8 {
		return nil, construction.Invalid("an agent reply carries at most 8 commands")
	}
	if len(out.Summary) > 2000 {
		out.Summary = out.Summary[:2000]
	}
	return &out, nil
}

// constructionAgentResult is one executed (or refused) agent command.
type constructionAgentResult struct {
	RequestID string                `json:"requestId,omitempty"`
	Receipt   *construction.Receipt `json:"receipt,omitempty"`
	Error     string                `json:"error,omitempty"`
	Status    int                   `json:"status"`
}

// applyConstructionAgentReply executes a steward reply's commands in order
// under the capability; the first refusal stops the rest (each command is
// its own atomic commit; nothing is merged silently).
func (s *Server) applyConstructionAgentReply(capID string, reply *constructionAgentReply) []constructionAgentResult {
	var out []constructionAgentResult
	for _, raw := range reply.Commands {
		var probe struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(raw, &probe)
		_, rc, err := s.constructionAgentCommand(capID, raw)
		if err != nil {
			out = append(out, constructionAgentResult{RequestID: probe.RequestID, Error: err.Error(), Status: construction.StatusOf(err)})
			break
		}
		out = append(out, constructionAgentResult{RequestID: probe.RequestID, Receipt: rc, Status: 200})
	}
	return out
}
