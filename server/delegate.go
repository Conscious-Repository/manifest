package server

// Delegation (big-change Phase 6): a todo becomes dispatchable to a harness.
// Strict propose-only decomposition — no new store, no new write boundary:
//
//   - "delegate" spools a run request into the CHOSEN harness (the existing
//     spool contract) with the todo's composite id riding in the request as a
//     `[todo:: <id>]` token, and marks a personal todo `[waiting:: <harness>]`
//     through the existing todos machinery. The spool file IS the work order.
//   - Results return as run reports and/or proposals (approvals inbox) that
//     carry the same token; nothing here executes anything.
//   - Read-side, every unified todo row gains a `delegation` projection
//     (queued | running | failed | done | proposed + harness) derived purely
//     from which trace files exist and carry the token.

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"manifest/spirits"
	"manifest/threads"
)

var todoTokenRe = regexp.MustCompile(`\[todo::\s*([^\]]+)\]`)

// phaseTokenRe (todo-panel plan Phase 4): a work order optionally carries
// `[phase:: plan|go|comment]` — recovered the same way the todo token is.
// No phase = the classic single-shot delegation.
var phaseTokenRe = regexp.MustCompile(`\[phase::\s*([a-z]+)\]`)

// delegationView is the row projection. When the run left a library brief, the
// projection carries it too — the board chip, the feed card and the run modal
// then all open the SAME file (see server/artifacts.go).
type delegationView struct {
	State   string `json:"state"` // queued | running | failed | done | proposed (+ plan-queued | plan-running | plan-ready | go-queued)
	Phase   string `json:"phase,omitempty"`
	Harness string `json:"harness"`
	RunID   string `json:"runId,omitempty"`
	// the result, ready to open: exactly one of these is ever set (§8 two media)
	ArtifactRef  string `json:"artifactRef,omitempty"`  // harness-relative → /api/spirits/file
	ArtifactPath string `json:"artifactPath,omitempty"` // vault-relative → the note view
	// Started is when the run began, used to prefer the newest run when two
	// completed runs share the same todo id. Server-side only: `-` keeps it off
	// the wire entirely (omitempty would not — encoding/json never treats a
	// struct as empty, and a real run's Started is non-zero anyway).
	Started time.Time `json:"-"`
}

