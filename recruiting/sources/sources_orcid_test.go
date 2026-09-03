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

// orcidServer serves a canned status/body from an httptest server and
// records every request it saw. Nothing here leaves the process: the
// adapter's BaseURL and Client both point at the test server.
type orcidServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
}

func newORCIDServer(t *testing.T, status int, body string) *orcidServer {
	t.Helper()
	s := &orcidServer{}
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

func (s *orcidServer) adapter() ORCID {
	return ORCID{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *orcidServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func orcidFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/orcid-expanded-search.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Rule 2 — the fixture becomes drafts that each cite the ORCID profile,
// dated, quoting the id, name and institutions the registry returned.
func TestORCIDParsesFixtureIntoCitedDrafts(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, orcidFixture(t))
	before := time.Now().Add(-time.Second)
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "diffusion MRI", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	// five hits in the fixture: one unnamed and one without an id are dropped
	if len(got) != 3 {
		t.Fatalf("drafts=%d want 3: %+v", len(got), got)
	}

	d := got[0]
	if d.SourceID != "orcid" || d.ExternalID != "0000-0002-1825-0097" || d.Name != "Dana M. Reyes" ||
		d.Org != "Example Institute of Technology" || d.Role != "role/mri-engineer" {
		t.Fatalf("draft: %+v", d)
	}
	if len(d.Links) != 1 || d.Links[0] != "https://orcid.org/0000-0002-1825-0097" {
		t.Errorf("links: %v", d.Links)
	}
	if !strings.Contains(d.Note, "D. Reyes") {
		t.Errorf("note lost the other names: %q", d.Note)
	}
	if len(d.Evidence) != 1 {
		t.Fatalf("evidence rows: %+v", d.Evidence)
	}
	ev := d.Evidence[0]
	if ev.SourceID != "orcid" || ev.URLOrFile != "https://orcid.org/0000-0002-1825-0097" || !ev.Cited() {
		t.Errorf("evidence is not a citation of the profile: %+v", ev)
	}
	if ev.RetrievedAt.IsZero() || ev.RetrievedAt.Before(before) {
		t.Errorf("evidence is undated: %+v", ev)
	}
	if ev.Kind != EvidenceAffiliation || ev.Trust != TrustHigh {
		t.Errorf("kind/trust: %+v", ev)
	}
	for _, want := range []string{"orcid-id: 0000-0002-1825-0097", "name: Dana M. Reyes",
		"Example Institute of Technology", "Northern Imaging Lab"} {
		if !strings.Contains(ev.Snippet, want) {
			t.Errorf("snippet lacks %q: %q", want, ev.Snippet)
		}
	}

	// the second has no credit-name: given + family is the name; one
	// institution becomes the org
	if got[1].Name != "Sam Okafor" || got[1].Org != "Northern Imaging Lab" ||
		got[1].ExternalID != "0000-0001-5000-0007" || got[1].Evidence[0].Kind != EvidenceAffiliation {
		t.Errorf("second draft: %+v", got[1])
	}
	// the third lists no institution: no org, and the one row is a page
	// citation rather than an affiliation claim
	if got[2].Name != "Priya Natarajan" || got[2].Org != "" || len(got[2].Evidence) != 1 ||
		got[2].Evidence[0].Kind != EvidencePage || got[2].Evidence[0].URLOrFile != "https://orcid.org/0000-0003-4444-2222" ||
		strings.Contains(got[2].Evidence[0].Snippet, "institution-name") {
		t.Errorf("third draft: %+v", got[2])
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Errorf("a search hit supports no relationship claim, got %+v", d.Edges)
		}
		if len(d.Evidence) == 0 {
			t.Errorf("%s: no evidence", d.Name)
		}
		for _, ev := range d.Evidence {
			if !strings.HasPrefix(ev.URLOrFile, "https://orcid.org/") || ev.RetrievedAt.IsZero() {
				t.Errorf("%s: evidence uncited or undated: %+v", d.Name, ev)
			}
		}
	}
}

// The request itself: the documented endpoint, the query, the polite
// headers, and exactly one call per run.
func TestORCIDRequestShape(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, orcidFixture(t))
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 10}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet || r.URL.Path != "/expanded-search/" {
		t.Errorf("%s %s", r.Method, r.URL.Path)
	}
	q := r.URL.Query()
	if q.Get("q") != "Dana Reyes" || q.Get("rows") != "10" {
		t.Errorf("query: %v", q)
	}
	if r.Header.Get("Accept") != "application/json" {
		t.Errorf("Accept: %q", r.Header.Get("Accept"))
	}
	if ua := r.Header.Get("User-Agent"); ua != ORCIDUserAgent || !strings.HasPrefix(ua, "manifest-aion-recruiting/") {
		t.Errorf("User-Agent: %q", ua)
	}
}

