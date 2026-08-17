package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/aion"
)

// The real-estate DOMAIN half of the RE surface (RE spec + owner decision
// 2026-08-12: one domain): the task+decision log at
// system/realestate/backlog.md, mirrored from the aion pattern. The store is
// an aion.Store pointed at the realestate root — the backlog grammar is
// domain-neutral — and ONLY backlog methods are wired here: the other corpus
// methods (people/vto/finances…) must never run against this root
// (system/realestate/people.md has its own curated surface).

// UseRe wires the real-estate decision-log store.
func (s *Server) UseRe(st *aion.Store) { s.re = st }

// handleReBacklog returns the RE backlog items + the goals `## Real Estate`
// area (the org-rock ladder the decisions lane groups against).
func (s *Server) handleReBacklog(w http.ResponseWriter, r *http.Request) {
	if s.re == nil {
		http.Error(w, "real-estate domain not available", http.StatusServiceUnavailable)
		return
	}
	doc := s.re.LoadBacklog()
	writeJSON(w, map[string]any{
		"items":     doc.Items(),
		"goalsArea": s.goalsAreaByName("Real Estate"),
		"publish":   s.rePublishInfo(),
	})
}

func (s *Server) handleReBacklogAdd(w http.ResponseWriter, r *http.Request) {
	if s.re == nil {
		http.Error(w, "real-estate domain not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Owner    string `json:"owner"`
		Rock     string `json:"rock"`
		Due      string `json:"due"`
		NeededBy string `json:"needed_by"`
		Source   string `json:"source"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	it := &aion.BacklogItem{
		Kind: b.Kind, Text: strings.TrimSpace(b.Title), Owner: b.Owner,
		Rock: b.Rock, Due: b.Due, NeededBy: b.NeededBy,
		Captured: time.Now().Format("2006-01-02"),
	}
	if b.Source != "" {
		it.Sources = []string{b.Source}
	}
	if err := s.re.AddItem(it); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": it.ID})
}

func (s *Server) handleReBacklogUpdate(w http.ResponseWriter, r *http.Request) {
	if s.re == nil {
		http.Error(w, "real-estate domain not available", http.StatusServiceUnavailable)
		return
	}
	var b map[string]string
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.re.UpdateItem(r.PathValue("id"), b, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleReBacklogLegacy answers the pre-wildcard route shape
// (/api/re/backlog/{id}/{verb}) so a browser holding the old JS keeps working.
// Slash-bearing portal ids never reached these paths — they 404'd at the mux —
// so only the two-segment slug/hex form needs answering.
func (s *Server) handleReBacklogLegacy(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.PathValue("legacy"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	r.SetPathValue("id", parts[0])
	switch parts[1] {
	case "update":
		s.handleReBacklogUpdate(w, r)
	case "delete":
		s.handleReBacklogDelete(w, r)
	case "decide":
		s.handleReBacklogDecide(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleReBacklogDelete(w http.ResponseWriter, r *http.Request) {
	if s.re == nil {
		http.Error(w, "real-estate domain not available", http.StatusServiceUnavailable)
		return
	}
	if err := s.re.DeleteItem(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleReBacklogDecide(w http.ResponseWriter, r *http.Request) {
	if s.re == nil {
		http.Error(w, "real-estate domain not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Outcome string `json:"outcome"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.re.Decide(r.PathValue("id"), b.Outcome, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
