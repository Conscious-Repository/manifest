package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/agentchat"
	"manifest/hermes"
	"manifest/ledger"
	"manifest/threads"
)

// AGENTS CHAT — the Hermes-family backend of the CHAT tab (agent-chat plan
// Phase 1, §3.2 adapter row "alfred + profiles", §3.3 Option B).
//
// Alfred (the default Hermes profile) and every named `hermes profile` are
// addressable agents whose conversations live in agentchat.Store files inside
// the primary harness tree. A send appends the user turn, flips the file to
// `status: thinking`, and runs ONE `hermes -z` turn on a goroutine (the request
// goroutine model the todo turns use — no new scheduler). Because a `-z` turn
// is always a fresh Hermes session (package hermes), MANIFEST composes the
// conversation window into the prompt; the reply lands as the agent's turn,
// the Hermes-side session_id and spend go to the ledger, and the file flips
// back to idle. Second sends during a turn queue and drain on the same
// goroutine.
//
// Routes mirror /api/chat/sessions (chat.go) verb for verb under
// /api/agents/chat/<agent>/…, so the dashboard swaps a base URL, not a client.

// agentChatCfg is the store + the short-lived profile cache.
type agentChatCfg struct {
	store *agentchat.Store

	pmu      sync.Mutex
	profiles []hermesProfile
	profAt   time.Time
	profErr  string
	// descriptions (`hermes profile describe`) by profile name, "default"
	// included — the roster tooltip and the suggest-agent hint (§2.5, §3.5).
	// Refreshed less often than the list: one exec per profile.
	descs    map[string]string
	descAt   time.Time
	descBusy bool // a background refresh is running

	recovered bool // the startup repair ran (agentChatRecover)
}

// hermesDescribeEvery bounds how often the descriptions are re-asked.
const hermesDescribeEvery = 5 * time.Minute

// hermesProfileDescriptions returns profile name → description text. The CLI
// is one process start per profile, so a stale map is served at once and
// refreshed in the background (at most every hermesDescribeEvery) — the
// first roster after boot carries no descriptions, the next one does. Nil
// when the runner is off.
func (s *Server) hermesProfileDescriptions(ctx context.Context) map[string]string {
	if s.agentChat == nil || !s.hermesEnabled() {
		return nil
	}
	profiles, _ := s.hermesProfilesCached(ctx)
	c := s.agentChat
	c.pmu.Lock()
	defer c.pmu.Unlock()
	if c.descBusy || (c.descs != nil && time.Since(c.descAt) < hermesDescribeEvery) {
		return c.descs
	}
	names := []string{"default"}
	for _, p := range profiles {
		if n := strings.ToLower(strings.TrimSpace(p.Name)); n != "" && n != "default" && agentchat.ValidAgent(n) {
			names = append(names, n)
		}
	}
	c.descBusy = true
	bin, env := s.hermesBin(), s.hermesEnv()
	go func() {
		out := map[string]string{}
		for _, n := range names {
			if d, err := hermesProfileCmd(context.Background(), bin, env, 5*time.Second, "describe", n); err == nil {
				if text := parseHermesDescribe(d); text != "" {
					out[n] = text
				}
			}
		}
		c.pmu.Lock()
		c.descs, c.descAt, c.descBusy = out, time.Now(), false
		c.pmu.Unlock()
	}()
	return c.descs
}

// alfredDescription is the default profile's description, else the standing
// one-liner (Alfred is the house do-bot whether or not a description is set).
const alfredDefaultDescription = "the default Hermes profile — the house do-bot; answers, digs, drafts plans; changes go through FEED approvals"

// agentDescription is the tooltip text for a roster token ("" = none): the
// Hermes family from `hermes profile describe`, the portal agents from their
// standing line, people none.
func (s *Server) agentDescription(token string) string {
	name := strings.TrimPrefix(token, "agent:")
	if name == token || name == "" {
		return ""
	}
	if ag, isPortal := s.portalChatAgent(name); isPortal {
		if ag == nil {
			return ""
		}
		return portalAgentDescription(ag)
	}
	descs := s.hermesProfileDescriptions(context.Background())
	if name == alfredAgent || name == "hermes" {
		if d := descs["default"]; d != "" {
			return d
		}
		return alfredDefaultDescription
	}
	return descs[name]
}

