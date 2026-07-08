package server

import (
	"net/http"
	"time"

	"manifest/feed"
)

// FEED CENTRAL — manifest's one inbox, promoted from a SPIRITS sub-tab to a
// first-class surface (plans/feed-central.md §1). Spirit items keep living in
// the excalibur artifacts/feed tree — the engine contract (file format, ids,
// statuses) is untouched; only the dashboard's address for them changed.
// App signals (§2) and virtual proposal cards (§4 pinned lane) join the same
// response in later phases.
//
// NOT gated on spirits: signals derive from contacts/goals and must flow even
// with excaliburPath unset — only the spirit-item slice needs s.spirits.

// feedInboxCount is THE badge compute. The list handler, the badge handler,
// and /api/spirits/status.feedInbox all call this one function so the counts
// can never drift (feed-central §1: count = new items + lapsed snoozes; app
// signals join the sum in Phase 3).
func (s *Server) feedInboxCount(now time.Time) int {
	n := 0
	if s.spirits != nil {
		n += len(s.spirits.Feed.List(feed.Filter{Status: "inbox"}, now))
	}
	return n
}

// handleFeedList serves the unified stream: spirit items (+ signals and
// virtual proposal cards in later phases) plus the badge, in one response.
func (s *Server) handleFeedList(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	items := []feed.Item{}
	if s.spirits != nil {
		f := feed.Filter{
			Status: r.URL.Query().Get("status"),
			Type:   r.URL.Query().Get("type"),
			Domain: r.URL.Query().Get("domain"),
		}
		items = s.spirits.Feed.List(f, now)
	}
	writeJSON(w, map[string]any{
		"items":     items,
		"signals":   []any{}, // Phase 3
		"proposals": []any{}, // Phase 2 (virtual, from pending approvals)
		"badge":     s.feedInboxCount(now),
	})
}

// handleFeedBadge is the thin nav-pill count (same compute as the list).
func (s *Server) handleFeedBadge(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"count": s.feedInboxCount(time.Now())})
}

// handleFeedStatus records a verdict (keep/discard/snooze/restore) — the
// user's own decision written back to item frontmatter.
func (s *Server) handleFeedStatus(w http.ResponseWriter, r *http.Request) {
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

// handleFeedSaveToVault promotes a feed item into a real extrinsic/ vault note
// (write-once) and records the note path back on the item. User-triggered.
func (s *Server) handleFeedSaveToVault(w http.ResponseWriter, r *http.Request) {
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
