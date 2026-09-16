package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A board run the CLI abandoned reads as failed with the CLI's own message;
// a completed turn, a written result, or a run still in flight do not.
func TestBoardRunFailureFromEvents(t *testing.T) {
	write := func(dir, name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	auth := t.TempDir()
	write(auth, "events.jsonl", `{"type":"thread.started","thread_id":"x"}
{"type":"turn.started"}
{"type":"error","message":"Your access token could not be refreshed"}
{"type":"turn.failed","error":{"message":"Your access token could not be refreshed"}}
`)
	write(auth, "exit", "1")
	ev := boardRunFailure(auth)
	if ev == nil || ev.State != "failed" || ev.Error != "Your access token could not be refreshed" || ev.ID != filepath.Base(auth) {
		t.Fatalf("auth failure: %+v", ev)
	}

	done := t.TempDir()
	write(done, "events.jsonl", `{"type":"turn.started"}
{"type":"turn.completed"}
`)
	write(done, "exit", "0")
	if ev := boardRunFailure(done); ev != nil {
		t.Fatalf("completed turn read as failed: %+v", ev)
	}

	crashed := t.TempDir()
	write(crashed, "events.jsonl", `{"type":"turn.started"}
`)
	write(crashed, "exit", "137")
	if ev := boardRunFailure(crashed); ev == nil || ev.Error == "" {
		t.Fatalf("exit without result must read as failed: %+v", ev)
	}

	running := t.TempDir()
	write(running, "events.jsonl", `{"type":"turn.started"}
`)
	if ev := boardRunFailure(running); ev != nil {
		t.Fatalf("in-flight run read as failed: %+v", ev)
	}

	resulted := t.TempDir()
	write(resulted, "events.jsonl", `{"type":"error","message":"late noise"}
`)
	write(resulted, "result.json", `{"status":"completed","summary":"ok"}`)
	if ev := boardRunFailure(resulted); ev != nil {
		t.Fatalf("a written result wins: %+v", ev)
	}
}

// The launch prompt renders as the work order it points at; the thread's run
// evidence carries the abandonment.
func TestBoardTranscriptOverlay(t *testing.T) {
	dir := t.TempDir()
	brief := filepath.Join(dir, "brief.md")
	if err := os.WriteFile(brief, []byte("MODEL: gpt\n\n# Board work order\n\nTASK: fix the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(`{"type":"turn.failed","error":{"message":"boom"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	tr := termTranscript{Turns: []termTurn{{Who: "user", Text: "Read the complete work order at " + brief + " and carry it through."}, {Who: "assistant"}}}
	full := tr
	s.boardTranscriptOverlay(termSession{BoardBrief: brief}, &tr, &full)
	if !tr.Turns[0].WorkOrder || tr.Turns[0].Text == "" || tr.Turns[0].Text[:6] != "MODEL:" {
		t.Fatalf("work order not substituted: %+v", tr.Turns[0])
	}
	if full.Run == nil || full.Run.State != "failed" || full.Run.Error != "boom" {
		t.Fatalf("failure evidence: %+v", full.Run)
	}
	// the owner reopened the session and the agent answered after the failure:
	// the launch failure is history, the thread is not "failed"
	later := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	tr2 := termTranscript{Turns: []termTurn{{Who: "user", Text: "try now?", TS: later}, {Who: "assistant", TS: later}}}
	full2 := tr2
	s.boardTranscriptOverlay(termSession{BoardBrief: brief}, &tr2, &full2)
	if full2.Run != nil {
		t.Fatalf("superseded failure still reported: %+v", full2.Run)
	}
	stale := termTranscript{Turns: []termTurn{{Who: "assistant", TS: "2020-01-01T00:00:00Z"}}}
	if ev := boardRunFailure(dir); ev == nil || boardFailureSuperseded(ev, stale) {
		t.Fatal("an older assistant turn must not clear the failure")
	}
}

// A run the CLI never started reads as exactly that, with the CLI's reason
// and the fix, not as unverified work plus a checkout-wide diff.
func TestCodingRecoveryNamesTheCause(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(`{"type":"thread.started","thread_id":"x"}
{"type":"turn.started"}
{"type":"error","message":"Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again."}
{"type":"turn.failed","error":{"message":"Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again."}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exit"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), codingRepo: t.TempDir()}}
	body := s.codingRecovery(dir, "nope")
	for _, want := range []string{"did not start", "refresh token was already used", "codex login", "untouched"} {
		if !strings.Contains(body, want) {
			t.Fatalf("recovery lacks %q:\n%s", want, body)
		}
	}
	for _, not := range []string{"Uncommitted work", "Reopen the coding session"} {
		if strings.Contains(body, not) {
			t.Fatalf("recovery for a run that never started still says %q:\n%s", not, body)
		}
	}
	// a run that did work and then died keeps the reopen path, led by the cause
	worked := t.TempDir()
	if err := os.WriteFile(filepath.Join(worked, "events.jsonl"), []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"working"}}
{"type":"turn.failed","error":{"message":"stream disconnected"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body = s.codingRecovery(worked, "nope")
	if !strings.Contains(body, "stream disconnected") || !strings.Contains(body, "Reopen the coding session") {
		t.Fatalf("mid-way failure:\n%s", body)
	}
}
