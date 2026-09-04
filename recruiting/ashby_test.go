package recruiting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- the fixture Ashby: an httptest server that speaks the RPC shape ----

const testAshbyKey = "ashby_test_key_SECRET_9f3c"

type fakeCall struct {
	HTTPMethod string
	Method     string
	User       string
	Pass       string
	HasAuth    bool
	Version    string
	Body       map[string]any
}

type fakeAshby struct {
	t          *testing.T
	srv        *httptest.Server
	mu         sync.Mutex
	calls      []fakeCall
	candidates map[string]map[string]any
	apps       map[string]map[string]any
	searchHits []map[string]any
	sources    []map[string]any
	postings   []map[string]any
	failWith   string // every call answers 401 with this text
	denyAdd    bool   // candidate.addProject answers 404
	nextID     int
	// onCall runs inside the handler before it answers: a test hooks it
	// to do something (edit the record) while a push is mid-flight.
	onCall func(method string)
}

func newFakeAshby(t *testing.T) *fakeAshby {
	t.Helper()
	f := &fakeAshby{t: t, candidates: map[string]map[string]any{}, apps: map[string]map[string]any{},
		sources: []map[string]any{{"id": "src_scout", "title": "Manifest Scout"}, {"id": "src_other", "title": "Referral"}}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAshby) client() *Ashby { return NewAshby(f.srv.URL, testAshbyKey, f.srv.Client()) }

func (f *fakeAshby) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s_%d", prefix, f.nextID)
}

func wireCandidate(id, name, email, linkedin string) map[string]any {
	c := map[string]any{"id": id, "name": name, "updatedAt": "2026-09-01T00:00:00Z", "socialLinks": []map[string]any{}}
	if email != "" {
		c["primaryEmailAddress"] = map[string]any{"value": email}
	}
	if linkedin != "" {
		c["socialLinks"] = []map[string]any{{"type": "LinkedIn", "url": linkedin}}
	}
	return c
}

func (f *fakeAshby) ok(w http.ResponseWriter, results any, extra map[string]any) {
	out := map[string]any{"success": true, "results": results}
	for k, v := range extra {
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// invalidInput is Ashby's parameter-validation refusal, verbatim in shape:
// HTTP 200, success:false, a bare code in `errors`, and the sentence that
// actually names the offending field in errorInfo.message.
func (f *fakeAshby) invalidInput(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"errors":  []string{"invalid_input"},
		"errorInfo": map[string]any{
			"code": "invalid_input", "message": msg, "requestId": "test-request",
		},
	})
}

func (f *fakeAshby) fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []string{msg}})
}

func (f *fakeAshby) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := fakeCall{HTTPMethod: r.Method, Method: strings.TrimPrefix(r.URL.Path, "/"), Version: r.Header.Get(AshbyAPIVersionHeader)}
	call.User, call.Pass, call.HasAuth = r.BasicAuth()
	_ = json.NewDecoder(r.Body).Decode(&call.Body)
	f.calls = append(f.calls, call)
	if f.onCall != nil {
		f.onCall(call.Method)
	}
	if r.Method != http.MethodPost {
		f.fail(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if f.failWith != "" {
		f.fail(w, http.StatusUnauthorized, f.failWith)
		return
	}
	if call.User != testAshbyKey || call.Pass != "" {
		f.fail(w, http.StatusUnauthorized, "bad credentials")
		return
	}
	str := func(k string) string { s, _ := call.Body[k].(string); return s }
	switch call.Method {
	case "apiKey.info":
		f.ok(w, map[string]any{"title": "manifest-scout", "permissions": []string{"candidatesWrite", "jobsRead"}}, nil)
	case "candidate.search":
		hits := f.searchHits
		if hits == nil {
			hits = []map[string]any{}
		}
		f.ok(w, hits, nil)
	case "candidate.info":
		c, ok := f.candidates[str("id")]
		if !ok {
			f.fail(w, http.StatusNotFound, "no such candidate")
			return
		}
		f.ok(w, c, nil)
	case "candidate.create":
		id := f.id("cand")
		li := ""
		if v := str("linkedInUrl"); v != "" {
			li = v
		}
		c := wireCandidate(id, str("name"), str("email"), li)
		f.candidates[id] = c
		f.ok(w, c, nil)
	case "candidate.createNote":
		if _, ok := f.candidates[str("candidateId")]; !ok {
			f.fail(w, http.StatusNotFound, "no such candidate")
			return
		}
		f.ok(w, map[string]any{"id": f.id("note")}, nil)
	case "candidate.addProject":
		if f.denyAdd {
			f.fail(w, http.StatusNotFound, "endpoint not found")
			return
		}
		f.ok(w, map[string]any{}, nil)
	case "application.create":
		id := f.id("app")
		app := map[string]any{"id": id, "status": "Active", "candidate": map[string]any{"id": str("candidateId")},
			"job": map[string]any{"id": str("jobId")},
			"currentInterviewStage": map[string]any{"id": "st_1", "title": "Application Review", "interviewPlanId": "plan_1"}}
		f.apps[id] = app
		f.ok(w, app, nil)
	case "interviewStage.list":
		if str("interviewPlanId") != "plan_1" {
			f.fail(w, http.StatusNotFound, "no such interview plan")
			return
		}
		f.ok(w, []map[string]any{
			{"id": "st_1", "title": "Application Review", "type": "PreInterviewScreen", "interviewPlanId": "plan_1", "orderInInterviewPlan": 0},
			{"id": "st_screen", "title": "Phone Screen", "type": "Active", "interviewPlanId": "plan_1", "orderInInterviewPlan": 1},
			{"id": "st_archived", "title": "Archived", "type": "Archived", "interviewPlanId": "plan_1", "orderInInterviewPlan": 2},
		}, map[string]any{"moreDataAvailable": false})
	case "application.info":
		app, ok := f.apps[str("applicationId")]
		if !ok {
			f.fail(w, http.StatusNotFound, "no such application")
			return
		}
		f.ok(w, app, nil)
	case "application.changeStage":
		// ⚠ THE REAL CONTRACT (probed 2026-09-04). Ashby requires
		// interviewStageId on EVERY changeStage — archiving is a move to the
		// plan's Archived stage carrying a reason, not a verb of its own — and
		// it refuses with HTTP 200 + errorInfo, not an HTTP error code. This
		// fixture was looser than the API, which is exactly how an archive that
		// could never work passed its test.
		if str("interviewStageId") == "" {
			f.invalidInput(w, "interviewStageId: Invalid input: expected string, received undefined")
			return
		}
		app, ok := f.apps[str("applicationId")]
		if !ok {
			f.fail(w, http.StatusNotFound, "no such application")
			return
		}
		title := "Phone Screen"
		if str("interviewStageId") == "st_archived" {
			title = "Archived"
		}
		app["currentInterviewStage"] = map[string]any{"id": str("interviewStageId"), "title": title, "interviewPlanId": "plan_1"}
		if reason := str("archiveReasonId"); reason != "" {
			app["status"] = "Archived"
			app["archiveReason"] = map[string]any{"id": reason, "text": "Not a fit"}
		}
		f.ok(w, app, nil)
	case "file.info":
		if str("fileHandle") == "" {
			f.invalidInput(w, "fileHandle: Invalid input: expected string, received undefined")
			return
		}
		f.ok(w, map[string]any{"url": f.srv.URL + "/blob/" + str("fileHandle")}, nil)
	case "source.list":
		f.ok(w, f.sources, map[string]any{"moreDataAvailable": false})
	case "jobPosting.list":
		if _, ok := call.Body["listedOnly"]; !ok {
			f.fail(w, http.StatusBadRequest, "listedOnly must be explicit")
			return
		}
		p := f.postings
		if p == nil {
			p = []map[string]any{}
		}
		f.ok(w, p, map[string]any{"moreDataAvailable": false})
	case "candidate.list":
		var all []map[string]any
		for _, c := range f.candidates {
			all = append(all, c)
		}
		if all == nil {
			all = []map[string]any{}
		}
		f.ok(w, all, map[string]any{"moreDataAvailable": false, "syncToken": "ct_1"})
	case "application.list":
		var all []map[string]any
		for _, a := range f.apps {
			all = append(all, a)
		}
		if all == nil {
			all = []map[string]any{}
		}
		f.ok(w, all, map[string]any{"moreDataAvailable": false, "syncToken": "at_1"})
	case "job.list", "project.list":
		f.ok(w, []map[string]any{}, map[string]any{"moreDataAvailable": false})
	default:
		f.fail(w, http.StatusNotFound, "unknown method "+call.Method)
	}
}

func (f *fakeAshby) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Method)
	}
	return out
}

