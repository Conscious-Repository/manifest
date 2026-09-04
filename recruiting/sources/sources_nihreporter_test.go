package sources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// nihServer serves one canned project-search response from an httptest
// server and records every request it saw, body included. Nothing here
// leaves the process: the adapter's BaseURL and Client both point at the
// test server.
type nihServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []*http.Request
	bodies []string

	status int
	body   string
}

func newNIHServer(t *testing.T, status int, body string) *nihServer {
	t.Helper()
	s := &nihServer{status: status, body: body}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.bodies = append(s.bodies, string(raw))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.URL.Path != "/v2/projects/search" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		}
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// newNIHFixtureServer answers the search from the checked-in fixture.
func newNIHFixtureServer(t *testing.T) *nihServer {
	t.Helper()
	return newNIHServer(t, http.StatusOK, nihFixture(t, "nihreporter-projects.json"))
}

func (s *nihServer) adapter() NIHRePORTER {
	return NIHRePORTER{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *nihServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func (s *nihServer) requestBodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func nihFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const nihEmptySearch = `{"meta":{"search_id":"x","total":0,"offset":0,"limit":25,"sort_field":null,"sort_order":null,"sorted_by_relevance":true,"properties":{}},"results":[]}`

// Rule 2 — the fixture becomes drafts that each cite the project's
// RePORTER page, dated, quoting the PI, title, project number, org, IC,
// fiscal year and abstract the record returned. One draft per PI per
// project; a PI on two projects folds into one draft with one evidence row
// per project.
func TestNIHRePORTERParsesFixtureIntoCitedDrafts(t *testing.T) {
	s := newNIHFixtureServer(t)
	before := time.Now().Add(-time.Second)
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "diffusion mri reconstruction", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	// four projects in the fixture: one has two PIs, one repeats the first
	// PI, one has no PIs, one has a PI with no profile_id and no appl_id
	if len(got) != 3 {
		t.Fatalf("drafts=%d want 3: %+v", len(got), got)
	}

	d := got[0]
	if d.SourceID != "nihreporter" || d.ExternalID != "5R01EB030001-03:7000001" || d.Name != "DANA M REYES" ||
		d.Org != "STANFORD UNIVERSITY" || d.Title != "Principal Investigator" || d.Location != "" ||
		d.Note != "" || d.Role != "role/mri-engineer" {
		t.Fatalf("draft: %+v", d)
	}
	if len(d.Links) != 2 || d.Links[0] != "https://reporter.nih.gov/project-details/10900001" ||
		d.Links[1] != "https://reporter.nih.gov/project-details/10900002" {
		t.Errorf("links: %v", d.Links)
	}
	if len(d.Evidence) != 2 {
		t.Fatalf("evidence rows: %+v", d.Evidence)
	}
	ev := d.Evidence[0]
	if ev.SourceID != "nihreporter" || ev.URLOrFile != "https://reporter.nih.gov/project-details/10900001" || !ev.Cited() {
		t.Errorf("evidence is not a citation of the project: %+v", ev)
	}
	if ev.RetrievedAt.IsZero() || ev.RetrievedAt.Before(before) {
		t.Errorf("evidence is undated: %+v", ev)
	}
	if ev.Kind != EvidenceGrant || ev.Trust != TrustHigh {
		t.Errorf("kind/trust: %+v", ev)
	}
	for _, want := range []string{"pi: DANA M REYES",
		"project: Self-supervised reconstruction of diffusion MRI with learned priors",
		"project_num: 5R01EB030001-03", "org: STANFORD UNIVERSITY", "ic: NIBIB", "fy: 2026",
		"abstract: PROJECT SUMMARY Diffusion MRI is limited by long acquisition times."} {
		if !strings.Contains(ev.Snippet, want) {
			t.Errorf("snippet lacks %q: %q", want, ev.Snippet)
		}
	}
	// the second project has no project_detail_url: the link is derived
	// from appl_id
	if sn := d.Evidence[1].Snippet; !strings.Contains(sn, "project_num: 1R21EB030002-01") ||
		!strings.Contains(sn, "pi: DANA M REYES") || !strings.Contains(sn, "Fast fiber tractography") {
		t.Errorf("second evidence row: %q", sn)
	}
	if d.Evidence[1].URLOrFile != "https://reporter.nih.gov/project-details/10900002" {
		t.Errorf("appl_id-derived link: %q", d.Evidence[1].URLOrFile)
	}

	// the co-PI on the first project is a separate draft with one row
	k := got[1]
	if k.ExternalID != "5R01EB030001-03:7000002" || k.Name != "KAI OKONKWO" || k.Org != "STANFORD UNIVERSITY" ||
		k.Title != "Principal Investigator" || len(k.Evidence) != 1 || len(k.Links) != 1 ||
		k.Links[0] != "https://reporter.nih.gov/project-details/10900001" {
		t.Errorf("co-PI draft: %+v", k)
	}

	// no full_name, no profile_id, no appl_id, null abstract: the name is
	// first/last joined, the fold key is the name, the link is the search
	// page filtered to the project number, and no abstract is quoted
	p := got[2]
	if p.ExternalID != "5R01NS030004-02:PRIYA NARAYAN" || p.Name != "PRIYA NARAYAN" ||
		p.Org != "MASSACHUSETTS GENERAL HOSPITAL" || len(p.Evidence) != 1 {
		t.Fatalf("name-keyed draft: %+v", p)
	}
	if p.Links[0] != "https://reporter.nih.gov/search/results?projects=5R01NS030004-02" ||
		p.Evidence[0].URLOrFile != p.Links[0] {
		t.Errorf("fallback link: %v / %q", p.Links, p.Evidence[0].URLOrFile)
	}
	if sn := p.Evidence[0].Snippet; !strings.Contains(sn, "ic: NS") || strings.Contains(sn, "abstract:") ||
		!strings.Contains(sn, "org: MASSACHUSETTS GENERAL HOSPITAL") {
		t.Errorf("fallback snippet: %q", sn)
	}

	for _, d := range got {
		// same_grant claims arrived with the intake build: two people named
		// as PIs on one project ARE running it together, and the registry
		// says so. What is still forbidden is an unnameable endpoint or an
		// unexplained claim.
		for _, e := range d.Edges {
			if e.Type != EdgeSameGrant {
				t.Errorf("%s: unexpected edge kind %q", d.Name, e.Type)
			}
			if !strings.HasPrefix(e.From, ExtNodePrefix) || e.To != "" {
				t.Errorf("%s: endpoints: %+v", d.Name, e)
			}
			if !strings.Contains(e.Basis, "principal investigators on") || e.Inferred {
				t.Errorf("%s: a stated co-PI claim: %+v", d.Name, e)
			}
		}
		for _, ev := range d.Evidence {
			if strings.TrimSpace(ev.URLOrFile) == "" || ev.RetrievedAt.IsZero() {
				t.Errorf("%s: evidence without URL or date: %+v", d.Name, ev)
			}
		}
	}
}

// Rule 4 — exactly one POST to the documented endpoint, JSON in and out,
// with the polite User-Agent, the query as the full-text criterion and the
// scope's Max as the limit at offset 0.
func TestNIHRePORTERRequestShape(t *testing.T) {
	s := newNIHFixtureServer(t)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "diffusion  mri ", Max: 7}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodPost || r.URL.Path != "/v2/projects/search" || r.URL.RawQuery != "" {
		t.Errorf("request: %s %s", r.Method, r.URL)
	}
	for k, want := range map[string]string{
		"Content-Type": "application/json", "Accept": "application/json", "User-Agent": NIHRePORTERUserAgent,
	} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("%s=%q want %q", k, got, want)
		}
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		t.Errorf("a keyless source sent Authorization: %q", auth)
	}
	var body struct {
		Criteria struct {
			ATS struct {
				Operator    string `json:"operator"`
				SearchField string `json:"search_field"`
				SearchText  string `json:"search_text"`
			} `json:"advanced_text_search"`
		} `json:"criteria"`
		IncludeFields []string `json:"include_fields"`
		Offset        int      `json:"offset"`
		Limit         int      `json:"limit"`
	}
	raw := s.requestBodies()[0]
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("body is not JSON: %v: %q", err, raw)
	}
	if body.Criteria.ATS.SearchText != "diffusion  mri" || body.Criteria.ATS.Operator != "and" ||
		body.Criteria.ATS.SearchField != "projecttitle,terms,abstracttext" {
		t.Errorf("criteria: %+v", body.Criteria)
	}
	if body.Limit != 7 || body.Offset != 0 {
		t.Errorf("limit/offset: %d/%d", body.Limit, body.Offset)
	}
	if len(body.IncludeFields) == 0 || !containsString(body.IncludeFields, "PrincipalInvestigators") ||
		!containsString(body.IncludeFields, "ProjectNum") {
		t.Errorf("include_fields: %v", body.IncludeFields)
	}
}

