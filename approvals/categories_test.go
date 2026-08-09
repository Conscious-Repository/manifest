package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceCategories(t *testing.T) {
	block := "---\ncategories:\n  - sync\ngranola-id: not_abc\n---\nbody\n"
	inline := "---\ncategories: [sync, projects]\ngranola-id: not_abc\n---\nbody\n"
	none := "---\ngranola-id: not_abc\n---\nbody\n"

	// block-list → canonical inline, other keys byte-intact, block consumed
	got := replaceCategories(block, []string{"sync", "aion"})
	want := "---\ncategories: [sync, aion]\ngranola-id: not_abc\n---\nbody\n"
	if got != want {
		t.Fatalf("block:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// inline → inline
	if got := replaceCategories(inline, []string{"aion"}); got != "---\ncategories: [aion]\ngranola-id: not_abc\n---\nbody\n" {
		t.Fatalf("inline:\n%s", got)
	}
	// insert as first key when absent
	if got := replaceCategories(none, []string{"aion"}); got != "---\ncategories: [aion]\ngranola-id: not_abc\n---\nbody\n" {
		t.Fatalf("insert:\n%s", got)
	}
	// empty list removes the key
	if got := replaceCategories(block, nil); got != "---\ngranola-id: not_abc\n---\nbody\n" {
		t.Fatalf("remove:\n%s", got)
	}
	// blank entries are dropped; no frontmatter → unchanged
	if got := replaceCategories(block, []string{" ", "aion", ""}); !strings.Contains(got, "categories: [aion]") {
		t.Fatalf("blanks:\n%s", got)
	}
	if got := replaceCategories("no frontmatter here\n", []string{"aion"}); got != "no frontmatter here\n" {
		t.Fatalf("no-fm:\n%s", got)
	}
}

func TestConfirmCreateNoteEditsCategories(t *testing.T) {
	s, vault := createNoteHarness(t)
	content := "---\ncategories:\n  - sync\ngranola-id: not_abc\n---\n[[rj tevonian]]\n\n## Transcript\n\n**Benjamin:** hi\n"
	fileCreateNote(t, s, "c7c7c7c7c7c7", "2026-08-07 RJ Sync.md", content)

	err := s.ConfirmCreateNote("c7c7c7c7c7c7", ConfirmEdits{
		Categories: []string{"sync", "aion"}, EditCategories: true,
		Attendees: []string{"RJ Tevonian"}, EditAttendees: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "log", "2026-08-07 rj sync.md"))
	if err != nil {
		t.Fatalf("note not written: %v", err)
	}
	if !strings.HasPrefix(string(got), "---\ncategories: [sync, aion]\ngranola-id: not_abc\n---\n") {
		t.Fatalf("categories not rewritten:\n%s", got)
	}
	// the attendee edit composed with the category edit
	if !strings.Contains(string(got), "[[RJ Tevonian]]") {
		t.Fatalf("attendee edit lost:\n%s", got)
	}
	// the approved record's proposed fence carries the same rewrite
	rec, err := s.LoadApproved("c7c7c7c7c7c7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Proposed, "categories: [sync, aion]") {
		t.Fatalf("approved record fence stale:\n%s", rec.Proposed)
	}
}