// UseAgentChat wires the Hermes-family chat store (the primary harness's
// artifacts/chats root). Sessions left thinking by a dead process are repaired
// before any request can start a turn — but ONLY on a box whose runner can
// own a turn (agentChatRecover): the store syncs across devices (plan Q7), so
// a dev twin with the runner off must never rewrite a session metis has in
// flight (one writer per transcript).
func (s *Server) UseAgentChat(st *agentchat.Store) {
	if st == nil {
		return
	}
	s.agentChat = &agentChatCfg{store: st}
	s.agentChatRecover()
}

// agentChatRecover runs the store's startup repair once both the store and
// the runner are wired (either may be wired first).
func (s *Server) agentChatRecover() {
	if s.agentChat == nil || !s.hermesEnabled() || s.agentChat.recovered {
		return
	}
	s.agentChat.recovered = true
	if fixed := s.agentChat.store.Recover(); len(fixed) > 0 {
		log.Printf("agent chat: repaired %d interrupted session(s): %s", len(fixed), strings.Join(fixed, ", "))
	}
}

// The rail's default identity is alfredAgent (hermes_dig.go) — an alias of the
// default Hermes profile (`-p` unset), display "Alfred" (plan §3.5).

// agentChatRosterEntry is one addressable agent for the rail.
type agentChatRosterEntry struct {
	Name        string `json:"name"`    // rail/route slug: alfred | <profile> | kairos | zeck
	Label       string `json:"label"`   // display
	Backend     string `json:"backend"` // "hermes" | "portal" (agentchat_portal.go)
	Profile     string `json:"profile"` // -p value ("" = default)
	Model       string `json:"model"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"` // the runner can take a turn
	Sessions    int    `json:"sessions"`
	// portal agents only: the artifact/access domain, the one-run gate, and
	// the persona intents the composer's @-typeahead offers (@kairos::brief)
	Domain   string   `json:"domain,omitempty"`
	Busy     bool     `json:"busy,omitempty"`
	Personas []string `json:"personas,omitempty"`
}

// hermesProfilesCached re-asks `hermes profile list` at most every 30s — the
// roster and every create/send validate against it.
func (s *Server) hermesProfilesCached(ctx context.Context) ([]hermesProfile, string) {
	c := s.agentChat
	c.pmu.Lock()
	defer c.pmu.Unlock()
	if time.Since(c.profAt) < 30*time.Second {
		return c.profiles, c.profErr
	}
	ps, err := hermesProfiles(ctx, s.hermesBin(), s.hermesEnv())
	c.profAt = time.Now()
	c.profErr = ""
	if err != nil {
		c.profErr = err.Error()
		ps = nil
	}
	c.profiles = ps
	return ps, c.profErr
}

// agentChatRoster lists Alfred first, then every non-default profile.
func (s *Server) agentChatRoster(ctx context.Context) []agentChatRosterEntry {
	enabled := s.hermesEnabled()
	var profiles []hermesProfile
	if enabled {
		profiles, _ = s.hermesProfilesCached(ctx)
	}
	out := []agentChatRosterEntry{{Name: alfredAgent, Label: "Alfred", Backend: "hermes", Enabled: enabled,
		Sessions: len(s.agentChat.store.List(alfredAgent)), Description: s.agentDescription("agent:" + alfredAgent)}}
	descs := s.hermesProfileDescriptions(ctx)
	for _, p := range profiles {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if name == "default" || name == alfredAgent {
			if p.Model != "" && out[0].Model == "" {
				out[0].Model = p.Model
			}
			continue
		}
		if !agentchat.ValidAgent(name) {
			continue
		}
		out = append(out, agentChatRosterEntry{Name: name, Label: name, Backend: "hermes", Profile: name,
			Model: p.Model, Enabled: enabled, Sessions: len(s.agentChat.store.List(name)), Description: descs[name]})
	}
	sort.SliceStable(out[1:], func(i, j int) bool { return out[1+i].Name < out[1+j].Name })
	return out
}