// delegationIndex scans every harness's traces ONCE per request: spool files
// (queued), run reports (running/failed/done), pending proposals (proposed).
// Precedence: proposed > running > queued > failed > done — a returned
// proposal is the thing awaiting the human; a live run beats history.
func (s *Server) delegationIndex() map[string]delegationView {
	rank := map[string]int{
		"done": 1, "plan-ready": 1, "failed": 2,
		"queued": 3, "plan-queued": 3, "go-queued": 3,
		"running": 4, "plan-running": 4, "proposed": 5,
	}
	out := map[string]delegationView{}
	set := func(id string, d delegationView) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if cur, ok := out[id]; !ok || rank[d.State] > rank[cur.State] ||
			(rank[d.State] == rank[cur.State] && d.Started.After(cur.Started)) {
			out[id] = d
		}
	}
	// phased folds the phase token into the base state name.
	phased := func(base, request string) (string, string) {
		m := phaseTokenRe.FindStringSubmatch(request)
		if m == nil {
			return base, ""
		}
		phase := m[1]
		switch {
		case phase == "plan" && base == "queued":
			return "plan-queued", phase
		case phase == "plan" && base == "running":
			return "plan-running", phase
		case phase == "plan" && base == "done":
			return "plan-ready", phase
		case phase == "go" && base == "queued":
			return "go-queued", phase
		}
		return base, phase
	}
	for _, h := range s.eachHarness() {
		if h.Spirits != nil {
			lib := harnessLibrary(h) // one library read per harness, at most
			for _, q := range h.Spirits.Queued() {
				if m := todoTokenRe.FindStringSubmatch(q.Request); m != nil {
					st, ph := phased("queued", q.Request)
					set(m[1], delegationView{State: st, Phase: ph, Harness: h.Name})
				}
			}
			for _, r := range h.Spirits.Runs() {
				var todoID string
				var doc spirits.LibraryDoc // the brief this run wrote, if any
				if m := todoTokenRe.FindStringSubmatch(r.Request); m != nil {
					todoID = strings.TrimSpace(m[1])
					doc, _ = libraryDocForRun(h, r.ID, lib)
				} else {
					// an OLD report whose `request:` frontmatter was truncated
					// past its trailing token (the engine preserves it now, but
					// history can't be rewritten). Recover the token so the todo
					// still resolves to this run's artifact: first from the
					// report BODY, which carries the request in full, then from
					// the brief the run wrote.
					sum, body, ok := h.Spirits.Run(r.ID)
					runID := r.ID
					if ok && sum.Run != "" {
						runID = sum.Run
					}
					var wroteBrief bool
					doc, wroteBrief = libraryDocForRunID(runID, r.ID, lib)
					if m := todoTokenRe.FindStringSubmatch(body); m != nil {
						todoID = strings.TrimSpace(m[1])
					} else if wroteBrief {
						if m := todoTokenRe.FindStringSubmatch(doc.Title + "\n" + doc.Body); m != nil {
							todoID = strings.TrimSpace(m[1])
						}
					}
				}
				if todoID == "" {
					continue
				}
				state := "done"
				switch r.Outcome {
				case "running", "":
					state = "running"
				case "completed":
					state = "done"
				default:
					state = "failed"
				}
				// phase recovery rides the same ladder as the todo token: the
				// report's `request:` may be TRUNCATED past the trailing
				// [phase::] token (the owner's first live test hit exactly
				// this) — fall back to the brief the run wrote.
				phaseSrc := r.Request
				if !phaseTokenRe.MatchString(phaseSrc) {
					if doc.Ref == "" {
						doc, _ = libraryDocForRun(h, r.ID, lib)
					}
					phaseSrc += "\n" + doc.Title + "\n" + doc.Body
				}
				st, ph := phased(state, phaseSrc)
				d := delegationView{State: st, Phase: ph, Harness: h.Name, RunID: r.ID}
				if ts, err := time.Parse(time.RFC3339, r.Started); err == nil {
					d.Started = ts
				}
				// the deliverable: the brief this run wrote, else the newest
				// brief carrying the same delegation token
				ref := doc.Ref
				if ref == "" {
					ref = libraryRefForToken(todoID, lib)
				}
				d.ArtifactPath, d.ArtifactRef = s.artifactRefSplit(h, ref)
				set(todoID, d)
			}
		}
		if h.Approvals != nil {
			for _, p := range h.Approvals.List("pending") {
				if m := todoTokenRe.FindStringSubmatch(p.Action + "\n" + p.Body); m != nil {
					set(m[1], delegationView{State: "proposed", Harness: h.Name})
				}
			}
		}
	}
	return out
}

// ---- the assign-to-agent lane (todo-panel plan Phase 4) ---------------------

// delegateTargetFor picks a harness's dispatch destination the same way the
// targets endpoint would: its first valid on-demand ritual, else the generic
// contract work order (hermes' shape).
func delegateTargetFor(h *Harness) (spirit, ritual string) {
	if h.Spirits != nil {
		for _, rit := range h.Spirits.Rituals(time.Now()) {
			if rit.Cadence == "" && rit.Valid {
				return rit.Spirit, rit.Ritual
			}
		}
	}
	return h.Name, "delegate"
}

// findHarness resolves a harness by name.
func (s *Server) findHarness(name string) *Harness {
	hs := s.eachHarness()
	for i := range hs {
		if hs[i].Name == name {
			return &hs[i]
		}
	}
	return nil
}

