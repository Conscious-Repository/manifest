package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func agentTermServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	tmuxTmp := filepath.Join(dir, "tmux")
	if err := os.MkdirAll(tmuxTmp, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Server{terminal: &termCfg{
		regPath: filepath.Join(dir, "terminals.json"), tmuxTmp: tmuxTmp, defaultWd: dir,
	}}
	t.Cleanup(func() { _ = s.terminal.tmux("kill-server") })
	return s
}

// A shell agent session registers a rail row AND comes up live in the same
// socket dir the browser attach uses.
func TestAgentTermSessionShell(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	s := agentTermServer(t)
	se, tn, err := s.createAgentTermSession("shell", s.terminal.defaultWd, "alfred · probe")
	if err != nil {
		t.Fatalf("createAgentTermSession: %v", err)
	}
	if tn != "manifest_"+se.ID {
		t.Fatalf("tmux name %q for id %q", tn, se.ID)
	}
	row, ok := s.terminal.find(se.ID)
	if !ok {
		t.Fatal("session not in registry")
	}
	if row.Name != "alfred · probe" || !row.Started {
		t.Fatalf("row = %+v; want name kept and Started", row)
	}
	if !s.terminal.liveSet()[tn] {
		t.Fatalf("tmux %s not live", tn)
	}

}

// A claude agent session mints the resume handle up front and, once spawned,
// resolves reopens to --resume (the handle Alfred drives with).
func TestAgentTermSessionClaudeMintsResume(t *testing.T) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not installed")
	}
	// strip PATH to tmux+bash only so the spawned pane can't launch a real
	// claude — the pane just exits, which is all this test needs.
	bin := t.TempDir()
	if os.Symlink(tmuxPath, filepath.Join(bin, "tmux")) != nil ||
		os.Symlink(bashPath, filepath.Join(bin, "bash")) != nil {
		t.Skip("cannot symlink")
	}
	t.Setenv("PATH", bin)

	s := agentTermServer(t)
	se, _, err := s.createAgentTermSession("claude", "", "")
	if err != nil {
		t.Fatalf("createAgentTermSession: %v", err)
	}
	if !resumeIDRe.MatchString(se.ResumeID) {
		t.Fatalf("resume id %q not mintable", se.ResumeID)
	}
	if !se.Started {
		t.Fatal("spawned session not marked Started")
	}
	if got, want := se.launchCmd(), "claude --resume "+se.ResumeID; got != want {
		t.Fatalf("launchCmd after start = %q; want %q", got, want)
	}
	if se.Name == "" {
		t.Fatal("no default name minted")
	}
}