func (f *fakeAshby) has(method string) bool {
	for _, m := range f.methods() {
		if m == method {
			return true
		}
	}
	return false
}

func (f *fakeAshby) last(method string) (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i], true
		}
	}
	return fakeCall{}, false
}

// ---- harness ----

// ashbyHarness is a record store with one candidate on the board, a role
// carrying Ashby ids, and a sync service bound to the fake.
type ashbyHarness struct {
	store *Store
	vault string
	sync  *AshbySync
	fake  *fakeAshby
	cand  Candidate
	state string
}

func newAshbyHarness(t *testing.T) *ashbyHarness {
	t.Helper()
	store, vault := testStore(t)
	fake := newFakeAshby(t)
	state := filepath.Join(t.TempDir(), "recruiting", "ashby.json")
	as, err := NewAshbySync(state, store, fake.client())
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.AddCandidate(QuickAdd{Text: "https://example.test/people/dana", Name: "Dana Reyes", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateCandidate(c.ID, map[string]string{"linkedin": "https://linkedin.example/in/dana", "email": "dana@example.test"}); err != nil {
		t.Fatal(err)
	}
	role := store.LoadRole("mri-engineer")
	role.Set("ashby_job_id", "job_mri")
	role.Set("ashby_project_id", "proj_mri")
	role.Set("ashby_posting_id", "post_mri")
	if err := store.SaveRole("mri-engineer", role); err != nil {
		t.Fatal(err)
	}
	return &ashbyHarness{store: store, vault: vault, sync: as, fake: fake, cand: c, state: state}
}

func (h *ashbyHarness) record(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.vault, "system/aion/recruiting/candidates", CandidateSlug(h.cand.ID)+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (h *ashbyHarness) stateFile(t *testing.T) AshbySyncState {
	t.Helper()
	b, err := os.ReadFile(h.state)
	if err != nil {
		t.Fatal(err)
	}
	var st AshbySyncState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// ---- client request shape ----

// Every call is a POST to <root>/<method> with the key as the Basic-auth
// username, an empty password, and the version header.
func TestAshbyRequestShape(t *testing.T) {
	fake := newFakeAshby(t)
	c := fake.client()
	if _, err := c.APIKeyInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListJobPostings(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls: %d", len(fake.calls))
	}
	for _, call := range fake.calls {
		if call.HTTPMethod != http.MethodPost {
			t.Errorf("%s: HTTP %s, want POST", call.Method, call.HTTPMethod)
		}
		if !call.HasAuth || call.User != testAshbyKey || call.Pass != "" {
			t.Errorf("%s: auth user=%q pass=%q has=%v", call.Method, call.User, call.Pass, call.HasAuth)
		}
		if call.Version != AshbyAPIVersion {
			t.Errorf("%s: version header %q", call.Method, call.Version)
		}
	}
	if call, _ := fake.last("jobPosting.list"); call.Body["listedOnly"] != false {
		t.Errorf("listedOnly not passed explicitly: %v", call.Body)
	}
}

// ---- probe ----

func TestAshbyProbeUnconfiguredIsNotAnError(t *testing.T) {
	store, _ := testStore(t)
	fake := newFakeAshby(t)
	as, err := NewAshbySync(filepath.Join(t.TempDir(), "ashby.json"), store, NewAshby(fake.srv.URL, "", fake.srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	p := as.Probe(context.Background())
	if p.Configured || len(p.Scopes) != 0 || p.Error != "" {
		t.Fatalf("probe: %+v", p)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("an unconfigured probe reached the network: %v", fake.methods())
	}
	b, _ := json.Marshal(p)
	if !strings.Contains(string(b), `"configured":false`) || !strings.Contains(string(b), `"scopes":[]`) {
		t.Fatalf("probe json: %s", b)
	}
	// every other method refuses without touching the network
	if _, err := as.Preflight(context.Background(), AshbyPushRequest{Candidate: "cand/x"}); !errors.Is(err, ErrAshbyUnconfigured) {
		t.Fatalf("preflight: %v", err)
	}
	if _, err := as.SyncBack(context.Background(), true, testNow); !errors.Is(err, ErrAshbyUnconfigured) {
		t.Fatalf("sync: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("an unconfigured write reached the network: %v", fake.methods())
	}
}

func TestAshbyProbeConfiguredReportsScopesNotKey(t *testing.T) {
	h := newAshbyHarness(t)
	p := h.sync.Probe(context.Background())
	if !p.Configured || strings.Join(p.Scopes, ",") != "candidatesWrite,jobsRead" || p.Title != "manifest-scout" {
		t.Fatalf("probe: %+v", p)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), testAshbyKey) {
		t.Fatalf("the probe echoed the key")
	}
	if got := h.fake.methods(); len(got) != 1 || got[0] != "apiKey.info" {
		t.Fatalf("calls: %v", got)
	}
}

// ---- key redaction ----

func TestAshbyErrorsRedactTheKey(t *testing.T) {
	fake := newFakeAshby(t)
	fake.failWith = "invalid api key " + testAshbyKey + " for this org"
	c := fake.client()
	_, err := c.APIKeyInfo(context.Background())
	if err == nil {
		t.Fatal("expected a failure")
	}
	var api *AshbyError
	if !errors.As(err, &api) || api.Status != http.StatusUnauthorized {
		t.Fatalf("not a typed error: %#v", err)
	}
	if strings.Contains(err.Error(), testAshbyKey) {
		t.Fatalf("the key leaked into the error: %s", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") || !strings.Contains(err.Error(), "apiKey.info") {
		t.Fatalf("error text: %s", err)
	}
	// a transport failure goes through the same scrubber
	dead := NewAshby("http://127.0.0.1:1", testAshbyKey, &http.Client{Timeout: 200 * time.Millisecond})
	if _, err := dead.ListSources(context.Background()); err == nil || strings.Contains(err.Error(), testAshbyKey) {
		t.Fatalf("transport error: %v", err)
	}
	// the probe surfaces the redacted text, not the key
	store, _ := testStore(t)
	as, _ := NewAshbySync(filepath.Join(t.TempDir(), "ashby.json"), store, c)
	if p := as.Probe(context.Background()); !p.Configured || p.Error == "" || strings.Contains(p.Error, testAshbyKey) {
		t.Fatalf("probe: %+v", p)
	}
}

// ---- authority map ----

func TestAshbyFieldAuthorityGuardsTheWritePath(t *testing.T) {
	for field, want := range map[string]Authority{
		"name": AuthorityShared, "linkedin": AuthorityShared, "email": AuthorityNever,
		"evidence": AuthorityManifest, "criteria": AuthorityManifest, "ashby_stage": AuthorityAshby,
		"ashby_candidate_id": AuthorityAshby, "role.criteria": AuthorityManifest, "role.posting": AuthorityShared,
		"something-new": AuthorityManifest, // unknown defaults to Manifest-owned
	} {
		if got := AuthorityOf(field); got != want {
			t.Errorf("%s: %s, want %s", field, got, want)
		}
	}
	if Pushable("email", false) || !Pushable("email", true) || Pushable("ashby_stage", true) || Pushable("evidence", true) || !Pushable("linkedin", false) {
		t.Fatal("pushable rule broken")
	}
	// the projection never carries a field the map withholds
	h := newAshbyHarness(t)
	doc := h.store.LoadCandidate(CandidateSlug(h.cand.ID))
	proj := candidateProjection(doc, false)
	if _, has := proj["email"]; has {
		t.Fatalf("email projected without the contact flag: %v", proj)
	}
	if proj["linkedin"] != "https://linkedin.example/in/dana" || proj["name"] != "Dana Reyes" {
		t.Fatalf("projection: %v", proj)
	}
	if p := candidateProjection(doc, true); p["email"] != "dana@example.test" {
		t.Fatalf("contact flag ignored: %v", p)
	}
}

// ---- preflight: found → link vs create; not found → create ----

func TestAshbyPreflightNotFoundProposesCreate(t *testing.T) {
	h := newAshbyHarness(t)
	prop, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject})
	if err != nil {
		t.Fatal(err)
	}
	if prop.Decision != DecisionCreate || len(prop.Matches) != 0 || prop.Conflict {
		t.Fatalf("proposal: %+v", prop)
	}
	if len(prop.NeedsChoice) != 0 || prop.ProjectID != "proj_mri" || prop.SourceID != "src_scout" {
		t.Fatalf("proposal: %+v", prop)
	}
	if strings.Join(prop.Writes, ",") != "candidate.create,candidate.addProject,candidate.info" {
		t.Fatalf("writes: %v", prop.Writes)
	}
	// searched by email, then by name; nothing written
	if search, ok := h.fake.last("candidate.search"); !ok || search.Body["name"] != "Dana Reyes" {
		t.Fatalf("search: %+v", search)
	}
	for _, m := range h.fake.methods() {
		if strings.Contains(m, "create") || strings.Contains(m, "add") || strings.Contains(m, "change") {
			t.Fatalf("preflight wrote: %s", m)
		}
	}
	diff := map[string]AshbyDiff{}
	for _, d := range prop.Diff {
		diff[d.Field] = d
	}
	if diff["linkedin"].Action != DiffWrite || diff["email"].Action != DiffSkip || diff["name"].Action != DiffWrite {
		t.Fatalf("diff: %+v", prop.Diff)
	}
}

func TestAshbyPreflightFoundNeedsLinkOrCreateDecision(t *testing.T) {
	h := newAshbyHarness(t)
	h.fake.candidates["cand_existing"] = wireCandidate("cand_existing", "Dana Reyes", "dana@example.test", "https://linkedin.example/in/dana")
	h.fake.searchHits = []map[string]any{h.fake.candidates["cand_existing"]}

	prop, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication})
	if err != nil {
		t.Fatal(err)
	}
	if len(prop.Matches) != 1 || prop.Matches[0].ID != "cand_existing" || prop.Decision != "" {
		t.Fatalf("proposal: %+v", prop)
	}
	if strings.Join(prop.NeedsChoice, ",") != "decision" {
		t.Fatalf("needsChoice: %v", prop.NeedsChoice)
	}
	// a push without the decision is refused and writes nothing
	_, err = h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true}, testNow)
	if err == nil || !strings.Contains(err.Error(), "explicit choice") {
		t.Fatalf("push: %v", err)
	}
	if h.fake.has("candidate.create") || h.fake.has("application.create") {
		t.Fatalf("an undecided push wrote: %v", h.fake.methods())
	}

	// link: the diff is against the match, and the writes skip candidate.create
	linkProp, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Decision: DecisionLink})
	if err != nil {
		t.Fatal(err)
	}
	if linkProp.Decision != DecisionLink || linkProp.Conflict || strings.Join(linkProp.Writes, ",") != "application.create,candidate.info" {
		t.Fatalf("link proposal: %+v", linkProp)
	}
	// create anyway: a namesake, not a duplicate
	createProp, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Decision: DecisionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if createProp.Writes[0] != "candidate.create" {
		t.Fatalf("create proposal: %+v", createProp)
	}
}

