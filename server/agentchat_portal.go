package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/chatthreads"
	"manifest/ledger"
	"manifest/spirits"
	"manifest/threads"
)

// AGENTS CHAT — the portal-spool backend (agent-chat plan Phase 2, §3.2 row
// "kairos / zeck", §7 Q5).
//
// Kairos (AION) and Zeck (OODA) already have a complete chat machine behind
// the two team portals: chatthreads stores (one writer = manifest), an ask
// that spools a run order into the agent's harness tree (chatAskFor →
// SpoolRunNow), and chatSweep as the reply return path (60 s on the
// AgentLoopTicker + on every read). This file adds NOTHING to that machine.
// It projects the same stores under the cockpit's /api/agents/chat/<agent>/…
// verbs, in the same JSON shapes the Hermes-family routes answer with, so the
// ONE rail + transcript renderer + composer in 48-chat.js drives them by
// swapping a base URL. The transcript body is rendered here into the spirit
// session grammar (`## Turn N — who · ts`, `### Step N — cast`) the renderer
// already parses; nothing on disk changes shape.
//
// Domain rules are the portals': attachments live in the agent's own artifact
// domain (aion | ooda) and are served only from it; the one-run-at-a-time
// gate is SpoolRunNow's (a second order while one is active → 409); the
// owner's messages are attributed to the portal admin identity so the team
// sees them as his in the portal, verbatim.

// portalChatAgent maps a route slug to its portal chat agent. isPortal is
// true for the two reserved names even when the store is unwired (nil agent
// → 503, never a fall-through into the Hermes-profile path).
func (s *Server) portalChatAgent(agent string) (ag *chatAgent, isPortal bool) {
	switch agent {
	case "kairos":
		return s.kairosAgent(), true
	case "zeck":
		return s.zeckAgent(), true
	}
	return nil, false
}

// portalChatRoute dispatches one /api/agents/chat/{agent}/… verb: the two
// portal agents go to portal, every other slug to the Hermes-family handler.
func (s *Server) portalChatRoute(portal func(*chatAgent, http.ResponseWriter, *http.Request), hermes http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ag, isPortal := s.portalChatAgent(r.PathValue("agent"))
		if !isPortal {
			hermes(w, r)
			return
		}
		if ag == nil {
			http.Error(w, r.PathValue("agent")+" chat is not wired here (no portal chat store)", http.StatusServiceUnavailable)
			return
		}
		portal(ag, w, r)
	}
}

// portalChatNoHermes is the Hermes-side fallback for verbs that only exist
// on the portal backend (attachments, engine).
func portalChatNoHermes(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not a portal chat agent", http.StatusNotFound)
}

// portalChatIdentity is who the cockpit posts as: the portal admin (the
// owner's email, so the team portal attributes the message to him by the
// same rule it uses for every member), falling back to the cockpit's owner
// token when no portal is configured.
func (s *Server) portalChatIdentity() (email, name string) {
	who := s.ownerIdentity()
	email, name = who.ID, who.Name
	if s.threads != nil && s.threads.admin.Email != "" {
		email = s.threads.admin.Email
		if s.threads.admin.Name != "" {
			name = s.threads.admin.Name
		}
	}
	return email, name
}

// portalChatRoster is the rail's view of the wired portal agents — the same
// entry type the Hermes roster uses, backend "portal". Enabled means the
// harness that runs the agent is configured here; Busy is the one-run gate.
func (s *Server) portalChatRoster() []agentChatRosterEntry {
	var intents []string
	for k, p := range s.personas() {
		if p.Enabled {
			intents = append(intents, k)
		}
	}
	sort.Strings(intents)
	var out []agentChatRosterEntry
	for _, ag := range s.chatAgents() {
		h := s.findHarness(ag.Name)
		enabled := h != nil && h.Spirits != nil
		n := 0
		for _, t := range ag.Store.Threads() {
			if !t.Archived {
				n++
			}
		}
		out = append(out, agentChatRosterEntry{
			Name: ag.Name, Label: ag.Display, Backend: "portal", Domain: ag.Domain,
			Description: portalAgentDescription(ag),
			Enabled:     enabled, Sessions: n, Busy: enabled && s.chatBusy(ag), Personas: intents,
		})
	}
	return out
}

