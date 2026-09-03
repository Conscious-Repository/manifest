package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/recruiting"
)

// The sync route end to end through the real capability layer: the public
// board is an httptest server (no test makes a network call), the writes go
// through `aion-recruiting`, the response carries what moved plus the view,
// and `## criteria` in every role file is byte-unchanged.
func TestRecruitingRolesSyncMirrorsPublicBoard(t *testing.T) {
	s, _, vault, _ := testRecruitingServer(t)
	fixture, err := os.ReadFile(filepath.Join("..", "recruiting", "testdata", "ashby-jobboard.json"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the public mirror sent a credential")
		}
		_, _ = w.Write(fixture)
	}))
	defer board.Close()
	s.UseAshbyPublic(recruiting.NewAshbyPublic(board.URL, board.Client()))

	dir := filepath.Join(vault, "system/aion/recruiting/roles")
	criteria := map[string]string{}
	for _, slug := range []string{"mri-engineer", "mechanical-engineer", "biomedical-engineer", "scientist-microscopy"} {
		b, err := os.ReadFile(filepath.Join(dir, slug+".md"))
		if err != nil {
			t.Fatal(err)
		}
		criteria[slug] = criteriaSection(string(b))
		if criteria[slug] == "" {
			t.Fatalf("%s: no criteria section to guard", slug)
		}
	}

	w := recruitingPost(t, s, s.handleRecruitingRolesSync, "/api/aion/recruiting/roles/sync", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("board fetched %d times", calls)
	}
	var out struct {
		Sync recruiting.AshbySyncResult `json:"sync"`
		View recruiting.View            `json:"view"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	if out.Sync.Postings != 4 || len(out.Sync.Updated) != 4 || len(out.Sync.Created) != 0 {
		t.Fatalf("sync: %+v", out.Sync)
	}
	if len(out.View.Roles) != 4 {
		t.Fatalf("view roles: %d", len(out.View.Roles))
	}
	for _, role := range out.View.Roles {
		if role.Source != recruiting.SourceAshbyPublic || role.AshbyPostingID == "" || role.Synced == "" {
			t.Errorf("%s: source=%q posting=%q synced=%q", role.Slug, role.Source, role.AshbyPostingID, role.Synced)
		}
		if !strings.Contains(role.Posting, "The role") {
			t.Errorf("%s: posting not mirrored", role.Slug)
		}
		if len(role.Criteria) == 0 {
			t.Errorf("%s: criteria vanished", role.Slug)
		}
	}
	for slug, was := range criteria {
		b, _ := os.ReadFile(filepath.Join(dir, slug+".md"))
		if got := criteriaSection(string(b)); got != was {
			t.Errorf("%s: `## criteria` changed across sync:\n%s\n---\n%s", slug, was, got)
		}
	}
}

// An upstream failure is a 502 that names the failure, and no role moves.
func TestRecruitingRolesSyncUpstreamFailureWritesNothing(t *testing.T) {
	s, _, vault, _ := testRecruitingServer(t)
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer board.Close()
	s.UseAshbyPublic(recruiting.NewAshbyPublic(board.URL, board.Client()))

	before, _ := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/roles/mri-engineer.md"))
	w := recruitingPost(t, s, s.handleRecruitingRolesSync, "/api/aion/recruiting/roles/sync", "", "")
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "HTTP 503") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	after, _ := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/roles/mri-engineer.md"))
	if string(before) != string(after) {
		t.Fatalf("a failed fetch changed a role record")
	}
}

// criteriaSection is the verbatim `## criteria` block, heading through the
// line before the next heading.
func criteriaSection(content string) string {
	at := strings.Index(content, "## criteria\n")
	if at < 0 {
		return ""
	}
	rest := content[at:]
	if next := strings.Index(rest[len("## criteria\n"):], "\n## "); next >= 0 {
		rest = rest[:len("## criteria\n")+next+1]
	}
	return rest
}
