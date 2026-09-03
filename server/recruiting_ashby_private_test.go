package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"manifest/recruiting"
)

const testPrivateKey = "ashby_route_key_SECRET_77"

// privateAshby is a minimal RPC fixture for the route tests: it records
// every method and answers the handful the push path calls.
type privateAshby struct {
	mu       sync.Mutex
	calls    []string
	bodies   []map[string]any
	users    []string
	failWith string
}

func (f *privateAshby) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	method := strings.TrimPrefix(r.URL.Path, "/")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	user, _, _ := r.BasicAuth()
	f.calls = append(f.calls, method)
	f.bodies = append(f.bodies, body)
	f.users = append(f.users, user)
	w.Header().Set("Content-Type", "application/json")
	if f.failWith != "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []string{f.failWith}})
		return
	}
	ok := func(results any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": results, "moreDataAvailable": false})
	}
	cand := map[string]any{"id": "cand_r1", "name": "Dana Reyes", "socialLinks": []map[string]any{}}
	switch method {
	case "apiKey.info":
		ok(map[string]any{"title": "route-key", "permissions": []string{"candidatesWrite"}})
	case "candidate.search", "source.list", "jobPosting.list", "candidate.list", "application.list":
		ok([]map[string]any{})
	case "candidate.create", "candidate.info":
		ok(cand)
	case "candidate.addProject":
		ok(map[string]any{})
	case "candidate.createNote":
		ok(map[string]any{"id": "note_r1"})
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []string{"unknown " + method}})
	}
}

func (f *privateAshby) has(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.calls {
		if m == method {
			return true
		}
	}
	return false
}

// testAshbyServer wires the recruiting server with a sync service bound to
// the fixture (key installed unless empty) and one candidate on the board.
func testAshbyServer(t *testing.T, key string) (*Server, *privateAshby, string, string) {
	t.Helper()
	s, _, vault, dataDir := testRecruitingServer(t)
	fake := &privateAshby{}
	srv := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(srv.Close)
	as, err := recruiting.NewAshbySync(filepath.Join(dataDir, "recruiting", "ashby.json"), s.recruiting,
		recruiting.NewAshby(srv.URL, key, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	s.UseAshbySync(as)
	view := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateAdd, "/api/aion/recruiting/candidate", "",
		`{"text":"https://example.test/people/dana","name":"Dana Reyes","role":"role/mri-engineer"}`))
	return s, fake, vault, view.Candidates[0].ID
}