// portalAgentDescription is a portal agent's standing roster line (its
// tooltip on every surface — the portals have no `describe`).
func portalAgentDescription(ag *chatAgent) string {
	return ag.Display + " — " + portalChatDomainLabel(ag.Domain) + " · spools an order to " + ag.Host + " · one run at a time"
}

func portalChatDomainLabel(domain string) string {
	if domain == "ooda" {
		return "OODA real estate"
	}
	return "AION team"
}

// portalChatSession is the rail/header projection of one thread — the
// Hermes-family Session keys, so the renderer reads it unchanged.
type portalChatSession struct {
	ID       string  `json:"id"`
	Agent    string  `json:"agent"`
	Title    string  `json:"title"`
	Created  string  `json:"created"`
	Updated  string  `json:"updated"`
	Status   string  `json:"status"` // idle | thinking (an order is pending its reply)
	Turns    int     `json:"turns"`
	SpentUSD float64 `json:"spentUsd"`
	Model    string  `json:"model"`
	Domain   string  `json:"domain"`
	Rock     string  `json:"rock,omitempty"`
	Archived bool    `json:"archived,omitempty"`
	Busy     bool    `json:"busy"` // the agent has a run in flight (any thread)
}

func (s *Server) portalChatSessionOf(ag *chatAgent, t chatthreads.Thread, msgs []chatthreads.Message, pending map[string]bool, busy bool) portalChatSession {
	updated := t.Created
	if n := len(msgs); n > 0 && msgs[n-1].At.After(updated) {
		updated = msgs[n-1].At
	}
	status := "idle"
	if pending[t.ID] {
		status = "thinking"
	}
	return portalChatSession{
		ID: t.ID, Agent: ag.Name, Title: t.Title, Created: t.Created.UTC().Format(time.RFC3339),
		Updated: updated.UTC().Format(time.RFC3339), Status: status, Turns: len(msgs),
		Domain: ag.Domain, Rock: t.Rock, Archived: t.Archived, Busy: busy,
	}
}

// portalChatPending indexes the store's in-flight orders by thread.
func portalChatPending(ag *chatAgent) map[string]bool {
	out := map[string]bool{}
	for _, p := range ag.Store.Pending() {
		out[p.Thread] = true
	}
	return out
}

// portalChatThread finds one thread (archived included — a thread stays
// readable after the cockpit's delete, which is the portal's archive).
func portalChatThread(ag *chatAgent, id string) (chatthreads.Thread, bool) {
	for _, t := range ag.Store.Threads() {
		if t.ID == id {
			return t, true
		}
	}
	return chatthreads.Thread{}, false
}

