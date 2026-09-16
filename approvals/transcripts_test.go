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
