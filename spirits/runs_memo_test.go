package spirits

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Runs() serves an unchanged report from memory, re-parses a rewritten one
// (the engine rewrites a report each step) and forgets a removed one.
func TestRunsMemoFollowsTheFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "artifacts", "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, outcome string, mod time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("---\nrun: "+name+"\nspirit: sage\nritual: skill-cast\nstarted: 2026-09-12T10:00:00Z\noutcome: "+outcome+"\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, mod, mod)
	}
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	write("r1.md", "running", base)
	write("r2.md", "completed", base)
	s := NewStore(root)
	if got := s.Runs(); len(got) != 2 {
		t.Fatal(got)
	}
	if len(s.runs.byID) != 2 {
		t.Fatal("memo not filled", len(s.runs.byID))
	}
	// same mtime + size → served from memory even if the bytes changed underneath
	write("r1.md", "running", base)
	before := s.runs.byID["r1.md"].sum
	if got := s.Runs(); got[0].ID != before.ID && got[1].ID != before.ID {
		t.Fatal("memo lost")
	}
	// the engine rewrote the report (new mtime): re-parsed
	write("r1.md", "completed", base.Add(time.Minute))
	found := false
	for _, r := range s.Runs() {
		if r.ID == "r1" && r.Outcome == "completed" {
			found = true
		}
	}
	if !found {
		t.Fatal("rewritten report not re-parsed")
	}
	// a removed report leaves the memo
	_ = os.Remove(filepath.Join(dir, "r2.md"))
	if got := s.Runs(); len(got) != 1 || len(s.runs.byID) != 1 {
		t.Fatal("removed report lingered", len(got), len(s.runs.byID))
	}
	// a literal Store (tests, ad-hoc readers) still works with no memo
	plain := &Store{root: root}
	if got := plain.Runs(); len(got) != 1 {
		t.Fatal("uncached path", got)
	}
}
