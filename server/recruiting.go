package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/recruiting"
)

// The PRIVATE AION recruiting surface (plan §4.2). Every route lives inside
// server.go's `if s.aion != nil { … }` block — the natural gate for "the AION
// cockpit is configured" — and none of them is ever mounted on the portal
// listener, which builds its own mux in portal.go and gains nothing here.
//
// The service holds a *recruiting.Store and nothing else: no *AionLive, no
// aion.Store, no pack renderer. Serving a candidate record to the team portal
// is not something this file declines to do — it is something it cannot
// express (server/aion_recruiting_leak_test.go pins that).

// UseRecruiting wires the private scout records (beside UseFundraising).
func (s *Server) UseRecruiting(r *recruiting.Store) { s.recruiting = r }

// recruitingReady reports the service, answering 503 when it is absent so a
// cockpit built without a vault degrades instead of panicking.
func (s *Server) recruitingReady(w http.ResponseWriter) bool {
	if s.recruiting == nil {
		http.Error(w, "recruiting unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) handleRecruitingView(w http.ResponseWriter, _ *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingSeeds(w http.ResponseWriter, _ *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	v := s.recruiting.View()
	writeJSON(w, map[string]any{"seeds": v.Seeds, "seedClasses": v.SeedClasses})
}

func (s *Server) handleRecruitingSeedAdd(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Class string `json:"class"`
		Name  string `json:"name"`
		Org   string `json:"org"`
		URL   string `json:"url"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("a seed needs a class and a name"))
		return
	}
	if _, err := s.recruiting.AddSeed(recruiting.Seed{
		Class: strings.ToLower(strings.TrimSpace(b.Class)), Name: b.Name, Org: b.Org, URL: b.URL,
	}, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingRole(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	slug := strings.TrimSpace(r.PathValue("slug"))
	for _, role := range s.recruiting.View().Roles {
		if role.Slug == slug {
			writeJSON(w, role)
			return
		}
	}
	http.Error(w, "role not found", http.StatusNotFound)
}

func (s *Server) handleRecruitingRoleCriteria(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Criteria []recruiting.Criterion `json:"criteria"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.recruiting.SetRoleCriteria(r.PathValue("slug"), b.Criteria); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingCandidateAdd(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b recruiting.QuickAdd
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("a candidate needs a url, a name, or a note"))
		return
	}
	if _, err := s.recruiting.AddCandidate(b, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingCandidateUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b map[string]string
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.recruiting.UpdateCandidate(r.PathValue("id"), b); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingCandidateStage(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Stage string `json:"stage"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.recruiting.SetStage(r.PathValue("id"), strings.TrimSpace(b.Stage)); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

// Archive is the D7 disposition: the record is retained and leaves the active
// board. It is not deletion, and there is deliberately no delete route.
//
// The triage verdict may also carry rejectInAshby + archiveReasonId: the
// applicant's Ashby application is rejected FIRST (the write with a real
// consequence — if it fails nothing archives, so the two never disagree),
// then the record archives locally. Restore does NOT un-reject in Ashby —
// the response says so and the client repeats it.
func (s *Server) handleRecruitingCandidateArchive(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Archived        bool   `json:"archived"`
		RejectInAshby   bool   `json:"rejectInAshby"`
		ArchiveReasonID string `json:"archiveReasonId"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id := r.PathValue("id")
	if b.Archived && b.RejectInAshby {
		if s.ashbySync == nil || !s.ashbySync.Configured() {
			httpError(w, errBadRequest("no Ashby key installed — cannot write the rejection"))
			return
		}
		if strings.TrimSpace(b.ArchiveReasonID) == "" {
			httpError(w, errBadRequest("a rejection needs an archive reason"))
			return
		}
		ctx, cancel := ashbyCtx(r)
		defer cancel()
		if _, err := s.ashbySync.ChangeStage(ctx, id, "", strings.TrimSpace(b.ArchiveReasonID), "owner triage", time.Now()); err != nil {
			ashbyError(w, err)
			return
		}
	}
	if _, err := s.recruiting.Archive(id, b.Archived, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

// handleRecruitingNetworkPersonAdd — "＋ someone I know": one curated network
// node in network/people.md. Email is hand-typed only (D15 lives upstream:
// adapters can never reach this route's fields).
func (s *Server) handleRecruitingNetworkPersonAdd(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Org   string `json:"org"`
		Title string `json:"title"`
		Type  string `json:"type"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Name) == "" {
		httpError(w, errBadRequest("a network person needs a name"))
		return
	}
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{
		Name: strings.TrimSpace(b.Name), Email: strings.TrimSpace(b.Email),
		Org: strings.TrimSpace(b.Org), Title: strings.TrimSpace(b.Title),
		Type: strings.TrimSpace(b.Type), Source: "owner",
		Added: time.Now().UTC().Format("2006-01-02"),
	}); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingCandidateEvidence(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		URL       string `json:"url"`
		File      string `json:"file"`
		Kind      string `json:"kind"`
		Snippet   string `json:"snippet"`
		Collected string `json:"collected"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.recruiting.AddEvidence(r.PathValue("id"), recruiting.Evidence{
		URL: strings.TrimSpace(b.URL), File: strings.TrimSpace(b.File),
		Kind: strings.TrimSpace(b.Kind), Snippet: b.Snippet, Collected: strings.TrimSpace(b.Collected),
	}, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

func (s *Server) handleRecruitingCandidateFit(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Criterion string   `json:"criterion"`
		Score     string   `json:"score"`
		Evidence  []string `json:"evidence"`
		Present   bool     `json:"present"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.recruiting.ScoreFit(r.PathValue("id"), b.Criterion,
		strings.TrimSpace(b.Score), b.Evidence, b.Present); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}

// The recorded gate override (D6). An empty `by` clears it; a set one
// requires a reason, because the point of the override is that the judgment
// is auditable rather than invisible.
func (s *Server) handleRecruitingCandidateOverride(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		By     string `json:"by"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.recruiting.SetOverride(r.PathValue("id"), b.By, b.Reason, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.recruiting.View())
}