// Rule 4 — Scope.Max is both the limit asked for and the most drafts that
// leave, whatever the server returned; zero means the default and anything
// over the cap is the cap.
func TestNIHRePORTERMaxBoundsRequestAndDrafts(t *testing.T) {
	limitSent := func(t *testing.T, s *nihServer) int {
		t.Helper()
		var body struct {
			Limit int `json:"limit"`
		}
		bodies := s.requestBodies()
		if len(bodies) != 1 {
			t.Fatalf("requests=%d", len(bodies))
		}
		if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &body); err != nil {
			t.Fatal(err)
		}
		return body.Limit
	}

	// the fixture yields 3 distinct PIs; Max 1 asks for 1 and returns 1
	s := newNIHFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "DANA M REYES" {
		t.Errorf("Max 1 drafts: %+v", got)
	}
	if n := limitSent(t, s); n != 1 {
		t.Errorf("limit sent=%d want 1", n)
	}
	// a PI already drafted still folds extra projects in under a tight Max
	if len(got[0].Evidence) != 2 {
		t.Errorf("folded evidence under Max 1: %+v", got[0].Evidence)
	}

	s = newNIHFixtureServer(t)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri"}); err != nil {
		t.Fatal(err)
	}
	if n := limitSent(t, s); n != nihDefaultMax {
		t.Errorf("default limit sent=%d want %d", n, nihDefaultMax)
	}

	s = newNIHFixtureServer(t)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5000}); err != nil {
		t.Fatal(err)
	}
	if n := limitSent(t, s); n != nihMaxResults {
		t.Errorf("capped limit sent=%d want %d", n, nihMaxResults)
	}
}