// spoolTodoWorkOrder composes and spools one phased work order. The request
// ALWAYS carries the todo's own text (the agent's only unambiguous handle)
// plus the durable [todo::] and [phase::] tokens. Comment-phase orders carry
// the FULL dialog context (description · current plan · thread tail) — the
// thread IS the conversation channel (owner decision 2026-08-15).
func (s *Server) spoolTodoWorkOrder(harness *Harness, todoID, phase, extra string) error {
	if harness == nil || harness.Spirits == nil {
		return errBadRequest("harness not available")
	}
	text, _ := s.openTodoText(todoID)
	rec := s.readPlanRecord(todoID)
	var b strings.Builder
	b.WriteString("TASK (from your todo board): " + text + "\n")
	if d := strings.TrimSpace(rec.Description); d != "" && phase != "go" {
		b.WriteString("DESCRIPTION:\n" + d + "\n")
	}
	switch phase {
	case "plan":
		if extra != "" {
			b.WriteString("OWNER'S ASK:\n" + extra + "\n")
		}
		b.WriteString(agentProtocolReminder)
	case "go":
		b.WriteString("EXECUTE the approved plan below. The owner reviewed and fired it — do the work and " +
			"write the result as your library brief.\nAPPROVED PLAN:\n" + extra + "\n")
	case "comment":
		if p := strings.TrimSpace(rec.Plan); p != "" {
			b.WriteString("CURRENT PLAN (the canon plan on the record — the owner may have edited it):\n" + p + "\n")
		}
		if tail := s.threadTail(todoID, 6); tail != "" {
			b.WriteString("THREAD (newest last — this is your running dialog with the owner):\n" + tail + "\n")
		}
		if extra != "" {
			b.WriteString("NEW OWNER COMMENT (respond to this):\n" + extra + "\n")
		}
		b.WriteString(agentProtocolReminder)
	}
	b.WriteString("For this todo: [todo:: " + todoID + "] [phase:: " + phase + "]")
	spirit, ritual := delegateTargetFor(harness)
	return harness.Spirits.SpoolRunNow(spirit, ritual, b.String(), "")
}

// agentProtocolReminder is stamped on plan/comment orders — the manifest side
// of the one contract the ritual also states.
const agentProtocolReminder = "PROTOCOL: your library brief must be exactly ONE of — " +
	"(a) QUESTIONS: if you cannot proceed without answers, the ENTIRE brief is your questions " +
	"under a leading '# questions' heading, nothing else; " +
	"(b) PLAN: a complete, concrete, numbered plan with ZERO questions inside it. " +
	"Never mix the two. Do not execute anything in these phases.\n"

// threadTail renders the last n visible thread entries, author-labeled.
func (s *Server) threadTail(todoID string, n int) string {
	th := s.listThread(todoID)
	if len(th) == 0 {
		return ""
	}
	var lines []string
	for _, c := range th[max(0, len(th)-n):] {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		who := c.AuthorName
		if who == "" {
			who = c.Author
		}
		lines = append(lines, who+": "+strings.TrimSpace(c.Text))
	}
	return strings.Join(lines, "\n")
}

// --- the agent loop (todo-panel robustness pass, owner test 2026-08-15) -----
//
// agentLoopSweep runs on the signals cadence and makes the dialog robust:
//
//   - EVERY completed plan/comment-phase run is ingested: a questions-only
//     brief becomes a hermes thread comment (never a plan); a plan brief
//     materializes into the record's ## plan (§12 lane) + a "plan attached/
//     updated" thread comment; an embedded questions section (model drift)
//     is split out as its own comment on top of the materialized plan.
//   - go-phase completions post a result comment.
//   - owner comments that missed their relay (agent was mid-run) are retried.
//
// Idempotency markers (empty structural entries, meta.marker) live in the
// PRIVATE store regardless of where the visible dialog lands; visible agent
// comments route like any comment (aion → team thread, as Hermes).

