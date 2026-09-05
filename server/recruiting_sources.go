package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"manifest/graph"
	"manifest/ledger"
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
//
// Accept is also where the KNOWLEDGE overlay is derived (enrichment Phase
// 3): the draft's topics become person → topic expertise edges in the
// general graph, the person is registered with the links a source
// classified, and every claim that landed is ledgered under the candidate.
// The record is the truth and lands first; a graph write that fails is
// reported in the payload, never a reason to refuse the accept.
func (s *Server) handleRecruitingSourceAccept(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	now := time.Now()
	runID, draftID := r.PathValue("run"), r.PathValue("draft")
	run, c, err := s.recruitingRuns.Accept(runID, draftID, now)
	if err != nil {
		httpError(w, err)
		return
	}
	out := s.runsPayload(true)
	out["run"] = run
	out["candidate"] = c
	var draft recruiting.Draft
	for _, d := range run.Drafts {
		if d.ID == draftID {
			draft = d
		}
	}
	knowledge, kerr := s.deriveRecruitingKnowledge(draft, c, now)
	out["knowledge"] = knowledge
	if kerr != nil {
		out["knowledgeError"] = kerr.Error()
	}
	meta := map[string]any{
		"run": runID, "draft": draftID, "source": run.Source, "name": c.Name,
		"topics": len(knowledge.Claims.Edges), "edgesAdded": len(knowledge.AddedEdges),
		"entitiesAdded": len(knowledge.AddedEntities), "paths": len(c.Paths),
	}
	if len(c.Paths) > 0 {
		meta["path"] = c.Paths[0].Path
	}
	if kerr != nil {
		meta["knowledgeError"] = kerr.Error()
	}
	s.ledger(ledger.Entry{Source: "recruiting", Kind: "recruiting.draft.accepted", Actor: "owner",
		Object: ledger.Object{Kind: graph.KindPerson, ID: c.ID},
		Text:   ledger.Snip(c.Name+" accepted from "+run.Source+" ("+runID+"/"+draftID+")", 280), Meta: meta})
	writeJSON(w, out)
}

// deriveRecruitingKnowledge applies the draft's knowledge claims to the
// graph store and mirrors each added claim into the ledger (entity under
// itself, edge under the person, kind `graph.edge.derived`). Without a graph
// store it derives nothing and says so.
func (s *Server) deriveRecruitingKnowledge(d recruiting.Draft, c recruiting.Candidate, now time.Time) (recruiting.KnowledgeResult, error) {
	claims := recruiting.DeriveKnowledge(d.Draft, c.ID, s.recruiting.Rel("candidates/"+c.Slug+".md"), now)
	if s.graphStore == nil {
		return recruiting.KnowledgeResult{Claims: claims, AddedEntities: []graph.Entity{}, AddedEdges: []graph.Edge{}},
			errors.New("the graph store is not configured — no knowledge edges derived")
	}
	res, err := recruiting.ApplyKnowledge(s.graphStore, claims)
	for _, e := range res.AddedEntities {
		s.graphEntityEvent(e, "recruiting")
	}
	for _, e := range res.AddedEdges {
		s.graphEdgeEventKind(e, "recruiting", "graph.edge.derived")
	}
	return res, err
}

// draftEvent ledgers a decision about one queued draft under the draft
// itself (a queued person has no record yet): recruiting.draft.passed and
// its reversal, so a pass and its undo read as a pair in the history.
func (s *Server) draftEvent(kind, verb, runID, draftID string, run recruiting.Run) {
	name := ""
	for _, d := range run.Drafts {
		if d.ID == draftID {
			name = d.Draft.Name
		}
	}
	s.ledger(ledger.Entry{Source: "recruiting", Kind: kind, Actor: "owner",
		Object: ledger.Object{Kind: "draft", ID: runID + "/" + draftID},
		Text:   ledger.Snip(verb+" "+orStr(name, draftID)+" for "+run.Source+" run "+runID, 280),
		Meta:   map[string]any{"run": runID, "draft": draftID, "source": run.Source, "name": name}})
}

// POST /api/aion/recruiting/sources/lookup/{run}/{draft} — ask the other
// public indexes what they hold under this exact name and merge what matches
// into the draft. No board view rides along: a lookup enriches the QUEUE, and
// writes no record (recruiting/lookup.go).
func (s *Server) handleRecruitingSourceLookup(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	run, res, err := s.recruitingRuns.Lookup(ctx, r.PathValue("run"), r.PathValue("draft"), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	out := s.runsPayload(false)
	out["run"] = run
	out["lookup"] = res
	writeJSON(w, out)
}

// POST /api/aion/recruiting/sources/reject/{run}/{draft} {reason?} — mark one
// draft passed. No RECORD reaches the vault; a tombstone does (passed.md), so
// the next sweep of the same place does not re-ask a question already
// answered. The reason is optional and free text — Ashby's lesson is that the
// reason is what separates "I judged this person" from "this went stale", and
// an unlabelled pass is still a pass.
func (s *Server) handleRecruitingSourceReject(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &b) // a bare POST is a pass with no reason, not an error
	runID, draftID := r.PathValue("run"), r.PathValue("draft")
	run, err := s.recruitingRuns.Reject(runID, draftID, strings.TrimSpace(b.Reason), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	s.draftEvent("recruiting.draft.passed", "passed on", runID, draftID, run)
	out := s.runsPayload(false)
	out["run"] = run
	writeJSON(w, out)
}

// POST /api/aion/recruiting/sources/unreject/{run}/{draft} — undo a pass
// (Phase 3). The draft returns to `new` exactly as it was; the reversal is
// ledgered beside the pass. Nothing reaches the vault.
func (s *Server) handleRecruitingSourceUnreject(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	runID, draftID := r.PathValue("run"), r.PathValue("draft")
	run, err := s.recruitingRuns.Unreject(runID, draftID, time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	s.draftEvent("recruiting.draft.unpassed", "undid the pass on", runID, draftID, run)
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
