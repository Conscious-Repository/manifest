package vaultwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveExtrinsicCreatesNote(t *testing.T) {
	vault := t.TempDir()
	w := New(vault)
	rel, err := w.SaveExtrinsic("Bioelectric Signaling", "paper",
		"maps to aging work", "https://example.com/a", "biorxiv", "")
	if err != nil {
		t.Fatalf("SaveExtrinsic: %v", err)
	}
	if rel != filepath.Join("extrinsic", "bioelectric signaling.md") { // lowercased to the vault convention
		t.Fatalf("rel path: %q", rel)
	}
	b, err := os.ReadFile(filepath.Join(vault, rel))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "categories: [papers]") {
		t.Errorf("missing categories frontmatter:\n%s", s)
	}
	if !strings.Contains(s, "#paper") || !strings.Contains(s, "https://example.com/a") ||
		!strings.Contains(s, "Source: biorxiv") {
		t.Errorf("body missing expected content:\n%s", s)
	}
}

func TestWriteOnceNeverOverwrites(t *testing.T) {
	vault := t.TempDir()
	w := New(vault)
	// Pre-existing user note (lowercase, the vault convention SaveExtrinsic targets).
	_ = os.MkdirAll(filepath.Join(vault, "extrinsic"), 0o755)
	note := filepath.Join(vault, "extrinsic", "aion.md")
	_ = os.WriteFile(note, []byte("MY HAND-AUTHORED NOTE\n"), 0o644)

	rel, err := w.SaveExtrinsic("Aion", "company", "why", "", "src", "")
	if err != nil {
		t.Fatalf("SaveExtrinsic: %v", err)
	}
	if rel != filepath.Join("extrinsic", "aion.md") {
		t.Fatalf("rel: %q", rel)
	}
	b, _ := os.ReadFile(note)
	if string(b) != "MY HAND-AUTHORED NOTE\n" {
		t.Fatalf("write-once violated; note was overwritten:\n%s", b)
	}
}

func TestTitleTraversalIsBlocked(t *testing.T) {
	vault := t.TempDir()
	w := New(vault)
	// A malicious title with slashes gets sanitized to a flat name, staying in extrinsic/.
	rel, err := w.SaveExtrinsic("../../etc/passwd", "finding", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The real safety property: the note must resolve to a flat file inside extrinsic/.
	full := filepath.Join(vault, rel)
	extrinsic := filepath.Join(vault, "extrinsic")
	if filepath.Dir(full) != extrinsic {
		t.Fatalf("path escaped extrinsic/: %q", full)
	}
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("note not created under extrinsic: %v", err)
	}
	// And nothing was written to /etc.
	if _, err := os.Stat("/etc/passwd.md"); err == nil {
		t.Fatal("traversal wrote outside the vault!")
	}
}

func TestDisabledWithoutVault(t *testing.T) {
	w := New("")
	if w.Enabled() {
		t.Fatal("empty vault should be disabled")
	}
	if _, err := w.SaveExtrinsic("X", "paper", "", "", "", ""); err == nil {
		t.Fatal("expected error with no vault")
	}
}