// briefQuestions extracts a `# questions`-style section from a brief.
// questionsOnly = the brief LEADS with the questions heading (the protocol's
// questions shape); a mid-document section is the defensive split-out case.
func briefQuestions(brief string) (questions string, questionsOnly bool) {
	lines := strings.Split(brief, "\n")
	firstHeading, qStart, qLevel := -1, -1, 0
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		level := 0
		for level < len(t) && t[level] == '#' {
			level++
		}
		title := strings.ToLower(strings.TrimSpace(t[level:]))
		if firstHeading < 0 {
			firstHeading = i
		}
		if qStart < 0 && strings.Contains(title, "question") {
			qStart, qLevel = i, level
		}
	}
	if qStart < 0 {
		return "", false
	}
	end := len(lines)
	for i := qStart + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "#") {
			lvl := 0
			for lvl < len(t) && t[lvl] == '#' {
				lvl++
			}
			if lvl <= qLevel {
				end = i
				break
			}
		}
	}
	body := strings.TrimSpace(strings.Join(lines[qStart+1:end], "\n"))
	return body, firstHeading == qStart
}

func agentIdentity(harness string) threads.Identity {
	name := harness
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	return threads.Identity{ID: "agent:" + harness, Name: name}
}

func (s *Server) agentLoopSweep(index map[string]delegationView) {
	if s.threads == nil || s.todoPlans == nil || s.vault == nil || s.threads.private == nil {
		return
	}
	priv := s.threads.private
	for id, d := range index {
		if d.RunID == "" {
			continue
		}
		var mode string // "brief" (plan|questions) or "result"
		switch {
		case d.State == "plan-ready":
			mode = "brief"
		case d.State == "done" && d.Phase == "comment":
			mode = "brief"
		case d.State == "done" && d.Phase == "go":
			mode = "result"
		default:
			continue
		}
		h := s.findHarness(d.Harness)
		if h == nil || h.Spirits == nil {
			continue
		}
		hermes := agentIdentity(d.Harness)
		meta := map[string]any{"run": d.RunID, "harness": d.Harness}
		if d.ArtifactRef != "" {
			meta["artifactRef"] = d.ArtifactRef
		}
		if mode == "result" {
			if priv.HasAction(id, threads.ActResult, d.RunID) {
				continue
			}
			_, _ = s.addThreadEntry(hermes, id, threads.ActComment,
				"result is ready — review it, then check the todo or send it back with a comment", nil, nil, meta)
			s.markerAdd(id, threads.ActResult, d.RunID)
			continue
		}
		if priv.HasAction(id, threads.ActPlan, d.RunID) || priv.HasAction(id, threads.ActQuestions, d.RunID) {
			continue
		}
		doc, ok := libraryDocForRun(*h, d.RunID, harnessLibrary(*h))
		brief := strings.TrimSpace(doc.Body)
		if !ok || brief == "" { // no brief — fall back to the run report body
			if _, body, ok2 := h.Spirits.Run(d.RunID); ok2 {
				brief = strings.TrimSpace(body)
			}
		}
		if brief == "" {
			continue
		}
		// a phased brief implies engagement: backfill the record assignee when
		// unset (pre-auto-assign history) so follow-up comments relay
		if rec := s.readPlanRecord(id); rec.Assignee == "" {
			_ = s.setPlanAssignee(id, "agent:"+d.Harness)
		}
		questions, questionsOnly := briefQuestions(brief)
		if questionsOnly && questions != "" {
			if _, err := s.addThreadEntry(hermes, id, threads.ActComment, questions, nil, nil, meta); err != nil {
				continue
			}
			s.markerAdd(id, threads.ActQuestions, d.RunID)
			continue
		}
		if err := s.writePlanSection("todo-plans-agent", id, "plan", brief); err != nil {
			continue // capability/store hiccup — retry next sweep
		}
		_, _ = s.addThreadEntry(hermes, id, threads.ActComment,
			"plan attached to this task — answer in the thread to refine it, edit it directly, or fire to execute", nil, nil, meta)
		if questions != "" { // drift guard: embedded questions still surface as dialog
			_, _ = s.addThreadEntry(hermes, id, threads.ActComment, questions, nil, nil, meta)
		}
		s.markerAdd(id, threads.ActPlan, d.RunID)
	}
	s.relaySweep(index)
}

// markerAdd writes a hidden idempotency marker into the private store.
func (s *Server) markerAdd(id, action, runID string) {
	_, _ = s.threads.private.Add(threads.Identity{ID: "system", Name: "system"}, id, action, "",
		nil, nil, map[string]any{"run": runID, "marker": true}, time.Now())
}

