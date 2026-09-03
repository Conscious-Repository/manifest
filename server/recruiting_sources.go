package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"manifest/recruiting"
)

// The source-run surface (plan §4.9, Phase 3a): run one adapter over one
// explicit scope, review the draft queue, accept or reject ONE draft at a
// time, pin a run's cache. Every route lives inside server.go's
// `if s.recruiting != nil` block beside the other recruiting routes and, like
// them, is never mounted on the portal listener.
//
// There is deliberately no accept-all route. A route that accepted a whole
// run would turn a search result into vault PII in one click; the
// per-draft shape is the point, and server/recruiting_sources_test.go pins
// that the accept route always names a draft.

// UseRecruitingRuns wires the run cache (beside UseRecruiting).
func (s *Server) UseRecruitingRuns(r *recruiting.RunStore) { s.recruitingRuns = r }

func (s *Server) recruitingRunsReady(w http.ResponseWriter) bool {
	if !s.recruitingReady(w) {
		return false
	}
	if s.recruitingRuns == nil {
		http.Error(w, "recruiting sources unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// runsPayload is what every sources route answers with: the whole run list
// (freshly swept) and, after a mutation that could have touched the board,
// the board view too — so the caller never has to guess what moved.
func (s *Server) runsPayload(withView bool) map[string]any {
	out := map[string]any{"runs": s.recruitingRuns.Runs(time.Now()), "ttlDays": int(recruiting.RunTTL.Hours() / 24)}
	if withView {
		out["view"] = s.recruiting.View()
	}
	return out
}

// GET /api/aion/recruiting/sources — the adapter rail.
func (s *Server) handleRecruitingSources(w http.ResponseWriter, _ *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	writeJSON(w, map[string]any{
		"sources":    s.recruitingRuns.Sources(),
		"defaultMax": recruiting.DefaultRunMax,
		"maxMax":     recruiting.MaxRunMax,
		"ttlDays":    int(recruiting.RunTTL.Hours() / 24),
	})
}

// GET /api/aion/recruiting/sources/runs — every run, newest first, with its
// queue. Listing sweeps expired caches (D14).
func (s *Server) handleRecruitingSourceRuns(w http.ResponseWriter, _ *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	writeJSON(w, s.runsPayload(false))
}

// POST /api/aion/recruiting/sources/run — execute one adapter over one
// scope. `dryRun` defaults to TRUE when the body omits it: the safe reading
// of an absent checkbox is the checked one.
func (s *Server) handleRecruitingSourceRun(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	var b struct {
		Source string            `json:"source"`
		Role   string            `json:"role"`
		Query  string            `json:"query"`
		Max    int               `json:"max"`
		DryRun *bool             `json:"dryRun"`
		Fields map[string]string `json:"fields"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("a run needs a source and a query"))
		return
	}
	dry := true
	if b.DryRun != nil {
		dry = *b.DryRun
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	run, err := s.recruitingRuns.Execute(ctx, recruiting.RunRequest{
		Source: strings.TrimSpace(b.Source), Role: b.Role, Query: b.Query,
		Max: b.Max, DryRun: dry, Fields: b.Fields,
	}, time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	out := s.runsPayload(false)
	out["run"] = run
	writeJSON(w, out)
}

// POST /api/aion/recruiting/sources/accept/{run}/{draft} — promote exactly
// one draft into a candidate record. The board view rides along because a
// record was written.
func (s *Server) handleRecruitingSourceAccept(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	run, c, err := s.recruitingRuns.Accept(r.PathValue("run"), r.PathValue("draft"), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	out := s.runsPayload(true)
	out["run"] = run
	out["candidate"] = c
	writeJSON(w, out)
}

// POST /api/aion/recruiting/sources/reject/{run}/{draft} — mark one draft
// rejected. Nothing reaches the vault.
func (s *Server) handleRecruitingSourceReject(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	run, err := s.recruitingRuns.Reject(r.PathValue("run"), r.PathValue("draft"), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	out := s.runsPayload(false)
	out["run"] = run
	writeJSON(w, out)
}

// POST /api/aion/recruiting/sources/pin/{run} — {pinned: bool}; an empty
// body pins.
func (s *Server) handleRecruitingSourcePin(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	b := struct {
		Pinned *bool `json:"pinned"`
	}{}
	_ = decode(r, &b)
	pinned := true
	if b.Pinned != nil {
		pinned = *b.Pinned
	}
	run, err := s.recruitingRuns.Pin(r.PathValue("run"), pinned)
	if err != nil {
		httpError(w, err)
		return
	}
	out := s.runsPayload(false)
	out["run"] = run
	writeJSON(w, out)
}
