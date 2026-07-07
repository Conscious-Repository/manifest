package approvals

import (
	"os"
	"path/filepath"
	"testing"
)

// createNoteHarness builds a vault with a nested excalibur harness + approvals
// store wired to the vault root, mirroring how main.go constructs it.
func createNoteHarness(t *testing.T) (*Store, string) {
	t.Helper()
	vault := t.TempDir()
	harness := filepath.Join(vault, "excalibur")
	if err := os.MkdirAll(harness, 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewStore(filepath.Join(harness, "artifacts")).WithVaultRoot(vault)
	return s, vault
}

// fileCreateNote drops a pending create-vault-note proposal exactly as the
// engine's granola cast files it.
func fileCreateNote(t *testing.T, s *Store, id, applyPath, content string) {
	t.Helper()
	body := "New Granola transcript (12 segments).\n\n````proposed\n" + content + "\n````"
	md := "---\ntype: create-vault-note\nid: " + id + "\naction: Create vault note: " + applyPath +
		"\nagent: ea-coordinator\ncreated: 2026-07-02T08:00:00Z\napply-path: " + applyPath + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(s.dir, "pending", id+".md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCreateVaultNoteAllowed(t *testing.T) {
	ok := []string{"2026-07-02 Aion sync.md", "2026-12-31 A B C.md", "2026-01-01 x.md"}
	bad := []string{
		"", "Aion sync.md", "2026-07-02.md", "sub/2026-07-02 x.md",
		"2026-07-02 x.txt", "../2026-07-02 x.md", "2026-7-2 x.md",
		"2026-07-02 has/slash.md",
	}
	for _, p := range ok {
		if !CreateVaultNotePathAllowed(p) {
			t.Errorf("CreateVaultNotePathAllowed(%q) = false, want true", p)
		}
	}
	for _, p := range bad {
		if CreateVaultNotePathAllowed(p) {
			t.Errorf("CreateVaultNotePathAllowed(%q) = true, want false", p)
		}
	}
}

func TestConfirmCreatesVaultNote(t *testing.T) {
	s, vault := createNoteHarness(t)
	content := "---\ncategories:\n  - sync\ngranola-id: not_abc\n---\n[[jane doe]]\n\n## Transcript\n\n**Benjamin:** hi\n"
	fileCreateNote(t, s, "a1a1a1a1a1a1", "2026-07-02 Aion sync.md", content)

	if err := s.Confirm("a1a1a1a1a1a1"); err != nil {
		t.Fatalf("Confirm should write the note, got %v", err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "2026-07-02 Aion sync.md"))
	if err != nil {
		t.Fatalf("note not written: %v", err)
	}
	if string(got) != content+"\n" && string(got) != content {
		t.Fatalf("note content mismatch:\n%s", got)
	}
	if n := len(s.List("approved")); n != 1 {
		t.Fatalf("expected 1 approved, got %d", n)
	}
}

func TestConfirmCreateNoteRefusesExisting(t *testing.T) {
	s, vault := createNoteHarness(t)
	// the note already exists — confirming must refuse, not overwrite
	existing := filepath.Join(vault, "2026-07-02 Aion sync.md")
	if err := os.WriteFile(existing, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileCreateNote(t, s, "b2b2b2b2b2b2", "2026-07-02 Aion sync.md", "NEW CONTENT")
	if err := s.Confirm("b2b2b2b2b2b2"); err == nil {
		t.Fatal("Confirm must refuse to overwrite an existing note")
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "ORIGINAL" {
		t.Fatalf("existing note must be untouched, got %q", got)
	}
	if n := len(s.List("pending")); n != 1 {
		t.Fatalf("refused proposal must stay pending, got %d", n)
	}
}

func TestConfirmCreateNoteRefusesBadPath(t *testing.T) {
	s, _ := createNoteHarness(t)
	fileCreateNote(t, s, "c3c3c3c3c3c3", "sub/2026-07-02 x.md", "content")
	if err := s.Confirm("c3c3c3c3c3c3"); err == nil {
		t.Fatal("Confirm must refuse a non-vault-root path")
	}
}

func TestConfirmCreateNoteRefusesNoVaultRoot(t *testing.T) {
	// a store without WithVaultRoot must refuse create-vault-note applies
	harness := t.TempDir()
	s := NewStore(filepath.Join(harness, "artifacts"))
	fileCreateNote(t, s, "d4d4d4d4d4d4", "2026-07-02 x.md", "content")
	if err := s.Confirm("d4d4d4d4d4d4"); err == nil {
		t.Fatal("Confirm must refuse create-vault-note without a vault root")
	}
}

func TestReplaceAttendeeLine(t *testing.T) {
	withAtt := "---\ncategories:\n  - sync\ngranola-id: not_x\n---\n[[old one]] [[old two]]\n\n## Transcript\n\n**Benjamin:** hi\n"
	noAtt := "---\ncategories:\n  - sync\ngranola-id: not_x\n---\n\n## Transcript\n\n**Benjamin:** hi\n"

	// replace existing attendees
	got := replaceAttendeeLine(withAtt, []string{"Jane Doe", "Ada Lovelace"})
	if !contains(got, "[[Jane Doe]] [[Ada Lovelace]]") || contains(got, "old one") {
		t.Fatalf("replace failed:\n%s", got)
	}
	if !contains(got, "## Transcript") || !contains(got, "granola-id: not_x") {
		t.Fatalf("frontmatter/transcript lost:\n%s", got)
	}
	// add attendees where there were none
	got2 := replaceAttendeeLine(noAtt, []string{"Jane Doe"})
	if !contains(got2, "---\n[[Jane Doe]]\n\n## Transcript") {
		t.Fatalf("add-to-empty failed:\n%s", got2)
	}
	// clear attendees to none
	got3 := replaceAttendeeLine(withAtt, nil)
	if contains(got3, "[[") {
		t.Fatalf("clear failed:\n%s", got3)
	}
	if !contains(got3, "---\n\n## Transcript") {
		t.Fatalf("cleared shape wrong:\n%s", got3)
	}
}

func TestConfirmCreateNoteEditsAttendees(t *testing.T) {
	s, vault := createNoteHarness(t)
	content := "---\ncategories:\n  - sync\ngranola-id: not_z\n---\n[[wrong person]]\n\n## Transcript\n\n**Benjamin:** hi\n"
	fileCreateNote(t, s, "e5e5e5e5e5e5", "2026-07-02 evan sync.md", content)

	if err := s.ConfirmCreateNote("e5e5e5e5e5e5", []string{"Evan Fisher", "Benjamin"}); err != nil {
		t.Fatalf("ConfirmCreateNote: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "2026-07-02 evan sync.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(got), "[[Evan Fisher]] [[Benjamin]]") || contains(string(got), "wrong person") {
		t.Fatalf("edited note attendees wrong:\n%s", got)
	}
}
