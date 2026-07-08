package server

import (
	"errors"
	"net/http"
	"time"

	"manifest/approvals"
	"manifest/spirits"
)

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
		"engineAlive": alive,
		"spirits":     s.spirits.Spirits(),
		"feedInbox":   s.feedInboxCount(time.Now()), // same compute as /api/feed — counts never drift
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
	// spool files not yet picked up. The client derives queued/running/done from
	// these files alone — no browser-held run state (plan §1).
	writeJSON(w, map[string]any{"data": s.spirits.Runs(), "queued": s.spirits.Queued()})
}

func (s *Server) handleSpiritsRun(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	sum, body, ok := s.spirits.Run(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"summary": sum, "body": body})
}

// handleSpiritsRunPrompt serves the preserved exact prompts — the §6.5 "show
// assembled prompt" affordance.
func (s *Server) handleSpiritsRunPrompt(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	sum, _, ok := s.spirits.Run(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	turns, err := s.spirits.RunPrompts(sum.Spirit, sum.Run)
	if err != nil {
		http.Error(w, "prompts not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"data": turns})
}

// Approvals — the ONE inbox (excalibur/artifacts/approvals, plan §2.5).
// Spirits file proposals via the write_approval cast; Confirm/Reject here only
// RECORD the human decision (a folder move) — nothing sends or executes.

func (s *Server) handleSpiritsApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, map[string]any{"pending": []any{}, "counts": map[string]int{}})
		return
	}
	// Actionable proposals (apply-path set) also carry the target's CURRENT
	// content and whether the path is in the allow-list, so the UI can render a
	// current-vs-proposed diff and disable Confirm on an out-of-list payload.
	type row struct {
		approvals.Proposal
		Allowed bool   `json:"allowed"`
		Current string `json:"current"`
	}
	pending := s.approvals.List("pending")
	rows := make([]row, 0, len(pending))
	for _, p := range pending {
		rr := row{Proposal: p}
		if p.ApplyPath != "" {
			if p.Type == approvals.TypeCreateVaultNote {
				// A new vault-root note: allowed by its own path rule, no current
				// content (the diff renders as an all-added new file).
				rr.Allowed = approvals.CreateVaultNotePathAllowed(p.ApplyPath)
			} else {
				rr.Allowed = approvals.ApplyPathAllowed(p.ApplyPath)
				if cur, ok := s.approvals.CurrentContent(p); ok {
					rr.Current = cur
				}
			}
		}
		rows = append(rows, rr)
	}
	writeJSON(w, map[string]any{"pending": rows, "counts": s.approvals.Counts()})
}

func (s *Server) handleSpiritsApprovalConfirm(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	// A create-vault-note may carry an edited attendee list (the user fixed the
	// auto-linked people before confirming). editAttendees distinguishes "no edit"
	// (nil) from "cleared to none" ([]).
	var b struct {
		Attendees     []string `json:"attendees"`
		EditAttendees bool     `json:"editAttendees"`
	}
	_ = decode(r, &b) // body is optional (plain confirm)
	id := r.PathValue("id")
	var err error
	if b.EditAttendees {
		err = s.approvals.ConfirmCreateNote(id, b.Attendees)
	} else {
		err = s.approvals.Confirm(id)
	}
	if err != nil {
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
	if err := s.approvals.Reject(r.PathValue("id"), b.Reason); err != nil {
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
func (s *Server) handleSpiritsFileGet(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	content, allowed, err := s.spirits.ReadFile(r.URL.Query().Get("path"))
	if !allowed {
		http.Error(w, "path not editable", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"path": r.URL.Query().Get("path"), "content": content})
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