// An empty query is refused before any request is made.
func TestNIHRePORTERRefusesEmptyQuery(t *testing.T) {
	s := newNIHFixtureServer(t)
	for _, q := range []string{"", "   ", "\t\n"} {
		got, err := s.adapter().Search(context.Background(), Scope{Query: q, Max: 5})
		if err == nil || !strings.Contains(err.Error(), "query") || got != nil {
			t.Errorf("query %q: drafts=%+v err=%v", q, got, err)
		}
	}
	if n := len(s.requests()); n != 0 {
		t.Errorf("an empty query made %d request(s)", n)
	}
}

// A search that matches nothing is an empty slice, not nil and not an error.
func TestNIHRePORTERNoResultsIsEmpty(t *testing.T) {
	s := newNIHServer(t, http.StatusOK, nihEmptySearch)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "nobody", Max: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("drafts=%#v", got)
	}
	// projects with no usable PI, no project number or no title also yield
	// nothing, without error
	s = newNIHServer(t, http.StatusOK, `{"meta":{"total":3},"results":[
		{"appl_id":1,"project_num":"X1","project_title":"T","principal_investigators":[]},
		{"appl_id":2,"project_num":"","project_title":"T","principal_investigators":[{"full_name":"A B"}]},
		{"appl_id":3,"project_num":"X3","project_title":"","principal_investigators":[{"full_name":"A B"}]}]}`)
	got, err = s.adapter().Search(context.Background(), Scope{Query: "x", Max: 5})
	if err != nil || len(got) != 0 {
		t.Errorf("drafts=%+v err=%v", got, err)
	}
}

