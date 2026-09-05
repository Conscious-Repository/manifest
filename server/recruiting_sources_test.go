package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// The source-run surface (plan §4.9, Phase 3a) through the real mux: the
// adapter rail lists what is registered, a run defaults to dry, accept
// promotes exactly one draft and hands the board view back, reject and pin
// touch the cache only, and there is no route that could accept a whole run.
// Everything runs against the same narrow-capability vault the board tests
// use, so an accept is a real audited record write and a reject is provably
// not one.

// testRecruitingSourcesServer is testRecruitingServer plus a run cache in a
// SEPARATE temp dir (the way main wires dataDir beside the vault) with only
// the manual adapter registered — the one adapter Phase 3a ships.
func testRecruitingSourcesServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	s, _, _, _ := testRecruitingServer(t)
	rs, err := recruiting.NewRunStore(filepath.Join(t.TempDir(), "recruiting", "runs"), s.recruiting)
	if err != nil {
		t.Fatal(err)
	}
	rs.Register(sources.Manual{Owner: "benjamin"})
	s.UseRecruitingRuns(rs)
	return s, s.Handler()
}

// sourcesPayload is the shape every sources route answers with; `View` is
// only present after a route that wrote a record.
type sourcesPayload struct {
	Run       *recruiting.Run       `json:"run"`
	Runs      []recruiting.Run      `json:"runs"`
	View      *recruiting.View      `json:"view"`
	Candidate *recruiting.Candidate `json:"candidate"`
	TTLDays   int                   `json:"ttlDays"`
	Raw       map[string]json.RawMessage
}

