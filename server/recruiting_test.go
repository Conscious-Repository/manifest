package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/record"
	"manifest/recruiting"
	"manifest/vaultwriter"
)

// testRecruitingServer builds a server over a temp vault whose ONLY write
// capability is `aion-recruiting`. Every assertion below therefore runs
// against the real capability layer: a write outside the pattern fails the
// way it would in production, and the audit log records what moved.
func testRecruitingServer(t *testing.T) (*Server, *vaultwriter.Writer, string, string) {
	t.Helper()
	vault := t.TempDir()
	dataDir := t.TempDir()
	vw := vaultwriter.New(vault).WithAudit(dataDir).Grant(
		vaultwriter.Capability{Name: "aion-recruiting", Zone: record.ZoneSystem,
			Pattern: "system/aion/recruiting/**", Actor: vaultwriter.ActorUserAction},
	)
	store := recruiting.NewStore(vault, "system/aion/recruiting", vw.BindAbs("aion-recruiting"))
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	return &Server{aion: aion.NewStore(vault, "system/aion", nil), recruiting: store}, vw, vault, dataDir
}

func recruitingPost(t *testing.T, s *Server, h http.HandlerFunc, path, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if id != "" {
		r.SetPathValue("id", id)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func decodeView(t *testing.T, w *httptest.ResponseRecorder) recruiting.View {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var v recruiting.View
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	return v
}

// The board's happy path, end to end through the handlers: the four roles
// paint, a quick-add lands in NEW, evidence and a score move the gate, and
// archiving retains the record while the rail count returns to where it was.
func TestRecruitingBoardHappyPath(t *testing.T) {
	s, _, vault, dataDir := testRecruitingServer(t)

	view := decodeView(t, recruitingGet(t, s.handleRecruitingView))
	if len(view.Roles) != 4 {
		t.Fatalf("roles: %+v", view.Roles)
	}
	before := roleOpenCount(t, view, "mri-engineer")

	added := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateAdd,
		"/api/aion/recruiting/candidate", "",
		`{"text":"https://example.test/people/dana","name":"Dana Reyes","role":"role/mri-engineer"}`))
	if len(added.Candidates) != 1 || added.Candidates[0].Stage != "new" {
		t.Fatalf("quick-add: %+v", added.Candidates)
	}
	id := added.Candidates[0].ID
	if n := roleOpenCount(t, added, "mri-engineer"); n != before+1 {
		t.Fatalf("the rail count moved by %d, not 1", n-before)
	}
	if added.Candidates[0].Gate.Passed {
		t.Fatalf("a fresh candidate passed the gate: %+v", added.Candidates[0].Gate)
	}

	// evidence, then a score that cites it → the gate names what is left
	ev := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateEvidence,
		"/api/aion/recruiting/candidate/evidence/"+id, id,
		`{"url":"https://example.test/paper","kind":"publication","snippet":"built a 64 mT coil"}`))
	if len(ev.Candidates[0].Evidence) != 2 {
		t.Fatalf("evidence: %+v", ev.Candidates[0].Evidence)
	}
	scored := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateFit,
		"/api/aion/recruiting/candidate/fit/"+id, id,
		`{"criterion":"low-field MRI hardware","score":"4","evidence":["ev2"]}`))
	g := scored.Candidates[0].Gate
	if g.Passed || g.Satisfied != 1 || g.Musts != 3 {
		t.Fatalf("gate after one evidenced must: %+v", g)
	}

	moved := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateStage,
		"/api/aion/recruiting/candidate/stage/"+id, id, `{"stage":"shortlist"}`))
	if moved.Candidates[0].Stage != "shortlist" {
		t.Fatalf("stage: %+v", moved.Candidates[0])
	}

	archived := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateArchive,
		"/api/aion/recruiting/candidate/archive/"+id, id, `{"archived":true}`))
	if archived.Candidates[0].Stage != "archived" || archived.Candidates[0].Archived == "" {
		t.Fatalf("archive: %+v", archived.Candidates[0])
	}
	if n := roleOpenCount(t, archived, "mri-engineer"); n != before {
		t.Fatalf("an archived candidate still counts against the rail: %d", n)
	}
	if _, err := os.Stat(filepath.Join(vault, "system/aion/recruiting/candidates/dana-reyes.md")); err != nil {
		t.Errorf("archiving deleted the record: %v", err)
	}

	// (b) every byte that moved is attributed to the narrow capability
	log, err := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if err != nil {
		t.Fatalf("no write-audit.log: %v", err)
	}
	lines := 0
	for _, ln := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		if ln == "" {
			continue
		}
		lines++
		if !strings.Contains(ln, "\taion-recruiting\t") {
			t.Errorf("an audited write was not tagged aion-recruiting: %s", ln)
		}
		if !strings.Contains(ln, "system/aion/recruiting/") {
			t.Errorf("an audited write landed outside the recruiting root: %s", ln)
		}
		if !strings.Contains(ln, "user-action") {
			t.Errorf("an audited write was not a user action: %s", ln)
		}
	}
	if lines == 0 {
		t.Error("nothing was audited")
	}
}

