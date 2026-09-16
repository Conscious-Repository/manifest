package server

import (
	"os"
	"path/filepath"
	"testing"
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
}