func sourcesDo(t *testing.T, mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func decodeSources(t *testing.T, w *httptest.ResponseRecorder) sourcesPayload {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var p sourcesPayload
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p.Raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	return p
}

// boardCandidates reads the board straight from the view route so a test can
// say what the vault holds without trusting the payload it is checking.
func boardCandidates(t *testing.T, mux http.Handler) []recruiting.Candidate {
	t.Helper()
	w := sourcesDo(t, mux, http.MethodGet, "/api/aion/recruiting", "")
	return decodeView(t, w).Candidates
}

// startRun executes one manual run through the route and returns it.
func startRun(t *testing.T, mux http.Handler, body string) recruiting.Run {
	t.Helper()
	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/run", body))
	if p.Run == nil {
		t.Fatalf("run route answered without a run: %s", string(p.Raw["run"]))
	}
	return *p.Run
}

const sourcesRunBody = `{"source":"manual","role":"role/mri-engineer","query":"https://example.test/people/dana-reyes"}`

func TestRecruitingSourcesListsTheManualAdapter(t *testing.T) {
	_, mux := testRecruitingSourcesServer(t)
	w := sourcesDo(t, mux, http.MethodGet, "/api/aion/recruiting/sources", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Sources    []recruiting.SourceInfo `json:"sources"`
		DefaultMax int                     `json:"defaultMax"`
		MaxMax     int                     `json:"maxMax"`
		TTLDays    int                     `json:"ttlDays"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sources) != 1 || body.Sources[0].ID != "manual" || body.Sources[0].Kind != sources.KindManual {
		t.Fatalf("the rail should list exactly the manual adapter: %+v", body.Sources)
	}
	if len(body.Sources[0].Fields) == 0 {
		t.Errorf("the manual adapter declared no scope fields")
	}
	if body.DefaultMax != recruiting.DefaultRunMax || body.MaxMax != recruiting.MaxRunMax || body.TTLDays != 30 {
		t.Errorf("caps: %+v", body)
	}
}

// A body that omits dryRun is a dry run: the safe reading of an absent
// checkbox is the checked one. Either way the run route answers with the
// run AND the whole run list, and never with the board view — a run does not
// write a record.
func TestRecruitingSourceRunDefaultsToDryRun(t *testing.T) {
	_, mux := testRecruitingSourcesServer(t)

	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/run", sourcesRunBody))
	if p.Run == nil || !p.Run.Scope.DryRun {
		t.Fatalf("a run with no dryRun field was not dry: %+v", p.Run)
	}
	if p.Run.Counts.Fetched != 1 || p.Run.Counts.New != 1 || len(p.Run.Drafts) != 1 {
		t.Fatalf("manual run counts: %+v", p.Run.Counts)
	}
	// a preview holds a real queue now, so its expiry clock waits for the
	// triage like any other run's (owner decision 2026-09-04)
	if !p.Run.TriagedAt.IsZero() || !p.Run.ExpiresAt.IsZero() {
		t.Errorf("a run with a pending draft started its expiry clock: %+v", p.Run.RunState)
	}
	if len(p.Runs) != 1 || p.Runs[0].ID != p.Run.ID {
		t.Fatalf("runs payload should carry the run just made: %+v", p.Runs)
	}
	if p.TTLDays != 30 {
		t.Errorf("ttlDays=%d", p.TTLDays)
	}
	if _, has := p.Raw["view"]; has {
		t.Errorf("a run answered with the board view, but a run writes no record")
	}

	// explicit false is honoured, and an explicit true still lands dry
	wet := startRun(t, mux, `{"source":"manual","role":"role/mri-engineer","query":"Dana Reyes","dryRun":false}`)
	if wet.Scope.DryRun {
		t.Errorf("dryRun:false was read as dry")
	}
	dry := startRun(t, mux, `{"source":"manual","query":"Dana Reyes","dryRun":true}`)
	if !dry.Scope.DryRun {
		t.Errorf("dryRun:true was read as wet")
	}
	// the max cap is applied on the way in
	capped := startRun(t, mux, `{"source":"manual","query":"Dana Reyes","max":999}`)
	if capped.Scope.Max != recruiting.MaxRunMax {
		t.Errorf("max=999 was not capped: %d", capped.Scope.Max)
	}

	// three runs, newest first, and still nothing on the board
	listed := decodeSources(t, sourcesDo(t, mux, http.MethodGet, "/api/aion/recruiting/sources/runs", ""))
	if len(listed.Runs) != 4 {
		t.Fatalf("runs listed: %d", len(listed.Runs))
	}
	if _, has := listed.Raw["view"]; has {
		t.Errorf("the runs listing carried a board view")
	}
	if n := len(boardCandidates(t, mux)); n != 0 {
		t.Errorf("running sources put %d candidate(s) on the board", n)
	}

	// and the 400s: no source, an unknown source, no query, a bad body, an
	// unknown role
	for name, body := range map[string]string{
		"no source":      `{"query":"x"}`,
		"unknown source": `{"source":"openalex","query":"x"}`,
		"no query":       `{"source":"manual"}`,
		"unknown role":   `{"source":"manual","query":"x","role":"role/nope"}`,
		"not json":       `{`,
	} {
		w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/run", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d: %s", name, w.Code, w.Body.String())
		}
	}
}

// Accept promotes exactly ONE draft into exactly ONE record and hands back
// the run, the run list, the new candidate and the board view — the caller
// never has to guess what moved. A second accept of the same draft, an
// accept on a dry run and an accept of a draft that is not there are all
// refused without touching the board.
func TestRecruitingSourceAcceptPromotesOneDraft(t *testing.T) {
	_, mux := testRecruitingSourcesServer(t)
	run := startRun(t, mux, `{"source":"manual","role":"role/mri-engineer","query":"https://example.test/people/dana-reyes","dryRun":false}`)
	if len(run.Drafts) != 1 || run.Drafts[0].Status != recruiting.DraftNew {
		t.Fatalf("queue: %+v", run.Drafts)
	}
	draft := run.Drafts[0].ID

	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost,
		"/api/aion/recruiting/sources/accept/"+run.ID+"/"+draft, ""))
	if p.View == nil || p.Candidate == nil {
		t.Fatalf("accept must answer with view and candidate: %s", strings.Join(keys(p.Raw), ","))
	}
	if len(p.View.Candidates) != 1 || p.View.Candidates[0].ID != p.Candidate.ID {
		t.Fatalf("view after accept: %+v", p.View.Candidates)
	}
	if p.Candidate.Role != "role/mri-engineer" || p.Candidate.Stage != "new" {
		t.Errorf("accepted candidate: %+v", p.Candidate)
	}
	if len(p.Candidate.Evidence) == 0 {
		t.Errorf("the accepted record lost its citation: %+v", p.Candidate)
	}
	if p.Run == nil || p.Run.Counts.Accepted != 1 || p.Run.Drafts[0].Status != recruiting.DraftAccepted ||
		p.Run.Drafts[0].CandidateID != p.Candidate.ID {
		t.Fatalf("run after accept: %+v", p.Run)
	}
	if p.Run.TriagedAt.IsZero() {
		t.Errorf("a fully-decided run should be triaged")
	}
	if len(p.Runs) != 1 || p.Runs[0].Counts.Accepted != 1 {
		t.Errorf("runs payload did not carry the accept: %+v", p.Runs)
	}
	if n := len(boardCandidates(t, mux)); n != 1 {
		t.Fatalf("board holds %d candidate(s) after one accept", n)
	}

	// the same draft cannot leave `new` twice
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/"+draft, ""); w.Code != http.StatusBadRequest {
		t.Errorf("a second accept answered %d: %s", w.Code, w.Body.String())
	}
	// a draft that is not there
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d99", ""); w.Code != http.StatusBadRequest {
		t.Errorf("accept of a missing draft answered %d", w.Code)
	}
	// an unknown run
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/nope/d1", ""); w.Code != http.StatusBadRequest {
		t.Errorf("accept on an unknown run answered %d", w.Code)
	}
	// ⚠ a preview run ACCEPTS (owner decision 2026-09-04). The checkbox never
	// protected anything — a run writes no record either way — so it no longer
	// forces an identical second call to someone else's API before a person
	// already on screen can be kept.
	dry := startRun(t, mux, `{"source":"manual","role":"role/mri-engineer","query":"Sam Okafor"}`)
	w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+dry.ID+"/d1", "")
	if w.Code != http.StatusOK {
		t.Errorf("accept from a preview run answered %d: %s", w.Code, w.Body.String())
	}
	if n := len(boardCandidates(t, mux)); n != 2 {
		t.Errorf("the accept did not reach the board: %d candidate(s)", n)
	}

	// a second run of the same person is a duplicate, and a duplicate cannot
	// be accepted onto the board a second time
	again := startRun(t, mux, `{"source":"manual","role":"role/mri-engineer","query":"https://example.test/people/dana-reyes","dryRun":false}`)
	if again.Counts.Duplicate != 1 || again.Drafts[0].Status != recruiting.DraftDuplicate ||
		again.Drafts[0].CandidateID != p.Candidate.ID {
		t.Fatalf("re-run did not dedupe against the board: %+v", again.Drafts)
	}
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+again.ID+"/d1", ""); w.Code != http.StatusBadRequest {
		t.Errorf("accepting a duplicate answered %d", w.Code)
	}
	if n := len(boardCandidates(t, mux)); n != 2 {
		t.Errorf("a duplicate reached the board: %d candidate(s)", n)
	}
}

// Reject marks the draft and nothing else: no record, no board view in the
// answer (the current contract — a view rides along only when a record was
// written), and the run is triaged once nothing is left to decide.
func TestRecruitingSourceRejectWritesNothing(t *testing.T) {
	_, mux := testRecruitingSourcesServer(t)
	run := startRun(t, mux, `{"source":"manual","role":"role/mri-engineer","query":"Dana Reyes","dryRun":false}`)
	if !run.TriagedAt.IsZero() {
		t.Fatalf("a wet run with a new draft was triaged early: %+v", run.RunState)
	}

	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost,
		"/api/aion/recruiting/sources/reject/"+run.ID+"/d1", ""))
	if _, has := p.Raw["view"]; has {
		t.Errorf("reject answered with a board view, but it writes no record")
	}
	if _, has := p.Raw["candidate"]; has {
		t.Errorf("reject answered with a candidate")
	}
	if p.Run == nil || p.Run.Counts.Rejected != 1 || p.Run.Drafts[0].Status != recruiting.DraftRejected {
		t.Fatalf("run after reject: %+v", p.Run)
	}
	if p.Run.TriagedAt.IsZero() || p.Run.ExpiresAt.IsZero() {
		t.Errorf("a fully-rejected run should be triaged with an expiry: %+v", p.Run.RunState)
	}
	if len(p.Runs) != 1 || p.Runs[0].Counts.Rejected != 1 {
		t.Errorf("runs payload did not carry the reject: %+v", p.Runs)
	}
	if n := len(boardCandidates(t, mux)); n != 0 {
		t.Errorf("reject put %d candidate(s) on the board", n)
	}
	// a rejected draft cannot be accepted afterwards, and cannot be rejected twice
	for _, path := range []string{
		"/api/aion/recruiting/sources/accept/" + run.ID + "/d1",
		"/api/aion/recruiting/sources/reject/" + run.ID + "/d1",
	} {
		if w := sourcesDo(t, mux, http.MethodPost, path, ""); w.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d after a reject", path, w.Code)
		}
	}
}

func TestRecruitingSourcePinToggles(t *testing.T) {
	_, mux := testRecruitingSourcesServer(t)
	run := startRun(t, mux, sourcesRunBody)
	if run.Pinned {
		t.Fatalf("a fresh run is pinned")
	}
	// an empty body pins
	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/pin/"+run.ID, ""))
	if p.Run == nil || !p.Run.Pinned || len(p.Runs) != 1 || !p.Runs[0].Pinned {
		t.Fatalf("pin: run=%+v runs=%+v", p.Run, p.Runs)
	}
	if _, has := p.Raw["view"]; has {
		t.Errorf("pin answered with a board view")
	}
	// {pinned:false} unpins
	p = decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/pin/"+run.ID, `{"pinned":false}`))
	if p.Run.Pinned || p.Runs[0].Pinned {
		t.Fatalf("unpin: %+v", p.Run)
	}
	// and it is remembered by the listing
	listed := decodeSources(t, sourcesDo(t, mux, http.MethodGet, "/api/aion/recruiting/sources/runs", ""))
	if listed.Runs[0].Pinned {
		t.Errorf("the listing forgot the unpin")
	}
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/pin/nope", ""); w.Code != http.StatusBadRequest {
		t.Errorf("pin of an unknown run answered %d", w.Code)
	}
}

// There is no accept-all. A route that accepted a whole run would turn a
// search result into vault PII in one click, so the only accept route names
// ONE draft; every shape that omits the draft, or asks for all of them, is
// not a route at all.
func TestRecruitingSourcesHaveNoBulkAccept(t *testing.T) {
	_, mux := testRecruitingSourcesServer(t)
	run := startRun(t, mux, `{"source":"manual","role":"role/mri-engineer","query":"Dana Reyes","dryRun":false}`)

	for _, path := range []string{
		"/api/aion/recruiting/sources/accept/" + run.ID,
		"/api/aion/recruiting/sources/accept/" + run.ID + "/",
		"/api/aion/recruiting/sources/accept/" + run.ID + "/all",
		"/api/aion/recruiting/sources/accept/" + run.ID + "/*",
		"/api/aion/recruiting/sources/accept-all/" + run.ID,
		"/api/aion/recruiting/sources/accept-all",
		"/api/aion/recruiting/sources/accept",
		"/api/aion/recruiting/sources/run/accept/" + run.ID,
		"/api/aion/recruiting/sources/runs/" + run.ID + "/accept",
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodGet} {
			w := sourcesDo(t, mux, method, path, "")
			if w.Code == http.StatusOK {
				t.Errorf("%s %s answered 200 — a bulk accept route exists", method, path)
				continue
			}
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusBadRequest {
				t.Errorf("%s %s answered %d, want 404/405/400", method, path, w.Code)
			}
		}
	}
	if n := len(boardCandidates(t, mux)); n != 0 {
		t.Fatalf("a bulk-shaped request put %d candidate(s) on the board", n)
	}
	listed := decodeSources(t, sourcesDo(t, mux, http.MethodGet, "/api/aion/recruiting/sources/runs", ""))
	if listed.Runs[0].Drafts[0].Status != recruiting.DraftNew {
		t.Fatalf("a bulk-shaped request moved the draft: %+v", listed.Runs[0].Drafts)
	}
	// the one accept route that exists is still the one that names a draft
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d1", ""); w.Code != http.StatusOK {
		t.Errorf("the per-draft accept answered %d: %s", w.Code, w.Body.String())
	}
}

// Without a run cache the sources surface degrades the way the board does
// without a store: the handlers answer 503, and the mux does not carry the
// routes at all — the board can be configured while sources are not.
func TestRecruitingSourcesUnavailableWithoutRuns(t *testing.T) {
	handlers := func(s *Server) []http.HandlerFunc {
		return []http.HandlerFunc{
			s.handleRecruitingSources, s.handleRecruitingSourceRuns, s.handleRecruitingSourceRun,
			s.handleRecruitingSourceAccept, s.handleRecruitingSourceReject, s.handleRecruitingSourcePin,
		}
	}
	// a board with no run cache
	withBoard, _, _, _ := testRecruitingServer(t)
	// and no board at all
	bare := &Server{}
	for name, s := range map[string]*Server{"board without runs": withBoard, "no board": bare} {
		for i, h := range handlers(s) {
			w := httptest.NewRecorder()
			h(w, httptest.NewRequest(http.MethodPost, "/api/aion/recruiting/sources", strings.NewReader("{}")))
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: handler %d answered %d, want 503", name, i, w.Code)
			}
		}
	}
	mux := withBoard.Handler()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/aion/recruiting/sources"},
		{http.MethodGet, "/api/aion/recruiting/sources/runs"},
		{http.MethodPost, "/api/aion/recruiting/sources/run"},
		{http.MethodPost, "/api/aion/recruiting/sources/accept/r/d1"},
		{http.MethodPost, "/api/aion/recruiting/sources/reject/r/d1"},
		{http.MethodPost, "/api/aion/recruiting/sources/pin/r"},
	} {
		w := sourcesDo(t, mux, tc.method, tc.path, "")
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s answered %d with no run cache, want 404", tc.method, tc.path, w.Code)
		}
	}
	// while the board itself is still there
	if w := sourcesDo(t, mux, http.MethodGet, "/api/aion/recruiting", ""); w.Code != http.StatusOK {
		t.Errorf("the board answered %d beside an unwired sources surface", w.Code)
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
