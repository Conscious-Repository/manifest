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
	"time"
)

// githubServer serves a canned search response and a per-login profile map
// from an httptest server and records every request it saw. Nothing here
// leaves the process: the adapter's BaseURL and Client both point at the
// test server.
type githubServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request
	// search is the status/body for GET /search/users.
	searchStatus int
	searchBody   string
	// profiles maps a login to its GET /users/<login> body; an absent login
	// is a 404. profileStatus overrides the status for every profile.
	profiles      map[string]json.RawMessage
	profileStatus int
}

func newGitHubServer(t *testing.T, status int, body string) *githubServer {
	t.Helper()
	s := &githubServer{searchStatus: status, searchBody: body, profiles: map[string]json.RawMessage{}, profileStatus: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case r.URL.Path == "/search/users":
			w.WriteHeader(s.searchStatus)
			_, _ = w.Write([]byte(s.searchBody))
		case strings.HasPrefix(r.URL.Path, "/users/"):
			login := strings.TrimPrefix(r.URL.Path, "/users/")
			body, ok := s.profiles[login]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			w.WriteHeader(s.profileStatus)
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// withProfiles loads the detail fixture so every login in it answers.
func (s *githubServer) withProfiles(t *testing.T) *githubServer {
	t.Helper()
	b, err := os.ReadFile("testdata/github-user-detail.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &s.profiles); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *githubServer) adapter() GitHub {
	return GitHub{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *githubServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func (s *githubServer) requestPaths() []string {
	var out []string
	for _, r := range s.requests() {
		out = append(out, r.URL.Path)
	}
	return out
}

func githubFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/github-users.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Rule 2 — the fixture becomes drafts that each cite the GitHub profile
// page, dated, quoting the login, name, company and counts GitHub returned.
func TestGitHubParsesFixtureIntoCitedDrafts(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
	before := time.Now().Add(-time.Second)
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "mri language:python", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	// six hits in the fixture: an Organization, an empty login and a hit
	// with no page are dropped
	if len(got) != 3 {
		t.Fatalf("drafts=%d want 3: %+v", len(got), got)
	}

	d := got[0]
	if d.SourceID != "github" || d.ExternalID != "1234567" || d.Name != "Dana M. Reyes" ||
		d.Org != "@example-institute" || d.Location != "Boston, MA" || d.Role != "role/mri-engineer" {
		t.Fatalf("draft: %+v", d)
	}
	if len(d.Links) != 2 || d.Links[0] != "https://github.com/dreyes" || d.Links[1] != "https://dreyes.example.org" {
		t.Errorf("links: %v", d.Links)
	}
	if !strings.Contains(d.Note, "diffusion models") {
		t.Errorf("note lost the bio: %q", d.Note)
	}
	if len(d.Evidence) != 1 {
		t.Fatalf("evidence rows: %+v", d.Evidence)
	}
	ev := d.Evidence[0]
	if ev.SourceID != "github" || ev.URLOrFile != "https://github.com/dreyes" || !ev.Cited() {
		t.Errorf("evidence is not a citation of the profile: %+v", ev)
	}
	if ev.RetrievedAt.IsZero() || ev.RetrievedAt.Before(before) {
		t.Errorf("evidence is undated: %+v", ev)
	}
	if ev.Kind != EvidencePage || ev.Trust != TrustMedium {
		t.Errorf("kind/trust: %+v", ev)
	}
	for _, want := range []string{"login: dreyes", "name: Dana M. Reyes", "company: @example-institute",
		"location: Boston, MA", "public_repos: 48", "followers: 512", "score: 42.5"} {
		if !strings.Contains(ev.Snippet, want) {
			t.Errorf("snippet lacks %q: %q", want, ev.Snippet)
		}
	}

	// the second has no name: the login is the name; no company → no org;
	// a schemeless blog is not a link; a bio carrying an address is not
	// quoted anywhere
	if got[1].Name != "sokafor" || got[1].Org != "" || got[1].Location != "" || got[1].ExternalID != "7654321" ||
		len(got[1].Links) != 1 || got[1].Note != "" {
		t.Errorf("second draft: %+v", got[1])
	}
	if sn := got[1].Evidence[0].Snippet; !strings.Contains(sn, "public_repos: 7") || strings.Contains(sn, "bio:") {
		t.Errorf("second snippet: %q", sn)
	}
	// the third: a plain company becomes the org, a mailto blog is not a
	// link, zero counts are still quoted
	if got[2].Name != "Priya Natarajan" || got[2].Org != "Northern Imaging Lab" || len(got[2].Links) != 1 ||
		got[2].Links[0] != "https://github.com/pnatarajan" {
		t.Errorf("third draft: %+v", got[2])
	}
	if sn := got[2].Evidence[0].Snippet; !strings.Contains(sn, "public_repos: 0") || !strings.Contains(sn, "followers: 0") {
		t.Errorf("third snippet: %q", sn)
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Errorf("a user search supports no relationship claim, got %+v", d.Edges)
		}
		if len(d.Evidence) == 0 {
			t.Errorf("%s: no evidence", d.Name)
		}
		for _, ev := range d.Evidence {
			if !strings.HasPrefix(ev.URLOrFile, "https://github.com/") || ev.RetrievedAt.IsZero() {
				t.Errorf("%s: evidence uncited or undated: %+v", d.Name, ev)
			}
		}
	}
}

// The request itself: the documented endpoints, the query, the GitHub
// media type, the polite User-Agent, and one search plus one profile fetch
// per usable hit — never more.
func TestGitHubRequestShape(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 10}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 4 {
		t.Fatalf("requests=%d want 4 (1 search + 3 profiles): %v", len(reqs), s.requestPaths())
	}
	r := reqs[0]
	if r.Method != http.MethodGet || r.URL.Path != "/search/users" {
		t.Errorf("%s %s", r.Method, r.URL.Path)
	}
	q := r.URL.Query()
	if q.Get("q") != "Dana Reyes" || q.Get("per_page") != "10" {
		t.Errorf("query: %v", q)
	}
	want := []string{"/search/users", "/users/dreyes", "/users/sokafor", "/users/pnatarajan"}
	if got := s.requestPaths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("paths=%v want %v", got, want)
	}
	for _, r := range reqs {
		if r.Method != http.MethodGet {
			t.Errorf("%s %s: not a GET", r.Method, r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("%s Accept: %q", r.URL.Path, r.Header.Get("Accept"))
		}
		if ua := r.Header.Get("User-Agent"); ua != GitHubUserAgent || !strings.HasPrefix(ua, "manifest-aion-recruiting/") {
			t.Errorf("%s User-Agent: %q", r.URL.Path, ua)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("%s sent credentials", r.URL.Path)
		}
	}
}

// Scope.Max is both what the adapter asks for and the most it returns —
// and the most profiles it fetches — even when the server ignores per_page
// and sends more.
func TestGitHubScopeMaxBoundsRequestAndResult(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("drafts=%d want 2 (fixture has 3 usable)", len(got))
	}
	if per := s.requests()[0].URL.Query().Get("per_page"); per != "2" {
		t.Errorf("per_page=%q want 2", per)
	}
	if n := len(s.requests()); n != 3 {
		t.Errorf("requests=%d want 3 (1 search + 2 profiles): %v", n, s.requestPaths())
	}

	// no max → the adapter's own default; an absurd max → its own ceiling;
	// it never asks for unbounded data
	for max, want := range map[int]string{0: "25", -1: "25", 100000: "100"} {
		s := newGitHubServer(t, http.StatusOK, `{"total_count":0,"incomplete_results":false,"items":[]}`)
		if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: max}); err != nil {
			t.Fatal(err)
		}
		if per := s.requests()[0].URL.Query().Get("per_page"); per != want {
			t.Errorf("max %d → per_page=%q want %s", max, per, want)
		}
	}
}

