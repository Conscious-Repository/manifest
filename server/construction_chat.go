package server

// Problem chat (2026-10-09): one ordinary Alfred conversation per
// construction problem, so the owner steers in chat — describe the issue,
// ask for research, narrow in on an approach — while the problem's records
// stay the durable place the answers land.
//
// Unlike a construction step (a retained packet, no tools), this is a normal
// chat turn: the conversation history, the chat's read-only tools (web search
// included), plus a brief of the problem's current state appended to the
// prompt. Alfred never writes the problem. When he wants a change he ends his
// reply with a ```construction block of typed operations; the page shows it
// as a proposal and the owner applies it through the ordinary command API,
// so every applied change is a revision with the owner as actor.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"manifest/agentchat"
	"manifest/construction"
)

const constructionChatPrefix = "cxchat-"

func constructionChatRequest(problemID string) string {
	return constructionChatPrefix + strings.TrimPrefix(problemID, construction.KindProblem+"-")
}

func (s *Server) registerConstructionChatRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/problems/{id}/chat", s.handleConstructionChatGet)
	mux.HandleFunc("POST "+p+"/problems/{id}/chat", s.handleConstructionChatPost)
}

type constructionChatTurn struct {
	N    int    `json:"n"`
	Who  string `json:"who"`
	At   string `json:"at"`
	Text string `json:"text"`
}

type constructionChatView struct {
	Agent   string                 `json:"agent"`
	Session string                 `json:"session,omitempty"`
	Href    string                 `json:"href,omitempty"`
	Pending bool                   `json:"pending"`
	Error   string                 `json:"error,omitempty"`
	Turns   []constructionChatTurn `json:"turns"`
}

// constructionChatSession finds the problem's chat without creating it.
func (s *Server) constructionChatSession(agent, problemID string) (agentchat.Session, bool) {
	if s.agentChat == nil {
		return agentchat.Session{}, false
	}
	want := constructionChatRequest(problemID)
	for _, sess := range s.agentChat.store.List(agent) {
		if sess.CreateRequest == want {
			return sess, true
		}
	}
	return agentchat.Session{}, false
}

func (s *Server) constructionChatView(agent string, sess agentchat.Session, ok bool) constructionChatView {
	v := constructionChatView{Agent: agent, Turns: []constructionChatTurn{}}
	if !ok {
		return v
	}
	v.Session = sess.ID
	v.Href = "#/chat/a/" + agent + "/" + sess.ID
	full, body, _, found := s.agentChat.store.Get(agent, sess.ID)
	if !found {
		return v
	}
	for _, d := range full.Deliveries {
		switch d.State {
		case agentchat.DeliveryQueued, agentchat.DeliveryRunning:
			v.Pending = true
		case agentchat.DeliveryFailed, agentchat.DeliveryInterrupted:
			v.Error = d.Error
		case agentchat.DeliveryCompleted:
			v.Error = ""
		}
	}
	turns := agentchat.ParseTurns(body)
	if len(turns) > 40 {
		turns = turns[len(turns)-40:]
	}
	for _, t := range turns {
		text := t.Text
		if t.Who != "user" {
			text = agentchat.SayBody(text)
		}
		v.Turns = append(v.Turns, constructionChatTurn{N: t.N, Who: t.Who, At: t.At, Text: text})
	}
	return v
}

func (s *Server) handleConstructionChatGet(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	st, err := s.construction.store.Load(sub, r.PathValue("id"))
	if err != nil {
		constructionError(w, err)
		return
	}
	agent := constructionAgentName(st.Problem.Steward.Agent)
	sess, found := s.constructionChatSession(agent, st.Problem.ID)
	constructionJSON(w, map[string]any{"chat": s.constructionChatView(agent, sess, found)})
}

type constructionChatBody struct {
	SchemaVersion int    `json:"schemaVersion"`
	RequestID     string `json:"requestId"`
	Text          string `json:"text"`
}

