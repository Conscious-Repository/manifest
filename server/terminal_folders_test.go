package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The folder chip's choices: the home folder, git repositories directly under
// ~/src (not plain folders), and the folders local coding sessions ran in,
// newest first, once each; remote-device and shell sessions are not offered.
func TestTermFoldersForTheNewChatFolderChip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{"src/manifest/.git", "src/lab-apps/.git", "src/notes", "src/.hidden/.git"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := fakeTmuxServer(t)
	s.terminal.defaultWd = home
	for _, se := range []termSession{
		{ID: "a1a1a1a1a1a1a1a1", Kind: "codex", Cwd: "/work/old", LastUsed: "2026-09-20T10:00:00Z"},
		{ID: "b2b2b2b2b2b2b2b2", Kind: "claude", Cwd: "/work/new/", LastUsed: "2026-09-26T10:00:00Z"},
		{ID: "c3c3c3c3c3c3c3c3", Kind: "codex", Cwd: "/work/old", LastUsed: "2026-09-25T10:00:00Z"},
		{ID: "d4d4d4d4d4d4d4d4", Kind: "codex", Cwd: "/remote/box", Device: "mac", LastUsed: "2026-09-27T10:00:00Z"},
		{ID: "e5e5e5e5e5e5e5e5", Kind: "shell", Cwd: "/tmp", LastUsed: "2026-09-27T11:00:00Z"},
	} {
		s.terminal.upsert(se)
	}
	w := httptest.NewRecorder()
	s.handleTermFolders(w, httptest.NewRequest("GET", "/api/terminal/folders", nil))
	var out struct {
		Home   string   `json:"home"`
		Repos  []string `json:"repos"`
		Recent []string `json:"recent"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Home != home {
		t.Fatalf("home = %q", out.Home)
	}
	if want := []string{filepath.Join(home, "src/lab-apps"), filepath.Join(home, "src/manifest")}; !reflect.DeepEqual(out.Repos, want) {
		t.Fatalf("repos = %v, want %v", out.Repos, want)
	}
	if want := []string{"/work/new", "/work/old"}; !reflect.DeepEqual(out.Recent, want) {
		t.Fatalf("recent = %v, want %v", out.Recent, want)
	}
}
