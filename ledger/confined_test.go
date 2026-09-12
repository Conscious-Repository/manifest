package ledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfinedShadowLedger(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	s, e := NewInRoot(root, time.UTC)
	if e != nil {
		t.Fatal(e)
	}
	entry := Entry{TS: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Source: "run", Kind: "run.refused", Actor: "extractor"}
	if e = s.Append(entry); e != nil {
		t.Fatal(e)
	}
	if got, e := s.Day("2000-01-01"); e != nil || len(got) != 1 || got[0].Kind != "run.refused" {
		t.Fatal("confined read/append failed")
	}
	if got := s.Days(); len(got) != 1 || got[0] != "2000-01-01" {
		t.Fatal("confined day listing failed")
	}
	entry.TS = entry.TS.Add(24 * time.Hour)
	if e = os.Symlink(filepath.Join(out, "escape"), filepath.Join(dir, "2000-01-02.jsonl")); e != nil {
		t.Fatal(e)
	}
	if e = s.Append(entry); e == nil {
		t.Fatal("ledger escaped root")
	}
	files, e := os.ReadDir(out)
	if e != nil || len(files) != 0 {
		t.Fatal("external ledger write")
	}
}