// A found match whose profile disagrees with the record, with no base to
// say who moved, is a conflict — the proposal says so and a push refuses.
func TestAshbyLinkWithDisagreementIsAConflict(t *testing.T) {
	h := newAshbyHarness(t)
	h.fake.candidates["cand_existing"] = wireCandidate("cand_existing", "Dana Reyes", "dana@example.test", "https://linkedin.example/in/dana-other")
	h.fake.searchHits = []map[string]any{h.fake.candidates["cand_existing"]}
	prop, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Decision: DecisionLink})
	if err != nil {
		t.Fatal(err)
	}
	if !prop.Conflict {
		t.Fatalf("no conflict flagged: %+v", prop.Diff)
	}
	_, err = h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Decision: DecisionLink, Approve: true}, testNow)
	if !errors.Is(err, ErrAshbyConflict) {
		t.Fatalf("push: %v", err)
	}
	if h.fake.has("candidate.addProject") || h.fake.has("candidate.createNote") {
		t.Fatalf("a conflicting push wrote: %v", h.fake.methods())
	}
	if strings.Contains(h.record(t), "ashby_candidate_id: cand_existing") {
		t.Fatal("a refused push linked the record")
	}
}

// ---- conflict: both sides changed since the last sync ----

func TestAshbyReconcileThreeWay(t *testing.T) {
	cases := []struct {
		m, a, base string
		hasBase    bool
		want       string
	}{
		{"x", "x", "", false, DiffKeep},
		{"x", "", "", false, DiffWrite},
		{"x", "y", "", false, DiffConflict},
		{"x", "y", "x", true, DiffPull},
		{"y", "x", "x", true, DiffWrite},
		{"y", "z", "x", true, DiffConflict},
		{"y", "y", "x", true, DiffKeep},
	}
	for _, c := range cases {
		if got := Reconcile("linkedin", c.m, c.a, c.base, c.hasBase).Action; got != c.want {
			t.Errorf("m=%q a=%q base=%q/%v: %s, want %s", c.m, c.a, c.base, c.hasBase, got, c.want)
		}
	}
}

