package server

import (
	"manifest/agentchat"
	"manifest/threads"
	"testing"
	"time"
)

func TestCodingResultsReturnOnlyToExplicitOrigin(t *testing.T) {
	s := codingFixture(t)
	s.threads = loopFixture(t).threads
	sess := agentchat.Session{Agent: "alfred", ID: "20260909-100000-abcd", Task: "inbox/current"}
	link := func(task, id string) {
		t.Helper()
		_, err := s.addThreadEntry(s.ownerIdentity(), task, threads.ActComment, "Origin", nil, nil, map[string]any{"chat": map[string]any{"agent": "alfred", "id": id, "canonical": true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	link("inbox/older", sess.ID)
	link("inbox/conflict", sess.ID)
	link("inbox/conflict", "20260909-100001-abcd")
	h := s.findHarness("codex")
	for i, task := range []string{"inbox/current", "inbox/older", "inbox/conflict", "inbox/unrelated", "inbox/older] [todo:: inbox/unrelated"} {
		if err := boardReport(h, boardRunID(), task, "go", "", "completed", "Result for "+task, time.Now().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	running := boardRunID()
	if err := boardReport(h, running, "inbox/current", "go", "", "running", "Not a result", time.Now()); err != nil {
		t.Fatal(err)
	}
	results := s.chatCodingResults(sess)
	if len(results) != 2 || results[0].Task != "inbox/current" || results[1].Task != "inbox/older" {
		t.Fatalf("wrong results: %+v", results)
	}
	for _, r := range results {
		if r.Agent != "codex" || r.Body == "" || r.Outcome != "completed" {
			t.Fatal(r)
		}
	}
	// Projection reads must not create copied task comments or mutate reports.
	if got := len(s.listThread("inbox/older")); got != 1 {
		t.Fatal("copied result", got)
	}
	if err := boardReport(h, results[0].ID, "inbox/unrelated", "go", "", "completed", "Reattributed", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := s.chatCodingResults(sess); len(got) != 1 || got[0].Task != "inbox/older" {
		t.Fatal("stale relationship", got)
	}
	sess.Task = "inbox/conflict"
	if got := s.chatCodingResults(sess); len(got) != 1 {
		t.Fatal("pointer overruled explicit origins", got)
	}
}
