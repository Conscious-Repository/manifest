package server

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/ledger"
	"manifest/spirits"
	"manifest/teamportal"
	"manifest/threads"
)

// The todo-panel thread layer (todo-panel plan D3): comments route THREE
// ways by todo id —
//
//   - `aion:` ids, when the team portal is configured, comment through the
//     EXISTING teamportal store: instantly team-visible on portal.aion.bio,
//     no new sharing machinery (attachments land content-addressed in the
//     same shared dir).
//   - real-estate ids (`prop:<slug>/<line>` and personal `real-estate/…`
//     domain ids) go to the SHARED RE thread store (realEstate.teamDir,
//     stood up now so a future RE surface reads it as-is).
//   - everything else — personal — goes to the private store in dataDir.
//     (dataDir never syncs: the cockpit is metis, so metis holds THE private
//     store; a laptop dev instance won't see those threads. Accepted per §8.)

type threadsCfg struct {
	private *threads.Store    // <dataDir>/todo-threads
	re      *threads.Store    // realEstate.teamDir (nil = fall back to private)
	aion    *teamportal.Store // the portal's team store (nil = fall back)
	aionFS  *threads.Store    // blob store rooted at the portal shared dir
	admin   teamportal.Identity
	sweepMu sync.Mutex // one agent-loop sweep at a time (ticker vs feed reads)
}

// UseThreads wires the thread stores (any but private may be nil).
func (s *Server) UseThreads(private, re *threads.Store, aionTeam *teamportal.Store, aionBlobs *threads.Store, adminEmail string) {
	s.threads = &threadsCfg{
		private: private, re: re, aion: aionTeam, aionFS: aionBlobs,
		admin: teamportal.Identity{Email: adminEmail, Name: "Benjamin"},
	}
}

// threadKind classifies a todo id for routing: "aion" | "re" | "private".
func (s *Server) threadKind(taskID string) string {
	if s.threads == nil {
		return "private"
	}
	if strings.HasPrefix(taskID, "aion:") && s.threads.aion != nil {
		return "aion"
	}
	if (strings.HasPrefix(taskID, "prop:") || strings.HasPrefix(taskID, "re:") ||
		strings.HasPrefix(taskID, "real-estate/")) && s.threads.re != nil {
		return "re"
	}
	return "private"
}

// threadStore returns the threads.Store backing a kind's BLOBS and (for
// re/private) its comments.
func (s *Server) threadStore(kind string) *threads.Store {
	switch kind {
	case "aion":
		return s.threads.aionFS
	case "re":
		return s.threads.re
	default:
		return s.threads.private
	}
}

// ownerIdentity is the thread author for panel writes (single-user cockpit).
func (s *Server) ownerIdentity() threads.Identity {
	name := "Benjamin"
	if s.threads != nil && s.threads.admin.Name != "" {
		name = s.threads.admin.Name
	}
	// the token is teamportal.OwnerActor so the FEED bridge recognises these
	// writes as his when a thread store shares a portal's team dir
	return threads.Identity{ID: teamportal.OwnerActor, Name: name}
}

// isMarker: hidden idempotency/relay entries never render.
func isMarker(c threads.Comment) bool {
	if c.Action == threads.ActRelay {
		return true
	}
	m, _ := c.Meta["marker"].(bool)
	return m
}

// visibleThread filters markers out of a store's entries.
func visibleThread(in []threads.Comment) []threads.Comment {
	out := make([]threads.Comment, 0, len(in))
	for _, c := range in {
		if !isMarker(c) {
			out = append(out, c)
		}
	}
	return out
}

