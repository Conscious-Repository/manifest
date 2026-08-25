package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"manifest/consume"
)

// The CONSUME routes — the private side of the lane. The PUBLIC side is a
// different listener entirely (consume.PublicHandler, wired in main.go) and
// shares nothing with this file but the service.
//
// Every handler nil-checks s.consume: the Use* wiring happens after New, so a
// build with no vault or no consume config answers empty rather than 500ing.

// UseConsume wires the CONSUME lane. xTokenPath is where the X bearer token
// lives (secrets tier, 0600) and publicURL is the curation feed's public
// address — display only, since this server never serves it.
func (s *Server) UseConsume(c *consume.Service, xTokenPath, publicURL string) {
	s.consume = c
	s.consumeXTokenPath = xTokenPath
	s.consumePublicURL = publicURL
}

// handleConsumeList — the lane and its CONSUME view.
// ?view=unread|all  ?list=<group>
func (s *Server) handleConsumeList(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"items": []any{}, "lists": []any{}, "unread": 0})
		return
	}
	q := r.URL.Query()
	list := q.Get("list")
	writeJSON(w, map[string]any{
		"items": s.consume.Cards(consume.Query{
			View: q.Get("view"), List: list, Sub: q.Get("sub"), Q: q.Get("q"),
		}),
		"lists": s.consume.Lists(),
		// Scoped to the active group so it agrees with the scoped
		// "mark all read" sitting next to it.
		"unread": s.consume.Unread(list),
		"total":  s.consume.Unread(""),
	})
}

// handleConsumeItem — the reader's fetch: one item with its sanitized body.
//
// The body is server-sanitized at poll time (consume/sanitize.go) and this is
// the only place it is handed to the client, which renders it through the
// single innerHTML sink in the FEED.
func (s *Server) handleConsumeItem(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	it, sub, ok := s.consume.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Opening the reader IS reading it — no separate call, no way to leave a
	// read item looking unread because a second request failed.
	s.consume.MarkRead(id)
	writeJSON(w, map[string]any{
		"id": it.ID, "title": it.Title, "url": it.URL, "author": it.Author,
		"source": it.Source, "list": sub.List, "body": it.Body,
		"published": it.PublishedAt, "chars": it.Chars, "preview": it.Preview,
		"curated": s.consumeCuratedFor(it.URL),
		"note":    s.consumeNoteFor(it.URL),
	})
}

// consumeCuratedFor / consumeNoteFor let the reader show the current curation
// state without a second round trip.
func (s *Server) consumeCuratedFor(url string) bool {
	_, ok := s.consumeEntryFor(url)
	return ok
}

func (s *Server) consumeNoteFor(url string) string {
	if e, ok := s.consumeEntryFor(url); ok {
		return e.Note
	}
	return ""
}

func (s *Server) consumeEntryFor(url string) (consume.CuratedEntry, bool) {
	if s.consume == nil || strings.TrimSpace(url) == "" {
		return consume.CuratedEntry{}, false
	}
	for _, e := range s.consume.Curated() {
		if consume.SameLink(e.URL, url) {
			return e, true
		}
	}
	return consume.CuratedEntry{}, false
}

func (s *Server) handleConsumeRead(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.MarkRead(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumeDismiss(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.Dismiss(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeUndismiss is the undo behind the dismiss toast.
func (s *Server) handleConsumeUndismiss(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.Undismiss(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeUnread bumps an archived item back into the queue.
func (s *Server) handleConsumeUnread(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil || !s.consume.MarkUnread(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeReadAll clears the unread backlog, optionally for one group.
func (s *Server) handleConsumeReadAll(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "marked": s.consume.MarkAllRead(r.URL.Query().Get("list"))})
}

// handleConsumePollAll refreshes every subscription now, ignoring intervals.
func (s *Server) handleConsumePollAll(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "polled": s.consume.PollAll(r.Context())})
}

// handleConsumeCurate — THE button. Writes an extrinsic/ note under the
// consume-curate capability; the public feed reads those notes and nothing
// else.
func (s *Server) handleConsumeCurate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // a note is optional
	entry, err := s.consume.Curate(r.PathValue("id"), body.Note)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "path": entry.Path, "note": entry.Note})
}

func (s *Server) handleConsumeUncurate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.Uncurate(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConsumeCurated — the private mirror of exactly what the public feed
// serves, so the owner can audit it without leaving the app.
func (s *Server) handleConsumeCurated(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"entries": []any{}})
		return
	}
	writeJSON(w, map[string]any{"entries": s.consume.Curated(), "public": s.consumePublicURL})
}

// ---- subscriptions ----

func (s *Server) handleConsumeSubs(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"subscriptions": []any{}})
		return
	}
	writeJSON(w, map[string]any{"subscriptions": s.consume.Statuses(), "xReady": s.consumeXReady()})
}

func (s *Server) handleConsumeSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Input  string `json:"input"`
		Title  string `json:"title"`
		List   string `json:"list"`
		Mirror string `json:"mirror"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sub, err := s.consume.Subscribe(r.Context(), body.Input, body.Title, body.List, body.Mirror)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "subscription": sub, "archived": s.consume.Seeded(sub.ID)})
}

func (s *Server) handleConsumeSubUpdate(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Title    string `json:"title"`
		List     string `json:"list"`
		Mirror   string `json:"mirror"`
		MinChars int    `json:"minChars"`
		Fulltext string `json:"fulltext"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	err := s.consume.UpdateSub(consume.Subscription{
		ID: r.PathValue("id"), Title: body.Title, List: body.List,
		Mirror: body.Mirror, MinChars: body.MinChars, Fulltext: body.Fulltext,
	})
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumeUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.Unsubscribe(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleConsumePoll(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.consume.PollNow(r.Context(), r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
