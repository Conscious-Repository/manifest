package feed

import (
	"testing"
	"time"
)

const scoutReply = "Here are today's finds:\n\n" +
	"```json\n" +
	`[{"type":"paper","title":"Bioelectric signaling in regeneration","why":"maps to your aging work","link":"https://example.com/a","source":"biorxiv","domain":"bioelectricity","confidence":"high"},` +
	`{"type":"person","title":"Dr. Jane Roe","why":"runs a relevant lab","link":"https://example.com/jane","source":"lab site","confidence":"medium"}]` +
	"\n```\n"

func TestMaterializeAndDedupe(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	created, err := s.Materialize(scoutReply, "domain-scout", "domain-scout", now)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected 2 new items, got %d", len(created))
	}
	// Re-materializing the same output must add nothing (dedupe by stable id).
	again, err := s.Materialize(scoutReply, "domain-scout", "domain-scout", now)
	if err != nil {
		t.Fatalf("Materialize 2: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected 0 new on repeat, got %d", len(again))
	}
	if got := s.List(Filter{}, now); len(got) != 2 {
		t.Fatalf("list should have 2, got %d", len(got))
	}
}

func TestMaterializeNoJSONIsNotError(t *testing.T) {
	s := NewStore(t.TempDir())
	created, err := s.Materialize("Nothing new to report today.", "domain-scout", "domain-scout", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("expected 0 items, got %d", len(created))
	}
}

func TestStatusAndSnoozeFilter(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	created, _ := s.Materialize(scoutReply, "domain-scout", "domain-scout", now)
	id := created[0].ID

	// discard hides it from the default list
	if _, err := s.SetStatus(id, "discarded"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(Filter{}, now)); n != 1 {
		t.Fatalf("discard should hide 1, default list = %d", n)
	}
	// but is visible with an explicit status filter
	if n := len(s.List(Filter{Status: "discarded"}, now)); n != 1 {
		t.Fatalf("explicit discarded filter = %d", n)
	}

	// snooze the other into the future → hidden now, visible later
	other := created[1].ID
	if _, err := s.Snooze(other, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(Filter{}, now)); n != 0 {
		t.Fatalf("both hidden now, got %d", n)
	}
	if n := len(s.List(Filter{}, now.Add(72*time.Hour))); n != 1 {
		t.Fatalf("snooze expired, expected 1 visible, got %d", n)
	}
}

func TestInboxAndAllModes(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	created, _ := s.Materialize(scoutReply, "domain-scout", "domain-scout", now)
	a, b := created[0].ID, created[1].ID

	// a: kept (has a verdict — leaves the inbox); b: stays new
	if _, err := s.SetStatus(a, "kept"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(Filter{Status: "inbox"}, now)); n != 1 {
		t.Fatalf("inbox should show only the 1 new item, got %d", n)
	}
	if n := len(s.List(Filter{Status: "kept"}, now)); n != 1 {
		t.Fatalf("kept filter = %d", n)
	}
	// discard b, then ALL must still show both (discarded included)
	if _, err := s.SetStatus(b, "discarded"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(Filter{Status: "inbox"}, now)); n != 0 {
		t.Fatalf("inbox now empty (one kept, one discarded), got %d", n)
	}
	if n := len(s.List(Filter{Status: "all"}, now)); n != 2 {
		t.Fatalf("ALL must include discarded: got %d", n)
	}
	// a lapsed snooze returns to the inbox
	if _, err := s.SetStatus(b, "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snooze(b, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(Filter{Status: "inbox"}, now)); n != 0 {
		t.Fatalf("still-snoozed item must not be in inbox, got %d", n)
	}
	if n := len(s.List(Filter{Status: "inbox"}, now.Add(48*time.Hour))); n != 1 {
		t.Fatalf("lapsed snooze must return to inbox, got %d", n)
	}
}

func TestSetVaultNoteMarksKept(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Now()
	created, _ := s.Materialize(scoutReply, "domain-scout", "domain-scout", now)
	it, err := s.SetVaultNote(created[0].ID, "extrinsic/Foo.md")
	if err != nil {
		t.Fatal(err)
	}
	if it.VaultNote != "extrinsic/Foo.md" || it.Status != "kept" {
		t.Fatalf("expected kept + vaultNote set, got %+v", it)
	}
}
