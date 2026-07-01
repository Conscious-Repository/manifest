package approvals

import (
	"testing"
	"time"
)

const eaReply = "I drafted these:\n```json\n" +
	`[{"action":"Send email to Lee","body":"Hi Lee, ..."},` +
	`{"action":"Propose refund on order 123","body":"Step plan: ..."}]` +
	"\n```\n"

func TestMaterializeThenLifecycle(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

	created, err := s.Materialize(eaReply, "ea-coordinator", now)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 proposals, got %d", len(created))
	}
	if n := len(s.List("pending")); n != 2 {
		t.Fatalf("pending = %d", n)
	}

	// Re-materialize: no duplicates.
	again, _ := s.Materialize(eaReply, "ea-coordinator", now)
	if len(again) != 0 {
		t.Fatalf("re-materialize should add 0, got %d", len(again))
	}

	// Confirm one, reject the other.
	id0 := created[0].ID
	id1 := created[1].ID
	if err := s.Confirm(id0); err != nil {
		t.Fatal(err)
	}
	if err := s.Reject(id1, "not now"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List("pending")); n != 0 {
		t.Fatalf("pending should be empty, got %d", n)
	}
	if n := len(s.List("approved")); n != 1 {
		t.Fatalf("approved = %d", n)
	}
	rej := s.List("rejected")
	if len(rej) != 1 {
		t.Fatalf("rejected = %d", len(rej))
	}
	if want := "> rejected: not now"; !contains(rej[0].Body, want) {
		t.Fatalf("reject reason not recorded: %q", rej[0].Body)
	}

	// A decided proposal must not resurrect on a later materialize.
	after, _ := s.Materialize(eaReply, "ea-coordinator", now)
	if len(after) != 0 {
		t.Fatalf("decided proposals resurrected: %d", len(after))
	}
}

func TestCounts(t *testing.T) {
	s := NewStore(t.TempDir())
	_, _ = s.Materialize(eaReply, "ea", time.Now())
	c := s.Counts()
	if c["pending"] != 2 || c["approved"] != 0 {
		t.Fatalf("counts: %+v", c)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