// An empty query is refused before any request is made.
func TestGitHubRefusesEmptyQuery(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
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

// Server and shape failures on the search each produce an error that says
// what happened, and never a partial draft list.
func TestGitHubErrorsAreClear(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"http 500":       {http.StatusInternalServerError, `{"message":"upstream exploded"}`, "HTTP 500"},
		"http 403":       {http.StatusForbidden, `{"message":"API rate limit exceeded"}`, "HTTP 403"},
		"http 422":       {http.StatusUnprocessableEntity, `{"message":"Validation Failed"}`, "HTTP 422"},
		"malformed json": {http.StatusOK, `{"total_count": 1, "items": [ {"login": "dreyes", "id": `, "malformed"},
		"not json":       {http.StatusOK, `<html>maintenance</html>`, "malformed"},
		"no items key":   {http.StatusOK, `{"total_count":0}`, "no items"},
		"null items":     {http.StatusOK, `{"total_count":7,"items":null}`, "no items"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newGitHubServer(t, c.status, c.body)
			got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
			if err == nil {
				t.Fatalf("no error; drafts=%+v", got)
			}
			if !strings.HasPrefix(err.Error(), "github:") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err=%q want it to name %q", err, c.want)
			}
			if got != nil {
				t.Errorf("an error still returned drafts: %+v", got)
			}
			if n := len(s.requests()); n != 1 {
				t.Errorf("a failed search still fetched profiles: %v", s.requestPaths())
			}
		})
	}

	// an honest empty list is not an error
	s := newGitHubServer(t, http.StatusOK, `{"total_count":0,"incomplete_results":false,"items":[]}`)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "nobody", Max: 5})
	if err != nil || len(got) != 0 {
		t.Errorf("empty: drafts=%+v err=%v", got, err)
	}
}