// relaySweep retries owner comments that missed their relay (the agent was
// mid-run when they posted — SpoolRunNow refuses while active).
func (s *Server) relaySweep(index map[string]delegationView) {
	priv := s.threads.private
	// candidate ids: anything with private entries, shared-RE entries, or an
	// active delegation — agent-assigned todos always land in at least one
	ids := map[string]bool{}
	for _, id := range priv.TodoIDs() {
		ids[id] = true
	}
	if s.threads.re != nil {
		for _, id := range s.threads.re.TodoIDs() {
			ids[id] = true
		}
	}
	for id := range index {
		ids[id] = true
	}
	for id := range ids {
		rec := s.readPlanRecord(id)
		harness := s.agentHarness(rec.Assignee)
		if harness == "" {
			continue
		}
		var lastOwner time.Time
		for _, c := range s.listThread(id) {
			if c.Action == threads.ActComment && !strings.HasPrefix(c.Author, "agent:") && c.At.After(lastOwner) {
				lastOwner = c.At
			}
		}
		if lastOwner.IsZero() {
			continue
		}
		var lastAct time.Time
		for _, c := range priv.Thread(id) {
			if c.Action == threads.ActRelay && c.At.After(lastAct) {
				lastAct = c.At
			}
		}
		if d, ok := index[id]; ok && d.Started.After(lastAct) {
			lastAct = d.Started
		}
		if !lastOwner.After(lastAct) {
			continue // the dialog is caught up
		}
		if err := s.spoolTodoWorkOrder(s.findHarness(harness), id, "comment", ""); err == nil {
			s.markerAdd(id, threads.ActRelay, "")
		}
	}
}

// handleTodoFire — the explicit go: snapshots the CURRENT plan bytes (the
// human may have hand-edited them — the human is the mutex) + the thread tail
// into a go-phase work order.
func (s *Server) handleTodoFire(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	id := strings.TrimSpace(b.ID)
	rec := s.readPlanRecord(id)
	if !rec.Exists || strings.TrimSpace(rec.Plan) == "" {
		httpError(w, errBadRequest("no plan to fire — write one or assign an agent first"))
		return
	}
	harness := s.agentHarness(rec.Assignee)
	if harness == "" {
		httpError(w, errBadRequest("this todo isn't assigned to an agent"))
		return
	}
	extra := rec.Plan
	// the thread tail rides along — the owner's comments are steer
	th := s.listThread(id)
	if n := len(th); n > 0 {
		var tail []string
		for _, c := range th[max(0, n-5):] {
			if c.Action == threads.ActComment && strings.TrimSpace(c.Text) != "" {
				tail = append(tail, c.AuthorName+": "+c.Text)
			}
		}
		if len(tail) > 0 {
			extra += "\n\nRECENT THREAD:\n" + strings.Join(tail, "\n")
		}
	}
	if err := s.spoolTodoWorkOrder(s.findHarness(harness), id, "go", extra); err != nil {
		if errors.Is(err, spirits.ErrAlreadyActive) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"active": true, "harness": harness})
			return
		}
		httpError(w, err)
		return
	}
	_, _ = s.addThreadEntry(s.ownerIdentity(), id, threads.ActFire,
		"fired — "+harness+" is executing the plan", nil, nil, map[string]any{"harness": harness})
	s.markerAdd(id, threads.ActFire, "") // the signal scan reads private markers
	writeJSON(w, map[string]any{"ok": true, "queued": true, "thread": s.listThread(id)})
}

// delegateTarget is one dispatchable destination: a harness's on-demand
// ritual, or the bare harness itself (contract-only trees like hermes — the
// spool file still carries spirit/ritual per the contract; the tree decides
// what to do with it).
type delegateTarget struct {
	Harness string `json:"harness"`
	Spirit  string `json:"spirit"`
	Ritual  string `json:"ritual"`
	Label   string `json:"label"`
}

