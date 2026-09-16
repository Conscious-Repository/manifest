package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptLegacyIdentityAndFence(t *testing.T) {
	s := NewStore(t.TempDir())
	content := "---\ngranola-id: fixture\n---\n## Transcript\n\n````\nprivate transcript\n"
	p := Proposal{ID: "legacy123", Type: TypeCreateVaultNote, Agent: "ea-coordinator", Action: "Old title", ApplyPath: "2026-09-12 old.md", Proposed: content, Body: "Old proposal"}
	got, created, e := s.ProposeTranscript("granola", "fixture", p)
	if e != nil || !created {
		t.Fatal(e)
	}
	parsed, e := s.LoadPending(got.ID)
	if e != nil || !strings.Contains(parsed.Proposed, "private transcript") {
		t.Fatal(parsed, e)
	}
	if e = s.Reject(got.ID, "no"); e != nil {
		t.Fatal(e)
	}
	p.ID = ""
	p.ApplyPath = "2026-09-12 renamed.md"
	p.Action = "Renamed"
	got, created, e = s.ProposeTranscript("granola", "fixture", p)
	if e != nil || created || got.ID != "legacy123" || got.Status != "rejected" {
		t.Fatal(got, created, e)
	}
	os.RemoveAll(filepath.Join(s.dir, "approved"))
	if _, _, e = s.ProposeTranscript("granola", "fixture", p); e == nil {
		t.Fatal("unreadable inventory accepted")
	}
}

func TestTranscriptConfirmEditsPreserveFencedBody(t *testing.T) {
	s, vault := createNoteHarness(t)
	content := "---\ngranola-id: fence-fixture\n---\n[[Jane]]\n\n## Transcript\n\nBefore\n````\nAfter\n"
	p, _, err := s.ProposeTranscript("granola", "fence-fixture", Proposal{Type: TypeCreateVaultNote, Action: "Transcript", ApplyPath: "2026-09-12 fence.md", Proposed: content, Body: "Review transcript."})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmCreateNote(p.ID, ConfirmEdits{EditAttendees: true, Attendees: []string{"Ada"}, EditCategories: true, Categories: []string{"sync"}}); err != nil {
		t.Fatal(err)
	}
	approved := s.List("approved")
	written, err := os.ReadFile(filepath.Join(vault, "log", "2026-09-12 fence.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(approved) != 1 || strings.TrimSpace(approved[0].Proposed) != strings.TrimSpace(string(written)) {
		t.Fatalf("approved transcript differs from vault: %+v", approved)
	}
}
