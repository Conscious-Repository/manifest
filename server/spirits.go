package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/secrets"
	"manifest/spirits"
)

// maskSecrets is the display-side defense for aion proposal bodies: any
// span matching a secret class renders as "•••" (the authoritative gates
// refuse at edit/apply — this is belt over braces for the card itself).
func maskSecrets(text string) string { return secrets.Mask(text) }

// SPIRITS — the excalibur harness console. The dashboard reads the sibling
// tree and records user decisions (keep/discard/snooze); execution belongs to
// the engine, which the dashboard only ever reaches by dropping a run-now
// request in the spool (excalibur-path-plan.md §7.5).

func (s *Server) handleSpiritsStatus(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	alive, at := s.spirits.EngineAlive()
	resp := map[string]any{
		"enabled":     true,
		"engineAlive": alive, // legacy top-level = the primary harness
		"spirits":     s.spirits.Spirits(),
		"feedInbox":   s.feedInboxCount(time.Now()), // same compute as /api/feed — counts never drift
		"harnesses":   s.harnessHeartbeats(),        // federation: per-harness liveness
	}
	if !at.IsZero() {
		resp["heartbeat"] = at.Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

// (Feed handlers moved to server/feed.go — FEED is a first-class surface now;
// SPIRITS keeps only the engine console: runs, rituals, approvals, status.)

func (s *Server) handleSpiritsRuns(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}, "queued": []any{}})
		return
	}
	// data = every run report (running ones included, outcome:running); queued =
	// spool files not yet picked up — merged across the harness federation,
	// tagged by source. The client derives queued/running/done from these
	// files alone — no browser-held run state (plan §1).
	writeJSON(w, map[string]any{"data": s.mergedRuns(), "queued": s.mergedQueued(),
		"primary": s.primaryHarnessName()})
}

func (s *Server) handleSpiritsRun(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	h, sum, body, ok := s.findRun(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"summary": sum, "body": body, "harness": h.Name})
}

// handleSpiritsRunPrompt serves the preserved exact prompts — the §6.5 "show
// assembled prompt" affordance.
func (s *Server) handleSpiritsRunPrompt(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	h, sum, _, ok := s.findRun(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	turns, err := h.Spirits.RunPrompts(sum.Spirit, sum.Run)
	if err != nil {
		http.Error(w, "prompts not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"data": turns})
}

// Approvals — the ONE inbox (excalibur/artifacts/approvals, plan §2.5).
// Spirits file proposals via the write_approval cast; Confirm/Reject here only
// RECORD the human decision (a folder move) — nothing sends or executes.

// approvalRow is a pending Proposal enriched for rendering: whether its
// apply-path is inside the type's allow-list (Confirm disabled otherwise) and
// the target's CURRENT content for the current-vs-proposed diff.
type approvalRow struct {
	approvals.Proposal
	Allowed bool   `json:"allowed"`
	Current string `json:"current"`
	Harness string `json:"harness,omitempty"` // federation source tag
	// AionPayload is the parsed structured payload of an aion proposal (nil
	// otherwise) — the editable card renders a form over it. Secret spans in
	// the body are masked display-side before it reaches the client.
	AionPayload *aion.ProposalPayload `json:"aionPayload,omitempty"`
	// AionLine is the exact record line Confirm would write (the diff target).
	AionLine string `json:"aionLine,omitempty"`
}

// approvalRows returns the enriched pending approvals, skipping any types in
// exclude. Shared by the SPIRITS endpoint (all rows — the Studio tuning panel
// reads it) and the FEED (which excludes append-x-queue: a draft's tweet-shaped
// card already carries Approve/Dismiss — one object, one card).
func (s *Server) approvalRows(exclude map[string]bool) []approvalRow {
	rows := []approvalRow{}
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		rows = append(rows, s.harnessApprovalRows(h, exclude)...)
	}
	return rows
}

// harnessApprovalRows enriches ONE harness's pending proposals; the apply
// allow-lists + current-content reads run against that harness's store.
func (s *Server) harnessApprovalRows(h Harness, exclude map[string]bool) []approvalRow {
	rows := []approvalRow{}
	store := h.Approvals
	for _, p := range store.List("pending") {
		if exclude[p.Type] {
			continue
		}
		rr := approvalRow{Proposal: p, Harness: s.harnessTag(h.Name)}
		if p.ApplyPath != "" {
			switch p.Type {
			case approvals.TypeCreateVaultNote:
				// A new vault-root note: allowed by its own path rule, no current
				// content (the diff renders as an all-added new file).
				rr.Allowed = approvals.CreateVaultNotePathAllowed(p.ApplyPath)
			case approvals.TypeAppendXQueue:
				// The bullet appends to the x-posts file; current content is that
				// file so the UI can show where it lands.
				rr.Allowed = approvals.AppendXQueuePathAllowed(p.ApplyPath)
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
			case approvals.TypeUpdateVaultSkill:
				// A skill-file edit: allowed only when the path is on the tight
				// allow-list AND the filing ritual is a tune ritual (D15).
				rr.Allowed = approvals.UpdateVaultSkillPathAllowed(p.ApplyPath) && p.Ritual == "tune"
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
			case approvals.TypeAionBacklog, approvals.TypeAionHeuristic, approvals.TypeReBacklog:
				// A domain extraction candidate (aion or real-estate): editable
				// payload + the exact line Confirm would append. Secret-masked.
				rr.Allowed = (p.Type == approvals.TypeAionBacklog && approvals.AionBacklogPathAllowed(p.ApplyPath)) ||
					(p.Type == approvals.TypeAionHeuristic && approvals.AionHeuristicPathAllowed(p.ApplyPath)) ||
					(p.Type == approvals.TypeReBacklog && approvals.ReBacklogPathAllowed(p.ApplyPath))
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
				if payload, ok := approvals.AionPayload(p); ok {
					rr.AionPayload = &payload
					rr.AionLine = aion.RenderItemLine(payload)
				} else {
					rr.Allowed = false
				}
				rr.Body = maskSecrets(rr.Body)
			default:
				rr.Allowed = approvals.ApplyPathAllowed(p.ApplyPath)
				if cur, ok := store.CurrentContent(p); ok {
					rr.Current = cur
				}
			}
		}
		rows = append(rows, rr)
	}
	return rows
}

func (s *Server) handleSpiritsApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, map[string]any{"pending": []any{}, "counts": map[string]int{}})
		return
	}
	counts := map[string]int{}
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		for k, v := range h.Approvals.Counts() {
			counts[k] += v
		}
	}
	writeJSON(w, map[string]any{"pending": s.approvalRows(nil), "counts": counts})
}

