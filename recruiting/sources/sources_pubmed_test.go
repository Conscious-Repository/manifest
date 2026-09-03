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

// pubmedServer serves canned esearch and esummary responses from an
// httptest server and records every request it saw. Nothing here leaves
// the process: the adapter's BaseURL and Client both point at the test
// server.
type pubmedServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request

	searchStatus  int
	searchBody    string
	summaryStatus int
	summaryBody   string
}

func newPubMedServer(t *testing.T, searchStatus int, searchBody string, summaryStatus int, summaryBody string) *pubmedServer {
	t.Helper()
	s := &pubmedServer{searchStatus: searchStatus, searchBody: searchBody, summaryStatus: summaryStatus, summaryBody: summaryBody}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/entrez/eutils/esearch.fcgi":
			w.WriteHeader(s.searchStatus)
			_, _ = w.Write([]byte(s.searchBody))
		case "/entrez/eutils/esummary.fcgi":
			w.WriteHeader(s.summaryStatus)
			_, _ = w.Write([]byte(s.summaryBody))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// newPubMedFixtureServer answers both endpoints from the checked-in fixtures.
func newPubMedFixtureServer(t *testing.T) *pubmedServer {
	t.Helper()
	return newPubMedServer(t, http.StatusOK, pubmedFixture(t, "pubmed-esearch.json"),
		http.StatusOK, pubmedFixture(t, "pubmed-esummary.json"))
}

func (s *pubmedServer) adapter() PubMed {
	return PubMed{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *pubmedServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func (s *pubmedServer) requestPaths() []string {
	var out []string
	for _, r := range s.requests() {
		out = append(out, r.URL.Path)
	}
	return out
}

func pubmedFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const pubmedEmptySearch = `{"header":{"type":"esearch","version":"0.3"},"esearchresult":{"count":"0","retmax":"0","retstart":"0","idlist":[],"errorlist":{"phrasesnotfound":["nobody"]},"warninglist":{}}}`

// Rule 2 — the fixture becomes drafts that each cite the paper's PubMed
// page, dated, quoting the author, title, journal, date and PMID the
// summary returned. One author per paper; papers sharing a first author
// fold into one draft with one evidence row per paper.
func TestPubMedParsesFixtureIntoCitedDrafts(t *testing.T) {
	s := newPubMedFixtureServer(t)
	before := time.Now().Add(-time.Second)
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "diffusion mri reconstruction", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	// four papers in the fixture: one has no authors, and two share a
	// first author (the collective name on the fourth is skipped)
	if len(got) != 2 {
		t.Fatalf("drafts=%d want 2: %+v", len(got), got)
	}

	d := got[0]
	if d.SourceID != "pubmed" || d.ExternalID != "39000001:Reyes DM" || d.Name != "Reyes DM" ||
		d.Org != "" || d.Title != "" || d.Location != "" || d.Note != "" || d.Role != "role/mri-engineer" {
		t.Fatalf("draft: %+v", d)
	}
	if len(d.Links) != 2 || d.Links[0] != "https://pubmed.ncbi.nlm.nih.gov/39000001/" ||
		d.Links[1] != "https://pubmed.ncbi.nlm.nih.gov/39000004/" {
		t.Errorf("links: %v", d.Links)
	}
	if len(d.Evidence) != 2 {
		t.Fatalf("evidence rows: %+v", d.Evidence)
	}
	ev := d.Evidence[0]
	if ev.SourceID != "pubmed" || ev.URLOrFile != "https://pubmed.ncbi.nlm.nih.gov/39000001/" || !ev.Cited() {
		t.Errorf("evidence is not a citation of the paper: %+v", ev)
	}
	if ev.RetrievedAt.IsZero() || ev.RetrievedAt.Before(before) {
		t.Errorf("evidence is undated: %+v", ev)
	}
	if ev.Kind != EvidencePublication || ev.Trust != TrustHigh {
		t.Errorf("kind/trust: %+v", ev)
	}
	for _, want := range []string{"author: Reyes DM",
		"title: Self-supervised reconstruction of diffusion MRI with learned priors.",
		"journal: Magnetic resonance in medicine", "pubdate: 2026 Mar 12", "pmid: 39000001",
		"doi: 10.1002/mrm.00001"} {
		if !strings.Contains(ev.Snippet, want) {
			t.Errorf("snippet lacks %q: %q", want, ev.Snippet)
		}
	}
	if sn := d.Evidence[1].Snippet; !strings.Contains(sn, "pmid: 39000004") ||
		!strings.Contains(sn, "journal: Radiology") || !strings.Contains(sn, "author: Reyes DM") ||
		strings.Contains(sn, "Consortium") {
		t.Errorf("second row: %q", sn)
	}

	// the second paper: no full journal name → the abbreviation; no DOI →
	// no doi part; month-only date quoted as given
	if got[1].Name != "Okafor S" || got[1].ExternalID != "39000002:Okafor S" || len(got[1].Links) != 1 ||
		got[1].Links[0] != "https://pubmed.ncbi.nlm.nih.gov/39000002/" || len(got[1].Evidence) != 1 {
		t.Errorf("second draft: %+v", got[1])
	}
	if sn := got[1].Evidence[0].Snippet; !strings.Contains(sn, "journal: Journal of magnetic resonance imaging : JMRI") ||
		!strings.Contains(sn, "pubdate: 2025 Nov") || !strings.Contains(sn, "pmid: 39000002") || strings.Contains(sn, "doi:") {
		t.Errorf("second snippet: %q", sn)
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Errorf("no coauthor edge in this phase, got %+v", d.Edges)
		}
		if len(d.Evidence) == 0 {
			t.Errorf("%s: no evidence", d.Name)
		}
		for _, ev := range d.Evidence {
			if !strings.HasPrefix(ev.URLOrFile, "https://pubmed.ncbi.nlm.nih.gov/") || ev.RetrievedAt.IsZero() {
				t.Errorf("%s: evidence uncited or undated: %+v", d.Name, ev)
			}
			if !strings.Contains(ev.Snippet, "title: ") || !strings.Contains(ev.Snippet, "pmid: ") ||
				!strings.Contains(ev.Snippet, "journal: ") || !strings.Contains(ev.Snippet, "pubdate: ") {
				t.Errorf("%s: snippet lacks a required part: %q", d.Name, ev.Snippet)
			}
		}
	}
}

// The request itself: the documented endpoints, db=pubmed, the query as
// term, JSON mode, the polite User-Agent, no key — and exactly one search
// plus one summary fetch, never more.
func TestPubMedRequestShape(t *testing.T) {
	s := newPubMedFixtureServer(t)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "diffusion MRI[Title]", Max: 10}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests=%d want 2 (esearch + esummary): %v", len(reqs), s.requestPaths())
	}
	q := reqs[0].URL.Query()
	if reqs[0].URL.Path != "/entrez/eutils/esearch.fcgi" || q.Get("db") != "pubmed" ||
		q.Get("term") != "diffusion MRI[Title]" || q.Get("retmax") != "10" || q.Get("retmode") != "json" {
		t.Errorf("esearch: %s %v", reqs[0].URL.Path, q)
	}
	q = reqs[1].URL.Query()
	if reqs[1].URL.Path != "/entrez/eutils/esummary.fcgi" || q.Get("db") != "pubmed" ||
		q.Get("id") != "39000001,39000002,39000003,39000004" || q.Get("retmode") != "json" {
		t.Errorf("esummary: %s %v", reqs[1].URL.Path, q)
	}
	for _, r := range reqs {
		if r.Method != http.MethodGet {
			t.Errorf("%s %s: not a GET", r.Method, r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); ua != PubMedUserAgent || !strings.HasPrefix(ua, "manifest-aion-recruiting/") {
			t.Errorf("%s User-Agent: %q", r.URL.Path, ua)
		}
		if r.Header.Get("Authorization") != "" || r.URL.Query().Get("api_key") != "" {
			t.Errorf("%s sent credentials", r.URL.Path)
		}
	}
}

// Scope.Max is both the retmax the adapter asks for and the most PMIDs it
// summarizes and returns — even when the server ignores retmax and sends
// more.
func TestPubMedScopeMaxBoundsRequestAndResult(t *testing.T) {
	s := newPubMedFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Reyes DM" || len(got[0].Evidence) != 1 {
		t.Errorf("drafts=%+v want only the first paper", got)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests: %v", s.requestPaths())
	}
	if rm := reqs[0].URL.Query().Get("retmax"); rm != "1" {
		t.Errorf("retmax=%q want 1", rm)
	}
	if id := reqs[1].URL.Query().Get("id"); id != "39000001" {
		t.Errorf("esummary id=%q want only the first PMID", id)
	}

	// no max → the adapter's own default; an absurd max → its own ceiling;
	// it never asks for unbounded data
	for max, want := range map[int]string{0: "25", -1: "25", 100000: "100"} {
		s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusOK, `{}`)
		if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: max}); err != nil {
			t.Fatal(err)
		}
		if rm := s.requests()[0].URL.Query().Get("retmax"); rm != want {
			t.Errorf("max %d → retmax=%q want %s", max, rm, want)
		}
	}
}