// resolveAgentChat maps a route slug to its Hermes profile: alfred → "" (the
// default profile), anything else must be a listed profile (fail closed —
// unknown names are never silently routed to the default).
func (s *Server) resolveAgentChat(ctx context.Context, agent string) (profile string, err error) {
	if !agentchat.ValidAgent(agent) {
		return "", errBadRequest("bad agent name")
	}
	if agent == alfredAgent {
		return "", nil
	}
	if !s.hermesEnabled() {
		return "", errBadRequest("the Hermes runner is not enabled here")
	}
	profiles, perr := s.hermesProfilesCached(ctx)
	for _, p := range profiles {
		if strings.EqualFold(strings.TrimSpace(p.Name), agent) {
			return agent, nil
		}
	}
	if perr != "" {
		return "", fmt.Errorf("couldn't list Hermes profiles: %s", perr)
	}
	return "", errBadRequest("no Hermes profile named " + agent)
}

// ---- handlers ----

func (s *Server) agentChatReady(w http.ResponseWriter) bool {
	if s.agentChat == nil {
		http.Error(w, "agent chat disabled (no primary harness)", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// GET /api/agents/chat/roster — the Hermes family (when its store is wired),
// then the portal agents (Phase 2). Never a 503: the rail still needs the
// portal sections on a box with no primary harness.
func (s *Server) handleAgentChatRoster(w http.ResponseWriter, r *http.Request) {
	agents := []agentChatRosterEntry{}
	if s.agentChat != nil {
		agents = append(agents, s.agentChatRoster(r.Context())...)
	}
	agents = append(agents, s.portalChatRoster()...)
	writeJSON(w, map[string]any{"agents": agents})
}

// GET /api/agents/chat/{agent}/sessions
func (s *Server) handleAgentChatSessions(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent := r.PathValue("agent")
	if !agentchat.ValidAgent(agent) {
		http.Error(w, "bad agent name", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"agent": agent, "sessions": s.agentChat.store.List(agent)})
}

// POST /api/agents/chat/{agent}/sessions {title?, model?, text?} — create, and
// (like the spirit route) send the first message in the same call.
func (s *Server) handleAgentChatSessionCreate(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent := r.PathValue("agent")
	var b struct {
		Title string            `json:"title"`
		Model string            `json:"model"`
		Text  string            `json:"text"`
		Files []threads.FileRef `json:"files"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	profile, err := s.resolveAgentChat(r.Context(), agent)
	if err != nil {
		httpError(w, err)
		return
	}
	// a first send the runner cannot take is refused BEFORE the file exists,
	// so a refused create never leaves an empty "new conversation" in the rail
	if (strings.TrimSpace(b.Text) != "" || len(b.Files) > 0) && !s.hermesEnabled() {
		httpError(w, errBadRequest("the Hermes runner is not enabled here"))
		return
	}
	title := strings.TrimSpace(b.Title)
	if title == "" && strings.TrimSpace(b.Text) != "" {
		title = firstLine(b.Text, 60)
	}
	id, err := s.agentChat.store.Create(agent, profile, title, b.Model)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	status := agentchat.StatusIdle
	if strings.TrimSpace(b.Text) != "" {
		if err := s.agentChatSend(agent, id, b.Text, b.Files); err != nil {
			httpError(w, err)
			return
		}
		status = agentchat.StatusThinking
	}
	writeJSON(w, map[string]any{"id": id, "status": status})
}

// GET /api/agents/chat/{agent}/sessions/{id}
func (s *Server) handleAgentChatSession(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	sess, body, queued, ok := s.agentChat.store.Get(r.PathValue("agent"), r.PathValue("id"))
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if queued == nil {
		queued = []string{}
	}
	writeJSON(w, map[string]any{"session": sess, "body": body, "queued": queued})
}

// POST /api/agents/chat/{agent}/sessions/{id}/messages {text, files?} — starts
// the turn goroutine (or queues behind the one in flight) and returns at once.
func (s *Server) handleAgentChatMessage(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	var b struct {
		Text  string            `json:"text"`
		Files []threads.FileRef `json:"files"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, _, _, ok := s.agentChat.store.Get(agent, id); !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	if err := s.agentChatSend(agent, id, b.Text, b.Files); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "status": agentchat.StatusThinking,
		"queued": len(s.agentChat.store.Queued(agent, id))})
}

// POST /api/agents/chat/{agent}/sessions/{id}/rename {title}
func (s *Server) handleAgentChatRename(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	var b struct {
		Title string `json:"title"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.agentChat.store.Rename(r.PathValue("agent"), r.PathValue("id"), b.Title); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// DELETE /api/agents/chat/{agent}/sessions/{id}
func (s *Server) handleAgentChatDelete(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	if err := s.agentChat.store.Delete(r.PathValue("agent"), r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- the turn (Option B) ----

// agentChatMaxChars caps a user message (the spirit spool's cap idiom).
const agentChatMaxChars = 24000

// fileTokenRe is how an attachment rides a user turn: `[file:: <sha256> <name>]`
// on its own line. The renderer turns it into a chip; the turn goroutine
// resolves it against the private thread blob store (the todo_thread file
// store the composer uploads to).
var fileTokenRe = regexp.MustCompile(`(?m)^\[file:: ([0-9a-f]{64}) (.+?)\]$`)

// agentChatSend records the user turn and starts (or queues behind) the turn.
func (s *Server) agentChatSend(agent, id, text string, files []threads.FileRef) error {
	text = strings.TrimSpace(text)
	if text == "" && len(files) == 0 {
		return errBadRequest("empty message")
	}
	if len(text) > agentChatMaxChars {
		text = text[:agentChatMaxChars]
	}
	for _, f := range files {
		if f.Hash == "" || f.Name == "" {
			continue
		}
		text += "\n[file:: " + f.Hash + " " + strings.ReplaceAll(f.Name, "]", ")") + "]"
	}
	text = strings.TrimSpace(text)
	if !s.hermesEnabled() {
		return errBadRequest("the Hermes runner is not enabled here")
	}
	s.ledger(ledger.Entry{Source: "chat", Kind: "chat.user", Actor: "owner",
		Session: id, Harness: "hermes", Text: ledger.Snip(text, 280), Meta: map[string]any{"agent": agent}})
	if !s.agentChat.store.Submit(agent, id, text) {
		return nil // queued behind the turn in flight; the goroutine drains it
	}
	if _, err := s.agentChat.store.AppendTurn(agent, id, "user", text, 0); err != nil {
		s.agentChat.store.Release(agent, id)
		return err
	}
	_ = s.agentChat.store.SetStatus(agent, id, agentchat.StatusThinking)
	go s.runAgentChatTurns(agent, id)
	return nil
}

// runAgentChatTurns runs the in-flight turn and every send queued behind it,
// one Hermes invocation each, then flips the file back to idle. Always
// releases the claim.
//
// Ordering matters: the file flips to idle WHILE this goroutine still holds
// the claim, and only then does Next release it. Releasing first would let a
// send land in between (claim → append → `thinking`) and the stale idle
// write would then clobber it — the file would say idle for the whole of a
// live turn and the poll would stop. If a send does arrive between the idle
// write and Next, Next hands it back and the file flips to thinking again.
func (s *Server) runAgentChatTurns(agent, id string) {
	st := s.agentChat.store
	defer st.Release(agent, id)
	for {
		s.runAgentChatTurn(agent, id)
		if len(st.Queued(agent, id)) == 0 {
			_ = st.SetStatus(agent, id, agentchat.StatusIdle)
		}
		next, more := st.Next(agent, id)
		if !more {
			return
		}
		_ = st.SetStatus(agent, id, agentchat.StatusThinking)
		if _, err := st.AppendTurn(agent, id, "user", next, 0); err != nil {
			log.Printf("agent chat %s/%s: append queued turn: %v", agent, id, err)
		}
	}
}

// runAgentChatTurn composes the window, invokes the CLI once, and lands the
// reply (or the failure) as a turn.
func (s *Server) runAgentChatTurn(agent, id string) {
	st := s.agentChat.store
	sess, body, _, ok := st.Get(agent, id)
	if !ok {
		return
	}
	who := "agent:" + agent
	prompt := s.composeAgentChatPrompt(agent, sess, body)
	res, err := s.hermes.runner.Run(context.Background(), hermes.Request{
		Prompt:   prompt,
		Model:    sess.Model,
		Toolsets: s.hermes.readTools, // chat turns are read-only (vault gate, §3.6)
		Profile:  sess.Profile,
	})
	if err != nil {
		log.Printf("agent chat %s/%s: %v", agent, id, err)
		_, _ = st.AppendTurn(agent, id, "system", "⚠ "+agentDisplayName("agent:"+agent)+" couldn't finish that — "+err.Error(), 0)
		s.ledger(ledger.Entry{Source: "run", Kind: "run.failed", Actor: who, Session: id, Harness: "hermes",
			Text: "chat turn failed — " + err.Error(), Meta: map[string]any{"agent": agent, "profile": sess.Profile}})
		return
	}
	reply := strings.TrimSpace(res.Reply)
	if reply == "" {
		reply = "(no reply)"
	}
	_, _ = st.AppendTurn(agent, id, agent, "### Step 1 — say\n\n"+reply, res.SpentUSD)
	if res.SessionID != "" {
		_ = st.SetHermesSession(agent, id, res.SessionID)
	}
	s.ledger(ledger.Entry{Source: "chat", Kind: "chat.assistant", Actor: who, Session: id, Harness: "hermes",
		Text: ledger.Snip(reply, 280),
		Meta: map[string]any{"agent": agent, "profile": sess.Profile, "sessionId": res.SessionID,
			"spentUsd": res.SpentUSD, "model": firstNonEmpty(res.Model, sess.Model)}})
}

// agentChatWindowChars bounds the transcript the prompt carries (oldest turns
// drop first; the latest user message always rides whole).
const agentChatWindowChars = 32000

// composeAgentChatPrompt is the Option-B window: identity line, the
// conversation so far (oldest first, most recent turns within budget), the
// owner's attachments on the latest message, and the reply instruction. Pure
// apart from attachment reads.
func (s *Server) composeAgentChatPrompt(agent string, sess agentchat.Session, body string) string {
	name := "Alfred"
	if agent != alfredAgent {
		name = agent
	}
	turns := agentchat.ParseTurns(body)
	// keep the newest turns that fit; the last turn is the user's message
	kept := turns
	total := 0
	start := len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		total += len(turns[i].Text) + 24
		if total > agentChatWindowChars && i < len(turns)-1 {
			break
		}
		start = i
	}
	kept = turns[start:]

	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, the owner's personal agent, in a chat thread titled %q inside his Manifest cockpit. ", name, sess.Title)
	b.WriteString("This is a continuing conversation; the transcript so far is below (oldest first). ")
	b.WriteString("Reply ONLY to the last user message, as yourself, in plain markdown. Do not repeat or quote the transcript, do not prefix your reply with a role label.\n")
	if start > 0 {
		fmt.Fprintf(&b, "(%d earlier turn(s) omitted for length.)\n", start)
	}
	b.WriteString("\nCONVERSATION:\n")
	for _, t := range kept {
		label := t.Who
		switch t.Who {
		case "user":
			label = "owner"
		case agent:
			label = name
		}
		text := t.Text
		if t.Who != "user" && t.Who != "system" {
			text = agentchat.SayBody(text)
		}
		text = fileTokenRe.ReplaceAllString(text, "(attached: $2)")
		fmt.Fprintf(&b, "\n[%s]\n%s\n", label, strings.TrimSpace(text))
	}
	if len(kept) > 0 {
		if att := s.agentChatAttachments(kept[len(kept)-1]); att != "" {
			b.WriteString("\n" + att)
		}
	}
	b.WriteString("\nYour reply:")
	return b.String()
}

// agentChatAttachments renders the [file::] tokens on a user turn into the
// prompt block the do-bot can consume — the hermes_delegate.go idiom: text
// inlined, images handed their on-disk path for the vision toolset.
func (s *Server) agentChatAttachments(t agentchat.Turn) string {
	if t.Who != "user" || s.threads == nil || s.threads.private == nil {
		return ""
	}
	st := s.threads.private
	var b strings.Builder
	for _, m := range fileTokenRe.FindAllStringSubmatch(t.Text, -1) {
		hash, name := m[1], m[2]
		path := st.BlobPath(hash)
		if path == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("ATTACHMENTS the owner shared on the last message — read them before you answer:\n")
		}
		f := threads.FileRef{Hash: hash, Name: name}
		if body, ok := readTextAttachment(path, f); ok {
			fmt.Fprintf(&b, "\n--- %s (attached file) ---\n%s\n--- end %s ---\n", name, body, name)
		} else if isImageExt(name) {
			fmt.Fprintf(&b, "\n- %s (image — view it with your vision tool) is at: %s\n", name, path)
		} else {
			fmt.Fprintf(&b, "\n- %s is at: %s\n", name, path)
		}
	}
	return b.String()
}