func TestAshbyPushRefusesWhenBothSidesChanged(t *testing.T) {
	h := newAshbyHarness(t)
	// first contact: create → base snapshot recorded
	res, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Approve: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.CandidateID == "" {
		t.Fatalf("no id: %+v", res)
	}
	st := h.stateFile(t)
	if st.Candidates[h.cand.ID].Base["linkedin"] != "https://linkedin.example/in/dana" {
		t.Fatalf("base: %+v", st.Candidates[h.cand.ID])
	}
	// Manifest moves…
	if _, err := h.store.UpdateCandidate(h.cand.ID, map[string]string{"linkedin": "https://linkedin.example/in/dana-new"}); err != nil {
		t.Fatal(err)
	}
	// …and so does Ashby
	h.fake.candidates[res.CandidateID]["socialLinks"] = []map[string]any{{"type": "LinkedIn", "url": "https://linkedin.example/in/dana-ashby"}}
	before := len(h.fake.calls)
	_, err = h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Approve: true, Note: "second push"}, testNow.Add(time.Hour))
	if !errors.Is(err, ErrAshbyConflict) {
		t.Fatalf("push: %v", err)
	}
	for _, c := range h.fake.calls[before:] {
		if c.Method != "candidate.info" && c.Method != "source.list" {
			t.Fatalf("a conflicting push called %s", c.Method)
		}
	}
	// the record keeps Manifest's value; nothing was overwritten either way
	if !strings.Contains(h.record(t), "dana-new") {
		t.Fatal("the record's own edit was lost")
	}
	// only Ashby moving is a pull, not a conflict, and the push proceeds
	if _, err := h.store.UpdateCandidate(h.cand.ID, map[string]string{"linkedin": "https://linkedin.example/in/dana"}); err != nil {
		t.Fatal(err)
	}
	prop, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range prop.Diff {
		if d.Field == "linkedin" && d.Action != DiffPull {
			t.Fatalf("linkedin: %+v", d)
		}
	}
	if prop.Conflict {
		t.Fatalf("pull flagged as conflict: %+v", prop.Diff)
	}
}

// ---- the approved push, end to end ----

func TestAshbyPushCreatesLinksAndPersists(t *testing.T) {
	h := newAshbyHarness(t)
	_, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Note: "strong low-field MRI fit"}, testNow)
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("an unapproved push went through: %v", err)
	}
	if len(h.fake.calls) != 0 {
		t.Fatalf("an unapproved push reached Ashby: %v", h.fake.methods())
	}

	res, err := h.sync.Push(context.Background(), AshbyPushRequest{
		Candidate: h.cand.ID, Handoff: HandoffProject, Note: "strong low-field MRI fit", Approve: true, Actor: "benjamin",
	}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.CandidateID != "cand_1" || res.ApplicationID != "" || res.NoteID == "" {
		t.Fatalf("result: %+v", res)
	}
	// order: search, search, sources, create, addProject, note, re-fetch
	got := h.fake.methods()
	want := []string{"candidate.search", "candidate.search", "source.list", "candidate.create", "candidate.addProject", "candidate.createNote", "candidate.info"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls:\n got %v\nwant %v", got, want)
	}
	create, _ := h.fake.last("candidate.create")
	if create.Body["name"] != "Dana Reyes" || create.Body["linkedInUrl"] != "https://linkedin.example/in/dana" || create.Body["sourceId"] != "src_scout" {
		t.Fatalf("create body: %v", create.Body)
	}
	if _, has := create.Body["email"]; has {
		t.Fatalf("email travelled without the contact flag: %v", create.Body)
	}
	note, _ := h.fake.last("candidate.createNote")
	if text, _ := note.Body["note"].(string); !strings.HasPrefix(text, AshbyScoutSource) || !strings.Contains(text, "strong low-field MRI fit") {
		t.Fatalf("note: %v", note.Body)
	}
	if note.Body["candidateId"] != "cand_1" || note.Body["sendNotifications"] != false {
		t.Fatalf("note body: %v", note.Body)
	}
	add, _ := h.fake.last("candidate.addProject")
	if add.Body["projectId"] != "proj_mri" || add.Body["candidateId"] != "cand_1" {
		t.Fatalf("addProject: %v", add.Body)
	}

	// persisted: ids on the record, stage moved to ashby, audit rows
	rec := h.record(t)
	for _, want := range []string{"ashby_candidate_id: cand_1", "stage: ashby", "ashby_synced: 2026-09-02", "## ashby",
		"[method:: candidate.create]", "[method:: candidate.addProject]", "[method:: candidate.createNote]", "[by:: benjamin]"} {
		if !strings.Contains(rec, want) {
			t.Errorf("record lacks %q:\n%s", want, rec)
		}
	}
	if strings.Contains(rec, testAshbyKey) {
		t.Fatal("the key reached the vault")
	}
	if res.Record.AshbyCandidateID != "cand_1" || res.Record.Stage != StageAshby {
		t.Fatalf("projection: %+v", res.Record)
	}
	audit := AshbyAuditOf(h.store.LoadCandidate(CandidateSlug(h.cand.ID)))
	if len(audit) != 3 || audit[0].Method != "candidate.create" || audit[0].AshbyID != "cand_1" {
		t.Fatalf("audit: %+v", audit)
	}
	// the record still round-trips byte-identically through parse → emit
	if again := SerializeCandidate(ParseCandidate(rec)); again != rec {
		t.Fatalf("audit rows broke the fixpoint:\n%s\n---\n%s", rec, again)
	}

	// state: outside the vault, 0600, base snapshot + audit, never the key
	if rel, _ := filepath.Rel(h.vault, h.state); !strings.HasPrefix(rel, "..") {
		t.Fatalf("state inside the vault: %s", h.state)
	}
	if fi, err := os.Stat(h.state); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state mode: %v %v", fi, err)
	}
	raw, _ := os.ReadFile(h.state)
	if strings.Contains(string(raw), testAshbyKey) {
		t.Fatal("the key reached the state file")
	}
	st := h.stateFile(t)
	if st.Candidates[h.cand.ID].AshbyID != "cand_1" || len(st.Audit) != 3 {
		t.Fatalf("state: %+v", st)
	}

	// a second preflight sees the link and re-fetches by id, no search
	before := len(h.fake.calls)
	prop, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject})
	if err != nil {
		t.Fatal(err)
	}
	if prop.Linked != "cand_1" || prop.Decision != DecisionLink || h.fake.calls[before].Method != "candidate.info" {
		t.Fatalf("linked preflight: %+v / %v", prop, h.fake.methods()[before:])
	}
}

func TestAshbyPushApplicationPathAndContactFlag(t *testing.T) {
	h := newAshbyHarness(t)
	res, err := h.sync.Push(context.Background(), AshbyPushRequest{
		Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true, IncludeContact: true,
	}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.ApplicationID != "app_2" || res.CandidateID != "cand_1" {
		t.Fatalf("result: %+v", res)
	}
	create, _ := h.fake.last("candidate.create")
	if create.Body["email"] != "dana@example.test" {
		t.Fatalf("approved contact did not travel: %v", create.Body)
	}
	app, _ := h.fake.last("application.create")
	if app.Body["jobId"] != "job_mri" || app.Body["candidateId"] != "cand_1" || app.Body["sourceId"] != "src_scout" {
		t.Fatalf("application.create: %v", app.Body)
	}
	if h.fake.has("candidate.addProject") || h.fake.has("candidate.createNote") {
		t.Fatalf("extra writes: %v", h.fake.methods())
	}
	rec := h.record(t)
	if !strings.Contains(rec, "ashby_application_id: app_2") || !strings.Contains(rec, "[method:: application.create]") {
		t.Fatalf("record:\n%s", rec)
	}
}

// A handoff needing an id the role does not carry is refused BEFORE any
// write — the choice is explicit, never defaulted.
func TestAshbyPushRefusesMissingHandoffTarget(t *testing.T) {
	h := newAshbyHarness(t)
	role := h.store.LoadRole("mri-engineer")
	role.Set("ashby_project_id", "")
	_ = h.store.SaveRole("mri-engineer", role)
	_, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Approve: true}, testNow)
	if err == nil || !strings.Contains(err.Error(), "projectId") {
		t.Fatalf("push: %v", err)
	}
	_, err = h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Approve: true}, testNow)
	if err == nil || !strings.Contains(err.Error(), "handoff") {
		t.Fatalf("push without handoff: %v", err)
	}
	if h.fake.has("candidate.create") {
		t.Fatalf("wrote before the choice: %v", h.fake.methods())
	}
	// an explicit projectId on the request satisfies it
	if _, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, ProjectID: "proj_x", Approve: true}, testNow); err != nil {
		t.Fatal(err)
	}
	if add, _ := h.fake.last("candidate.addProject"); add.Body["projectId"] != "proj_x" {
		t.Fatalf("addProject: %v", add.Body)
	}
}

