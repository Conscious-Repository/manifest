package capture

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureLifecycle(t *testing.T) {
	s := NewStore(t.TempDir())

	// add note + link
	note, err := s.Add("", "call the roofer", "", "manual")
	if err != nil || note.Kind != "note" || note.Status != "open" {
		t.Fatalf("note: %+v %v", note, err)
	}
	link, err := s.Add("", "", "https://example.com/x", "share")
	if err != nil || link.Kind != "link" {
		t.Fatalf("link: %+v %v", link, err)
	}
	if _, err := s.Add("", "", "", "manual"); err == nil {
		t.Fatal("empty capture must refuse")
	}
	if got := s.OpenCount(); got != 2 {
		t.Fatalf("open = %d", got)
	}

	// edit + keep
	if err := s.Update(note.ID, "roofer", "call the roofer today"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(note.ID, "kept"); err != nil {
		t.Fatal(err)
	}
	if got := s.OpenCount(); got != 1 {
		t.Fatalf("open after keep = %d", got)
	}
	if err := s.SetStatus(note.ID, "weird"); err == nil {
		t.Fatal("bad status must refuse")
	}

	// trash hides immediately, purges after TTL
	if err := s.Trash(link.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 1 {
		t.Fatalf("trashed card still listed")
	}
	// backdate the trash stamp past TTL → next List purges the file
	it, _ := s.load(link.ID)
	it.TrashedAt = time.Now().Add(-31 * 24 * time.Hour).Format(time.RFC3339)
	if err := s.save(it); err != nil {
		t.Fatal(err)
	}
	s.List()
	if _, err := os.Stat(s.itemPath(link.ID)); !os.IsNotExist(err) {
		t.Fatal("trashed card not purged after TTL")
	}
}

func TestCaptureMediaPathGuard(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.MediaPath("../secrets.txt"); err == nil {
		t.Fatal("traversal must refuse")
	}
	if _, err := s.MediaPath("a/b.png"); err == nil {
		t.Fatal("subpath must refuse")
	}
	p, err := s.MediaPath("123-0.png")
	if err != nil || filepath.Base(p) != "123-0.png" {
		t.Fatalf("media path: %q %v", p, err)
	}
}