// An empty query is refused before any request is made.
func TestPubMedRefusesEmptyQuery(t *testing.T) {
	s := newPubMedFixtureServer(t)
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

// A search that finds nothing is an empty slice, not an error — and makes
// no summary request.
func TestPubMedNoResultsIsEmpty(t *testing.T) {
	s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusInternalServerError, `boom`)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "nobody", Max: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("drafts=%#v want an empty, non-nil slice", got)
	}
	if paths := s.requestPaths(); len(paths) != 1 || paths[0] != "/entrez/eutils/esearch.fcgi" {
		t.Errorf("an empty search still fetched summaries: %v", paths)
	}
}

// Server and shape failures on either call each produce an error that says
// what happened, and never a partial draft list.
func TestPubMedErrorsAreClear(t *testing.T) {
	okSearch := pubmedFixture(t, "pubmed-esearch.json")
	okSummary := pubmedFixture(t, "pubmed-esummary.json")
	cases := map[string]struct {
		searchStatus, summaryStatus int
		searchBody, summaryBody     string
		want                        string
		requests                    int
	}{
		"search http 500":     {500, 200, `<html>Internal Server Error</html>`, okSummary, "HTTP 500", 1},
		"search http 429":     {429, 200, `{"error":"API rate limit exceeded"}`, okSummary, "HTTP 429", 1},
		"search malformed":    {200, 200, `{"esearchresult":{"idlist":["1",`, okSummary, "malformed", 1},
		"search not json":     {200, 200, `<html>maintenance</html>`, okSummary, "malformed", 1},
		"search no result":    {200, 200, `{"header":{"type":"esearch"}}`, okSummary, "no esearchresult", 1},
		"search error field":  {200, 200, `{"esearchresult":{"ERROR":"Unable to obtain query #1"}}`, okSummary, "Unable to obtain", 1},
		"search top error":    {200, 200, `{"error":"error forwarding request"}`, okSummary, "error forwarding", 1},
		"search no idlist":    {200, 200, `{"esearchresult":{"count":"3"}}`, okSummary, "no idlist", 1},
		"search null idlist":  {200, 200, `{"esearchresult":{"count":"3","idlist":null}}`, okSummary, "no idlist", 1},
		"summary http 500":    {200, 500, okSearch, `{"error":"upstream exploded"}`, "HTTP 500", 2},
		"summary malformed":   {200, 200, okSearch, `{"result":{"uids":["39000001"],"39000001":{"uid":`, "malformed", 2},
		"summary not json":    {200, 200, okSearch, `<html>maintenance</html>`, "malformed", 2},
		"summary no result":   {200, 200, okSearch, `{"header":{"type":"esummary"}}`, "no result", 2},
		"summary null result": {200, 200, okSearch, `{"result":null}`, "no result", 2},
		"summary top error":   {200, 200, okSearch, `{"error":"Invalid uid"}`, "Invalid uid", 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newPubMedServer(t, c.searchStatus, c.searchBody, c.summaryStatus, c.summaryBody)
			got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
			if err == nil {
				t.Fatalf("no error; drafts=%+v", got)
			}
			if !strings.HasPrefix(err.Error(), "pubmed:") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err=%q want it to name %q", err, c.want)
			}
			if got != nil {
				t.Errorf("an error still returned drafts: %+v", got)
			}
			if n := len(s.requests()); n != c.requests {
				t.Errorf("requests=%d want %d: %v", n, c.requests, s.requestPaths())
			}
		})
	}

	// a per-PMID error entry, a PMID the summary omits, a paper with no
	// authors and a paper with no title each drop that paper, not the run
	s := newPubMedServer(t, http.StatusOK, okSearch, http.StatusOK, `{"result":{"uids":["39000001","39000002","39000004"],
		"39000001":{"uid":"39000001","error":"cannot get document summary"},
		"39000002":{"uid":"39000002","title":"","authors":[{"name":"Okafor S","authtype":"Author"}]},
		"39000004":{"uid":"39000004","title":"A paper.","source":"Radiology","pubdate":"2026","authors":[{"name":"Natarajan P","authtype":"Author"}]}}}`)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Natarajan P" || got[0].ExternalID != "39000004:Natarajan P" {
		t.Errorf("drafts=%+v want only the one usable paper", got)
	}
}

