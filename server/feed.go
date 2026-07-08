package server

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/approvals"
	"manifest/daily"
	"manifest/feed"
	"manifest/spirits"
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

// handleFeedList serves the unified stream: spirit items, virtual proposal
// cards (+ signals in Phase 3), plus the badge — one response.
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
		"proposals": s.feedProposals(),
		"badge":     s.feedInboxCount(now),
	})
}

// proposalCard is a VIRTUAL pinned card derived from a pending actionable
// approval (ea-digest-and-tuning Part-2 amendment, virtual-cards decision):
// a pointer, not a control — its single affordance deep-links to the APPROVALS
// diff, and Confirm/Reject there resolves it atomically because pending/ is
// the only source of truth. Nothing is written to the engine's feed dir, so
// the tune ritual's kept/discarded evidence stays byte-identical.
type proposalCard struct {
	ID         string `json:"id"` // "prop:"-prefixed — can never collide into feed.Store
	ApprovalID string `json:"approvalId"`
	Title      string `json:"title"`
	Agent      string `json:"agent"`
	Created    string `json:"created"`
	Body       string `json:"body"` // evidence summary (the ```proposed fence stripped)
	ApplyPath  string `json:"applyPath"`
}

// the proposed block is always the LAST thing in an approval body (evidence
// first, then the fenced full file). Its content can contain nested code
// fences, so strip from the ```proposed opener to end-of-text rather than
// trying to match a same-length closer (RE2 has no backreferences).
var proposedFenceRe = regexp.MustCompile("(?s)`{3,}proposed.*$")

func (s *Server) feedProposals() []proposalCard {
	out := []proposalCard{}
	if s.approvals == nil {
		return out
	}
	for _, p := range s.approvals.List("pending") {
		if p.ApplyPath == "" || !approvals.ApplyPathAllowed(p.ApplyPath) {
			continue // only tune-style actionable proposals surface as cards
		}
		out = append(out, proposalCard{
			ID:         "prop:" + p.ID,
			ApprovalID: p.ID,
			Title:      "tune: " + p.Agent,
			Agent:      p.Agent,
			Created:    p.Created,
			Body:       strings.TrimSpace(proposedFenceRe.ReplaceAllString(p.Body, "")),
			ApplyPath:  p.ApplyPath,
		})
	}
	return out
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

// ---- card actions (feed-central §3) ----

// inlineFieldRe matches [k:: v]-shaped substrings — the day-note parser eats
// those, so a promoted title must not smuggle any in.
var inlineFieldRe = regexp.MustCompile(`\[[^\]]*::[^\]]*\]`)

// promoteText builds the task line: sanitized title + the item's link.
func promoteText(title, link string) string {
	t := strings.Join(strings.Fields(inlineFieldRe.ReplaceAllString(title, "")), " ")
	if r := []rune(t); len(r) > 200 {
		t = string(r[:200]) + "…"
	}
	if link != "" {
		t += " — " + link
	}
	return strings.TrimSpace(t)
}

// handleFeedPromote is the "→ today" action: any card becomes a task in
// today's manifest block, and the spirit item auto-Keeps (it proved useful).
// Date comes from the client like /api/day/pull — never computed server-side.
func (s *Server) handleFeedPromote(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		httpError(w, errBadRequest("date is required"))
		return
	}
	it, ok := s.spirits.Feed.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	text := promoteText(it.Title, it.Link)
	if text == "" {
		httpError(w, errBadRequest("nothing to promote"))
		return
	}
	// double-promote guard: if today's tasks already carry this item, don't
	// append a duplicate line (fast double-click beats the list refresh).
	if day, err := s.svc.Load(date); err == nil {
		marker := it.Link
		if marker == "" {
			marker = text
		}
		for _, t := range day.Tasks {
			if strings.Contains(t.Text, marker) {
				writeJSON(w, map[string]any{"already": true, "item": it})
				return
			}
		}
	}
	// AddTask first, verdict second — a failure in between leaves a task plus
	// an inboxed item (harmless); the reverse order could eat the task.
	if _, err := s.svc.AddTask(date, daily.Task{Text: text}); err != nil {
		httpError(w, err)
		return
	}
	updated, err := s.spirits.Feed.SetStatus(it.ID, "kept")
	if err != nil {
		updated = it // task landed; report the pre-verdict item rather than failing
	}
	writeJSON(w, map[string]any{"item": updated})
}

// handleFeedDig spools a run-now for the originating spirit with a request
// line carrying the item — findings arrive as new feed items, closing the
// loop in the feed itself. The target is the spirit's ON-DEMAND ritual
// (cadence-less + valid, exactly the castables rule); a spirit without one
// (ea-coordinator's digests) is un-diggable.
func (s *Server) handleFeedDig(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "spirits disabled", http.StatusServiceUnavailable)
		return
	}
	it, ok := s.spirits.Feed.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	ritual := s.onDemandRitual(it.Agent)
	if ritual == "" {
		http.Error(w, it.Agent+" has no on-demand ritual to dig with", http.StatusUnprocessableEntity)
		return
	}
	request := "go deeper on: " + it.Title
	if it.Link != "" {
		request += " " + it.Link
	}
	if err := s.spirits.SpoolRunNow(it.Agent, ritual, request, ""); err != nil {
		if errors.Is(err, spirits.ErrAlreadyActive) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"active": true, "spirit": it.Agent, "ritual": ritual})
			return
		}
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"spooled": true, "spirit": it.Agent, "ritual": ritual})
}

// onDemandRitual picks the spirit's cadence-less valid ritual (first
// alphabetically when several — single user, deterministic).
func (s *Server) onDemandRitual(spirit string) string {
	var names []string
	for _, rr := range s.spirits.Rituals(time.Now()) {
		if rr.Spirit != spirit || rr.Cadence != "" || !rr.Valid {
			continue
		}
		if rr.Spirit == "sage" && rr.Ritual == "skill-cast" {
			continue // cast a skill instead (castables rule)
		}
		names = append(names, rr.Ritual)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
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
