package server

import (
	"net/http"
	"time"

	"manifest/feed"
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
	}
	if !at.IsZero() {
		resp["heartbeat"] = at.Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

func (s *Server) handleSpiritsFeedList(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	f := feed.Filter{
		Status: r.URL.Query().Get("status"),
		Type:   r.URL.Query().Get("type"),
		Domain: r.URL.Query().Get("domain"),
	}
	writeJSON(w, map[string]any{"data": s.spirits.Feed.List(f, time.Now())})
}

// handleSpiritsFeedStatus mirrors handleFeedStatus against the excalibur feed
// surface — the user's own decision written back to item frontmatter.
func (s *Server) handleSpiritsFeedStatus(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Status string `json:"status"` // kept | discarded | snoozed | new
		Days   int    `json:"days"`   // for snooze
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id := r.PathValue("id")
	var (
		it  feed.Item
		err error
	)
	if b.Status == "snoozed" {
		days := b.Days
		if days <= 0 {
			days = 7
		}
		it, err = s.spirits.Feed.Snooze(id, time.Now().Add(time.Duration(days)*24*time.Hour))
	} else {
		it, err = s.spirits.Feed.SetStatus(id, b.Status)
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, it)
}

func (s *Server) handleSpiritsRuns(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.spirits.Runs()})
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

// handleSpiritsFeedSaveToVault promotes a feed item into a real extrinsic/
// vault note (write-once) and records the note path back on the item. The ONLY
// vault write, user-triggered — ported from the retired Hermes feed handler.
func (s *Server) handleSpiritsFeedSaveToVault(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault save unavailable", http.StatusServiceUnavailable)
		return
	}
	it, ok := s.spirits.Feed.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	rel, err := s.vault.SaveExtrinsic(it.Title, it.Type, it.Why, it.Link, it.Source, it.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	updated, err := s.spirits.Feed.SetVaultNote(it.ID, rel)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, updated)
}

// Approvals — the ONE inbox (excalibur/artifacts/approvals, plan §2.5).
// Spirits file proposals via the write_approval cast; Confirm/Reject here only
// RECORD the human decision (a folder move) — nothing sends or executes.

func (s *Server) handleSpiritsApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, map[string]any{"pending": []any{}, "counts": map[string]int{}})
		return
	}
	writeJSON(w, map[string]any{
		"pending": s.approvals.List("pending"),
		"counts":  s.approvals.Counts(),
	})
}

func (s *Server) handleSpiritsApprovalConfirm(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.approvals.Confirm(r.PathValue("id")); err != nil {
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

func (s *Server) handleSpiritsRunNow(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Spirit  string `json:"spirit"`
		Ritual  string `json:"ritual"`
		Request string `json:"request"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.spirits.SpoolRunNow(b.Spirit, b.Ritual, b.Request); err != nil {
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"spooled": true})
}
