package server

import (
	"net/http"
	"strings"
)

// EDIT AND CLEAR — the routes behind "delete the cheap things, archive the
// people" (owner, 2026-09-05).
//
// Until now seeds, network people and edges were append-only from the UI: a
// mistyped place or a duplicated person was permanent unless the owner opened
// the markdown by hand, and the board's honest "archive is reversible, there
// is no delete" accidentally read as a promise about the whole surface. It was
// not a policy here; it was an omission.
//
// The split these routes encode: a PLACE and an EDGE are re-addable in
// seconds, so they cut. A PERSON — candidate or connector — archives, because
// the row is the history of a decision and deleting it deletes the reasoning.
// Every response carries the fresh view, so the client never guesses what the
// list looks like after a write.

// DELETE /api/aion/recruiting/place/{id...} — cut one place.
func (s *Server) handleRecruitingPlaceDelete(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	if err := s.recruiting.RemoveSeed(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.recruiting.View())
}

// POST /api/aion/recruiting/place/{id...} {name?,class?,org?,url?,feed?} — edit
// one place. Absent keys are left alone; a key sent EMPTY clears that field,
// which is how a wrong url is removed rather than blanked.
func (s *Server) handleRecruitingPlaceUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b map[string]string
	if err := decode(r, &b); err != nil || len(b) == 0 {
		httpError(w, errBadRequest("say what to change"))
		return
	}
	if _, err := s.recruiting.UpdateSeed(r.PathValue("id"), b); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.recruiting.View())
}

// DELETE /api/aion/recruiting/network/person/{id...} — cut one connector.
// The edges naming them stay: an edge is a claim that was true when it was
// made, and removing the node does not make it false.
func (s *Server) handleRecruitingPersonDelete(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	if err := s.recruiting.RemoveNetworkPerson(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.recruiting.View())
}

// POST /api/aion/recruiting/network/person/{id...} — edit one connector.
// `archived` is a date to set aside and an empty string to restore: archiving
// is a field, not a second concept with its own route.
func (s *Server) handleRecruitingPersonUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b map[string]string
	if err := decode(r, &b); err != nil || len(b) == 0 {
		httpError(w, errBadRequest("say what to change"))
		return
	}
	if _, err := s.recruiting.UpdateNetworkPerson(r.PathValue("id"), b); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.recruiting.View())
}

// DELETE /api/aion/recruiting/network/edge {from,to,kind} — cut one claim.
// Addressed by its ENDS, the way the graph addresses it; an id would be a
// second spelling of a fact the endpoints already identify.
func (s *Server) handleRecruitingEdgeDelete(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		From string `json:"from"`
		To   string `json:"to"`
		Kind string `json:"kind"`
	}
	if err := decode(r, &b); err != nil ||
		strings.TrimSpace(b.From) == "" || strings.TrimSpace(b.To) == "" {
		httpError(w, errBadRequest("an edge is named by both its ends"))
		return
	}
	if err := s.recruiting.RemoveEdge(b.From, b.To, b.Kind); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.recruiting.View())
}

// GET /api/aion/recruiting/passed — the tombstones, so "who have I already
// declined" is answerable and correctable rather than invisible.
func (s *Server) handleRecruitingPassedList(w http.ResponseWriter, _ *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	writeJSON(w, map[string]any{"passed": s.recruiting.LoadPassed().Passed()})
}

// DELETE /api/aion/recruiting/passed/{key...} — lift one tombstone, so a
// person declined by mistake can be surfaced again by the next sweep.
func (s *Server) handleRecruitingPassedDelete(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	if err := s.recruiting.RemovePassed(r.PathValue("key")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"passed": s.recruiting.LoadPassed().Passed()})
}
