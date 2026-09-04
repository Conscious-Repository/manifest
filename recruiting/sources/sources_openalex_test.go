package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// openAlexServer serves the named fixture (or a canned status/body) from an
// httptest server and records every request it saw. Nothing here leaves the
// process: the adapter's BaseURL and Client both point at the test server,
// and a request to any other host fails in the transport.
type openAlexServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newOpenAlexServer(t *testing.T, status int, body string) *openAlexServer {
	t.Helper()
	s := &openAlexServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *openAlexServer) adapter() OpenAlex {
	return OpenAlex{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *openAlexServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func openAlexFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Rule 2 — the fixture becomes drafts that each cite the OpenAlex author
// page, dated, with the record's own numbers in the snippet.
func TestOpenAlexParsesFixtureIntoCitedDrafts(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, openAlexFixture(t, "openalex-authors.json"))
	before := time.Now().Add(-time.Second)
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "diffusion MRI", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("drafts=%d want 3: %+v", len(got), got)
	}

	d := got[0]
	if d.SourceID != "openalex" || d.ExternalID != "A5023888391" || d.Name != "Dana Reyes" ||
		d.Org != "Example Institute of Technology" || d.Location != "US" || d.Role != "role/mri-engineer" {
		t.Fatalf("draft: %+v", d)
	}
	if len(d.Links) != 2 || d.Links[0] != "https://openalex.org/A5023888391" ||
		d.Links[1] != "https://orcid.org/0000-0002-1825-0097" {
		t.Errorf("links: %v", d.Links)
	}
	if !strings.Contains(d.Note, "Diffusion MRI Reconstruction") {
		t.Errorf("note lost the topics: %q", d.Note)
	}
	if len(d.Evidence) != 2 {
		t.Fatalf("evidence rows: %+v", d.Evidence)
	}
	kinds := map[string]Evidence{}
	for _, ev := range d.Evidence {
		kinds[ev.Kind] = ev
		if ev.SourceID != "openalex" || ev.URLOrFile != "https://openalex.org/A5023888391" || !ev.Cited() {
			t.Errorf("evidence is not a citation of the author page: %+v", ev)
		}
		if ev.RetrievedAt.IsZero() || ev.RetrievedAt.Before(before) {
			t.Errorf("evidence is undated: %+v", ev)
		}
		if ev.Trust != TrustMedium {
			t.Errorf("an aggregator's citation is medium trust, got %q", ev.Trust)
		}
	}
	aff, ok := kinds[EvidenceAffiliation]
	if !ok || !strings.Contains(aff.Snippet, "Example Institute of Technology") || !strings.Contains(aff.Snippet, "US") {
		t.Errorf("affiliation evidence: %+v", aff)
	}
	pub, ok := kinds[EvidencePublication]
	if !ok {
		t.Fatalf("no publication evidence: %+v", d.Evidence)
	}
	for _, want := range []string{"works_count: 312", "cited_by_count: 8841", "h_index: 41",
		"Diffusion MRI Reconstruction", "Compressed Sensing", "Pulse Sequence Design"} {
		if !strings.Contains(pub.Snippet, want) {
			t.Errorf("publication snippet lacks %q: %q", want, pub.Snippet)
		}
	}

	// the second record names its institution only through the plural field
	// the API is moving to, and has no ORCID: org still lands, no orcid link
	if got[1].Org != "Northern Imaging Lab" || got[1].Location != "CA" || len(got[1].Links) != 1 {
		t.Errorf("second draft: %+v", got[1])
	}
	// the third has no institution at all: no affiliation row, but the
	// publication row still cites the page, and a bare ORCID became a URL
	if got[2].Org != "" || len(got[2].Evidence) != 1 || got[2].Evidence[0].Kind != EvidencePublication ||
		got[2].Links[1] != "https://orcid.org/0000-0001-5000-0007" {
		t.Errorf("third draft: %+v", got[2])
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Errorf("an author record supports no relationship claim, got %+v", d.Edges)
		}
	}
}

// The request itself: the documented endpoint, the query, the polite
// headers, and exactly one call per run.
func TestOpenAlexRequestShape(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, openAlexFixture(t, "openalex-authors.json"))
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 10}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet || r.URL.Path != "/authors" {
		t.Errorf("%s %s", r.Method, r.URL.Path)
	}
	q := r.URL.Query()
	if q.Get("search") != "Dana Reyes" || q.Get("per-page") != "10" {
		t.Errorf("query: %v", q)
	}
	if r.Header.Get("Accept") != "application/json" {
		t.Errorf("Accept: %q", r.Header.Get("Accept"))
	}
	if ua := r.Header.Get("User-Agent"); ua != OpenAlexUserAgent || !strings.HasPrefix(ua, "manifest-aion-recruiting/") {
		t.Errorf("User-Agent: %q", ua)
	}
}

// Scope.Max is both what the adapter asks for and the most it returns —
// even when the server ignores per-page and sends more.
func TestOpenAlexScopeMaxBoundsRequestAndResult(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, openAlexFixture(t, "openalex-authors.json"))
	got, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("drafts=%d want 2 (fixture has 3)", len(got))
	}
	if pp := s.requests()[0].URL.Query().Get("per-page"); pp != "2" {
		t.Errorf("per-page=%q want 2", pp)
	}

	// no max → the adapter's own default; an absurd max → its own ceiling;
	// it never asks for unbounded data
	for max, want := range map[int]string{0: "25", -1: "25", 100000: "100"} {
		s := newOpenAlexServer(t, http.StatusOK, `{"results":[]}`)
		if _, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: max}); err != nil {
			t.Fatal(err)
		}
		if pp := s.requests()[0].URL.Query().Get("per-page"); pp != want {
			t.Errorf("max %d → per-page=%q want %s", max, pp, want)
		}
	}
}