func recruitingGet(t *testing.T, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/api/aion/recruiting", nil))
	return w
}

func roleOpenCount(t *testing.T, v recruiting.View, slug string) int {
	t.Helper()
	for _, r := range v.Roles {
		if r.Slug == slug {
			return r.OpenCount
		}
	}
	t.Fatalf("no role %q in %+v", slug, v.Roles)
	return 0
}

// (a) A write outside the pattern is a capability violation that NAMES the
// capability — the boundary is enforced by the writer, not by convention.
func TestRecruitingWriteOutsideThePatternIsRefused(t *testing.T) {
	_, vw, vault, _ := testRecruitingServer(t)
	write := vw.BindAbs("aion-recruiting")
	for _, rel := range []string{
		"system/aion/hiring.md",       // D9 — published at portal.aion.bio
		"system/aion/backlog.md",      // the export contract's own corpus
		"system/crm/fundraising/x.md", // another private domain
	} {
		err := write(filepath.Join(vault, filepath.FromSlash(rel)), []byte("x"))
		if err == nil {
			t.Fatalf("%s was writable through aion-recruiting", rel)
		}
		if !strings.Contains(err.Error(), "aion-recruiting") {
			t.Errorf("the refusal did not name the capability: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(vault, filepath.FromSlash(rel))); statErr == nil {
			t.Errorf("%s was created despite the refusal", rel)
		}
	}
}

// (c)/(d) The 400s: closed sets are closed at the HTTP boundary too.
func TestRecruitingRefusesInvalidTransitions(t *testing.T) {
	s, _, _, _ := testRecruitingServer(t)
	added := decodeView(t, recruitingPost(t, s, s.handleRecruitingCandidateAdd,
		"/api/aion/recruiting/candidate", "", `{"text":"Dana Reyes","role":"role/mri-engineer"}`))
	id := added.Candidates[0].ID

	for name, tc := range map[string]struct {
		h    http.HandlerFunc
		id   string
		body string
	}{
		"stage outside the closed set":   {s.handleRecruitingCandidateStage, id, `{"stage":"hired"}`},
		"archived via the stage route":   {s.handleRecruitingCandidateStage, id, `{"stage":"archived"}`},
		"stage on an unknown candidate":  {s.handleRecruitingCandidateStage, "cand/ghost", `{"stage":"new"}`},
		"score out of range":             {s.handleRecruitingCandidateFit, id, `{"criterion":"x","score":"9"}`},
		"score that is not a number":     {s.handleRecruitingCandidateFit, id, `{"criterion":"x","score":"high"}`},
		"criterion-less fit row":         {s.handleRecruitingCandidateFit, id, `{"criterion":"","score":"3"}`},
		"evidence with nothing to cite":  {s.handleRecruitingCandidateEvidence, id, `{"kind":"publication"}`},
		"override with no reason":        {s.handleRecruitingCandidateOverride, id, `{"by":"benjamin"}`},
		"quick-add with nothing in it":   {s.handleRecruitingCandidateAdd, "", `{"text":"   "}`},
		"quick-add onto an unknown role": {s.handleRecruitingCandidateAdd, "", `{"text":"X","role":"role/nope"}`},
		"unknown profile field":          {s.handleRecruitingCandidateUpdate, id, `{"salary":"100"}`},
		"a candidate id that traverses":  {s.handleRecruitingCandidateStage, "cand/../../hiring", `{"stage":"new"}`},
		"a duplicate name on the board":  {s.handleRecruitingCandidateAdd, "", `{"text":"Dana Reyes","role":"role/mri-engineer"}`},
		"a nameless candidate rename":    {s.handleRecruitingCandidateUpdate, id, `{"name":"  "}`},
		"a seed class outside the set":   {s.handleRecruitingSeedAdd, "", `{"class":"candidate","name":"x"}`},
		"a seed with no name":            {s.handleRecruitingSeedAdd, "", `{"class":"lab","name":""}`},
	} {
		w := recruitingPost(t, s, tc.h, "/api/aion/recruiting/x", tc.id, tc.body)
		if w.Code == http.StatusOK {
			t.Errorf("%s was accepted (200): %s", name, w.Body.String())
		}
	}
}

func TestRecruitingSeedAndCriteriaRoutes(t *testing.T) {
	s, _, _, _ := testRecruitingServer(t)

	seeded := decodeView(t, recruitingPost(t, s, s.handleRecruitingSeedAdd,
		"/api/aion/recruiting/seed", "",
		`{"class":"lab","name":"Example University BME","url":"https://example.test/bme"}`))
	if len(seeded.Seeds) != 3 {
		t.Fatalf("seeds: %+v", seeded.Seeds)
	}
	last := seeded.Seeds[len(seeded.Seeds)-1]
	if last.Class != "lab" || last.Source != "owner" || last.Added == "" {
		t.Fatalf("the seed lost its provenance: %+v", last)
	}

	// the seeds route is a read of the same derivation the board uses
	w := httptest.NewRecorder()
	s.handleRecruitingSeeds(w, httptest.NewRequest(http.MethodGet, "/api/aion/recruiting/seeds", nil))
	var seedsView struct {
		Seeds       []recruiting.Seed `json:"seeds"`
		SeedClasses []string          `json:"seedClasses"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &seedsView); err != nil {
		t.Fatal(err)
	}
	// the class list is the store's closed set, not a number typed here — the
	// vocabulary widens by decision (media, 2026-09-04) and a magic count
	// turns that into a spurious failure
	if len(seedsView.Seeds) != 3 || len(seedsView.SeedClasses) != len(recruiting.SeedClasses) {
		t.Fatalf("seeds view: %+v", seedsView)
	}

	// criteria edit
	req := httptest.NewRequest(http.MethodPut, "/api/aion/recruiting/roles/mechanical-engineer/criteria",
		strings.NewReader(`{"criteria":[{"criterion":"finite element modelling","class":"must","weight":3}]}`))
	req.SetPathValue("slug", "mechanical-engineer")
	cw := httptest.NewRecorder()
	s.handleRecruitingRoleCriteria(cw, req)
	view := decodeView(t, cw)
	for _, r := range view.Roles {
		if r.Slug != "mechanical-engineer" {
			continue
		}
		if len(r.Criteria) != 1 || r.Criteria[0].Class != "must" {
			t.Fatalf("criteria: %+v", r.Criteria)
		}
	}

	// a single role read
	rr := httptest.NewRequest(http.MethodGet, "/api/aion/recruiting/roles/mri-engineer", nil)
	rr.SetPathValue("slug", "mri-engineer")
	rw := httptest.NewRecorder()
	s.handleRecruitingRole(rw, rr)
	if rw.Code != http.StatusOK || !strings.Contains(rw.Body.String(), "MRI Engineer") {
		t.Fatalf("role read: %d %s", rw.Code, rw.Body.String())
	}
	missing := httptest.NewRequest(http.MethodGet, "/api/aion/recruiting/roles/nope", nil)
	missing.SetPathValue("slug", "nope")
	mw := httptest.NewRecorder()
	s.handleRecruitingRole(mw, missing)
	if mw.Code != http.StatusNotFound {
		t.Errorf("an unknown role answered %d", mw.Code)
	}
}

// Without a store the surface degrades rather than panicking — the routes are
// only registered when the store is present, and the handlers say so anyway.
func TestRecruitingUnavailableWithoutAStore(t *testing.T) {
	s := &Server{}
	for _, h := range []http.HandlerFunc{
		s.handleRecruitingView, s.handleRecruitingSeeds, s.handleRecruitingSeedAdd,
		s.handleRecruitingCandidateAdd, s.handleRecruitingCandidateStage,
		s.handleRecruitingCandidateArchive, s.handleRecruitingCandidateEvidence,
		s.handleRecruitingCandidateFit, s.handleRecruitingCandidateOverride,
		s.handleRecruitingCandidateUpdate, s.handleRecruitingRole, s.handleRecruitingRoleCriteria,
	} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, "/api/aion/recruiting", strings.NewReader("{}")))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("handler answered %d without a store", w.Code)
		}
	}
	// and the mux does not carry the routes at all — the AION cockpit can be
	// configured while recruiting is not, and then the surface simply is not
	// there, rather than being there and refusing.
	withAion := &Server{aion: aion.NewStore(t.TempDir(), "system/aion", nil)}
	mux := withAion.Handler()
	for _, path := range []string{
		"/api/aion/recruiting", "/api/aion/recruiting/seeds",
		"/api/aion/recruiting/roles/mri-engineer",
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s answered %d with no recruiting store, want 404", path, w.Code)
		}
	}
}