// Scope.Max is both what the adapter asks for and the most it returns —
// even when the server ignores rows and sends more.
func TestORCIDScopeMaxBoundsRequestAndResult(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, orcidFixture(t))
	got, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("drafts=%d want 2 (fixture has 3 usable)", len(got))
	}
	if rows := s.requests()[0].URL.Query().Get("rows"); rows != "2" {
		t.Errorf("rows=%q want 2", rows)
	}

	// no max → the adapter's own default; an absurd max → its own ceiling;
	// it never asks for unbounded data
	for max, want := range map[int]string{0: "25", -1: "25", 100000: "100"} {
		s := newORCIDServer(t, http.StatusOK, `{"num-found":0,"expanded-result":[]}`)
		if _, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: max}); err != nil {
			t.Fatal(err)
		}
		if rows := s.requests()[0].URL.Query().Get("rows"); rows != want {
			t.Errorf("max %d → rows=%q want %s", max, rows, want)
		}
	}
}

// An empty query is refused before any request is made.
func TestORCIDRefusesEmptyQuery(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, orcidFixture(t))
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
func TestORCIDErrorsAreClear(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"http 500":       {http.StatusInternalServerError, `{"error":"upstream exploded"}`, "HTTP 500"},
		"http 429":       {http.StatusTooManyRequests, "slow down", "HTTP 429"},
		"malformed json": {http.StatusOK, `{"expanded-result": [ {"orcid-id": "0000-0002-1825-0097", "given-names": `, "malformed"},
		"not json":       {http.StatusOK, `<html>maintenance</html>`, "malformed"},
		"no result key":  {http.StatusOK, `{"response-code":200}`, "no expanded-result"},
		"null with hits": {http.StatusOK, `{"num-found":7,"expanded-result":null}`, "no expanded-result"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newORCIDServer(t, c.status, c.body)
			got, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: 5})
			if err == nil {
				t.Fatalf("no error; drafts=%+v", got)
			}
			if !strings.HasPrefix(err.Error(), "orcid:") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err=%q want it to name %q", err, c.want)
			}
			if got != nil {
				t.Errorf("an error still returned drafts: %+v", got)
			}
		})
	}

	// an honest empty list is not an error — in either form ORCID sends it
	for _, body := range []string{`{"num-found":0,"expanded-result":[]}`, `{"num-found":0,"expanded-result":null}`} {
		s := newORCIDServer(t, http.StatusOK, body)
		got, err := s.adapter().Search(context.Background(), Scope{Query: "nobody", Max: 5})
		if err != nil || len(got) != 0 {
			t.Errorf("%s: drafts=%+v err=%v", body, got, err)
		}
	}
}

// A transport failure — the network is down, the host is wrong — is an
// error too, and the default base URL is never reached from a test.
func TestORCIDTransportFailureIsAnError(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, `{"num-found":0,"expanded-result":[]}`)
	s.srv.Close()
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "MRI", Max: 5}); err == nil ||
		!strings.HasPrefix(err.Error(), "orcid:") {
		t.Errorf("closed server: err=%v", err)
	}
	if got := (ORCID{}).Scope(); len(got) != 3 || got[0].Key != "role" || got[1].Key != "query" || !got[1].Required || got[2].Key != "max" {
		t.Errorf("scope fields must be role/query/max with query required: %+v", got)
	}
	if (ORCID{}).ID() != "orcid" || (ORCID{}).Kind() != KindScholarly {
		t.Error("id/kind")
	}
}

// Rule 3 — no contact details (D15). The fixture carries email lists the way
// the live API can; they must not reach the draft anywhere: not Contact, not
// a link, not a note, not a snippet.
func TestORCIDNeverEmitsContactDetails(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, orcidFixture(t))
	got, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Errorf("%s: contact fields set: %+v", d.Name, d.Contact)
		}
		texts := append([]string{d.Note, d.Title, d.Org, d.Location}, d.Links...)
		for _, ev := range d.Evidence {
			texts = append(texts, ev.Snippet, ev.URLOrFile)
			if ev.Kind == EvidenceContactPublished {
				t.Errorf("%s: a contact evidence row was emitted: %+v", d.Name, ev)
			}
		}
		for _, s := range texts {
			if strings.Contains(s, "@") || strings.HasPrefix(s, "mailto:") || strings.Contains(s, "example.test") {
				t.Errorf("%s: an address reached the draft: %q", d.Name, s)
			}
		}
	}
}

// Enrich is a no-op: a second call per draft is a later phase's, not this.
func TestORCIDEnrichChangesNothing(t *testing.T) {
	s := newORCIDServer(t, http.StatusOK, orcidFixture(t))
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
