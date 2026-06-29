package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newQ(t *testing.T) *Queue {
	t.Helper()
	q, err := NewQueue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// A reader scanning inbox/ must never observe a partial file: Post writes to
// tmp/ and atomically renames into inbox/, so files appear only when complete.
func TestAtomicHandoff(t *testing.T) {
	q := newQ(t)
	body := strings.Repeat("x", 200_000) + "\nEND"
	done := make(chan struct{})
	go func() {
		for i := 0; i < 30; i++ {
			if _, err := q.Post(Task{Type: "digest", Body: body}); err != nil {
				t.Errorf("post: %v", err)
			}
		}
		close(done)
	}()
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		entries, _ := os.ReadDir(q.dir("inbox"))
		for _, e := range entries {
			content, err := os.ReadFile(q.dir("inbox", e.Name()))
			if err != nil {
				continue
			}
			if len(content) > 0 && !strings.HasSuffix(strings.TrimRight(string(content), "\n"), "END") {
				t.Fatalf("partial file observed in inbox (%d bytes, no END marker)", len(content))
			}
		}
	}
}

// N goroutines racing to claim the same inbox item: exactly one wins.
func TestClaimExactlyOneWinner(t *testing.T) {
	q := newQ(t)
	if _, err := q.Post(Task{Type: "x", Body: "hello"}); err != nil {
		t.Fatal(err)
	}
	const N = 50
	var wg sync.WaitGroup
	var winners int64
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, ok, _ := q.Claim(fmt.Sprintf("w%d", i)); ok {
				atomic.AddInt64(&winners, 1)
			}
		}(i)
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
	if countMD(q.dir("inbox")) != 0 {
		t.Fatal("the task should have left inbox")
	}
}

// A stale claim (older than the timeout) returns to inbox on Sweep; a fresh one
// does not.
func TestSweepReclaimsStale(t *testing.T) {
	q := newQ(t)
	if _, err := q.Post(Task{ID: "t1", Type: "x", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	task, ok, _ := q.Claim("w1")
	if !ok {
		t.Fatal("claim failed")
	}
	claimed := q.dir("claimed", "w1", task.ID+".md")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(claimed, old, old); err != nil {
		t.Fatal(err)
	}
	if n := q.Sweep(30 * time.Minute); n != 1 {
		t.Fatalf("expected 1 reclaimed, got %d", n)
	}
	if countMD(q.dir("inbox")) != 1 {
		t.Fatal("reclaimed task should be back in inbox")
	}
	if _, ok, _ := q.Claim("w2"); !ok {
		t.Fatal("re-claim failed")
	}
	if n := q.Sweep(30 * time.Minute); n != 0 {
		t.Fatalf("a fresh claim must not be reclaimed, got %d", n)
	}
}

// Replaying a completed id is a no-op: the worker checks the done/ set first.
func TestIdempotentReplay(t *testing.T) {
	q := newQ(t)
	if _, err := q.Post(Task{ID: "job1", Type: "x", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	task, ok, _ := q.Claim("w1")
	if !ok {
		t.Fatal("claim failed")
	}
	exec := 0
	run := func(id string) {
		if q.IsDone(id) {
			return // idempotency guard
		}
		exec++
		_ = q.Complete("w1", id)
	}
	run(task.ID)
	run(task.ID) // replay
	if exec != 1 {
		t.Fatalf("expected execution exactly once, got %d", exec)
	}
	if !q.IsDone("job1") {
		t.Fatal("job1 should be done")
	}
}

// No irreversible action executes while a proposal is pending; execution (here
// stubbed) is only possible after confirm.
func TestApprovalsGate(t *testing.T) {
	q := newQ(t)
	executed := 0

	prop, err := q.Propose(Approval{Action: "send-email", Agent: "hermes", Body: "Send the drafted reply."})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Approvals("pending")) != 1 {
		t.Fatal("proposal should be pending")
	}
	if executed != 0 {
		t.Fatal("must not execute while pending")
	}

	rej, _ := q.Propose(Approval{Action: "spend", Agent: "hermes", Body: "Buy thing"})
	if err := q.Reject(rej.ID, "no"); err != nil {
		t.Fatal(err)
	}
	if len(q.Approvals("rejected")) != 1 {
		t.Fatal("rejected proposal should be in rejected/")
	}

	if err := q.Confirm(prop.ID); err != nil {
		t.Fatal(err)
	}
	approved := q.Approvals("approved")
	if len(approved) != 1 || approved[0].ID != prop.ID {
		t.Fatalf("proposal should be approved: %+v", approved)
	}
	for range approved { // stubbed executor: runs only for approved items
		executed++
	}
	if executed != 1 {
		t.Fatalf("execute exactly once after confirm, got %d", executed)
	}
	if len(q.Approvals("pending")) != 0 {
		t.Fatal("no pending proposals should remain")
	}
}

func TestParseAgentDef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hermes.md")
	content := "---\ntype: agent\nmodel: claude-haiku\nschedule: \"0 7 * * *\"\n" +
		"tools: [read-vault, write-outbox]\npermissions: [read, propose]\nhandles: [digest, triage]\n---\n" +
		"You are Hermes. Write a digest.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := ParseAgentDef(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "hermes" || d.Model != "claude-haiku" {
		t.Fatalf("scalars wrong: %+v", d)
	}
	if len(d.Tools) != 2 || d.Tools[0] != "read-vault" {
		t.Fatalf("tools: %v", d.Tools)
	}
	if len(d.Handles) != 2 || d.Handles[1] != "triage" {
		t.Fatalf("handles: %v", d.Handles)
	}
	if !strings.Contains(d.Brief, "Write a digest") {
		t.Fatalf("brief: %q", d.Brief)
	}
}