// A transport failure — the network is down, the host is wrong — is an
// error too, and the default base URL is never reached from a test.
func TestPubMedTransportFailureIsAnError(t *testing.T) {
	s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusOK, `{}`)
	s.srv.Close()
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5}); err == nil ||
		!strings.HasPrefix(err.Error(), "pubmed:") {
		t.Errorf("closed server: err=%v", err)
	}
	if got := (PubMed{}).Scope(); len(got) != 3 || got[0].Key != "role" || got[1].Key != "query" || !got[1].Required || got[2].Key != "max" {
		t.Errorf("scope fields must be role/query/max with query required: %+v", got)
	}
	if (PubMed{}).ID() != "pubmed" || (PubMed{}).Kind() != KindScholarly {
		t.Error("id/kind")
	}
}

// Rule 3 — no contact details (D15). The summary fixture carries an email
// on an author entry the way some live records do; it may not reach the
// draft anywhere: not Contact, not a link, not a note, not a snippet.
func TestPubMedNeverEmitsContactDetails(t *testing.T) {
	s := newPubMedFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
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
	// an author entry whose name is itself an address is not an author
	s = newPubMedServer(t, http.StatusOK, `{"esearchresult":{"idlist":["1"]}}`, http.StatusOK,
		`{"result":{"uids":["1"],"1":{"uid":"1","title":"T.","authors":[{"name":"dana@example.test","authtype":"Author"},{"name":"Reyes DM","authtype":"Author"}]}}}`)
	got, err = s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
	if err != nil || len(got) != 1 || got[0].Name != "Reyes DM" {
		t.Errorf("drafts=%+v err=%v", got, err)
	}
}

// Enrich is a no-op and makes no request: the bounded summary fetch already
// happened inside Search.
func TestPubMedEnrichChangesNothing(t *testing.T) {
	s := newPubMedFixtureServer(t)
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
	if edges, err := s.adapter().GraphEdges(context.Background(), got[0]); err != nil || len(edges) != 0 {
		t.Errorf("edges=%+v err=%v", edges, err)
	}
	if n := len(s.requests()); n != before {
		t.Errorf("Enrich/GraphEdges made a request: %d → %d", before, n)
	}
}
