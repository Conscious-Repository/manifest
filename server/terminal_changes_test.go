package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkingChangesReadOnlyReview(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	git("init")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "user.name", "Fixture")
	file := filepath.Join(dir, "example.txt")
	os.WriteFile(file, []byte("base\n"), 0600)
	git("add", "example.txt")
	git("commit", "-m", "fixture")
	os.WriteFile(file, []byte("staged\n"), 0600)
	git("add", "example.txt")
	os.WriteFile(file, []byte("staged\nunstaged\n"), 0600)
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("UNTRACKED_CONTENT_MUST_NOT_APPEAR"), 0600)
	git("config", "diff.external", "false") // Review must not execute this failing external helper.
	text, err := workingChanges(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-base", "+staged", "+unstaged", "untracked.txt", "not attributed exclusively"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing", want, text)
		}
	}
	if strings.Contains(text, "UNTRACKED_CONTENT_MUST_NOT_APPEAR") {
		t.Fatal("untracked contents included")
	}
	s, _ := workspaceFixture(t)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	s.terminal.upsert(termSession{ID: "abcdef123456", Kind: "codex", Backend: "herdr", Cwd: dir, LaunchPhase: "draft"})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/terminal/session/abcdef123456/changes", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(w.Body.String(), "+unstaged") {
		t.Fatal(w.Code, w.Body.String())
	}
	snapshot := httptest.NewRecorder()
	s.Handler().ServeHTTP(snapshot, httptest.NewRequest("POST", "/api/terminal/session/abcdef123456/changes/snapshot", strings.NewReader("{}")))
	if snapshot.Code != 200 {
		t.Fatal(snapshot.Code, snapshot.Body.String())
	}
	var saved struct{ ID, Revision, Task string }
	if err := json.Unmarshal(snapshot.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	refs := []artifactContextRef{{ID: saved.ID, Revision: saved.Revision}}
	se, _ := s.terminal.find("abcdef123456")
	before, err := s.scopedArtifactContext("", s.runtimeArtifactScope(se), refs, nil)
	if err != nil || !strings.Contains(before, "+unstaged") {
		t.Fatal("standalone snapshot unavailable", err)
	}
	if _, err := s.scopedArtifactContext("", "unrelated-conversation", refs, nil); err == nil {
		t.Fatal("snapshot leaked into unrelated context")
	}
	if _, err := s.scopedArtifactContext("", "explicit-related", refs, refs); err != nil {
		t.Fatal("explicit version handoff failed", err)
	}
	if err := os.WriteFile(file, []byte("later working bytes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := s.scopedArtifactContext("", s.runtimeArtifactScope(se), refs, nil)
	if err != nil || after != before {
		t.Fatal("snapshot changed with working file", err)
	}
	cmd := exec.Command("git", "show", ":example.txt")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || string(out) != "staged\n" {
		t.Fatal("index changed", string(out), err)
	}
	if _, err := workingChanges(context.Background(), t.TempDir()); err == nil {
		t.Fatal("nonrepository accepted")
	}
}

func TestPortalCannotReadRuntimeChanges(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/terminal/session/abcdef123456/changes", nil))
	if w.Code == 200 {
		t.Fatal("private changes exposed")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/terminal/session/abcdef123456/changes/snapshot", strings.NewReader("{}")))
	if w.Code == 200 {
		t.Fatal("portal created private snapshot")
	}
}

func TestWorkingChangesOutputLimit(t *testing.T) {
	var b changeOutput
	p := []byte(strings.Repeat("x", 2*1024*1024))
	n, err := b.Write(p)
	if err != nil || n != len(p) || !b.overflow || b.Len() != 1536*1024 {
		t.Fatal(n, err, b.Len())
	}
}