func (s *Server) handleConstructionChatPost(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	raw, ok := readConstructionBody(w, r, 64<<10)
	if !ok {
		return
	}
	var in constructionChatBody
	if err := construction.DecodeRequest(raw, &in); err != nil {
		constructionError(w, err)
		return
	}
	text := strings.TrimSpace(in.Text)
	if in.SchemaVersion != 1 || !construction.ValidRequestID(in.RequestID) || text == "" || len(text) > 8000 {
		constructionError(w, construction.Invalid("schemaVersion 1, requestId and text (≤8000) are required"))
		return
	}
	if s.agentChat == nil {
		constructionError(w, construction.Unavailable("chat is not wired here"))
		return
	}
	st, err := s.construction.store.Load(sub, r.PathValue("id"))
	if err != nil {
		constructionError(w, err)
		return
	}
	pid := st.Problem.ID
	agent := constructionAgentName(st.Problem.Steward.Agent)
	profile, err := s.resolveAgentChat(r.Context(), agent)
	if err != nil {
		constructionError(w, construction.Unavailable("agent "+agent+" is unavailable here: "+err.Error()))
		return
	}
	conv, err := s.agentChat.store.CreateOnce(agent, profile, "Construction · "+strings.TrimPrefix(pid, "cp-")[:12], "", constructionChatRequest(pid))
	if err != nil {
		constructionError(w, construction.Unavailable(err.Error()))
		return
	}
	desc := agentConversation("hermes", agent, conv, "private", "").Key
	if _, err := s.construction.store.AttachConversation(sub, pid, construction.ConversationRef{Agent: agent, Conversation: desc, Session: conv, Purpose: "chat"}, constructionChatRequest(pid)); err != nil {
		constructionError(w, err)
		return
	}
	recipient := agentchat.Recipient{Agent: agent, Profile: profile}
	if _, err := s.agentChat.store.Accept(agent, conv, "cxc-"+in.RequestID, text, &agentchat.MessageContext{Conversation: desc, Agent: agent, Recipient: &recipient}); err != nil {
		constructionError(w, construction.Unavailable(err.Error()))
		return
	}
	s.startAgentChatDelivery(agent, conv)
	sess, found := s.constructionChatSession(agent, pid)
	constructionJSON(w, map[string]any{"chat": s.constructionChatView(agent, sess, found)})
}

// constructionChatBrief is appended to a problem chat's prompt: the problem
// as it stands now and how to propose changes. "" for any other chat.
func (s *Server) constructionChatBrief(sess agentchat.Session) string {
	if s.construction == nil || !strings.HasPrefix(sess.CreateRequest, constructionChatPrefix) {
		return ""
	}
	pid := construction.KindProblem + "-" + strings.TrimPrefix(sess.CreateRequest, constructionChatPrefix)
	refs, err := s.construction.store.Problems()
	if err != nil {
		return ""
	}
	for _, ref := range refs {
		if ref.ID != pid {
			continue
		}
		st, err := s.construction.store.Load(ref.Subject, pid)
		if err != nil {
			return ""
		}
		return constructionBriefText(st)
	}
	return ""
}

// the operations a proposal may carry, with their fields
var constructionChatOps = []string{"AddQuestion", "AnswerQuestion", "SetQuestionState", "AddFact", "SetContext", "SetProblemText",
	"CreateVariant", "SetAssemblyText", "SetJunctionStrategy", "SetWallCondition", "SetPitch", "SetDimension", "SetMaterial", "SetProduct",
	"RemoveComponent", "ProposeDecision"}

// constructionOpFields lists an operation's JSON fields and their types
// (optional ones marked "?"), read from the struct so it never drifts.
func constructionOpFields(op any) map[string]string {
	out := map[string]string{}
	t := reflect.TypeOf(op)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" || name == "op" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		typ := ft.Kind().String()
		switch ft.Kind() {
		case reflect.Slice:
			typ = "[]" + ft.Elem().Kind().String()
		case reflect.Map:
			typ = "map[string]" + ft.Elem().Kind().String()
		case reflect.Float64, reflect.Int:
			typ = "number"
		}
		if strings.Contains(opts, "omitempty") {
			typ += "?"
		}
		out[name] = typ
	}
	return out
}

