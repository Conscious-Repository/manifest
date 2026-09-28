package vaultwriter

import (
	"os"
	"path/filepath"
	"testing"
)

// ⚠ THE VAULT'S PERSON-NOTE SHAPE: a lowercase "first last.md" at the vault
// root, `aliases:` (Obsidian's key — never `alias:`) above
// `categories: [people]`. "make them a contact" writes exactly this.
func TestCreatePersonNoteFollowsTheVaultConvention(t *testing.T) {
	root := t.TempDir()
	w := New(root)
	rel, err := w.CreatePersonNote("Ron Boger", []string{"Ronald"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "ron boger.md" {
		t.Fatalf("filename %q, want a lowercase name at the root", rel)
	}
	b, _ := os.ReadFile(filepath.Join(root, rel))
	if want := "---\naliases: [Ronald]\ncategories: [people]\n---\n"; string(b) != want {
		t.Fatalf("got:\n%q\nwant:\n%q", b, want)
	}
	rel, _ = w.CreatePersonNote("RJ Tevonian", nil, "")
	if b, _ := os.ReadFile(filepath.Join(root, rel)); rel != "rj tevonian.md" || string(b) != "---\ncategories: [people]\n---\n" {
		t.Fatalf("%q %q", rel, b)
	}
}