// An empty query is refused before any request is made.
func TestOpenAlexRefusesEmptyQuery(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, openAlexFixture(t, "openalex-authors.json"))
	for _, q := range []string{"", "   ", "\t\n"} {
		if _, err := s.adapter().Search(context.Background(), Scope{Query: q, Max: 5}); err == nil ||
			!strings.Contains(err.Error(), "query") {
			t.Errorf("query %q: err=%v", q, err)
		}
	}
	if n := len(s.requests()); n != 0 {
		t.Errorf("an empty query still made %d request(s)", n)
	}
}

// Server and shape failures each produce an error that says what happened,
// and never a partial draft list.
func TestOpenAlexErrorsAreClear(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"http 500":       {http.StatusInternalServerError, `{"error":"upstream exploded"}`, "HTTP 500"},
		"http 429":       {http.StatusTooManyRequests, "slow down", "HTTP 429"},
		"malformed json": {http.StatusOK, `{"results": [ {"id": "https://openalex.org/A1", "display_name": `, "malformed"},
		"not json":       {http.StatusOK, `<html>maintenance</html>`, "malformed"},
		"no results key": {http.StatusOK, `{"meta":{"count":0}}`, "no results"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newOpenAlexServer(t, c.status, c.body)
			got, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: 5})
			if err == nil {
				t.Fatalf("no error; drafts=%+v", got)
			}
			if !strings.HasPrefix(err.Error(), "openalex:") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err=%q want it to name %q", err, c.want)
			}
			if got != nil {
				t.Errorf("an error still returned drafts: %+v", got)
			}
		})
	}

	// an honest empty list is not an error
	s := newOpenAlexServer(t, http.StatusOK, `{"meta":{"count":0},"results":[]}`)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "nobody", Max: 5})
	if err != nil || len(got) != 0 {
		t.Errorf("empty results: drafts=%+v err=%v", got, err)
	}

	// a record with no id cannot be cited and is dropped, not emitted bare
	s = newOpenAlexServer(t, http.StatusOK, `{"results":[{"display_name":"No Id"},{"id":"https://openalex.org/A7","display_name":""},{"id":"A8","display_name":"Bare Id"}]}`)
	got, err = s.adapter().Search(context.Background(), Scope{Query: "x", Max: 5})
	if err != nil || len(got) != 1 || got[0].Name != "Bare Id" || got[0].Links[0] != "https://openalex.org/A8" ||
		got[0].ExternalID != "A8" {
		t.Errorf("uncitable records: drafts=%+v err=%v", got, err)
	}
}

// A transport failure — the network is down, the host is wrong — is an
// error too, and the default base URL is never reached from a test.
func TestOpenAlexTransportFailureIsAnError(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, `{"results":[]}`)
	s.srv.Close()
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: 5}); err == nil ||
		!strings.HasPrefix(err.Error(), "openalex:") {
		t.Errorf("closed server: err=%v", err)
	}
	// query is no longer flagged Required because a run may name ONE PAPER
	// instead (the works path). The adapter still refuses a scope with
	// neither — the requirement moved from a UI flag to the adapter itself.
	got := (OpenAlex{}).Scope()
	if len(got) == 0 || got[1].Key != "query" || got[2].Key != "work" {
		t.Errorf("scope offers a query or a paper: %+v", got)
	}
	if _, err := s.adapter().Search(context.Background(), Scope{Max: 5}); err == nil ||
		!strings.Contains(err.Error(), "query") {
		t.Errorf("a scope with neither a query nor a paper must be refused: %v", err)
	}
	if (OpenAlex{}).ID() != "openalex" || (OpenAlex{}).Kind() != KindScholarly {
		t.Error("id/kind")
	}
}

// Rule 3 — no contact details (D15). The fixture carries an email the way a
// future API field might; it must not reach the draft anywhere: not Contact,
// not a link, not a note, not a snippet.
func TestOpenAlexNeverEmitsContactDetails(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, openAlexFixture(t, "openalex-authors.json"))
	got, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Errorf("%s: contact fields set: %+v", d.Name, d.Contact)
		}
		texts := append([]string{d.Note, d.Title}, d.Links...)
		for _, ev := range d.Evidence {
			texts = append(texts, ev.Snippet, ev.URLOrFile)
		}
		for _, s := range texts {
			if strings.Contains(s, "@") || strings.HasPrefix(s, "mailto:") || strings.Contains(s, "dana@example.test") {
				t.Errorf("%s: an address reached the draft: %q", d.Name, s)
			}
		}
	}
}

// Enrich is a no-op: a second call per draft is Phase 4's, not this.
func TestOpenAlexEnrichChangesNothing(t *testing.T) {
	s := newOpenAlexServer(t, http.StatusOK, openAlexFixture(t, "openalex-authors.json"))
	got, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("drafts=%+v err=%v", got, err)
	}
	enriched, err := s.adapter().Enrich(context.Background(), got[0])
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Name != got[0].Name || len(enriched.Evidence) != len(got[0].Evidence) || len(enriched.Links) != len(got[0].Links) {
		t.Errorf("Enrich changed the draft:\n%+v\n%+v", got[0], enriched)
	}
	if edges, err := s.adapter().GraphEdges(context.Background(), got[0]); err != nil || len(edges) != 0 {
		t.Errorf("edges=%+v err=%v", edges, err)
	}
	if n := len(s.requests()); n != 1 {
		t.Errorf("Enrich/GraphEdges made a request: total %d", n)
	}
}