// listThread returns a todo's thread in the uniform shape, regardless of
// which store holds it. For aion todos the view MERGES the team thread with
// the private structural trail (assign/plan/fire/result) — the panel shows
// the whole conversation (owner report 2026-08-15: the trail was invisible
// on team todos).
func (s *Server) listThread(taskID string) (result []threads.Comment) {
	defer func() {
		seen := map[string]bool{}
		for _, c := range result {
			seen[c.ID] = true
		}
		for _, c := range s.plannerComments(taskID) {
			if !seen[c.ID] {
				result = append(result, c)
			}
		}
		sort.SliceStable(result, func(i, j int) bool { return result[i].At.Before(result[j].At) })
	}()
	if s.threads == nil {
		return nil
	}
	if s.threadKind(taskID) == "aion" {
		item := strings.TrimPrefix(taskID, "aion:")
		var out []threads.Comment
		for _, c := range s.threads.aion.Ext().Comments[item] {
			files := make([]threads.FileRef, 0, len(c.Files))
			for _, f := range c.Files {
				files = append(files, threads.FileRef{Hash: f.Hash, Name: f.Name, Size: f.Size, Mime: f.Mime})
			}
			out = append(out, threads.Comment{
				ID: c.ID, TaskID: taskID, Action: threads.ActComment,
				Author: c.Author, AuthorName: c.AuthorName,
				Text: c.Text, Files: files, At: c.At, Meta: portalCommentContext(c),
			})
		}
		if s.threads.private != nil {
			out = append(out, visibleThread(s.threads.private.Thread(taskID))...)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
		return out
	}
	return visibleThread(s.threadStore(s.threadKind(taskID)).Thread(taskID))
}

// addThreadEntry routes one entry to the right store. Structural actions
// (assign/plan/fire/result) always land in the threads-shaped store — for
// aion todos the TEXT of plain comments goes team-visible via teamportal,
// structural entries stay in the private machine trail.
func (s *Server) addThreadEntry(author threads.Identity, taskID, action, text string, mentions []string, files []threads.FileRef, meta map[string]any) (threads.Comment, error) {
	now := time.Now()
	if s.plannerNotesPath(taskID) != "" && action == threads.ActComment && len(files) == 0 && len(mentions) == 0 && len(meta) == 0 {
		return s.addPlannerComment(taskID, text, author)
	}
	if s.threads == nil {
		return threads.Comment{}, errBadRequest("threads not configured")
	}
	if s.threadKind(taskID) == "aion" && action == threads.ActComment {
		pf := make([]teamportal.FileRef, 0, len(files))
		for _, f := range files {
			pf = append(pf, teamportal.FileRef{Hash: f.Hash, Name: f.Name, Size: f.Size, Mime: f.Mime})
		}
		// the AGENT speaks with its own identity on the team thread (owner
		// decision 2026-08-15) — everyone else posts as the portal owner
		actor := s.threads.admin
		if strings.HasPrefix(author.ID, "agent:") {
			actor = teamportal.Identity{Email: author.ID, Name: author.Name}
		}
		var context []teamportal.ArtifactReference
		if refs, ok := meta["context"].([]artifactContextRef); ok {
			for _, ref := range refs {
				context = append(context, teamportal.ArtifactReference{ID: ref.ID, Revision: ref.Revision})
			}
		}
		c, err := s.threads.aion.AddCommentWithContext(actor, strings.TrimPrefix(taskID, "aion:"), text, pf, mentions, context, now)
		if err != nil {
			return threads.Comment{}, err
		}
		s.ledger(ledger.Entry{TS: now, Source: "thread", Kind: "thread." + action,
			Actor: author.ID, Object: ledger.Object{Kind: ledger.ObjTask, ID: taskID}, Task: taskID, Text: ledger.Snip(text, 280)})
		return threads.Comment{ID: c.ID, TaskID: taskID, Action: threads.ActComment,
			Author: c.Author, AuthorName: c.AuthorName, Text: c.Text, Files: files, At: c.At, Meta: portalCommentContext(c)}, nil
	}
	kind := s.threadKind(taskID)
	if kind == "aion" {
		kind = "private" // structural trail for aion todos stays private
	}
	c, err := s.threadStore(kind).Add(author, taskID, action, text, mentions, files, meta, now)
	if err == nil && !isMarker(c) {
		s.ledger(ledger.Entry{TS: now, Source: "thread", Kind: "thread." + action,
			Actor: author.ID, Object: ledger.Object{Kind: ledger.ObjTask, ID: taskID}, Task: taskID, Text: ledger.Snip(text, 280)})
	}
	return c, err
}

// --- assignment (todo-panel plan Phase 3) ------------------------------------

// setTaskOwner patches the owner field in the todo's SOURCE file (the
// non-HTTP three-way sibling of handleTaskUpdate's owner patch).
func (s *Server) setTaskOwner(id, owner string) error {
	switch {
	case strings.HasPrefix(id, "aion:"), strings.HasPrefix(id, "re:"):
		store, bare, ok := s.backlogStoreFor(id)
		if !ok {
			return errBadRequest("backlog not configured")
		}
		return store.UpdateItem(bare, map[string]string{"owner": owner}, time.Now())
	case strings.HasPrefix(id, "prop:"):
		slug, lineID := splitPropID(id)
		if slug == "" {
			return errBadRequest("malformed property todo id")
		}
		list, rel, ok := s.realestate.LoadTasks(slug)
		if !ok {
			return errBadRequest("property not found")
		}
		n := list.Find(lineID)
		if n == nil {
			return errBadRequest("todo not found")
		}
		n.Task.Owner = owner
		if err := s.vault.ReplaceSectionCap("realestate", rel, list.Section, list.Emit()); err != nil {
			return err
		}
		if s.index != nil {
			_ = s.index.ReindexPaths([]string{rel})
		}
		return nil
	default:
		doc, err := s.tasksStore.Load()
		if err != nil {
			return err
		}
		_, t := doc.Find(id)
		if t == nil {
			return errBadRequest("todo not found")
		}
		t.Owner = owner
		return s.tasksStore.Save(doc)
	}
}

// assignTask — the uniform assignee write core: pins identity, ensures the
// plan record (assignee in frontmatter), patches [owner::] in the source
// file, logs the replayable `assign` thread action ATTRIBUTED to the actor
// (a portal member's assign shows their name, not the owner's), and — when
// the token carries the hard `agent:` prefix (validated against the roster;
// unknown → error) — kicks the plan-phase delegation (§12 lane entry point).
// Returns the pinned id.
func (s *Server) assignTask(actor threads.Identity, rawID, owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	harness := ""
	if strings.HasPrefix(owner, "agent:") {
		if strings.Contains(owner, "::") {
			return "", errBadRequest("assignee must be the bare agent token — intent (::" +
				strings.SplitN(owner, "::", 2)[1] + ") is per-message, not per-assignment")
		}
		if harness = s.agentHarness(owner); harness == "" {
			return "", errBadRequest("unknown agent " + owner + " — only configured harnesses are assignable")
		}
	}
	id, ok := s.pinTaskID(strings.TrimSpace(rawID))
	if !ok {
		return "", errBadRequest("todo not found")
	}
	if err := s.setTaskOwner(id, owner); err != nil {
		return "", err
	}
	if err := s.setPlanAssignee(id, owner); err != nil {
		return "", err
	}
	text := "assigned to " + orStr(owner, "me")
	if _, err := s.addThreadEntry(actor, id, threads.ActAssign, text, nil, nil,
		map[string]any{"assignee": owner}); err != nil {
		return "", err
	}
	if harness != "" {
		if err := s.assignAgentHook(id, harness); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (s *Server) handleTaskAssign(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID, Owner string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	id, err := s.assignTask(s.ownerIdentity(), b.ID, b.Owner)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "record": s.readPlanRecord(id), "thread": s.listThread(id)})
}

// --- endpoints ---------------------------------------------------------------

func (s *Server) handleTaskThreadGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	writeJSON(w, map[string]any{"thread": s.listThread(id)})
}