// A profile fetch that fails — rate-limited, missing, malformed — does not
// fail the run: that hit degrades to what the search said, still cited.
func TestGitHubProfileFailureDegradesToSearchHit(t *testing.T) {
	for name, tweak := range map[string]func(*githubServer){
		"404 profile":       func(s *githubServer) { delete(s.profiles, "dreyes") },
		"403 rate limit":    func(s *githubServer) { s.profileStatus = http.StatusForbidden },
		"malformed profile": func(s *githubServer) { s.profiles["dreyes"] = json.RawMessage(`{"login": "dreyes", "name": `) },
	} {
		t.Run(name, func(t *testing.T) {
			s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
			tweak(s)
			got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 1})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("drafts=%+v", got)
			}
			d := got[0]
			if d.Name != "dreyes" || d.ExternalID != "1234567" || d.Org != "" || d.Location != "" || d.Note != "" ||
				len(d.Links) != 1 || d.Links[0] != "https://github.com/dreyes" {
				t.Errorf("degraded draft: %+v", d)
			}
			if len(d.Evidence) != 1 || d.Evidence[0].URLOrFile != "https://github.com/dreyes" || d.Evidence[0].RetrievedAt.IsZero() {
				t.Errorf("degraded draft is uncited: %+v", d.Evidence)
			}
			if sn := d.Evidence[0].Snippet; !strings.Contains(sn, "login: dreyes") || !strings.Contains(sn, "score: 42.5") ||
				strings.Contains(sn, "name:") {
				t.Errorf("degraded snippet: %q", sn)
			}
		})
	}
}

// A transport failure — the network is down, the host is wrong — is an
// error too, and the default base URL is never reached from a test.
func TestGitHubTransportFailureIsAnError(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, `{"total_count":0,"items":[]}`)
	s.srv.Close()
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5}); err == nil ||
		!strings.HasPrefix(err.Error(), "github:") {
		t.Errorf("closed server: err=%v", err)
	}
	// a run names a user search OR one repo; the adapter, not a UI flag,
	// enforces that it names one of them
	if got := (GitHub{}).Scope(); len(got) != 4 || got[0].Key != "role" ||
		got[1].Key != "query" || got[2].Key != "repo" || got[3].Key != "max" {
		t.Errorf("scope fields must be role/query/repo/max: %+v", got)
	}
	if _, err := s.adapter().Search(context.Background(), Scope{Max: 5}); err == nil ||
		!strings.Contains(err.Error(), "query") {
		t.Errorf("a scope with neither a query nor a repo must be refused: %v", err)
	}
	if (GitHub{}).ID() != "github" || (GitHub{}).Kind() != KindCode {
		t.Error("id/kind")
	}
}

// Rule 3 — no contact details (D15). The profile fixture carries email and
// twitter fields the way the live API does, a mailto blog, and a bio with
// an address in it; none may reach the draft anywhere: not Contact, not a
// link, not a note, not a snippet.
func TestGitHubNeverEmitsContactDetails(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 25})
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
		texts := append([]string{d.Note, d.Title, d.Org, d.Location}, d.Links...)
		for _, ev := range d.Evidence {
			texts = append(texts, ev.Snippet, ev.URLOrFile)
			if ev.Kind == EvidenceContactPublished {
				t.Errorf("%s: a contact evidence row was emitted: %+v", d.Name, ev)
			}
		}
		for _, s := range texts {
			if containsAddress(s) || strings.Contains(s, "mailto:") || strings.Contains(s, "example.test") ||
				strings.Contains(s, "dreyes_mri") {
				t.Errorf("%s: an address reached the draft: %q", d.Name, s)
			}
		}
	}
	// the helper itself: a GitHub org mention is not an address, a bare
	// address or one inside prose is
	for text, want := range map[string]bool{
		"@example-institute": false, "Boston, MA": false, "": false, "http://x.example.org/@u": false,
		"dana@example.test": true, "reach me at sam@example.test.": true, "<x@y.org>": true,
	} {
		if containsAddress(text) != want {
			t.Errorf("containsAddress(%q)=%v want %v", text, !want, want)
		}
	}
}

// Enrich is a no-op and makes no request: the bounded profile fetch already
// happened inside Search.
func TestGitHubEnrichChangesNothing(t *testing.T) {
	s := newGitHubServer(t, http.StatusOK, githubFixture(t)).withProfiles(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "Dana Reyes", Max: 1})
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
