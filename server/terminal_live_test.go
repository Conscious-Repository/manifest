package server

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHerdrAttachExecutableServicePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	binary := filepath.Join(home, ".local", "bin", "herdr")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := herdrExecutable()
	if err != nil || got != binary {
		t.Fatalf("service executable: %q, %v", got, err)
	}
	if err := os.Chmod(binary, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := herdrExecutable(); err == nil {
		t.Fatal("accepted nonexecutable herdr")
	}
}

func TestTerminalLiveInventoryIncludesOrphansNotHistory(t *testing.T) {
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) { herdrFixtureSnapshot(c, "idle", 1) })
	s := &Server{terminal: &termCfg{herdr: h, regPath: filepath.Join(t.TempDir(), "terminals.json"), run: func(...string) ([]byte, error) { return nil, nil }}}
	h.server = s
	history := termSession{ID: "abcdef12", Kind: "codex", Name: "dead history", Pinned: true}
	if err := s.terminal.upsertChecked(history); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleTermLive(w, httptest.NewRequest("GET", "/api/terminal/live", nil))
	var got struct {
		Sessions []terminalLiveRow `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || !strings.HasPrefix(got.Sessions[0].ID, "live:") || got.Sessions[0].Name == history.Name {
		t.Fatalf("inventory is registry history: %s", w.Body.String())
	}
	id, err := parseTerminalHandle(got.Sessions[0].Handle)
	if err != nil || id.Occupant != "t_1" {
		t.Fatal("missing exact backend handle")
	}
	if len(s.terminal.load()) != 1 {
		t.Fatal("orphan adopted into registry")
	}
}
func TestTerminalLiveCloseRejectsReplacementHandle(t *testing.T) {
	var closes atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		if r.Method == "pane.close" {
			closes.Add(1)
		}
		herdrFixtureSnapshot(c, "idle", 1)
	})
	s := &Server{terminal: &termCfg{herdr: h}}
	id := herdrFixtureID(t, h)
	id.Occupant = "old"
	body, _ := json.Marshal(map[string]string{"handle": terminalHandle(id)})
	w := httptest.NewRecorder()
	s.handleTermLiveClose(w, httptest.NewRequest("POST", "/api/terminal/live/close", strings.NewReader(string(body))))
	if w.Code != 502 || closes.Load() != 0 {
		t.Fatal("closed replacement pane")
	}
	for _, raw := range []string{"tmux:name", "herdr:e30", "herdr:" + strings.Repeat("a", 4096)} {
		if _, err := parseTerminalHandle(raw); err == nil {
			t.Fatal("accepted invalid handle")
		}
	}
}
func TestTerminalAgentCreateExplicitHerdrHandle(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
	mappingFixtureHerdr(t, s)
	w := httptest.NewRecorder()
	s.handleTermAgentCreate(w, httptest.NewRequest("POST", "/api/terminal/agent-session", strings.NewReader(`{"kind":"codex","backend":"herdr","model":"gpt-6-astra"}`)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if _, exists := got["tmux"]; exists {
		t.Fatal("herdr response fabricated tmux name")
	}
	if _, err := parseTerminalHandle(got["handle"].(string)); err != nil {
		t.Fatal(err)
	}
}