// handleTaskThreadPost adds a comment. The composer uploads files first
// (handleTaskThreadFile) and posts their refs here. Mentions are structural
// roster tokens (typeahead-inserted — no new syntax); a mentioned agent
// triggers the comment-phase spool (Phase 4 hook).
//
// Agent-chat plan §3.4 (2026-09-04): the composer posts a MODE —
//
//	comment  record only; never spends a turn (the reply guard, Q6) unless
//	         the text itself addresses an agent (`@alfred …`, structural or
//	         typed), which makes it an Ask (`@alfred::plan` → a Do)
//	ask      one turn, answered in this thread (the `info` persona relay)
//	do       the full lifecycle: assign → plan persona → ## plan → fire
//
// and an optional AGENT token (the roster; default = the assignee, else
// Alfred). The comment stays the record either way, with meta.mode/agent.
func (s *Server) handleTaskThreadPost(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID, Text string
		Context  []artifactContextRef
		Mentions []string
		Files    []threads.FileRef
		Mode     string
		Agent    string
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	id, ok := s.pinTaskID(strings.TrimSpace(b.ID))
	if !ok {
		http.Error(w, "todo not found", http.StatusNotFound)
		return
	}
	contextText, err := s.taskArtifactContext(id, b.Context)
	if err != nil {
		httpError(w, err)
		return
	}
	c, err := s.postAndDispatchContext(id, b.Mode, b.Agent, b.Mentions, b.Files, b.Text, b.Context, contextText)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "comment": c, "thread": s.listThread(id)})
}