func (s *Server) handleSpiritsApprovalConfirm(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	// A create-vault-note may carry edited attendees, a retitled filename,
	// and edited frontmatter categories (the `aion` category is the
	// automation key — it makes the written note trigger extraction).
	// Edit* flags distinguish "no edit" from "cleared to none".
	var b struct {
		Attendees      []string `json:"attendees"`
		EditAttendees  bool     `json:"editAttendees"`
		Title          string   `json:"title"` // create-vault-note: owner-edited filename title
		Categories     []string `json:"categories"`
		EditCategories bool     `json:"editCategories"`
	}
	_ = decode(r, &b) // body is optional (plain confirm)
	id := r.PathValue("id")
	store := s.approvalsFor(id) // federation: route to the harness holding the id
	// run-errand payload must be read BEFORE confirm (confirm moves the file);
	// a failed confirm never enqueues, so approval maps 1:1 to one execution.
	pending, loadErr := store.LoadPending(id)
	var err error
	if b.EditAttendees || b.EditCategories || strings.TrimSpace(b.Title) != "" {
		err = store.ConfirmCreateNote(id, approvals.ConfirmEdits{
			Attendees: b.Attendees, EditAttendees: b.EditAttendees,
			Title:      b.Title,
			Categories: b.Categories, EditCategories: b.EditCategories,
		})
	} else {
		err = store.Confirm(id)
	}
	if err != nil {
		httpError(w, err)
		return
	}
	// Extraction nudge: a freshly-written transcript with the aion category
	// should spool the extractor NOW, not on the watcher's debounce. The
	// sink re-checks category + content hash, so non-aion notes and the
	// watcher's duplicate event are no-ops. The written filename is the
	// (possibly retitled) apply path, lowercased by the apply.
	if loadErr == nil && pending.Type == approvals.TypeCreateVaultNote && s.aionSink != nil {
		if approved, err := store.LoadApproved(id); err == nil && approved.ApplyPath != "" {
			s.aionSink.Notify([]string{"log/" + strings.ToLower(approved.ApplyPath)})
		}
	}
	if loadErr == nil && pending.Type == approvals.TypeRunErrand {
		if s.errandExec == nil {
			httpError(w, errBadRequest("approved, but errands are not available to enqueue"))
			return
		}
		if _, err := s.errandExec.Enqueue(pending.ErrandText, pending.ErrandAccount, "proposal", id, pending.ErrandGoal); err != nil {
			httpError(w, errBadRequest("approved, but enqueue failed: "+err.Error()))
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSpiritsApprovalAion rewrites a pending aion proposal's editable
// payload (kind/title/owner/rock/due/outcome, heuristic new⇄reinforce flip)
// — the pre-Confirm edit lane. The id never changes.
func (s *Server) handleSpiritsApprovalAion(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var payload aion.ProposalPayload
	if err := decode(r, &payload); err != nil {
		httpError(w, err)
		return
	}
	if err := s.approvalsFor(r.PathValue("id")).SetAionPayload(r.PathValue("id"), payload); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleSpiritsApprovalReject(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &b) // reason is optional
	if err := s.approvalsFor(r.PathValue("id")).Reject(r.PathValue("id"), b.Reason); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// RITUALS board — every ritual across spirits with computed next-fire, last
// outcome, ceiling, and validity (plans/spirits-console-upgrade.md §1).
func (s *Server) handleSpiritsRituals(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.spirits.Rituals(time.Now())})
}

// handleSpiritsFileGet / Put — the raw markdown editor over the allow-listed
// harness config files (§2). Paths off the allow-list 404; PUT lints and blocks
// hard breakage (422) while letting warnings through.
// Reads span the FEDERATION (fix 2026-08-12): an artifact brief lives in the
// harness that wrote it, so a hermes card asking for its own library file must
// not 404 against excalibur. `harness` narrows the search when the client knows
// the tag; otherwise every tree is tried, primary first. Writes stay
// primary-only (PUT below) — the federation contract is read-side voluntary.
func (s *Server) handleSpiritsFileGet(w http.ResponseWriter, r *http.Request) {
	hs := s.eachHarness()
	if len(hs) == 0 {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	path := r.URL.Query().Get("path")
	want := r.URL.Query().Get("harness")
	offList := false
	for _, h := range hs {
		if h.Spirits == nil {
			continue
		}
		if want != "" && h.Name != want && s.harnessTag(h.Name) != want {
			continue
		}
		content, allowed, err := h.Spirits.ReadFile(path)
		if !allowed {
			offList = true
			continue // the allow-list is identical across trees
		}
		if err != nil {
			continue // not in THIS tree — try the next one
		}
		writeJSON(w, map[string]any{"path": path, "harness": s.harnessTag(h.Name), "content": content})
		return
	}
	if offList {
		http.Error(w, "path not editable", http.StatusNotFound)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) handleSpiritsFilePut(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Content string `json:"content"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	res, allowed, err := s.spirits.WriteFile(r.URL.Query().Get("path"), b.Content)
	if !allowed {
		http.Error(w, "path not editable", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	if !res.OK {
		w.WriteHeader(http.StatusUnprocessableEntity) // lint blocked the save
	}
	writeJSON(w, res)
}

// handleSpiritsNewRitual / NewSpirit — quick create (§3).
func (s *Server) handleSpiritsNewRitual(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit string `json:"spirit"`
		Name   string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	path, err := s.spirits.ScaffoldRitual(b.Spirit, b.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"path": path})
}

func (s *Server) handleSpiritsNewSpirit(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.ScaffoldSpirit(b.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"path": "spirits/" + b.Name + "/cornerstone.md"})
}

func (s *Server) handleSpiritsRunNow(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit  string `json:"spirit"`
		Ritual  string `json:"ritual"`
		Request string `json:"request"`
		Skill   string `json:"skill"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.SpoolRunNow(b.Spirit, b.Ritual, b.Request, b.Skill); err != nil {
		if errors.Is(err, spirits.ErrAlreadyActive) {
			w.WriteHeader(http.StatusConflict) // the ritual is already queued/running
			writeJSON(w, map[string]any{"active": true, "error": "already queued or running"})
			return
		}
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"spooled": true})
}

// handleSpiritsCastables lists what the command bar can cast: the summoner's
// vault skills (each cast through sage) and the on-demand rituals.
func (s *Server) handleSpiritsCastables(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.spirits.Castables(time.Now())})
}