func constructionBriefText(st *construction.State) string {
	p := st.Problem
	type comp struct {
		ID, Name, Role, Type string
		Material, Product    string `json:",omitempty"`
	}
	type asm struct {
		ID, Name, Summary, Lifecycle string
		Junction                     map[string]string
		Applicability                string
		Critical, Advisory           int
		CriticalIssues               []string `json:",omitempty"`
		Parameters                   map[string]any
		Components                   []comp
	}
	var asms []asm
	for _, id := range p.Alternatives {
		a := st.Assemblies[id]
		if a == nil {
			continue
		}
		x := asm{ID: a.ID, Name: a.Name, Summary: a.Summary, Lifecycle: a.Lifecycle, Applicability: a.Applicability.Status, Parameters: map[string]any{},
			Junction: map[string]string{"orientation": a.Junction.Orientation, "strategy": a.Junction.Strategy, "wall": a.Junction.WallCondition.Value,
				"supported": strings.Join(a.Junction.SupportedStrategies, ", ")}}
		for k, q := range a.Parameters {
			if q.Value != nil {
				x.Parameters[k] = fmt.Sprintf("%v %s", *q.Value, q.Unit)
			} else {
				x.Parameters[k] = "unresolved"
			}
		}
		for _, c := range a.Components {
			cc := comp{ID: c.ID, Name: c.Name, Role: c.Role, Type: c.Type}
			if c.Material != nil {
				cc.Material = c.Material.ID
			}
			if c.Product != nil {
				cc.Product = c.Product.ID
			}
			x.Components = append(x.Components, cc)
		}
		if rep := st.Validation[id]; rep != nil {
			x.Critical, x.Advisory = rep.Counts.Critical, rep.Counts.Advisory
			for _, is := range rep.Issues {
				if is.Severity == "critical-unresolved" || is.Severity == "blocking" {
					x.CriticalIssues = append(x.CriticalIssues, is.Message)
				}
			}
		}
		asms = append(asms, x)
	}
	var decs []map[string]string
	for _, id := range p.Decisions {
		if d := st.Decisions[id]; d != nil {
			decs = append(decs, map[string]string{"id": d.ID, "title": d.Title, "status": d.Status, "assembly": d.Assembly.ID})
		}
	}
	shape := map[string]any{}
	for _, name := range constructionChatOps {
		if op, err := construction.EmptyOperation(name); err == nil {
			shape[name] = constructionOpFields(op)
		}
	}
	var mats []map[string]string
	if st.Catalog != nil {
		for _, m := range st.Catalog.Materials {
			mats = append(mats, map[string]string{"id": m.ID, "name": m.Name, "family": m.Family})
		}
	}
	state := map[string]any{"catalogMaterials": mats, "problemId": p.ID, "title": p.Title, "narrative": p.Narrative, "lifecycle": p.Lifecycle,
		"existingConditions": p.Existing, "proposedConditions": p.Proposed, "location": p.Location, "jurisdiction": p.Jurisdiction, "climate": p.Climate,
		"decisionPoints": p.Questions, "approaches": asms, "decisions": decs, "activeApproach": p.ActiveAssembly}
	sb, _ := json.MarshalIndent(state, "", " ")
	ob, _ := json.Marshal(shape)
	keys := make([]string, 0, len(shape))
	for k := range shape {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("\n\n---\nCONSTRUCTION PROBLEM — you are the steward of this problem in the owner's Construction lab. The owner is not an architect: answer in plain words, short, and say what each choice changes.\n")
	b.WriteString("The way of working: (1) on a new problem, research it properly — use web search for building codes (IRC/IBC as adopted locally), manufacturers' installation instructions, and trade guidance (NRCA, BIA technical notes, SMACNA) — and cite the sources you used; ")
	b.WriteString("(2) propose two to four distinct approaches, each as its own model (CreateVariant from an existing approach, then SetAssemblyText and SetJunctionStrategy/SetWallCondition/dimensions); ")
	b.WriteString("(3) raise the decision points that narrow the choice (AddQuestion with plain options and why it matters; stage \"approach\"); ")
	b.WriteString("(4) once the owner has chosen, move to specifics — materials, products, exact dimensions — raising stage \"specifics\" questions and proposing SetMaterial/SetProduct/SetDimension. ")
	b.WriteString("Record researched findings as AddFact (list \"existing\" for site conditions, \"proposed\" for design intent) with an honest provenance and the source in the text.\n")
	b.WriteString("You cannot change the problem yourself. To propose changes, end your reply with ONE fenced block:\n```construction\n{\"summary\":\"one line\",\"changes\":[{\"assemblyId\":\"asm-… (omit for problem-level operations)\",\"operations\":[{\"op\":\"…\"}]}]}\n```\n")
	b.WriteString("The owner reviews and applies it; each applied change is a revision they can undo. New ids are \"<kind>-<32 lowercase hex>\" you choose (dq- questions, clm- facts, asm- approaches, cmp- components, dec- decisions). Never approve a decision. Nothing here is approved for construction.\n")
	b.WriteString("Provenance values: verified-fact, directly-applicable-guidance, adapted-precedent, engineering-inference, user-assumption, unknown. Assembly operations go in a change with that approach's assemblyId; CreateVariant copies the change's assemblyId into a new approach. SetMaterial takes a materialId from catalogMaterials.\n")
	b.WriteString("Model rules (a change that breaks one is refused): every id is \"<kind>-\" plus EXACTLY 32 lowercase hex characters. SetJunctionStrategy and SetWallCondition create parts the strategy needs and take their new ids in newComponentIds — slots apron, sidewall-flashing, through-wall, weeps, end-dams, cavity (give a new cmp- id for each slot the change might need). apron-through-wall-flashing needs a cavity wall: SetWallCondition value \"cavity\" (with newComponentIds.cavity) first, in the same change; a solid wall cannot take it. Reglet strategies need a counterflashing part. If the model can't represent an approach, describe it in SetAssemblyText and raise what's missing as a question instead of forcing it.\n")
	b.WriteString("Operations (" + strings.Join(keys, ", ") + ") and their fields: " + string(ob) + "\n")
	b.WriteString("Current state:\n" + string(sb) + "\n---")
	return b.String()
}
