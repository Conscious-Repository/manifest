package sharedhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedHomeConflictAndIsolation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "goals.md")
	write := func(p string, b []byte) error { return os.WriteFile(p, b, 0600) }
	write(p, []byte("## Home\nShared\n"))
	s := &Store{p, write}
	a, sa := s.Load("# Goals\n## Work\nPRIVATE\n## Home\nOld\n")
	b, sb := s.Load("# Goals\n## Personal\nOLGA\n")
	if strings.Contains(b, "PRIVATE") || strings.Contains(a, "Old") {
		t.Fatal(a, b)
	}
	var private string
	save := func(v string) error { private = v; return nil }
	if err := s.Save(strings.Replace(a, "Shared", "Updated", 1), sa, save); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(private, "Home") {
		t.Fatal(private)
	}
	if err := s.Save(b, sb, save); err != nil {
		t.Fatal("private-only save overwrote or rejected newer Home", err)
	}
	if err := s.Save(strings.Replace(b, "Shared", "Stale", 1), sb, save); err == nil {
		t.Fatal("stale overwrite accepted")
	}
	if err := s.Save("# Goals\n## Other\n", sa, save); err == nil {
		t.Fatal("Home removed")
	}
}
