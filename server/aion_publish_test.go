package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
)

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// aionPublishFixture: a seeded vault store + a checkout cloned from a bare
// remote (one initial commit on main).
func aionPublishFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	vault := t.TempDir()
	dataDir := t.TempDir()
	scratch := t.TempDir()
	remote := filepath.Join(scratch, "remote.git")
	checkout := filepath.Join(scratch, "aionbio")

	if err := os.MkdirAll(filepath.Join(vault, "system", "aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, seed := range aion.SeedFiles {
		if err := os.WriteFile(filepath.Join(vault, "system", "aion", name), []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(t, scratch, "-c", "init.defaultBranch=main", "init", "--bare", remote)
	git(t, scratch, "clone", remote, checkout)
	git(t, checkout, "checkout", "-b", "main")
	// untracked clutter at root — must never block or leak into a commit
	if err := os.WriteFile(filepath.Join(checkout, "Medbed_TAM_Model.xlsx"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("aionbio\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "add", "README.md")
	git(t, checkout, "commit", "-m", "init")
	git(t, checkout, "push", "-u", "origin", "main")

	store := aion.NewStore(vault, "system/aion", func(abs string, data []byte) error {
		return os.WriteFile(abs, data, 0o644)
	})
	srv := &Server{}
	srv.UseAion(store, checkout, "origin", "main", dataDir)
	return srv, checkout, remote
}

func doJSON(t *testing.T, h http.HandlerFunc, method, url, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestAionPublishEndToEnd(t *testing.T) {
	srv, checkout, remote := aionPublishFixture(t)

	// preview: all nine files new, no blockers, hash present
	code, prev := doJSON(t, srv.handleAionPublishPreview, "GET", "/api/aion/publish/preview", "")
	if code != 200 {
		t.Fatalf("preview: %d %v", code, prev)
	}
	if b, _ := prev["blockers"].([]any); len(b) != 0 {
		t.Fatalf("blockers: %v", prev["blockers"])
	}
	hash, _ := prev["hash"].(string)
	if hash == "" {
		t.Fatal("no hash")
	}
	// preview wrote NOTHING
	if _, err := os.Stat(filepath.Join(checkout, "public")); err == nil {
		t.Fatal("preview touched the checkout")
	}

	// stale hash → 409
	req := httptest.NewRequest("POST", "/api/aion/publish", strings.NewReader(`{"hash":"stale"}`))
	rec := httptest.NewRecorder()
	srv.handleAionPublish(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d", rec.Code)
	}

	// confirm with the real hash
	code, res := doJSON(t, srv.handleAionPublish, "POST", "/api/aion/publish", `{"hash":"`+hash+`"}`)
	if code != 200 || res["ok"] != true {
		t.Fatalf("publish: %d %v", code, res)
	}
	commit, _ := res["commit"].(string)
	if len(commit) < 7 {
		t.Fatalf("commit: %q", commit)
	}
	// the commit reached the bare remote
	if remoteHead := git(t, remote, "rev-parse", "refs/heads/main"); remoteHead != commit {
		t.Fatalf("remote head %s != %s", remoteHead, commit)
	}
	// commit contents = contract paths ONLY (the untracked xlsx never rides along)
	shown := git(t, checkout, "show", "--name-only", "--format=", "HEAD")
	for _, line := range strings.Split(shown, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "public/portal/") {
			t.Fatalf("non-contract path in commit: %q", line)
		}
	}
	if strings.Contains(shown, "xlsx") {
		t.Fatal("clutter committed")
	}
	// receipt recorded ok with the hash
	recs := srv.publishLog().list()
	if len(recs) != 1 || recs[0].Status != "ok" || recs[0].Commit != commit {
		t.Fatalf("receipts: %+v", recs)
	}

	// idempotent: nothing to publish now
	_, prev2 := doJSON(t, srv.handleAionPublishPreview, "GET", "/api/aion/publish/preview", "")
	hash2, _ := prev2["hash"].(string)
	code, res2 := doJSON(t, srv.handleAionPublish, "POST", "/api/aion/publish", `{"hash":"`+hash2+`"}`)
	if code == 200 && res2["ok"] == true {
		t.Fatalf("empty publish accepted: %v", res2)
	}
}

func TestAionPublishScopedDirtyCheck(t *testing.T) {
	srv, checkout, _ := aionPublishFixture(t)
	// first publish to create the contract paths
	_, prev := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	hash, _ := prev["hash"].(string)
	if code, res := doJSON(t, srv.handleAionPublish, "POST", "/x", `{"hash":"`+hash+`"}`); code != 200 {
		t.Fatalf("seed publish: %v", res)
	}
	// a SIBLING file in the contract dirs (roadmap.js is hand-maintained
	// portal-side by contract) must NOT block — publish never touches it
	roadmap := filepath.Join(checkout, "public", "portal", "data", "roadmap.js")
	if err := os.WriteFile(roadmap, []byte("window.ROADMAP_DATA = {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "add", "--", "public/portal/data/roadmap.js")
	git(t, checkout, "commit", "-m", "roadmap baseline")
	if err := os.WriteFile(roadmap, []byte("window.ROADMAP_DATA = {edited: 1};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, prevR := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	if b, _ := prevR["blockers"].([]any); len(b) != 0 {
		t.Fatalf("sibling roadmap.js edit blocked publish: %v", b)
	}
	git(t, checkout, "checkout", "--", "public/portal/data/roadmap.js")

	// a human edit to a CONTRACT file blocks, with the path spelled intact
	if err := os.WriteFile(filepath.Join(checkout, "public", "portal", "data", "people.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, prev2 := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	blockers, _ := prev2["blockers"].([]any)
	if len(blockers) == 0 {
		t.Fatal("in-contract edit did not block")
	}
	if !strings.Contains(blockers[0].(string), "public/portal/data/people.json") {
		t.Fatalf("blocker path mangled: %v", blockers[0])
	}
	// clean it up → root clutter alone never blocks
	git(t, checkout, "checkout", "--", "public/portal/data/people.json")
	_, prev3 := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	if b, _ := prev3["blockers"].([]any); len(b) != 0 {
		t.Fatalf("root clutter blocked: %v", b)
	}
}

func TestAionPublishUntrackedBaselineFlow(t *testing.T) {
	srv, checkout, _ := aionPublishFixture(t)
	// pre-existing portal work git doesn't know about (the aionbio-side drop)
	legacy := filepath.Join(checkout, "public", "portal", "data", "backlog.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{\"items\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// preview: NOT a blocker — surfaced as untracked
	_, prev := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	if b, _ := prev["blockers"].([]any); len(b) != 0 {
		t.Fatalf("untracked treated as blocker: %v", b)
	}
	un, _ := prev["untracked"].([]any)
	if len(un) == 0 {
		t.Fatalf("untracked not surfaced: %v", prev)
	}
	// publish refuses while unpreserved, pointing at the fix
	hash, _ := prev["hash"].(string)
	_, res := doJSON(t, srv.handleAionPublish, "POST", "/x", `{"hash":"`+hash+`"}`)
	if res["ok"] != false || !strings.Contains(res["error"].(string), "baseline") {
		t.Fatalf("publish with untracked: %v", res)
	}
	// the baseline action preserves everything verbatim in its own commit
	code, bres := doJSON(t, srv.handleAionPublishBaseline, "POST", "/x", "")
	if code != 200 || bres["ok"] != true {
		t.Fatalf("baseline: %d %v", code, bres)
	}
	baselineCommit, _ := bres["commit"].(string)
	if shown := git(t, checkout, "show", "--name-only", "--format=", baselineCommit); !strings.Contains(shown, "public/portal/data/backlog.json") {
		t.Fatalf("baseline commit missing the file:\n%s", shown)
	}
	// clean now → publish goes through and the old bytes survive in history
	_, prev2 := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	if u, _ := prev2["untracked"].([]any); len(u) != 0 {
		t.Fatalf("still untracked after baseline: %v", u)
	}
	hash2, _ := prev2["hash"].(string)
	if code, res2 := doJSON(t, srv.handleAionPublish, "POST", "/x", `{"hash":"`+hash2+`"}`); code != 200 || res2["ok"] != true {
		t.Fatalf("publish after baseline: %d %v", code, res2)
	}
	if old := git(t, checkout, "show", baselineCommit+":public/portal/data/backlog.json"); old != "{\"items\":[]}" {
		t.Fatalf("pre-manifest bytes lost from history: %q", old)
	}
	// baseline with nothing to preserve refuses
	if code, _ := doJSON(t, srv.handleAionPublishBaseline, "POST", "/x", ""); code == 200 {
		t.Fatal("empty baseline accepted")
	}
}

func TestAionPublishBranchMismatchRefused(t *testing.T) {
	srv, checkout, _ := aionPublishFixture(t)
	git(t, checkout, "checkout", "-b", "feature")
	_, prev := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	blockers, _ := prev["blockers"].([]any)
	found := false
	for _, b := range blockers {
		if strings.Contains(b.(string), "configured branch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("branch mismatch not blocked: %v", blockers)
	}
}

func TestAionPublishPushFailureRecovery(t *testing.T) {
	srv, checkout, _ := aionPublishFixture(t)
	// break the remote AFTER clone
	git(t, checkout, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nowhere.git"))
	_, prev := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	hash, _ := prev["hash"].(string)
	code, res := doJSON(t, srv.handleAionPublish, "POST", "/x", `{"hash":"`+hash+`"}`)
	if code != 200 || res["ok"] == true || res["stage"] != "push" {
		t.Fatalf("push failure: %d %v", code, res)
	}
	// the failed receipt still carries the local commit hash
	recs := srv.publishLog().list()
	if len(recs) != 1 || recs[0].Status != "failed" || recs[0].Stage != "push" || recs[0].Commit == "" {
		t.Fatalf("failed receipt: %+v", recs)
	}
	// fix the remote → re-publish completes push-only (no new file changes)
	fixed := filepath.Join(t.TempDir(), "fixed.git")
	git(t, checkout, "-c", "init.defaultBranch=main", "init", "--bare", fixed)
	git(t, checkout, "remote", "set-url", "origin", fixed)
	_, prev2 := doJSON(t, srv.handleAionPublishPreview, "GET", "/x", "")
	if up, _ := prev2["unpushed"].(float64); up < 1 {
		t.Fatalf("unpushed not surfaced: %v", prev2["unpushed"])
	}
	hash2, _ := prev2["hash"].(string)
	code, res2 := doJSON(t, srv.handleAionPublish, "POST", "/x", `{"hash":"`+hash2+`"}`)
	if code != 200 || res2["ok"] != true {
		t.Fatalf("push-only recovery: %d %v", code, res2)
	}
	if head := git(t, fixed, "rev-parse", "refs/heads/main"); head == "" {
		t.Fatal("recovery push never landed")
	}
}

func TestWriteAionContractCanary(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"src/index.html",
		"../escape.json",
		"public/portal/data/../../../etc/passwd",
		"public/portal/data/extra.json",
		"/abs/public/portal/data/meta.json",
	} {
		if err := writeAionContract(root, rel, []byte("x")); err == nil {
			t.Errorf("canary accepted %q", rel)
		}
	}
	// nothing was created
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("canary wrote files: %v", entries)
	}
	// a legal path works
	if err := writeAionContract(root, "public/portal/data/meta.json", []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
}

func TestAionPublishAbsentVaultNo500(t *testing.T) {
	// store over a vault with NO system/aion — snapshot + preview must not 500
	vault := t.TempDir()
	store := aion.NewStore(vault, "system/aion", nil)
	srv := &Server{}
	srv.UseAion(store, "", "origin", "main", t.TempDir())
	req := httptest.NewRequest("GET", "/api/aion", nil)
	rec := httptest.NewRecorder()
	srv.handleAion(rec, req)
	if rec.Code != 200 {
		t.Fatalf("empty-vault snapshot: %d %s", rec.Code, rec.Body.String())
	}
}
