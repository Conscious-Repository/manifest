package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentConfirmAppliesOnlyOnce(t *testing.T) {
	s, vault := appendTestStore(t)
	p, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## Approval race\nExactly one appended section"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- s.Confirm(p.ID) }()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d requests reported applying one proposal", succeeded)
	}
	b, err := os.ReadFile(filepath.Join(vault, "log/2026-08-10 roof bid.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "## Approval race") != 1 {
		t.Fatalf("duplicate application: %s", b)
	}
	if len(s.List("pending")) != 0 || len(s.List("approved")) != 1 {
		t.Fatal("decision did not settle")
	}
}

func TestConcurrentConfirmRejectHasOneOutcome(t *testing.T) {
	s, vault := appendTestStore(t)
	p, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## Decision race\nConditional section"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- s.Confirm(p.ID) }()
	go func() { <-start; results <- s.Reject(p.ID, "other surface") }()
	close(start)
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatalf("expected exactly one success: %v, %v", a, b)
	}
	approved, rejected := len(s.List("approved")), len(s.List("rejected"))
	if approved+rejected != 1 || len(s.List("pending")) != 0 {
		t.Fatal("conflicting final decisions")
	}
	content, err := os.ReadFile(filepath.Join(vault, "log/2026-08-10 roof bid.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "## Decision race") != approved {
		t.Fatal("effect disagrees with final decision")
	}
}

func TestOperationCallbackCanSettleWithoutLockInversion(t *testing.T) {
	s := NewStore(t.TempDir())
	p, err := s.Propose(Proposal{Type: TypeManifestOperation, Action: "Operation", ApplyPath: "operation-1"})
	if err != nil {
		t.Fatal(err)
	}
	s.WithOperationDecision(func(_ string, decision string) error { return s.Settle(p.ID, decision) })
	// Callback settlement can race the outer final archive; it must not deadlock.
	done := make(chan struct{})
	go func() { _ = s.Confirm(p.ID); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("operation callback deadlocked on settlement")
	}
	if len(s.List("approved")) != 1 {
		t.Fatal("callback did not settle")
	}
}

func TestAutomaticAppendAndManualConfirmShareDecisionLock(t *testing.T) {
	s, vault := appendTestStore(t)
	p, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## Automatic race\nOne section"))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan int, 2)
	go func() {
		<-start
		if s.Confirm(p.ID) == nil {
			results <- 1
		} else {
			results <- 0
		}
	}()
	go func() { <-start; n, _ := s.AutoApplyAppends(nil); results <- n }()
	close(start)
	if n := (<-results) + (<-results); n != 1 {
		t.Fatalf("%d applications", n)
	}
	b, err := os.ReadFile(filepath.Join(vault, "log/2026-08-10 roof bid.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "## Automatic race") != 1 {
		t.Fatalf("duplicated section: %s", b)
	}
}
