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

// a repo server: the contributors list plus the same profile endpoint the
// user search uses.
type githubRepoServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	reqs     []string
	contribs string
	profiles map[string]json.RawMessage
}

func newGitHubRepoServer(t *testing.T, contribs string) *githubRepoServer {
	t.Helper()
	s := &githubRepoServer{contribs: contribs, profiles: map[string]json.RawMessage{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.URL.Path)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch {
		case strings.HasSuffix(r.URL.Path, "/contributors"):
			_, _ = w.Write([]byte(s.contribs))
		case strings.HasPrefix(r.URL.Path, "/users/"):
			if b, ok := s.profiles[strings.TrimPrefix(r.URL.Path, "/users/")]; ok {
				_, _ = w.Write(b)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *githubRepoServer) adapter() GitHub {
	return GitHub{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func repoFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/github-contributors.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGitHubRepoDraftsHumanContributors(t *testing.T) {
	s := newGitHubRepoServer(t, repoFixture(t))
	got, err := s.adapter().Search(context.Background(), Scope{
		Role: "role/mri-engineer", Max: 25,
		Fields: map[string]string{"repo": "https://github.com/aion/coil-sim"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// the bot is not a person and the one-commit drive-by is not a contributor
	if len(got) != 2 {
		t.Fatalf("two human contributors: %+v", names(got))
	}
	for _, d := range got {
		if strings.Contains(strings.ToLower(d.Name), "bot") {
			t.Fatalf("a bot became a candidate: %+v", d)
		}
		var repoEv *Evidence
		for i := range d.Evidence {
			if d.Evidence[i].Kind == EvidenceRepo {
				repoEv = &d.Evidence[i]
			}
		}
		if repoEv == nil {
			t.Fatalf("%s cites no repo: %+v", d.Name, d.Evidence)
		}
		if repoEv.URLOrFile != "https://github.com/aion/coil-sim" {
			t.Fatalf("citation url: %q", repoEv.URLOrFile)
		}
		if !strings.Contains(repoEv.Snippet, "commits") || !strings.Contains(repoEv.Snippet, "aion/coil-sim") {
			t.Fatalf("snippet says what was contributed: %q", repoEv.Snippet)
		}
	}
}

func TestGitHubRepoEmitsSameRepoEdges(t *testing.T) {
	s := newGitHubRepoServer(t, repoFixture(t))
	got, _ := s.adapter().Search(context.Background(), Scope{Max: 25,
		Fields: map[string]string{"repo": "aion/coil-sim"}})
	for _, d := range got {
		if len(d.Edges) != 1 {
			t.Fatalf("%s: one co-contributor, one claim: %+v", d.Name, d.Edges)
		}
		e := d.Edges[0]
		if e.Type != EdgeSameRepo || e.To != "" || !strings.HasPrefix(e.From, ExtNodePrefix+"github/") {
			t.Fatalf("edge: %+v", e)
		}
		if !strings.Contains(e.Basis, "aion/coil-sim") || e.Inferred {
			t.Fatalf("basis: %+v", e)
		}
		// the bot must not be an endpoint either
		if strings.Contains(strings.ToLower(e.From), "bot") {
			t.Fatalf("a bot became a graph node: %+v", e)
		}
	}
}

func TestGitHubRepoCrowdClaimsNoRelationship(t *testing.T) {
	var rows []string
	for i := 0; i < 20; i++ {
		rows = append(rows, `{"login":"dev`+string(rune('a'+i))+`","id":`+itoaTest(600+i)+
			`,"html_url":"https://github.com/x","type":"User","contributions":50}`)
	}
	s := newGitHubRepoServer(t, "["+strings.Join(rows, ",")+"]")
	got, err := s.adapter().Search(context.Background(), Scope{Max: 50,
		Fields: map[string]string{"repo": "big/project"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("every contributor still lands: %d", len(got))
	}
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Fatalf("a crowded repo claims no relationship: %+v", d.Edges)
		}
	}
}

func TestSplitRepoRef(t *testing.T) {
	for in, want := range map[string]string{
		"numpy/numpy":                          "numpy/numpy",
		"https://github.com/numpy/numpy":       "numpy/numpy",
		"https://github.com/numpy/numpy.git":   "numpy/numpy",
		"github.com/numpy/numpy/tree/main/doc": "numpy/numpy",
		"https://api.github.com/repos/a/b":     "a/b",
	} {
		owner, repo, err := SplitRepoRef(in)
		if err != nil || owner+"/"+repo != want {
			t.Errorf("%q → %q/%q, %v (want %q)", in, owner, repo, err, want)
		}
	}
	if _, _, err := SplitRepoRef("torvalds"); err == nil {
		t.Error("a bare user is not a repo")
	}
}

func names(ds []CandidateDraft) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