// A partial failure (candidate created, project add 404s — the doctrine's
// probe-first endpoint) keeps the created id so the next push links rather
// than duplicates.
func TestAshbyPushPartialFailureKeepsTheCreatedID(t *testing.T) {
	h := newAshbyHarness(t)
	h.fake.denyAdd = true
	_, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Approve: true}, testNow)
	var api *AshbyError
	if !errors.As(err, &api) || api.Method != "candidate.addProject" || api.Status != http.StatusNotFound {
		t.Fatalf("push: %v", err)
	}
	rec := h.record(t)
	if !strings.Contains(rec, "ashby_candidate_id: cand_1") || strings.Contains(rec, "stage: ashby") {
		t.Fatalf("partial persist:\n%s", rec)
	}
	h.fake.denyAdd = false
	before := len(h.fake.calls)
	if _, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Approve: true}, testNow); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.fake.calls[before:] {
		if c.Method == "candidate.create" {
			t.Fatal("the retry created a duplicate")
		}
	}
}

// ---- stage change incl. archive reason ----

func TestAshbyChangeStageAdvanceAndArchive(t *testing.T) {
	h := newAshbyHarness(t)
	if _, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true}, testNow); err != nil {
		t.Fatal(err)
	}
	app, err := h.sync.ChangeStage(context.Background(), h.cand.ID, "st_screen", "", "", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if app.Stage != "Phone Screen" || app.Status != "Active" {
		t.Fatalf("advance: %+v", app)
	}
	call, _ := h.fake.last("application.changeStage")
	if call.Body["interviewStageId"] != "st_screen" || call.Body["applicationId"] != "app_2" {
		t.Fatalf("changeStage body: %v", call.Body)
	}
	if _, has := call.Body["archiveReasonId"]; has {
		t.Fatalf("archive reason sent on an advance: %v", call.Body)
	}
	if !strings.Contains(h.record(t), "ashby_stage: Phone Screen") {
		t.Fatalf("record:\n%s", h.record(t))
	}

	app, err = h.sync.ChangeStage(context.Background(), h.cand.ID, "", "reason_nofit", "benjamin", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if app.Status != "Archived" || app.ArchiveReason != "Not a fit" {
		t.Fatalf("archive: %+v", app)
	}
	// ⚠ AN ARCHIVE CARRIES BOTH IDS. The owner names only a reason; the sync
	// resolves the archived stage of this application's own interview plan and
	// sends the pair, because a reason alone is what Ashby refuses.
	call, _ = h.fake.last("application.changeStage")
	if call.Body["archiveReasonId"] != "reason_nofit" {
		t.Fatalf("archive body: %v", call.Body)
	}
	if call.Body["interviewStageId"] != "st_archived" {
		t.Fatalf("archive did not move to the plan's archived stage: %v", call.Body)
	}
	if !h.fake.has("interviewStage.list") {
		t.Fatal("the archived stage was guessed rather than read from the plan")
	}
	rec := h.record(t)
	if !strings.Contains(rec, "ashby_stage: archived: Not a fit") || !strings.Contains(rec, "[detail:: archived: reason_nofit]") {
		t.Fatalf("record:\n%s", rec)
	}
	// a move with no stage named is refused locally, before the round trip
	if _, err := h.fake.client().ChangeStage(context.Background(), AshbyStageChange{ApplicationID: "app_2"}); err == nil {
		t.Fatal("changeStage with no move accepted")
	}
	if _, err := h.fake.client().ChangeStage(context.Background(), AshbyStageChange{ApplicationID: "app_2", ArchiveReasonID: "b"}); err == nil {
		t.Fatal("changeStage with a reason but no stage accepted — that is the call Ashby refuses")
	}
	// a candidate never handed off has nothing to move
	c2, _ := h.store.AddCandidate(QuickAdd{Text: "https://example.test/x", Name: "Nobody Yet"}, testNow)
	if _, err := h.sync.ChangeStage(context.Background(), c2.ID, "st_1", "", "", testNow); err == nil || !strings.Contains(err.Error(), "no Ashby application") {
		t.Fatalf("unlinked: %v", err)
	}
}

// ⚠ A REFUSAL MUST ARRIVE WITH ITS REASON. Ashby answers a bad call with
// HTTP 200, a bare code in `errors` ("invalid_input") and the sentence naming
// the offending field in errorInfo.message. Reporting only the code is what
// turned a one-line contract bug into an afternoon of guessing (2026-09-04).
func TestAshbyErrorCarriesTheAPIsOwnMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"errors":["invalid_input"],"errorInfo":{"code":"invalid_input","message":"interviewStageId: Invalid input: expected string, received undefined","requestId":"01M1"}}`))
	}))
	defer srv.Close()

	_, err := NewAshby(srv.URL, testAshbyKey, srv.Client()).GetApplication(context.Background(), "app_1")
	if err == nil {
		t.Fatal("a failed call returned no error")
	}
	if !strings.Contains(err.Error(), "interviewStageId: Invalid input") {
		t.Fatalf("the API's explanation was dropped: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid_input") {
		t.Fatalf("the code was dropped: %v", err)
	}
}

// ⚠ THE APPLICANT'S OWN SUBMISSION. application.list omits resumeFileHandle
// and customFields — only application.info carries them — so Detail reads the
// application in full, mints a SHORT-LIVED download url from the file handle
// (never storing the handle), and reports honestly that Ashby exposes no
// application-form endpoint. RecordResume keeps a reference on the record; the
// bytes belong to the artifacts pool, never the vault.
func TestAshbyDetailReadsResumeAndFields(t *testing.T) {
	h := newAshbyHarness(t)
	res, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	app := h.fake.apps[res.ApplicationID]
	app["createdAt"] = "2026-09-03T10:00:00Z"
	app["job"] = map[string]any{"id": "job_mri", "title": "MRI Engineer"}
	app["resumeFileHandle"] = map[string]any{"id": "file_1", "name": "Dana_Reyes_CV.pdf", "handle": "handle_abc"}
	app["customFields"] = []map[string]any{
		{"id": "cf_1", "title": "Why this role?", "value": "low-field MRI is the whole reason I applied"},
		{"id": "cf_2", "title": "Willing to relocate", "value": true},
		{"id": "cf_3", "title": "Blank", "value": ""},
	}

	det, err := h.sync.Detail(context.Background(), h.cand.ID)
	if err != nil {
		t.Fatal(err)
	}
	if det.ResumeName != "Dana_Reyes_CV.pdf" {
		t.Fatalf("resume name: %q", det.ResumeName)
	}
	if !strings.HasSuffix(det.ResumeURL, "/blob/handle_abc") {
		t.Fatalf("the file handle was not traded for a url: %q", det.ResumeURL)
	}
	if det.JobTitle != "MRI Engineer" || det.AppliedAt != "2026-09-03T10:00:00Z" {
		t.Fatalf("application facts: %+v", det)
	}
	if !det.FormsUnavailable {
		t.Fatal("the API's form-response limit must be stated, not implied by an empty lane")
	}
	// a blank value is not an answer, and a non-string value still reads
	if len(det.Fields) != 2 {
		t.Fatalf("fields: %+v", det.Fields)
	}
	if det.Fields[0].Title != "Why this role?" || !strings.Contains(det.Fields[0].Value, "low-field MRI") {
		t.Fatalf("field 0: %+v", det.Fields[0])
	}
	if det.Fields[1].Value != "true" {
		t.Fatalf("a boolean answer must still render: %+v", det.Fields[1])
	}

	// the URL is short-lived, so it is never serialized to a client
	blob, err := json.Marshal(det)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "/blob/") || strings.Contains(string(blob), "handle_abc") {
		t.Fatalf("the signed url reached the client payload: %s", blob)
	}

	// the record keeps a REFERENCE — hash and their filename, no bytes
	if err := h.sync.RecordResume(h.cand.ID, "Dana_Reyes_CV.pdf", strings.Repeat("a", 64), testNow); err != nil {
		t.Fatal(err)
	}
	rec := h.record(t)
	if !strings.Contains(rec, "ashby_resume: "+strings.Repeat("a", 64)) ||
		!strings.Contains(rec, "ashby_resume_name: Dana_Reyes_CV.pdf") {
		t.Fatalf("record:\n%s", rec)
	}
	// and it reaches the board projection the inspector renders from
	var got Candidate
	for _, c := range h.store.View().Candidates {
		if c.ID == h.cand.ID {
			got = c
		}
	}
	if got.Resume.Name != "Dana_Reyes_CV.pdf" || got.Resume.Hash == "" {
		t.Fatalf("projection: %+v", got.Resume)
	}
}

// ⚠ AN ASHBY-SIDE ARCHIVE LANDS ON THE BOARD. Mirroring only `ashby_stage`
// left thirteen archived applicants still sitting in the triage queue
// (2026-09-04) — the board column is what the owner sees. A record still in
// the `ashby` column follows the ATS archive; a record the owner moved into
// his own funnel does not; and the reverse (un-archive in Ashby) never
// resurrects a locally archived record.
func TestAshbySyncBackMirrorsAshbyArchiveOntoTheBoard(t *testing.T) {
	h := newAshbyHarness(t)
	appFor := func(id, cand, job, status string) map[string]any {
		return map[string]any{"id": id, "status": status,
			"candidate": map[string]any{"id": cand}, "job": map[string]any{"id": job},
			"currentInterviewStage": map[string]any{"id": "st_1", "title": "Application Review", "interviewPlanId": "plan_1"}}
	}
	// two applicants land in the triage queue
	h.fake.candidates["cand_ava"] = wireCandidate("cand_ava", "Ava Applicant", "ava@example.test", "")
	h.fake.apps["app_ava"] = appFor("app_ava", "cand_ava", "job_mri", "Active")
	h.fake.candidates["cand_moved"] = wireCandidate("cand_moved", "Mona Moved", "mona@example.test", "")
	h.fake.apps["app_moved"] = appFor("app_moved", "cand_moved", "job_mri", "Active")
	if _, err := h.sync.SyncBack(context.Background(), true, testNow); err != nil {
		t.Fatal(err)
	}
	// the owner pulls Mona into his own funnel — she is his to archive now
	if _, err := h.store.SetStage("cand/mona-moved", "reviewing"); err != nil {
		t.Fatal(err)
	}

	// both get archived IN ASHBY
	for _, id := range []string{"app_ava", "app_moved"} {
		h.fake.apps[id]["status"] = "Archived"
		h.fake.apps[id]["archiveReason"] = map[string]any{"id": "r1", "text": "Not a fit"}
	}
	out, err := h.sync.SyncBack(context.Background(), true, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Archived, ",") != "cand/ava-applicant" {
		t.Fatalf("archived: %+v", out)
	}
	ava, err := os.ReadFile(filepath.Join(h.vault, "system/aion/recruiting/candidates/ava-applicant.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stage: archived", "archived: " + testNow.Add(time.Hour).UTC().Format("2006-01-02"), "ashby_stage: archived: Not a fit"} {
		if !strings.Contains(string(ava), want) {
			t.Fatalf("ava lacks %q:\n%s", want, ava)
		}
	}
	mona, err := os.ReadFile(filepath.Join(h.vault, "system/aion/recruiting/candidates/mona-moved.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mona), "stage: reviewing") || !strings.Contains(string(mona), "ashby_stage: archived: Not a fit") {
		t.Fatalf("the owner's funnel stage was not his own:\n%s", mona)
	}

	// idempotent — a second sync archives nothing again
	out2, err := h.sync.SyncBack(context.Background(), true, testNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(out2.Archived) != 0 {
		t.Fatalf("second sync re-archived: %+v", out2)
	}

	// un-archiving in Ashby never resurrects the record here
	h.fake.apps["app_ava"]["status"] = "Active"
	delete(h.fake.apps["app_ava"], "archiveReason")
	if _, err := h.sync.SyncBack(context.Background(), true, testNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ava, _ = os.ReadFile(filepath.Join(h.vault, "system/aion/recruiting/candidates/ava-applicant.md"))
	if !strings.Contains(string(ava), "stage: archived") {
		t.Fatalf("an Ashby un-archive resurrected the record:\n%s", ava)
	}
}

// ⚠ A SKIPPED APPLICANT IS COUNTED, NOT HIDDEN. A live application to a job no
// Manifest role mirrors is correctly left off the board — there is nothing to
// file it under — but silently, the owner cannot tell "nobody applied" from
// "eight people applied to a job you never mirrored" (2026-09-04, Ultrasound
// Engineer: a job closed in Ashby with no posting for `sync roles` to mirror).
func TestAshbySyncBackCountsApplicantsItCannotFile(t *testing.T) {
	h := newAshbyHarness(t)
	appFor := func(id, cand, job, status string) map[string]any {
		return map[string]any{"id": id, "status": status,
			"candidate": map[string]any{"id": cand}, "job": map[string]any{"id": job, "title": "Ultrasound Engineer"},
			"currentInterviewStage": map[string]any{"id": "st_1", "title": "Application Review", "interviewPlanId": "plan_1"}}
	}
	h.fake.candidates["cand_uma"] = wireCandidate("cand_uma", "Uma Unmirrored", "uma@example.test", "")
	h.fake.apps["app_uma"] = appFor("app_uma", "cand_uma", "job_closed", "Active")
	// an ARCHIVED application to the same unmirrored job is not a skip worth
	// reporting — nothing was lost
	h.fake.candidates["cand_old"] = wireCandidate("cand_old", "Otto Old", "otto@example.test", "")
	h.fake.apps["app_old"] = appFor("app_old", "cand_old", "job_closed", "Archived")

	before := len(h.store.CandidateSlugs())
	out, err := h.sync.SyncBack(context.Background(), true, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(h.store.CandidateSlugs()) != before {
		t.Fatal("an applicant to an unmirrored job reached the board")
	}
	if out.SkippedJobs["Ultrasound Engineer"] != 1 {
		t.Fatalf("the live applicant nobody can see was not reported: %+v", out.SkippedJobs)
	}
	if len(out.SkippedJobs) != 1 {
		t.Fatalf("an archived application counted as a skip: %+v", out.SkippedJobs)
	}
}

// A record that was never handed off has no application to read.
func TestAshbyDetailRefusesAnUnlinkedRecord(t *testing.T) {
	h := newAshbyHarness(t)
	if _, err := h.sync.Detail(context.Background(), h.cand.ID); err == nil ||
		!strings.Contains(err.Error(), "not linked") {
		t.Fatalf("unlinked: %v", err)
	}
}

// ---- sync-back: user-actioned, incremental via syncToken ----

func TestAshbySyncBackMirrorsInboundOnly(t *testing.T) {
	h := newAshbyHarness(t)
	h.fake.postings = []map[string]any{{"id": "post_mri", "title": "MRI Engineer", "jobId": "job_from_ashby", "isListed": true}}
	res, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	// Ashby moves the application and the candidate's profile
	h.fake.apps[res.ApplicationID]["currentInterviewStage"] = map[string]any{"id": "st_2", "title": "Onsite"}
	h.fake.candidates[res.CandidateID]["socialLinks"] = []map[string]any{{"type": "LinkedIn", "url": "https://linkedin.example/in/dana-moved"}}
	// an Ashby candidate with NO application must not become a record — the
	// Phase 8 import is driven by live applications to mirrored roles only
	h.fake.candidates["cand_stranger"] = wireCandidate("cand_stranger", "Some Stranger", "s@example.test", "")

	before := len(h.store.CandidateSlugs())
	out, err := h.sync.SyncBack(context.Background(), true, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Full || out.Postings != 1 || out.Candidates != 2 || out.Applications != 1 {
		t.Fatalf("sync: %+v", out)
	}
	if strings.Join(out.RolesLinked, ",") != "mri-engineer" || strings.Join(out.Updated, ",") != h.cand.ID {
		t.Fatalf("sync: %+v", out)
	}
	if strings.Join(out.Drifted, ",") != h.cand.ID || len(out.Conflicts) != 0 {
		t.Fatalf("drift: %+v", out)
	}
	if len(h.store.CandidateSlugs()) != before {
		t.Fatal("sync-back created a record from an Ashby candidate")
	}
	role := h.store.LoadRole("mri-engineer")
	if role.Get("ashby_job_id") != "job_from_ashby" {
		t.Fatalf("role job id: %q", role.Get("ashby_job_id"))
	}
	rec := h.record(t)
	if !strings.Contains(rec, "ashby_stage: Onsite") {
		t.Fatalf("stage not mirrored:\n%s", rec)
	}
	// Manifest's own profile field was NOT overwritten by the drift
	if !strings.Contains(rec, "https://linkedin.example/in/dana]") || strings.Contains(rec, "dana-moved") {
		t.Fatalf("sync-back rewrote a shared profile field:\n%s", rec)
	}
	// jobPosting.list was explicit about listedOnly
	if p, _ := h.fake.last("jobPosting.list"); p.Body["listedOnly"] != false {
		t.Fatalf("listedOnly: %v", p.Body)
	}
	st := h.stateFile(t)
	if st.SyncTokens["candidate.list"] != "ct_1" || st.SyncTokens["application.list"] != "at_1" || st.LastFull == "" {
		t.Fatalf("tokens: %+v", st)
	}
	// the base advanced to Ashby's value, so the next preflight reads it as
	// a Manifest-side write, not a conflict
	if st.Candidates[h.cand.ID].Base["linkedin"] != "https://linkedin.example/in/dana-moved" {
		t.Fatalf("base: %+v", st.Candidates[h.cand.ID])
	}

	// incremental: the stored tokens travel
	if _, err := h.sync.SyncBack(context.Background(), false, testNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.fake.last("candidate.list"); c.Body["syncToken"] != "ct_1" {
		t.Fatalf("candidate.list incremental: %v", c.Body)
	}
	if a, _ := h.fake.last("application.list"); a.Body["syncToken"] != "at_1" {
		t.Fatalf("application.list incremental: %v", a.Body)
	}
	// a full sync drops them
	if _, err := h.sync.SyncBack(context.Background(), true, testNow.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.fake.last("candidate.list"); c.Body["syncToken"] != nil {
		t.Fatalf("full sync sent a token: %v", c.Body)
	}
}

// ---- Phase 8: applicants existing only in Ashby land on the board ----

// Owner decision 2026-09-03: Manifest is the single management surface,
// Ashby the distribution channel. A live application to a mirrored role
// imports its candidate (created into the `ashby` column, contact included —
// first-party applicant data, not a scrape); a matching unlinked record is
// ADOPTED instead of duplicated; archived applications, unmirrored jobs and
// application-less candidates stay out; and a second sync imports nothing
// twice.
func TestAshbySyncBackImportsApplicants(t *testing.T) {
	h := newAshbyHarness(t)
	appFor := func(id, cand, job, status string) map[string]any {
		return map[string]any{"id": id, "status": status,
			"candidate": map[string]any{"id": cand}, "job": map[string]any{"id": job},
			"currentInterviewStage": map[string]any{"id": "st_1", "title": "Application Review"}}
	}
	// a stranger who applied to the mirrored job — imports
	ava := wireCandidate("cand_ava", "Ava Applicant", "ava@example.test", "https://linkedin.example/in/ava")
	ava["position"] = "MRI Physicist"
	ava["company"] = "Fieldline"
	ava["primaryPhoneNumber"] = map[string]any{"value": "+1 314 555 0100"}
	h.fake.candidates["cand_ava"] = ava
	h.fake.apps["app_ava"] = appFor("app_ava", "cand_ava", "job_mri", "Active")
	// rejected in Ashby — stays out
	h.fake.candidates["cand_rex"] = wireCandidate("cand_rex", "Rex Rejected", "rex@example.test", "")
	h.fake.apps["app_rex"] = appFor("app_rex", "cand_rex", "job_mri", "Archived")
	// live application, but to a job no Manifest role mirrors — stays out
	h.fake.candidates["cand_fay"] = wireCandidate("cand_fay", "Fay Foreign", "fay@example.test", "")
	h.fake.apps["app_fay"] = appFor("app_fay", "cand_fay", "job_elsewhere", "Active")
	// the scouted person who then applied: Dana is on the board, unlinked,
	// with her email typed by the owner — the import must LINK, not duplicate
	h.fake.candidates["cand_dana"] = wireCandidate("cand_dana", "Dana Reyes", "dana@example.test", "")
	h.fake.apps["app_dana"] = appFor("app_dana", "cand_dana", "job_mri", "Active")

	before := len(h.store.CandidateSlugs())
	out, err := h.sync.SyncBack(context.Background(), true, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Imported, ",") != "cand/ava-applicant" {
		t.Fatalf("imported: %+v", out)
	}
	if strings.Join(out.Adopted, ",") != h.cand.ID {
		t.Fatalf("adopted: %+v", out)
	}
	if got := len(h.store.CandidateSlugs()); got != before+1 {
		t.Fatalf("records: %d → %d (rex/fay must stay out)", before, got)
	}
	// the created record: ashby column, role tether, ATS stage, contact
	b, err := os.ReadFile(filepath.Join(h.vault, "system/aion/recruiting/candidates/ava-applicant.md"))
	if err != nil {
		t.Fatal(err)
	}
	rec := string(b)
	for _, want := range []string{"stage: ashby", "role: role/mri-engineer",
		"ashby_candidate_id: cand_ava", "ashby_application_id: app_ava",
		"ashby_stage: Application Review", "pii: true",
		"inbound: ", // the untriaged-applicant stamp the board's INBOUND cut reads
		"ava@example.test", "+1 314 555 0100", "https://linkedin.example/in/ava", "MRI Physicist"} {
		if !strings.Contains(rec, want) {
			t.Fatalf("imported record lacks %q:\n%s", want, rec)
		}
	}
	// the adopted record gained the link and moved to the ashby column, and
	// the owner-typed profile survived
	drec := h.record(t)
	for _, want := range []string{"ashby_candidate_id: cand_dana", "ashby_application_id: app_dana", "stage: ashby", "dana@example.test"} {
		if !strings.Contains(drec, want) {
			t.Fatalf("adopted record lacks %q:\n%s", want, drec)
		}
	}
	// both got base snapshots, so the next sync diffs instead of re-importing
	st := h.stateFile(t)
	if st.Candidates["cand/ava-applicant"].AshbyID != "cand_ava" || st.Candidates[h.cand.ID].AshbyID != "cand_dana" {
		t.Fatalf("state: %+v", st.Candidates)
	}

	// idempotent: the second full sync creates and adopts nothing
	out2, err := h.sync.SyncBack(context.Background(), true, testNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(out2.Imported) != 0 || len(out2.Adopted) != 0 {
		t.Fatalf("second sync imported again: %+v", out2)
	}
	if got := len(h.store.CandidateSlugs()); got != before+1 {
		t.Fatalf("second sync changed the board: %d", got)
	}

	// a dangling application (its candidate deleted in Ashby since) is
	// skipped, never a sync failure
	delete(h.fake.candidates, "cand_ava")
	h.fake.apps["app_ghost"] = appFor("app_ghost", "cand_gone", "job_mri", "Active")
	if _, err := h.sync.SyncBack(context.Background(), true, testNow.Add(3*time.Hour)); err != nil {
		t.Fatalf("dangling application failed the sync: %v", err)
	}
}

// ---- no poller, no background write path ----

// The sync service is constructed, wired, and left alone: nothing reaches
// Ashby until a method is called by a route. And the source cannot contain
// a ticker, a timer, or a goroutine — the write path is user-actioned by
// construction, not by discipline.
func TestAshbyHasNoBackgroundWritePath(t *testing.T) {
	h := newAshbyHarness(t)
	time.Sleep(50 * time.Millisecond)
	if len(h.fake.calls) != 0 {
		t.Fatalf("something called Ashby without a user action: %v", h.fake.methods())
	}
	src, err := os.ReadFile("ashby.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"time.NewTicker", "time.Tick(", "time.AfterFunc", "time.After(", "go func", "go a.", "go c."} {
		if strings.Contains(string(src), banned) {
			t.Errorf("ashby.go contains %q — the write path must be user-actioned", banned)
		}
	}
	// and the state file refuses to live in the vault
	if _, err := NewAshbySync(filepath.Join(h.vault, "system", "ashby.json"), h.store, h.fake.client()); err == nil {
		t.Fatal("sync state accepted inside the vault")
	}
}

// ---- lost-update guard: a record edited mid-push keeps the edit ----

// A push spends its network round-trips on a document loaded before them.
// The owner edits the record meanwhile (a profile field, through the
// store's own locked route); when the push persists its ids it must
// re-read the record, not save the stale copy over the edit.
func TestAshbyPushKeepsAConcurrentEdit(t *testing.T) {
	h := newAshbyHarness(t)
	h.fake.onCall = func(method string) {
		if method != "candidate.create" {
			return
		}
		if _, err := h.store.UpdateCandidate(h.cand.ID, map[string]string{"title": "Staff Scientist"}); err != nil {
			t.Error(err)
		}
	}
	res, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffProject, Approve: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	rec := h.record(t)
	for _, want := range []string{"Staff Scientist", "ashby_candidate_id: cand_1", "stage: ashby", "[method:: candidate.create]"} {
		if !strings.Contains(rec, want) {
			t.Errorf("record lacks %q after a push over a concurrent edit:\n%s", want, rec)
		}
	}
	if res.Record.Profile["title"] != "Staff Scientist" {
		t.Fatalf("returned record: %+v", res.Record.Profile)
	}

	// the same guard on a stage change and a sync-back
	h.fake.onCall = func(method string) {
		if method == "application.changeStage" || method == "candidate.list" {
			if _, err := h.store.UpdateCandidate(h.cand.ID, map[string]string{"org": "Example Imaging Group · " + method}); err != nil {
				t.Error(err)
			}
		}
	}
	if _, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true}, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sync.ChangeStage(context.Background(), h.cand.ID, "st_screen", "", "", testNow); err != nil {
		t.Fatal(err)
	}
	if rec := h.record(t); !strings.Contains(rec, "application.changeStage]") || !strings.Contains(rec, "ashby_stage: Phone Screen") {
		t.Fatalf("stage change lost the edit or the stage:\n%s", rec)
	}
	if _, err := h.sync.SyncBack(context.Background(), true, testNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if rec := h.record(t); !strings.Contains(rec, "candidate.list") || !strings.Contains(rec, "ashby_synced:") {
		t.Fatalf("sync-back lost the edit:\n%s", rec)
	}
	// and the inbound writer refuses a field Ashby does not own
	if _, err := h.sync.applyToCandidate(CandidateSlug(h.cand.ID), ashbyRecordPatch{set: map[string]string{"title": "x"}}); err == nil {
		t.Fatal("applyToCandidate accepted a shared field")
	}
	if err := h.sync.applyToRole("mri-engineer", map[string]string{"title": "x"}); err == nil {
		t.Fatal("applyToRole accepted a shared field")
	}
}

// ---- a linked candidate with an application is not re-applied ----

// A second application-handoff push of a record that already carries its
// ashby_application_id drops application.create from the proposal and
// skips it in the push: the note and the re-fetch still run, the
// application count on the Ashby side stays at one.
func TestAshbyRepushWithApplicationDoesNotDuplicate(t *testing.T) {
	h := newAshbyHarness(t)
	first, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.ApplicationID == "" {
		t.Fatalf("first push: %+v", first)
	}
	prop, err := h.sync.Preflight(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication})
	if err != nil {
		t.Fatal(err)
	}
	if prop.ApplicationID != first.ApplicationID || strings.Join(prop.Writes, ",") != "candidate.info" || len(prop.NeedsChoice) != 0 {
		t.Fatalf("linked-with-application proposal: %+v", prop)
	}
	// even with the role's job id gone, the existing application needs none
	role := h.store.LoadRole("mri-engineer")
	role.Set("ashby_job_id", "")
	if err := h.store.SaveRole("mri-engineer", role); err != nil {
		t.Fatal(err)
	}
	before := len(h.fake.calls)
	res, err := h.sync.Push(context.Background(), AshbyPushRequest{Candidate: h.cand.ID, Handoff: HandoffApplication, Approve: true, Note: "second look"}, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res.ApplicationID != first.ApplicationID || res.CandidateID != first.CandidateID {
		t.Fatalf("re-push: %+v", res)
	}
	for _, c := range h.fake.calls[before:] {
		if c.Method == "application.create" || c.Method == "candidate.create" {
			t.Fatalf("the re-push duplicated: %v", h.fake.methods()[before:])
		}
	}
	if len(h.fake.apps) != 1 {
		t.Fatalf("applications on the Ashby side: %d", len(h.fake.apps))
	}
	if note, ok := h.fake.last("candidate.createNote"); !ok || note.Body["candidateId"] != first.CandidateID {
		t.Fatalf("the note did not travel: %v", h.fake.methods()[before:])
	}
	if rec := h.record(t); strings.Count(rec, "ashby_application_id:") != 1 || strings.Count(rec, "[method:: application.create]") != 1 {
		t.Fatalf("record:\n%s", rec)
	}
}
