package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

type ctServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newCTServer(t *testing.T, status int, body string) *ctServer {
	t.Helper()
	s := &ctServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != ctStudiesPath {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func ctFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/clinicaltrials-studies.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (s *ctServer) adapter() ClinicalTrials {
	return ClinicalTrials{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

// A sponsor sweep names the officials on its trials, folded by person, each
// citing the study page — and the field is counted honestly.
func TestClinicalTrialsSponsorSweep(t *testing.T) {
	s := newCTServer(t, http.StatusOK, ctFixture(t))
	drafts, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "Hyperfine", Max: 10})
	if err != nil {
		t.Fatal(err)
	}
	if ret.Available == nil || *ret.Available != 3 || ret.Read != 3 || ret.PeopleSeen != 3 || ret.Unit != "studies" {
		t.Fatalf("retrieval: %+v", ret)
	}
	if len(drafts) != 3 {
		t.Fatalf("three distinct officials: %+v", drafts)
	}
	byName := map[string]CandidateDraft{}
	for _, d := range drafts {
		byName[d.Name] = d
	}
	dana := byName["Dana Reyes, MD"]
	if dana.Org != "Hyperfine, Inc." || dana.Title != "Study Director" {
		t.Fatalf("org and role come from the record: %+v", dana)
	}
	if len(dana.Evidence) != 2 || len(dana.Links) != 2 {
		t.Fatalf("one person on two studies is ONE draft with two citations: %+v", dana)
	}
	if !strings.HasPrefix(dana.Links[0], ClinicalTrialsStudyURL+"NCT05550001") {
		t.Fatalf("citation is the study page: %v", dana.Links)
	}
	if dana.Evidence[0].Kind != EvidenceTrial || dana.Evidence[0].Trust != TrustHigh {
		t.Fatalf("a registry record is primary: %+v", dana.Evidence[0])
	}
	if !strings.Contains(dana.Evidence[0].Snippet, "nct: NCT05550001") || !strings.Contains(dana.Evidence[0].Snippet, "start: 2025-03") {
		t.Fatalf("snippet quotes the record: %q", dana.Evidence[0].Snippet)
	}
	// dated at the sponsor by the newest trial
	if dana.Active != 2025 {
		t.Fatalf("active year is the newest study's start: %d", dana.Active)
	}
	if priya := byName["Priya Raman"]; priya.Active != 2019 || !priya.IsFormer(2026) {
		t.Fatalf("a 2019 official is former by 2026: %+v", priya)
	}
	if kai := byName["Kai Ito, PhD"]; kai.IsFormer(2026) {
		t.Fatalf("a 2025 official is current: %+v", kai)
	}
	// the upstream query is the sponsor filter, newest first, and the
	// contacts sibling that carries phone/email is never requested
	q := s.reqs[0].URL.Query()
	if q.Get("query.spons") != "Hyperfine" || q.Get("sort") != "StartDate:desc" || q.Get("countTotal") != "true" {
		t.Fatalf("upstream query: %v", q)
	}
	if strings.Contains(q.Get("fields"), "centralContacts") {
		t.Fatalf("D15: the contacts list with phone/email must never be requested: %s", q.Get("fields"))
	}
}

// D15, structurally: an official whose "name" is an address is dropped, the
// draft type has no contact slot this adapter fills, and no edges are
// claimed because a name is not a durable key.
func TestClinicalTrialsWritesNoContactAndClaimsNoEdges(t *testing.T) {
	s := newCTServer(t, http.StatusOK, ctFixture(t))
	drafts, err := s.adapter().Search(context.Background(), Scope{Query: "Hyperfine"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drafts {
		if len(d.Contact) != 0 || strings.Contains(d.Name, "@") {
			t.Fatalf("a contact field or an address-shaped name reached a draft: %+v", d)
		}
		if len(d.Edges) != 0 {
			t.Fatalf("no durable id, no edge: %+v", d.Edges)
		}
		raw, _ := json.Marshal(d)
		if strings.Contains(string(raw), "555-0100") || strings.Contains(string(raw), "trials@example.com") {
			t.Fatalf("the central contact leaked into a draft: %s", raw)
		}
	}
}

// The cap is on PEOPLE shown, and the field is still counted past it.
func TestClinicalTrialsCapCountsPastIt(t *testing.T) {
	s := newCTServer(t, http.StatusOK, ctFixture(t))
	drafts, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "Hyperfine", Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || ret.PeopleSeen != 3 {
		t.Fatalf("cap 1 shows one, sees three: %d / %+v", len(drafts), ret)
	}
}

func TestClinicalTrialsRefusesAnEmptySponsorAndNamesErrors(t *testing.T) {
	s := newCTServer(t, http.StatusOK, ctFixture(t))
	if _, err := s.adapter().Search(context.Background(), Scope{}); err == nil || !strings.Contains(err.Error(), "sponsor") {
		t.Fatalf("an empty sponsor must refuse in words: %v", err)
	}
	bad := newCTServer(t, http.StatusBadRequest, `{"message":"invalid query"}`)
	if _, err := bad.adapter().Search(context.Background(), Scope{Query: "x"}); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("a non-200 names its status: %v", err)
	}
	none := newCTServer(t, http.StatusOK, `{"studies":[],"totalCount":0}`)
	drafts, ret, err := none.adapter().SearchCounted(context.Background(), Scope{Query: "nobody"})
	if err != nil || len(drafts) != 0 || ret.Available == nil || *ret.Available != 0 {
		t.Fatalf("an honest zero is not an error: %v %+v", err, ret)
	}
}
