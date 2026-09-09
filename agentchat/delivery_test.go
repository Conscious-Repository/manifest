package agentchat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeliveryRestartRetainsQueueWithoutReplayingRunning(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "m")
	if _, err := s.Accept("alfred", id, "request-first", "first"); err != nil {
		t.Fatal(err)
	}
	d, claimed, err := s.Claim("alfred", id)
	if err != nil || !claimed {
		t.Fatal(err)
	}
	if _, err := s.Accept("alfred", id, "request-second", "second"); err != nil {
		t.Fatal(err)
	}
	fresh := New(s.Root())
	fresh.Recover()
	fresh.Recover()
	r, ok := fresh.Receipt("alfred", id, d.ID)
	if !ok || r.State != DeliveryInterrupted {
		t.Fatal(r)
	}
	if q := fresh.Queued("alfred", id); len(q) != 1 || q[0] != "second" {
		t.Fatal(q)
	}
	if a, err := fresh.Accept("alfred", id, "request-first", "first"); err != nil || a.New || a.Delivery.State != DeliveryInterrupted {
		t.Fatal(a, err)
	}
	next, claimed, err := fresh.Claim("alfred", id)
	if err != nil || !claimed || next.ID != "request-second" {
		t.Fatal(next, err)
	}
	if err := fresh.Finish("alfred", id, next.ID, "alfred", "answer", DeliveryCompleted, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Finish("alfred", id, next.ID, "alfred", "answer", DeliveryCompleted, "", 0); err != nil {
		t.Fatal(err)
	}
	_, body, _, _ := fresh.Get("alfred", id)
	turns := ParseTurns(body)
	if len(turns) != 4 || turns[0].Text != "first" || turns[2].Text != "second" || turns[3].Text != "answer" {
		t.Fatalf("replayed or lost turn: %+v", turns)
	}
}
func TestConcurrentDeliveryRetriesClaimOnce(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	var wg sync.WaitGroup
	var accepted, claimed atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.Accept("alfred", id, "request-shared", "one instruction")
			if err != nil {
				t.Error(err)
				return
			}
			if a.New {
				accepted.Add(1)
			}
			_, yes, err := s.Claim("alfred", id)
			if err != nil {
				t.Error(err)
			}
			if yes {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || claimed.Load() != 1 {
		t.Fatal(accepted.Load(), claimed.Load())
	}
	if _, err := s.Accept("alfred", id, "request-shared", "different instruction"); !errors.Is(err, ErrRequestConflict) {
		t.Fatal(err)
	}
}
func TestDeliveryCreateRetryAfterRestart(t *testing.T) {
	s := New(t.TempDir())
	id, err := s.CreateOnce("alfred", "", "first", "model", "create-request")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("alfred", id, "edited title"); err != nil {
		t.Fatal(err)
	}
	fresh := New(s.Root())
	got, err := fresh.CreateOnce("alfred", "", "first", "model", "create-request")
	if err != nil || got != id || len(fresh.List("alfred")) != 1 {
		t.Fatal(got, err)
	}
	if _, err := fresh.CreateOnce("alfred", "", "changed payload", "model", "create-request"); !errors.Is(err, ErrRequestConflict) {
		t.Fatal(err)
	}
}
func TestDeliveryRejectsCorruptJournalAndFailedPersistence(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	p := filepath.Join(s.Root(), "alfred", id+".md")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "---\n", "---\ndeliveries: broken\n", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept("alfred", id, "request-corrupt", "hello"); err == nil {
		t.Fatal("forgot corrupt journal")
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Accept("alfred", id, "request-failure", "hello"); err == nil {
		t.Fatal("acknowledged unpersisted message")
	}
	if _, ok := s.Receipt("alfred", id, "request-failure"); ok {
		t.Fatal("failed write became a receipt")
	}
}