// Rule 4 — a non-200, a malformed body, a JSON body with no results field,
// and a transport failure are each a clear error naming the source; none
// is an empty success.
func TestNIHRePORTERErrorsAreClear(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"http 500", http.StatusInternalServerError, `{"message":"upstream exploded"}`, "HTTP 500"},
		{"http 400", http.StatusBadRequest, `{"message":"limit must be <= 500"}`, "HTTP 400"},
		{"malformed", http.StatusOK, `{"meta":{"total":1},"results":[{`, "malformed"},
		{"not json", http.StatusOK, `<html>maintenance</html>`, "malformed"},
		{"no results field", http.StatusOK, `{"meta":{"total":0}}`, "no results field"},
		{"error envelope", http.StatusOK, `{"message":"invalid criteria"}`, "invalid criteria"},
	}
	for _, c := range cases {
		s := newNIHServer(t, c.status, c.body)
		got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
		if err == nil {
			t.Errorf("%s: no error, drafts=%+v", c.name, got)
			continue
		}
		if !strings.HasPrefix(err.Error(), "nihreporter:") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err=%v want prefix nihreporter: and %q", c.name, err, c.want)
		}
		if got != nil {
			t.Errorf("%s: drafts alongside an error: %+v", c.name, got)
		}
	}
	// a non-200 quotes the body so the owner sees what the API said
	s := newNIHServer(t, http.StatusInternalServerError, `{"message":"upstream exploded"}`)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5}); err == nil ||
		!strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("500 error does not quote the body: %v", err)
	}

	s = newNIHServer(t, http.StatusOK, nihEmptySearch)
	s.srv.Close()
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5}); err == nil ||
		!strings.HasPrefix(err.Error(), "nihreporter:") {
		t.Errorf("closed server: err=%v", err)
	}
	if got := (NIHRePORTER{}).Scope(); len(got) != 3 || got[0].Key != "role" || got[1].Key != "query" ||
		!got[1].Required || got[2].Key != "max" {
		t.Errorf("scope fields must be role/query/max with query required: %+v", got)
	}
	if (NIHRePORTER{}).ID() != "nihreporter" || (NIHRePORTER{}).Kind() != KindGrant {
		t.Error("id/kind")
	}
}

// Rule 3 — no contact details (D15). The fixture carries an email on a PI
// entry, as a defensive check against a shape change; it may not reach the
// draft anywhere: not Contact, not a link, not a note, not a snippet.
func TestNIHRePORTERNeverEmitsContactDetails(t *testing.T) {
	s := newNIHFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("drafts=%d", len(got))
	}
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Errorf("%s: contact fields set: %+v", d.Name, d.Contact)
		}
		texts := append([]string{d.Name, d.ExternalID, d.Note, d.Title, d.Org, d.Location}, d.Links...)
		for _, ev := range d.Evidence {
			texts = append(texts, ev.Snippet, ev.URLOrFile)
			if ev.Kind == EvidenceContactPublished {
				t.Errorf("%s: a contact evidence row was emitted: %+v", d.Name, ev)
			}
		}
		for _, s := range texts {
			if containsAddress(s) || strings.Contains(s, "mailto:") || strings.Contains(s, "example.test") {
				t.Errorf("%s: an address reached the draft: %q", d.Name, s)
			}
		}
	}
	// a PI entry whose name is itself an address is not a PI
	s = newNIHServer(t, http.StatusOK, `{"meta":{"total":1},"results":[{"appl_id":9,"project_num":"X9","project_title":"T",
		"principal_investigators":[{"full_name":"dana@example.test"},{"first_name":"Dana","last_name":"Reyes"}]}]}`)
	got, err = s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
	if err != nil || len(got) != 1 || got[0].Name != "Dana Reyes" {
		t.Errorf("drafts=%+v err=%v", got, err)
	}
}

// Enrich is a no-op and makes no request: everything came back in the one
// bounded search response.
func TestNIHRePORTEREnrichChangesNothing(t *testing.T) {
	s := newNIHFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("drafts=%+v err=%v", got, err)
	}
	before := len(s.requests())
	enriched, err := s.adapter().Enrich(context.Background(), got[0])
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Name != got[0].Name || len(enriched.Evidence) != len(got[0].Evidence) || len(enriched.Links) != len(got[0].Links) {
		t.Errorf("Enrich changed the draft:\n%+v\n%+v", got[0], enriched)
	}
	// GraphEdges hands back what the search already built — it must not go
	// and fetch anything of its own
	if edges, err := s.adapter().GraphEdges(context.Background(), got[0]); err != nil ||
		len(edges) != len(got[0].Edges) {
		t.Errorf("edges=%+v err=%v", edges, err)
	}
	if n := len(s.requests()); n != before {
		t.Errorf("Enrich/GraphEdges made a request: %d → %d", before, n)
	}
}
