package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

type pvServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newPVServer(t *testing.T, status int, body string) *pvServer {
	t.Helper()
	s := &pvServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Api-Key") == "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
			return
		}
		if r.URL.Path != pvPatentPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func pvFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/patentsview-patents.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (s *pvServer) adapter() PatentsView {
	return PatentsView{BaseURL: s.srv.URL, Key: "test-key", Client: *s.srv.Client()}
}

// An assignee sweep names the inventors on its patents, folded by the
// office's own inventor id, each citing the patent page, dated by the newest
// grant — and the field counted honestly.
func TestPatentsAssigneeSweep(t *testing.T) {
	s := newPVServer(t, http.StatusOK, pvFixture(t))
	drafts, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "Hyperfine", Max: 50})
	if err != nil {
		t.Fatal(err)
	}
	if ret.Available == nil || *ret.Available != 12 || ret.Read != 3 || ret.Unit != "patents" {
		t.Fatalf("retrieval: %+v", ret)
	}
	// 2 + 1 new on the second (the keyless one is dropped) + 16 on the crowd
	if ret.PeopleSeen != 19 || len(drafts) != 19 {
		t.Fatalf("people: seen %d shown %d", ret.PeopleSeen, len(drafts))
	}
	byName := map[string]CandidateDraft{}
	for _, d := range drafts {
		byName[d.Name] = d
	}
	dana := byName["Dana Reyes"]
	if dana.ExternalID != "fl:d_ln:reyes-3" || dana.Org != "Hyperfine, Inc." || dana.Title != "Inventor" {
		t.Fatalf("identity and org from the record: %+v", dana)
	}
	if len(dana.Evidence) != 2 || len(dana.Links) != 2 || dana.Active != 2024 {
		t.Fatalf("two patents, one draft, dated by the newest: %+v", dana)
	}
	if dana.Evidence[0].Kind != EvidencePatent || dana.Evidence[0].Trust != TrustHigh ||
		!strings.Contains(dana.Evidence[0].Snippet, "patent: US11934567") {
		t.Fatalf("evidence: %+v", dana.Evidence[0])
	}
	if !strings.HasPrefix(dana.Links[0], PatentsViewPatentURL+"11934567") {
		t.Fatalf("citation is the patent page: %v", dana.Links)
	}
	if priya := byName["Priya Raman"]; priya.Active != 2019 || !priya.IsFormer(2026) {
		t.Fatalf("a 2019 inventor is former by 2026: %+v", priya)
	}
	if _, ok := byName["No Key"]; ok {
		t.Fatal("an inventor without an inventor_id cannot be pointed at again and is not a draft")
	}
	// the key rides the header, never a draft
	if s.reqs[0].Header.Get("X-Api-Key") != "test-key" {
		t.Fatal("the key must ride the request header")
	}
	q := s.reqs[0].URL.Query()
	if !strings.Contains(q.Get("q"), `"assignees.assignee_organization":"Hyperfine"`) || !strings.Contains(q.Get("s"), "patent_date") {
		t.Fatalf("upstream query: %v", q)
	}
}

// D-I on patents: coinventor claims carry the patent as a work with its
// inventor count and year; a second shared patent is a second work on the
// same pair; the 15-inventor rule claims nothing from a department patent.
func TestPatentsCoinventorEdgesAreFractionalAndCapped(t *testing.T) {
	s := newPVServer(t, http.StatusOK, pvFixture(t))
	drafts, err := s.adapter().Search(context.Background(), Scope{Query: "Hyperfine", Max: 50})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]CandidateDraft{}
	for _, d := range drafts {
		byName[d.Name] = d
	}
	dana := byName["Dana Reyes"]
	if len(dana.Edges) != 2 {
		t.Fatalf("Dana shares one patent with Kai and one with Priya: %+v", dana.Edges)
	}
	for _, e := range dana.Edges {
		if e.Type != EdgeCoinventor || e.Inferred || !strings.HasPrefix(e.From, ExtNodePrefix+"patentsview/") || e.To != "" {
			t.Fatalf("a coinventor claim names the other inventor by durable key, far end empty until accept: %+v", e)
		}
		if len(e.Works) != 1 || e.Works[0].Year == 0 || e.Works[0].Authors < 2 || !strings.HasPrefix(e.Works[0].Ref, "US") {
			t.Fatalf("the shared work carries year and inventor count: %+v", e.Works)
		}
		if e.Evidence == "" {
			t.Fatalf("a tie without a citation is only as good as its prose: %+v", e)
		}
	}
	// the 2019 patent has three named inventors but one is keyless: the
	// work still counts two authors — the ones that could be pointed at
	if one := byName["A One"]; len(one.Edges) != 0 {
		t.Fatalf("a 16-inventor patent claims no ties: %d", len(one.Edges))
	}
}

func TestPatentsRefusesWithoutAKey(t *testing.T) {
	s := newPVServer(t, http.StatusOK, pvFixture(t))
	a := s.adapter()
	a.Key = ""
	if a.Configured() {
		t.Fatal("no key, not configured")
	}
	if _, err := a.Search(context.Background(), Scope{Query: "x"}); err == nil || !strings.Contains(err.Error(), PatentsViewKeyEnv) {
		t.Fatalf("an unconfigured adapter says which variable: %v", err)
	}
	if _, err := s.adapter().Search(context.Background(), Scope{}); err == nil || !strings.Contains(err.Error(), "assignee") {
		t.Fatalf("an empty assignee refuses in words: %v", err)
	}
	for _, d := range mustDrafts(t, s) {
		if len(d.Contact) != 0 {
			t.Fatalf("D15: %+v", d)
		}
	}
}

func mustDrafts(t *testing.T, s *pvServer) []CandidateDraft {
	t.Helper()
	out, err := s.adapter().Search(context.Background(), Scope{Query: "Hyperfine"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