// portalChatBody renders a thread into the shared transcript grammar. A
// person's message is a `user` turn (a teammate's carries their name, the
// owner's does not); attachments ride as [file::] lines so the renderer's
// chips work; the agent's reply is a spirit turn: a trace step naming the
// ritual, elapsed and report, then the `say` body, then any proposals as a
// list (the portals own apply/discard).
func portalChatBody(ag *chatAgent, msgs []chatthreads.Message, self string) string {
	var b strings.Builder
	for i, m := range msgs {
		ts := m.At.UTC().Format(time.RFC3339)
		switch m.Kind {
		case ag.Name:
			fmt.Fprintf(&b, "## Turn %d — %s · %s\n\n", i+1, ag.Name, ts)
			step := 1
			if m.Ritual != "" || m.Elapsed != "" || m.Report != "" {
				detail := strings.TrimSpace(strings.Join(nonEmpty(m.Outcome, m.Elapsed, m.Report), " · "))
				fmt.Fprintf(&b, "### Step %d — %s\n\n- result: %s\n\n", step, orStr(m.Ritual, "run"), detail)
				step++
			}
			text := strings.TrimSpace(m.Text)
			if m.Outcome == "failed" {
				text = "⚠ " + ag.Display + "'s run failed — " + orStr(text, "no report body")
			}
			fmt.Fprintf(&b, "### Step %d — say\n\n%s\n", step, orStr(text, "(no reply)"))
			if len(m.Props) > 0 {
				fmt.Fprintf(&b, "\n**Proposals** (decide in the %s portal):\n", portalChatDomainLabel(ag.Domain))
				for _, p := range m.Props {
					what := p.Type
					if p.Type == "set-field" {
						what = "set `" + p.Field + "` → `" + p.Value + "`"
					} else if p.Type == "replace-section" {
						what = "replace `## " + p.Section + "`"
					}
					fmt.Fprintf(&b, "- %s on `%s` — %s\n", what, p.ItemID, orStr(p.State, "pending"))
				}
			}
		case "system":
			fmt.Fprintf(&b, "## Turn %d — system · %s\n\n%s\n", i+1, ts, strings.TrimSpace(m.Text))
		default:
			text := strings.TrimSpace(m.Text)
			if m.Author != self && m.Author != "owner" && m.AuthName != "" {
				text = m.AuthName + " — " + text
			}
			for _, f := range m.Files {
				if f.Hash == "" || f.Name == "" {
					continue
				}
				text += "\n[file:: " + f.Hash + " " + strings.ReplaceAll(f.Name, "]", ")") + "]"
			}
			fmt.Fprintf(&b, "## Turn %d — user · %s\n\n%s\n", i+1, ts, text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func nonEmpty(in ...string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// portalChatSend records the owner's message and spools the order through
// the portal's own ask path (persona intent from `@kairos::brief`, context
// ids resolved server-side, attachments composed from the artifact pool).
// spirits.ErrAlreadyActive passes through for the 409.
func (s *Server) portalChatSend(ag *chatAgent, threadID, text, ritual string, context []string, files []threads.FileRef) error {
	text = strings.TrimSpace(text)
	if len(text) > agentChatMaxChars {
		text = text[:agentChatMaxChars]
	}
	for _, f := range files {
		if f.Hash != "" {
			context = append(context, attachPrefix+f.Hash)
		}
	}
	if text == "" && len(context) > 0 {
		text = "(see the attached)"
	}
	if text == "" {
		return errBadRequest("empty message")
	}
	email, name := s.portalChatIdentity()
	if err := s.chatAskFor(ag, threadID, text, ritual, context, email, name); err != nil {
		return err
	}
	s.ledger(ledger.Entry{Source: "chat", Kind: "chat.user", Actor: "owner", Session: threadID,
		Harness: ag.Name, Text: ledger.Snip(text, 280), Meta: map[string]any{"agent": ag.Name, "ritual": orStr(ritual, "ask"), "via": "cockpit"}})
	return nil
}

// portalChatErr maps a send failure: the one-run gate → 409, the rest → 400.
func portalChatErr(ag *chatAgent, w http.ResponseWriter, err error) {
	if errors.Is(err, spirits.ErrAlreadyActive) {
		http.Error(w, ag.Display+" is running — one order at a time; send again when it finishes", http.StatusConflict)
		return
	}
	httpError(w, err)
}

// ---- handlers (the /api/agents/chat/{agent}/… verbs, portal side) ----

// GET /api/agents/chat/{kairos|zeck}/sessions — non-archived threads, newest
// activity first. A read-driven sweep runs first so a just-finished run shows
// without waiting for the ticker (the portals' idiom, chatThreadsFor).
func (s *Server) handlePortalChatSessions(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	s.chatSweep()
	pending := portalChatPending(ag)
	busy := s.chatBusy(ag)
	out := []portalChatSession{}
	for _, t := range ag.Store.Threads() {
		if t.Archived {
			continue
		}
		out = append(out, s.portalChatSessionOf(ag, t, ag.Store.Messages(t.ID), pending, busy))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	writeJSON(w, map[string]any{"agent": ag.Name, "sessions": out})
}

// POST /api/agents/chat/{agent}/sessions {title?, text?, ritual?, context?, files?}
// — create, and send the first message in the same call. The busy gate is
// checked BEFORE the thread exists so a refused first send leaves nothing
// behind.
func (s *Server) handlePortalChatSessionCreate(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	var b struct {
		Title   string            `json:"title"`
		Text    string            `json:"text"`
		Ritual  string            `json:"ritual"`
		Context []string          `json:"context"`
		Files   []threads.FileRef `json:"files"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	sending := strings.TrimSpace(b.Text) != "" || len(b.Files) > 0
	if sending {
		// refuse BEFORE the thread exists: a harness that cannot take the
		// order (not configured here) or is busy must not leave an empty
		// shared thread behind in the portal
		if h := s.findHarness(ag.Name); h == nil || h.Spirits == nil {
			httpError(w, errBadRequest(ag.Name+" is not configured"))
			return
		}
		if s.chatBusy(ag) {
			portalChatErr(ag, w, spirits.ErrAlreadyActive)
			return
		}
	}
	title := strings.TrimSpace(b.Title)
	if title == "" && strings.TrimSpace(b.Text) != "" {
		title = firstLine(b.Text, 60)
	}
	if title == "" {
		title = "untitled"
	}
	email, name := s.portalChatIdentity()
	id := fmt.Sprintf("t%d", time.Now().UnixNano())
	if _, err := s.chatThreadFor(ag, "create", id, title, "", email, name); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	status := "idle"
	if sending {
		if err := s.portalChatSend(ag, id, b.Text, b.Ritual, b.Context, b.Files); err != nil {
			portalChatErr(ag, w, err)
			return
		}
		status = "thinking"
	}
	writeJSON(w, map[string]any{"id": id, "status": status})
}

// GET /api/agents/chat/{agent}/sessions/{id} — the thread as a session +
// transcript body in the shared grammar. Sweeps first (the reply path).
func (s *Server) handlePortalChatSession(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	s.chatSweep()
	t, ok := portalChatThread(ag, r.PathValue("id"))
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	msgs := ag.Store.Messages(t.ID)
	self, _ := s.portalChatIdentity()
	writeJSON(w, map[string]any{
		"session": s.portalChatSessionOf(ag, t, msgs, portalChatPending(ag), s.chatBusy(ag)),
		"body":    portalChatBody(ag, msgs, self),
		"queued":  []string{},
	})
}

// POST /api/agents/chat/{agent}/sessions/{id}/messages {text, ritual?, context?, files?}
// — spools the order (409 while one is active; nothing queues — the portal
// rule, §6 risk row).
func (s *Server) handlePortalChatMessage(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	var b struct {
		Text    string            `json:"text"`
		Ritual  string            `json:"ritual"`
		Context []string          `json:"context"`
		Files   []threads.FileRef `json:"files"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id := r.PathValue("id")
	if _, ok := portalChatThread(ag, id); !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if err := s.portalChatSend(ag, id, b.Text, b.Ritual, b.Context, b.Files); err != nil {
		portalChatErr(ag, w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "status": "thinking", "queued": 0})
}

// POST /api/agents/chat/{agent}/sessions/{id}/rename {title}
func (s *Server) handlePortalChatRename(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	var b struct {
		Title string `json:"title"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if strings.TrimSpace(b.Title) == "" {
		httpError(w, errBadRequest("empty title"))
		return
	}
	email, name := s.portalChatIdentity()
	if _, err := s.chatThreadFor(ag, "rename", r.PathValue("id"), b.Title, "", email, name); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// DELETE /api/agents/chat/{agent}/sessions/{id} — the portal stores have no
// delete (shared team objects); the cockpit's delete is the portal's ARCHIVE:
// the thread leaves the rail and stays readable in the portal's archive.
func (s *Server) handlePortalChatDelete(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	email, name := s.portalChatIdentity()
	if _, err := s.chatThreadFor(ag, "archive", r.PathValue("id"), "", "", email, name); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "archived": true})
}

// POST /api/agents/chat/{agent}/attach?name=&thread= — the portal's own
// upload path (chat_attach.go), filed in the agent's artifact domain.
func (s *Server) handlePortalChatAttach(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	email, name := s.portalChatIdentity()
	s.handleChatAttach(ag.Domain, w, r, email, name)
}

// GET /api/agents/chat/{agent}/attach/{hash} — served only when the agent's
// domain owns the hash (the portals' access rule, verbatim).
func (s *Server) handlePortalChatAttachGet(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	s.handleChatAttachGet(ag.Domain, w, r, r.PathValue("hash"))
}

// GET /api/agents/chat/{agent}/engine — liveness + active claim + pending.
func (s *Server) handlePortalChatEngine(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.chatEngine(ag))
}
