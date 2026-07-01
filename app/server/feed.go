package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"manifest/feed"
	"manifest/hermes"
)

// Feed (Step 3) — the research feed. Agents generate on Hermes; the dashboard
// materializes their structured output into the local feed store. Cards support
// keep/discard/snooze and Save-to-vault (the only vault write, user-triggered).

const scoutProfile = "domain-scout" // the profile driving Refresh

func (s *Server) handleFeedList(w http.ResponseWriter, r *http.Request) {
	if s.feed == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	f := feed.Filter{
		Status: r.URL.Query().Get("status"),
		Type:   r.URL.Query().Get("type"),
		Domain: r.URL.Query().Get("domain"),
	}
	writeJSON(w, map[string]any{"data": s.feed.List(f, time.Now())})
}

// handleFeedRefresh runs the domain-scout profile once (on-demand generation) and
// materializes any new items. Synchronous — an agent run can take ~30–90s.
func (s *Server) handleFeedRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.feedRunReady(w) {
		return
	}
	created, err := s.runScout(r.Context(), scoutProfile, "Run your scan now and return the results.")
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"new": len(created), "items": created})
}

// handleFeedRun runs an on-demand scout profile (e.g. options-scout) with a free-form
// request and materializes its structured output into the feed. This is the §5.3 path:
// "buy X, find 5 options" → options-scout returns a type:artifact card. Same run-and-
// materialize plumbing as Refresh, but the caller chooses the profile + request.
func (s *Server) handleFeedRun(w http.ResponseWriter, r *http.Request) {
	if !s.feedRunReady(w) {
		return
	}
	var b struct {
		Profile string `json:"profile"`
		Request string `json:"request"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if strings.TrimSpace(b.Profile) == "" || strings.TrimSpace(b.Request) == "" {
		http.Error(w, "profile and request are required", http.StatusBadRequest)
		return
	}
	created, err := s.runScout(r.Context(), b.Profile, b.Request)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"new": len(created), "items": created})
}

// feedRunReady guards the on-demand feed generators: feed store, a configured Hermes,
// and profiles must all be wired. Writes a 503 and reports false when they aren't.
func (s *Server) feedRunReady(w http.ResponseWriter) bool {
	if s.feed == nil || s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "feed/hermes not configured", http.StatusServiceUnavailable)
		return false
	}
	if s.profiles == nil {
		http.Error(w, "profiles not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// runScout runs one profile once (on-demand generation) and materializes its output
// into the feed store. Shared by Refresh (domain-scout, fixed prompt) and Run (any
// scout profile + a user request). The 4-minute deadline bounds a slow agent run.
func (s *Server) runScout(parent context.Context, profileName, userMsg string) ([]feed.Item, error) {
	prof, ok := s.profiles.Get(profileName)
	if !ok {
		return nil, fmt.Errorf("profile %q not found", profileName)
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	text, err := s.hermes.RunOnce(ctx, hermes.ChatRequest{
		System:   prof.Brief,
		Messages: []hermes.Message{{Role: "user", Content: userMsg}},
	})
	if err != nil {
		return nil, err
	}
	return s.feed.Materialize(text, profileName, profileName, time.Now())
}

// handleFeedBackfill scans recent cron sessions on the box and materializes any feed
// items in their output (dedup makes this safe to re-run). This is the always-on path:
// crons fire even while the Mac sleeps; on wake this pulls what they produced.
func (s *Server) handleFeedBackfill(w http.ResponseWriter, r *http.Request) {
	if s.feed == nil || s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "feed/hermes not configured", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	sessions, err := s.hermes.ListSessions(ctx, "cron")
	if err != nil {
		httpError(w, err)
		return
	}
	total := 0
	for _, sess := range sessions {
		text, err := s.hermes.LastAssistantText(ctx, sess.ID)
		if err != nil || text == "" {
			continue
		}
		created, err := s.feed.Materialize(text, sess.Title, "cron", time.Now())
		if err == nil {
			total += len(created)
		}
	}
	writeJSON(w, map[string]any{"new": total})
}

func (s *Server) handleFeedStatus(w http.ResponseWriter, r *http.Request) {
	if s.feed == nil {
		http.Error(w, "feed disabled", http.StatusServiceUnavailable)
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
		it, err = s.feed.Snooze(id, time.Now().Add(time.Duration(days)*24*time.Hour))
	} else {
		it, err = s.feed.SetStatus(id, b.Status)
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, it)
}

// handleFeedSaveToVault promotes a kept item into a real extrinsic/ note (write-once)
// and records the note path back on the item. The ONLY vault write, user-triggered.
func (s *Server) handleFeedSaveToVault(w http.ResponseWriter, r *http.Request) {
	if s.feed == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault save unavailable", http.StatusServiceUnavailable)
		return
	}
	it, ok := s.feed.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}
	rel, err := s.vault.SaveExtrinsic(it.Title, it.Type, it.Why, it.Link, it.Source, it.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	updated, err := s.feed.SetVaultNote(it.ID, rel)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"vaultNote": rel, "item": updated})
}
