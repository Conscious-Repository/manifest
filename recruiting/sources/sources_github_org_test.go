package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newGitHubOrgServer(t *testing.T, members int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/orgs/hyperfine/public_members":
			var list []map[string]any
			for i := 1; i <= members; i++ {
				list = append(list, map[string]any{"login": "dev" + strconv.Itoa(i), "id": 1000 + i,
					"html_url": "https://github.com/dev" + strconv.Itoa(i), "type": "User"})
			}
			list = append(list, map[string]any{"login": "ci-runner[bot]", "id": 1, "html_url": "https://github.com/apps/ci", "type": "Bot"})
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(r.URL.Path, "/users/"):
			login := strings.TrimPrefix(r.URL.Path, "/users/")
			_ = json.NewEncoder(w).Encode(map[string]any{"login": login, "id": 7, "html_url": "https://github.com/" + login,
				"type": "User", "name": "Dev " + strings.TrimPrefix(login, "dev"), "company": "@hyperfine", "location": "Guilford, CT",
				"email": "leak@example.com", "twitter_username": "leaky"})
		default:
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An org's public members become drafts citing the org page, claim
// same_company under the team cap, and never carry the profile's email.
func TestGitHubOrgPublicMembers(t *testing.T) {
	srv := newGitHubOrgServer(t, 3)
	g := GitHub{BaseURL: srv.URL, Client: *srv.Client()}
	for _, ref := range []string{"hyperfine", "https://github.com/hyperfine/", "@hyperfine"} {
		drafts, err := g.Search(context.Background(), Scope{Max: 10, Fields: map[string]string{"org": ref}})
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if len(drafts) != 3 {
			t.Fatalf("%s: three humans, no bot: %d", ref, len(drafts))
		}
		d := drafts[0]
		if d.Name != "Dev 1" || d.Org != "@hyperfine" || d.Active != 0 {
			t.Fatalf("profile fills the draft; a member list is current by construction: %+v", d)
		}
		if !containsString(d.Links, "https://github.com/hyperfine") {
			t.Fatalf("the org page is cited: %v", d.Links)
		}
		if len(d.Edges) != 2 || d.Edges[0].Type != EdgeSameCompany || !strings.HasPrefix(d.Edges[0].From, ExtNodePrefix+"github/dev") {
			t.Fatalf("same_company to the other members by durable key: %+v", d.Edges)
		}
		raw, _ := json.Marshal(d)
		if strings.Contains(string(raw), "leak@example.com") || strings.Contains(string(raw), "leaky") || len(d.Contact) != 0 {
			t.Fatalf("D15: the profile's email reached a draft: %s", raw)
		}
	}
}

func TestGitHubOrgDirectoryClaimsNoEdges(t *testing.T) {
	srv := newGitHubOrgServer(t, githubMaxEdgeContributors+1)
	g := GitHub{BaseURL: srv.URL, Client: *srv.Client()}
	drafts, err := g.Search(context.Background(), Scope{Max: 100, Fields: map[string]string{"org": "hyperfine"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != githubMaxEdgeContributors+1 {
		t.Fatalf("every member still lands: %d", len(drafts))
	}
	for _, d := range drafts {
		if len(d.Edges) != 0 {
			t.Fatalf("a directory is not a team: %+v", d.Edges)
		}
	}
	if _, err := g.Search(context.Background(), Scope{Fields: map[string]string{"org": "a/b"}}); err == nil {
		t.Fatal("a repo reference is not an org")
	}
}

// The run substrate checks a scope before it fetches; an org names a scope.
func TestGitHubOrgIsAScope(t *testing.T) {
	if _, err := (GitHub{}).PrepareScope(Scope{Fields: map[string]string{"org": "numpy"}}); err != nil {
		t.Fatalf("an org is a scope on its own: %v", err)
	}
	if _, err := (GitHub{}).PrepareScope(Scope{Fields: map[string]string{"org": "a/b"}}); err == nil {
		t.Fatal("a repo reference is not an org")
	}
}