func ashbyGet(t *testing.T, s *Server, h http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// No key: the probe is a 200 with configured:false and an empty scope list,
// nothing reaches the network, and every write route refuses with a 409
// that does not carry a key (there is none to carry, and the response
// shape must not have a slot for one).
func TestRecruitingAshbyProbeUnconfigured(t *testing.T) {
	s, fake, _, id := testAshbyServer(t, "")
	for _, w := range []*httptest.ResponseRecorder{
		ashbyGet(t, s, s.handleRecruitingAshbyProbe, "/api/aion/recruiting/ashby/probe"),
		recruitingPost(t, s, s.handleRecruitingAshbyProbe, "/api/aion/recruiting/ashby/probe", "", ""),
	} {
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var p recruiting.AshbyProbe
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Configured || len(p.Scopes) != 0 || p.Error != "" {
			t.Fatalf("probe: %s", w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"configured":false`) || !strings.Contains(w.Body.String(), `"scopes":[]`) {
			t.Fatalf("probe body: %s", w.Body.String())
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("unconfigured probe reached the network: %v", fake.calls)
	}
	w := recruitingPost(t, s, s.handleRecruitingAshbyPush, "/api/aion/recruiting/ashby/push/"+id, id,
		`{"approve":true,"handoff":"project"}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "ASHBY_API_KEY") {
		t.Fatalf("push without a key: %d %s", w.Code, w.Body.String())
	}
	w = recruitingPost(t, s, s.handleRecruitingAshbySync, "/api/aion/recruiting/ashby/sync", "", `{"full":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("sync without a key: %d %s", w.Code, w.Body.String())
	}
	if len(fake.calls) != 0 {
		t.Fatalf("an unconfigured write reached the network: %v", fake.calls)
	}
	// the state route is readable and keyless
	w = ashbyGet(t, s, s.handleRecruitingAshbyState, "/api/aion/recruiting/ashby/state")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatalf("state: %d %s", w.Code, w.Body.String())
	}
}

func TestRecruitingAshbyProbeConfigured(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	w := ashbyGet(t, s, s.handleRecruitingAshbyProbe, "/api/aion/recruiting/ashby/probe")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var p recruiting.AshbyProbe
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if !p.Configured || strings.Join(p.Scopes, ",") != "candidatesWrite" {
		t.Fatalf("probe: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), testPrivateKey) {
		t.Fatal("the probe echoed the key")
	}
	if len(fake.users) != 1 || fake.users[0] != testPrivateKey {
		t.Fatalf("auth: %v", fake.users)
	}
	// a rejected key is reported redacted, still a 200 state
	fake.failWith = "key " + testPrivateKey + " revoked"
	w = ashbyGet(t, s, s.handleRecruitingAshbyProbe, "/api/aion/recruiting/ashby/probe")
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), testPrivateKey) || !strings.Contains(w.Body.String(), "[redacted]") {
		t.Fatalf("rejected probe: %d %s", w.Code, w.Body.String())
	}
}

// Preflight renders the proposal and writes nothing; push without approval
// is refused; the approved push runs create → addProject → note → re-fetch
// and the record carries the ids.
func TestRecruitingAshbyPreflightThenPush(t *testing.T) {
	s, fake, vault, id := testAshbyServer(t, testPrivateKey)
	w := recruitingPost(t, s, s.handleRecruitingAshbyPreflight, "/api/aion/recruiting/ashby/preflight/"+id, id,
		`{"handoff":"project","projectId":"proj_9","note":"scout summary"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("preflight: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Proposal recruiting.AshbyProposal `json:"proposal"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Proposal.Decision != recruiting.DecisionCreate || len(out.Proposal.NeedsChoice) != 0 || out.Proposal.ProjectID != "proj_9" {
		t.Fatalf("proposal: %+v", out.Proposal)
	}
	if fake.has("candidate.create") || fake.has("candidate.createNote") {
		t.Fatalf("preflight wrote: %v", fake.calls)
	}

	w = recruitingPost(t, s, s.handleRecruitingAshbyPush, "/api/aion/recruiting/ashby/push/"+id, id,
		`{"handoff":"project","projectId":"proj_9","note":"scout summary"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "approval") {
		t.Fatalf("unapproved push: %d %s", w.Code, w.Body.String())
	}
	if fake.has("candidate.create") {
		t.Fatalf("unapproved push wrote: %v", fake.calls)
	}

	w = recruitingPost(t, s, s.handleRecruitingAshbyPush, "/api/aion/recruiting/ashby/push/"+id, id,
		`{"handoff":"project","projectId":"proj_9","note":"scout summary","approve":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("push: %d %s", w.Code, w.Body.String())
	}
	var pushed struct {
		Push recruiting.AshbyPushResult `json:"push"`
		View recruiting.View            `json:"view"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pushed); err != nil {
		t.Fatal(err)
	}
	if pushed.Push.CandidateID != "cand_r1" || pushed.Push.NoteID != "note_r1" || pushed.View.Candidates[0].AshbyCandidateID != "cand_r1" {
		t.Fatalf("push: %s", w.Body.String())
	}
	if pushed.View.Candidates[0].Stage != recruiting.StageAshby {
		t.Fatalf("stage: %s", pushed.View.Candidates[0].Stage)
	}
	for _, m := range []string{"candidate.search", "candidate.create", "candidate.addProject", "candidate.createNote", "candidate.info"} {
		if !fake.has(m) {
			t.Errorf("missing call %s in %v", m, fake.calls)
		}
	}
	if strings.Contains(w.Body.String(), testPrivateKey) {
		t.Fatal("the push response echoed the key")
	}
	rec, _ := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/candidates", recruiting.CandidateSlug(id)+".md"))
	if !strings.Contains(string(rec), "ashby_candidate_id: cand_r1") || !strings.Contains(string(rec), "[method:: candidate.createNote]") {
		t.Fatalf("record:\n%s", rec)
	}
	// a push that still needs a choice is a 409 carrying the proposal
	w = recruitingPost(t, s, s.handleRecruitingAshbyPush, "/api/aion/recruiting/ashby/push/"+id, id, `{"approve":true}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"proposal"`) || !strings.Contains(w.Body.String(), "handoff") {
		t.Fatalf("undecided push: %d %s", w.Code, w.Body.String())
	}
}

// The sync-back is a route, not a ticker: it runs when hit, records the
// run, and nothing runs otherwise.
func TestRecruitingAshbySyncIsUserActioned(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	if len(fake.calls) != 0 {
		t.Fatalf("calls before any action: %v", fake.calls)
	}
	w := recruitingPost(t, s, s.handleRecruitingAshbySync, "/api/aion/recruiting/ashby/sync", "", `{"full":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Sync recruiting.AshbySyncBackResult `json:"sync"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Sync.Full || out.Sync.Synced == "" {
		t.Fatalf("sync: %+v", out.Sync)
	}
	for _, m := range []string{"jobPosting.list", "candidate.list", "application.list"} {
		if !fake.has(m) {
			t.Errorf("missing %s in %v", m, fake.calls)
		}
	}
	n := len(fake.calls)
	w = ashbyGet(t, s, s.handleRecruitingAshbyState, "/api/aion/recruiting/ashby/state")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"lastFull"`) {
		t.Fatalf("state: %d %s", w.Code, w.Body.String())
	}
	if len(fake.calls) != n {
		t.Fatalf("reading state called Ashby: %v", fake.calls[n:])
	}
}