// postAndDispatch records the owner's text as a thread comment (the record,
// with meta.mode/agent on Ask/Do) and then resolves the mode into at most
// one turn. Shared by the panel composer and the capture bar.
func (s *Server) postAndDispatch(id, mode, agent string, mentions []string, files []threads.FileRef, text string) (threads.Comment, error) {
	return s.postAndDispatchContext(id, mode, agent, mentions, files, text, nil, "")
}

func (s *Server) postAndDispatchContext(id, mode, agent string, mentions []string, files []threads.FileRef, text string, refs []artifactContextRef, contextText string) (threads.Comment, error) {
	if link := s.taskChatLink(id, s.listThread(id), ""); link != nil && link.Canonical {
		return threads.Comment{}, errBadRequest("continue in the task's linked conversation")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "ask" && mode != "do" {
		mode = "comment"
	}
	agent = strings.TrimSpace(agent)
	if mode == "comment" {
		agent = "" // a comment addresses nobody unless its text does (the reply guard)
	} else if base, _ := splitAgentToken(agent); agent != "" && s.agentHarness(base) == "" {
		return threads.Comment{}, errBadRequest("unknown agent " + agent + " — pick one from the roster")
	}
	mentions = mergeMentions(mentions, s.textMentions(text))
	// assignment lands BEFORE the ask in the thread — "assigned to Alfred
	// (asked)" then the question, the order a reader expects
	plan := s.resolveDispatch(id, mode, agent, mentions)
	// the record says what actually happened: the RESOLVED mode + agent (a
	// `@alfred::plan` typed into a Comment is recorded as the Do it became)
	var meta map[string]any
	if plan != nil {
		meta = map[string]any{"mode": plan.Mode, "agent": plan.Agent}
	}
	if len(refs) > 0 {
		if meta == nil {
			meta = map[string]any{}
		}
		meta["context"] = refs
	}
	s.dispatchAssign(id, plan)
	c, err := s.addThreadEntry(s.ownerIdentity(), id, threads.ActComment, text, mentions, files, meta)
	if err != nil {
		return c, err
	}
	s.dispatchRelay(id, plan, text+contextText)
	return c, nil
}

// threadDialogHook is the portal's (and any structural caller's) comment
// entry, called AFTER the comment is on record: a plain comment with the
// reply guard, where only an agent mention spends a turn. Same semantics as
// the dashboard composer's Comment mode.
func (s *Server) threadDialogHook(taskID string, mentions []string, text string) {
	plan := s.resolveDispatch(taskID, "comment", "", mergeMentions(mentions, s.textMentions(text)))
	// This hook is the team-portal entry; coding execution is authorized by
	// Benjamin's personal board, whose composer uses postAndDispatch directly.
	if plan != nil && isCodingAgent(s.agentHarness(plan.Agent)) {
		return
	}
	s.dispatchAssign(taskID, plan)
	s.dispatchRelay(taskID, plan, text)
}

// textMentionRe finds @name with optional ::intent and ::model:slug segments.
// See docs/board-model-selection.md. Names are matched against
// the roster; anything unknown stays prose (fail closed — Buzz identity rule).
var textMentionRe = regexp.MustCompile(`(?:^|[^\w@])@([a-z0-9-]+)((?:::[a-z0-9_.:-]*)*)`)

// textMentions parses the roster mentions typed into a comment, as
// structural tokens (`agent:alfred`, `agent:alfred::plan`).
func (s *Server) textMentions(text string) []string {
	var out []string
	for _, m := range textMentionRe.FindAllStringSubmatch(strings.ToLower(text), -1) {
		tok := "agent:" + m[1]
		if s.agentHarness(tok) == "" {
			continue
		}
		if m[2] != "" {
			tok += strings.TrimRight(m[2], ".")
		}
		out = append(out, tok)
	}
	return out
}

// mergeMentions appends the typed mentions the structural list doesn't
// already carry (order preserved, duplicates dropped).
func mergeMentions(structural, typed []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{structural, typed} {
		for _, m := range list {
			m = strings.TrimSpace(m)
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// dispatchPlan is a resolved request for ONE turn: who, in which mode, with
// which persona intent — and whether the todo must change hands first.
type dispatchPlan struct {
	Agent  string // roster token (agent:alfred | agent:<profile> | agent:kairos …)
	Mode   string // ask | do
	Model  string // requested coding model; empty means best
	Intent string // persona intent (info / brief / … for ask; plan for do)
	Assign string // "" = keep the assignment; else the reason the agent takes the todo
}

// resolveDispatch turns a posted comment's mode into at most ONE turn (nil =
// record only, the reply guard):
//
//   - comment: the first agent mention (structural or typed) makes it an
//     Ask (intent from the token, default `info`), `::plan` makes it a Do;
//     no mention → nil. Mentioned people stay record-only.
//   - ask: the agent (given, else the assignee, else Alfred) takes one
//     comment-phase turn; an unassigned todo is auto-assigned first.
//   - do: the agent is assigned if it doesn't hold the todo, then takes the
//     plan-phase turn with the text as the opening brief; fire stays explicit.
func (s *Server) resolveDispatch(taskID, mode, agent string, mentions []string) *dispatchPlan {
	agent, intent, model := parseAgentToken(agent)
	// An explicit address takes precedence over the Ask/Do picker's default.
	// Keep an explicitly model-qualified agent field authoritative; otherwise
	// resolve the recipient before looking for that recipient's model options.
	if mode != "comment" && model == "" {
		for _, m := range mentions {
			base, in, requested := parseAgentToken(m)
			if s.agentHarness(base) != "" {
				agent, model = base, requested
				if intent == "" {
					intent = in
				}
				break
			}
		}
	}
	if mode == "comment" {
		for _, m := range mentions {
			base, in, requested := parseAgentToken(m)
			if s.agentHarness(base) != "" {
				agent, intent, model = base, in, requested
				break
			}
		}
		if agent == "" {
			return nil // record only — never a turn
		}
		mode = "ask"
		if intent == "plan" {
			mode = "do"
		}
	}
	rec := s.readPlanRecord(taskID)
	if agent == "" {
		if s.agentHarness(rec.Assignee) != "" {
			agent = rec.Assignee
		} else {
			agent = s.defaultAgentToken()
		}
	}
	if s.agentHarness(agent) == "" {
		return nil
	}
	// Ask/Do may carry the override in their selected agent or a matching mention.
	if mode != "comment" && model == "" {
		for _, m := range mentions {
			base, in, requested := parseAgentToken(m)
			if base == agent {
				if requested != "" {
					model = requested
				}
				if intent == "" && in != "" {
					intent = in
				}
				if requested != "" {
					break
				}
			}
		}
	}
	if intent == "plan" {
		mode = "do"
	}
	p := &dispatchPlan{Agent: agent, Mode: mode, Intent: intent, Model: model}
	switch mode {
	case "do":
		p.Intent = "plan"
		if isCodingAgent(s.agentHarness(agent)) && intent != "plan" {
			p.Intent = "execute"
		}
		if rec.Assignee != agent {
			p.Assign = "do"
		}
	default:
		if p.Intent == "" {
			p.Intent = "info"
		}
		if s.agentHarness(rec.Assignee) == "" {
			p.Assign = "asked"
		}
	}
	return p
}

// dispatchAssign is the plan's assignment write — without assignAgentHook's
// empty plan order, because the dispatch that follows carries the owner's
// text as the brief (agent-chat plan §3.4a: "the ask text as the opening
// brief"). No-op when the plan keeps the assignment.
func (s *Server) dispatchAssign(taskID string, p *dispatchPlan) {
	if p == nil || p.Assign == "" {
		return
	}
	_ = s.setTaskOwner(taskID, p.Agent)
	_ = s.setPlanAssignee(taskID, p.Agent)
	_, _ = s.addThreadEntry(s.ownerIdentity(), taskID, threads.ActAssign,
		"assigned to "+p.Agent+" ("+p.Assign+")", nil, nil, map[string]any{"assignee": p.Agent})
}

// dispatchRelay spends the plan's one turn.
func (s *Server) dispatchRelay(taskID string, p *dispatchPlan, text string) {
	if p == nil {
		return
	}
	s.relayToAgent(taskID, p.Agent, text, p.Intent, p.Model)
}

// actRelayPending is the private marker a refused relay leaves (agent busy):
// the sweep retries it and the closing ActRelay marker supersedes it.
const actRelayPending = "relay-pending"

// relay spools one turn for an agent token: comment-phase (or plan-phase
// on the `plan` intent) with the owner's text, and writes the ActRelay
// marker on success. Errors surface to the caller.
func (s *Server) relay(taskID, agent, text, intent string, model ...string) error {
	harness := s.agentHarness(agent)
	if harness == "" {
		return errBadRequest("unknown agent " + agent)
	}
	err := s.spoolTaskWorkOrderAs(s.findHarness(harness), agent, taskID, personaPhase(intent), text, intent, model...)
	if err == nil {
		s.markerAdd(taskID, threads.ActRelay, "")
	}
	return err
}

// relayToAgent is relay for the request path: a busy agent (ErrAlreadyActive
// on either transport) parks the ask as a relay-pending marker the sweep
// retries; other failures are logged (the comment is still on record).
func (s *Server) relayToAgent(taskID, agent, text, intent string, model ...string) {
	err := s.relay(taskID, agent, text, intent, model...)
	switch {
	case err == nil:
	case errors.Is(err, spirits.ErrAlreadyActive):
		s.markerAddMeta(taskID, actRelayPending, "", map[string]any{"agent": agent, "intent": intent, "text": text, "model": firstModel(model)})
	default:
		log.Printf("todo relay %s → %s: %v", taskID, agent, err)
		if isCodingAgent(s.agentHarness(agent)) {
			_, _ = s.addThreadEntry(agentTokenIdentity(agent), taskID, threads.ActComment,
				"couldn't start the coding task — "+err.Error(), nil, nil, nil)
		}
	}
}

// assignAgentHook dispatches an explicit assignment. Coding owners execute
// directly (2026-09-06); other owners retain the existing plan/fire lane.
func (s *Server) assignAgentHook(taskID, harness string) error {
	phase, intent := "plan", "plan"
	if isCodingAgent(harness) {
		phase, intent = "go", "execute"
	}
	err := s.spoolTaskWorkOrder(s.findHarness(harness), taskID, phase, "", intent)
	if errors.Is(err, spirits.ErrAlreadyActive) {
		if isCodingAgent(harness) {
			s.markerAddMeta(taskID, actRelayPending, "", map[string]any{"agent": "agent:" + harness, "intent": intent, "text": "Execute the assigned task."})
		}
		return nil // busy dispatches use the existing pending-relay retry
	}
	return err
}

// handleTaskThreadFile stores one attachment blob (raw body) and returns its
// content-addressed ref — the composer attaches refs on the comment POST.
func (s *Server) handleTaskThreadFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if id == "" || name == "" {
		httpError(w, errBadRequest("id and name are required"))
		return
	}
	if s.threads == nil {
		httpError(w, errBadRequest("threads not configured"))
		return
	}
	st := s.threadStore(s.threadKind(id))
	if st == nil {
		httpError(w, errBadRequest("no store for this todo"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, threads.MaxBlobSize+1)
	ref, err := st.SaveBlob(r.Body, name, r.Header.Get("Content-Type"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "file": ref})
}

// handleTaskThreadBlob serves a stored attachment.
func (s *Server) handleTaskThreadBlob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	hash := r.PathValue("hash")
	if s.threads == nil {
		http.Error(w, "threads not configured", http.StatusServiceUnavailable)
		return
	}
	st := s.threadStore(s.threadKind(id))
	p := ""
	if st != nil {
		p = st.BlobPath(hash)
	}
	if p == "" && s.threads.private != nil { // tolerate stale routing
		p = s.threads.private.BlobPath(hash)
	}
	if p == "" {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	if dl := r.URL.Query().Get("dl"); dl == "1" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	http.ServeFile(w, r, p)
}

func portalCommentContext(c teamportal.Comment) map[string]any {
	if len(c.Context) == 0 {
		return nil
	}
	return map[string]any{"context": c.Context}
}
