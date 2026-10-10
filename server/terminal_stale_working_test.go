package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A runtime "working" guessed from a frozen window title gives way to the
// agent's own recorded turn end once the transcript has been quiet (owner
// 2026-10-11: "WORKING 7h 47m" on a finished chat). A turn still in a tool
// call stays working however quiet it is, and a fresh file is never second-
// guessed.
func TestSettleStaleWorking(t *testing.T) {
	wd, projects := t.TempDir(), t.TempDir()
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: wd, claudeProjects: projects}}
	se := termSession{ID: "t1", Kind: "claude", Cwd: wd, ResumeID: "3e36da10-c163-3952-95d4-10eadeb2fd97"}
	path := s.terminal.transcriptPath(se)
	if path == "" {
		t.Fatal("no transcript path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(stop string, at time.Time) {
		ts := at.UTC().Format(time.RFC3339Nano)
		body := `{"type":"user","uuid":"u1","timestamp":"` + ts + `","message":{"role":"user","content":"hi"}}` + "\n" +
			`{"type":"assistant","uuid":"a1","timestamp":"` + ts + `","message":{"role":"assistant","model":"claude-opus-5-5","stop_reason":"` + stop + `","content":[{"type":"text","text":"done"}]}}` + "\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	working := terminalObservation{AgentState: "working", Connectivity: "connected", Process: "running"}
	old := time.Now().Add(-8 * time.Hour)

	write("end_turn", old)
	if got := s.settleStaleWorking(se, working).AgentState; got != "idle" {
		t.Fatalf("ended turn, quiet 8h: got %q, want idle", got)
	}
	write("tool_use", old.Add(time.Second))
	if got := s.settleStaleWorking(se, working).AgentState; got != "working" {
		t.Fatalf("turn still in a tool call: got %q, want working", got)
	}
	write("end_turn", time.Now())
	if got := s.settleStaleWorking(se, working).AgentState; got != "working" {
		t.Fatalf("turn ended just now: got %q, want working (not yet quiet)", got)
	}
	write("end_turn", old.Add(2*time.Second))
	blocked := working
	blocked.AgentState = "blocked"
	if got := s.settleStaleWorking(se, blocked).AgentState; got != "blocked" {
		t.Fatalf("only working is settled: got %q", got)
	}
	board := se
	board.BoardBrief = "/x/brief.md"
	if got := s.settleStaleWorking(board, working).AgentState; got != "working" {
		t.Fatalf("board runs keep their own handling: got %q", got)
	}
}
