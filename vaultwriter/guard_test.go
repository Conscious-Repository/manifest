package vaultwriter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The centralized write guard (system-root-plan §3). Two zones, one invariant:
//   - engine-owned regions (system/agents, system/excalibur + legacy roots) are
//     NEVER written by the dashboard, under any class;
//   - database-class writes (CRMs, home board, aion — the markdown databases)
//     are legal ONLY under system/;
//   - raw-user-class writes (the note editor, contact saves — the user's own
//     hands) stay legal anywhere else, exactly as shipped today.
func TestGuardClasses(t *testing.T) {
	w := New(t.TempDir()).WithZoneRoots("system", "extrinsic")
	cases := []struct {
		rel   string
		class WriteClass
		ok    bool
	}{
		// engine-owned: refused for every class
		{"system/excalibur/spirits/x/identity.md", WriteRawUser, false},
		{"system/excalibur/artifacts/feed/item.md", WriteDatabase, false},
		{"system/agents/brief.md", WriteRawUser, false},
		{"Agents/legacy.md", WriteRawUser, false},     // legacy root (pre-reorg)
		{"excalibur/chargebook.md", WriteRawUser, false},

		// database class: structured roots only — system/ AND extrinsic/ (books)
		{"system/crm/fundraising/acme ventures.md", WriteDatabase, true},
		{"system/home/board.md", WriteDatabase, true},
		{"extrinsic/esoterika.md", WriteDatabase, true},    // a book record
		{"extrinsic/some article.md", WriteDatabase, true}, // extrinsic zone is writable
		{"intrinsic/2026-07-08.md", WriteDatabase, false},  // knowledge zone refused
		{"alice.md", WriteDatabase, false},

		// raw-user class: the user's own edits stay legal in both zones
		{"alice.md", WriteRawUser, true},
		{"intrinsic/2026-07-08.md", WriteRawUser, true},
		{"system/crm/fundraising/acme ventures.md", WriteRawUser, true}, // hand-editable by design
		{"system/home/board.md", WriteRawUser, true},

		// traversal is refused regardless of class
		{"../outside.md", WriteRawUser, false},
		{"system/../../outside.md", WriteDatabase, false},
	}
	for _, c := range cases {
		err := w.Guard(c.rel, c.class)
		if c.ok && err != nil {
			t.Errorf("Guard(%q, %v) = %v, want ok", c.rel, c.class, err)
		}
		if !c.ok && err == nil {
			t.Errorf("Guard(%q, %v) = ok, want refusal", c.rel, c.class)
		}
	}
}

// Every existing write entry point flows through the guard: an engine-owned
// path is refused end-to-end, and the shipped user writes still work.
func TestWriterEntryPointsAreGuarded(t *testing.T) {
	root := t.TempDir()
	w := New(root).WithZoneRoots("system", "extrinsic")

	// seed an engine-owned file + a knowledge-zone note on disk
	for _, rel := range []string{"system/excalibur/spirits/x/identity.md", "alice.md"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\ncategories: [people]\n---\n- [ ] task\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	engineRel := "system/excalibur/spirits/x/identity.md"
	if err := w.WriteNote(engineRel, "overwritten"); err == nil {
		t.Fatal("WriteNote into an engine-owned path must be refused")
	}
	if err := w.ReplaceBody(engineRel, "x"); err == nil {
		t.Fatal("ReplaceBody into an engine-owned path must be refused")
	}
	if err := w.AddFrontmatterValue(engineRel, "email", "x@y.com"); err == nil {
		t.Fatal("AddFrontmatterValue into an engine-owned path must be refused")
	}
	if err := w.ToggleTask(engineRel, 3, true); err == nil {
		t.Fatal("ToggleTask into an engine-owned path must be refused")
	}
	// the engine file is untouched
	b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(engineRel)))
	if string(b) != "---\ncategories: [people]\n---\n- [ ] task\n" {
		t.Fatalf("engine-owned file was modified:\n%s", b)
	}

	// shipped user writes still work in the knowledge zone
	if err := w.WriteNote("alice.md", "---\ncategories: [people]\n---\nhello"); err != nil {
		t.Fatalf("knowledge-zone WriteNote must keep working: %v", err)
	}
	if _, err := w.CreatePersonNote("Bob Jones", nil, "friend"); err != nil {
		t.Fatalf("CreatePersonNote must keep working: %v", err)
	}
	if rel, err := w.SaveExtrinsic("Some Find", "finding", "why", "", "", "body"); err != nil || !strings.HasPrefix(rel, "extrinsic/") {
		t.Fatalf("SaveExtrinsic must keep working: rel=%q err=%v", rel, err)
	}
}