func (s *Server) handleDelegateTargets(w http.ResponseWriter, r *http.Request) {
	targets := []delegateTarget{}
	now := time.Now()
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		n := 0
		for _, rit := range h.Spirits.Rituals(now) {
			if rit.Cadence != "" || !rit.Valid {
				continue // scheduled or broken — not a dispatch target
			}
			targets = append(targets, delegateTarget{
				Harness: h.Name, Spirit: rit.Spirit, Ritual: rit.Ritual,
				Label: h.Name + " · " + rit.Spirit + " / " + rit.Ritual,
			})
			n++
		}
		if n == 0 {
			// contract-only tree: a generic work order it may consume
			targets = append(targets, delegateTarget{
				Harness: h.Name, Spirit: h.Name, Ritual: "delegate",
				Label: h.Name + " · (contract work order)",
			})
		}
	}
	writeJSON(w, map[string]any{"targets": targets})
}

// handleDelegate spools the work order + stamps waiting on personal todos.
func (s *Server) handleDelegate(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID      string `json:"id"` // unified composite id
		Harness string `json:"harness"`
		Spirit  string `json:"spirit"`
		Ritual  string `json:"ritual"`
		Brief   string `json:"brief"`
		// Comment is the owner's steer when a REVIEWED result goes back out
		// (board: Review → Delegated). It rides the work order verbatim and
		// labelled, so the agent can't mistake it for the original brief.
		Comment string `json:"comment"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.ID == "" || b.Harness == "" || b.Spirit == "" || b.Ritual == "" {
		httpError(w, errBadRequest("id, harness, spirit and ritual are required"))
		return
	}
	var target *Harness
	hs := s.eachHarness()
	for i := range hs {
		if hs[i].Name == b.Harness {
			target = &hs[i]
			break
		}
	}
	if target == nil || target.Spirits == nil {
		httpError(w, errBadRequest("unknown harness "+b.Harness))
		return
	}
	// the request line: brief + ALWAYS the todo's own text + the durable token.
	// The todo text must never be dropped — it is the only unambiguous handle
	// the agent has on what the id refers to. Sending only the (opaque,
	// sometimes shared) id forces the agent to guess the subject, which is how
	// a delegation ends up producing the wrong artifact (see the courier run
	// that wrote "analytical psych" for the UAE goal in Aug 2026).
	var todoText string
	if s.todosStore != nil {
		if doc, err := s.todosStore.Load(); err == nil {
			if _, t := doc.Find(b.ID); t != nil {
				todoText = t.Text
			}
		}
	}
	var request string
	if todoText != "" {
		request = "TASK (from your todo board): " + todoText
		if brief := strings.TrimSpace(b.Brief); brief != "" {
			request += "\nBRIEF: " + brief
		}
		if c := strings.TrimSpace(b.Comment); c != "" {
			request += "\nOWNER COMMENT (on the previous result): " + c
		}
		request += "\nFor this todo: [todo:: " + b.ID + "]"
	} else {
		// no todo text found — fall back to brief-only (never silently drop text)
		text := strings.TrimSpace(b.Brief)
		if text == "" {
			text = "work the delegated todo"
		}
		if c := strings.TrimSpace(b.Comment); c != "" {
			text += " — owner comment on the previous result: " + c
		}
		request = text + " [todo:: " + b.ID + "]"
	}
	if err := target.Spirits.SpoolRunNow(b.Spirit, b.Ritual, request, ""); err != nil {
		if errors.Is(err, spirits.ErrAlreadyActive) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"active": true, "harness": b.Harness, "spirit": b.Spirit, "ritual": b.Ritual})
			return
		}
		httpError(w, err)
		return
	}
	// waiting stamp — personal todos only (the chip derives from traces for
	// every source; waiting is the belt on the todo line itself)
	if s.todosStore != nil && !strings.ContainsAny(b.ID, ":") {
		if doc, err := s.todosStore.Load(); err == nil {
			if _, t := doc.Find(b.ID); t != nil {
				t.Waiting = b.Harness
				t.Since = time.Now().Format("2006-01-02")
				_ = s.todosStore.Save(doc)
			}
		}
	}
	writeJSON(w, map[string]any{"ok": true, "queued": true})
}
