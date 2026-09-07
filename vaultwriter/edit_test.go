package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestEditorExactBytesAndConflicts(t *testing.T) {
	for _, raw := range []string{"no newline", "# Unicode 🌿\r\n\r\ntext\r\n", "---\na: b\n---\n", ""} {
		w := New(t.TempDir())
		rev, err := w.CreateNote("note.md", raw)
		if err != nil {
			t.Fatal(err)
		}
		next := raw + " owner edit"
		saved, err := w.WriteNoteIfRevision("note.md", next, rev)
		if err != nil {
			t.Fatal(err)
		}
		bytes, _ := os.ReadFile(filepath.Join(w.vault, "note.md"))
		if string(bytes) != next || saved != Revision(bytes) {
			t.Fatal("bytes changed")
		}
		backup, _ := os.ReadFile(filepath.Join(w.vault, "note.md.pre-write-"+rev))
		if string(backup) != raw {
			t.Fatal("backup differs")
		}
		_, err = w.WriteNoteIfRevision("note.md", "stale", rev)
		var conflict *Conflict
		if !errors.As(err, &conflict) || conflict.Raw != next {
			t.Fatalf("wrong conflict: %v", err)
		}
		if _, err = w.WriteNoteIfRevision("note.md", "bad", ""); err == nil {
			t.Fatal("missing precondition accepted")
		}
		_ = os.Remove(filepath.Join(w.vault, "note.md"))
		_, err = w.WriteNoteIfRevision("note.md", "recreate", saved)
		if !errors.As(err, &conflict) || !conflict.Missing {
			t.Fatal("deletion not a conflict")
		}
	}
}

func TestEditorHistoryOutsideVault(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	w := New(root).WithHistory(data)
	rev, err := w.CreateNote("note.md", "original 🌿\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.WriteNoteIfRevision("note.md", "next", rev); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(w.historyDir, Revision([]byte("note.md")), rev+".md")
	if b, err := os.ReadFile(backup); err != nil || string(b) != "original 🌿\r\n" {
		t.Fatal("history lost original bytes", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || entries[0].Name() != "note.md" {
		t.Fatal("autosave littered vault", entries)
	}
	// Another vault with the same filename must not share its history namespace.
	other := New(t.TempDir()).WithHistory(data)
	if other.historyDir == w.historyDir {
		t.Fatal("vault histories collide")
	}
	// A broken history store must fail before overwriting the note.
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "note.md"), backup); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("original 🌿\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteNoteIfRevision("note.md", "must not land", rev); err == nil {
		t.Fatal("unsafe history accepted")
	}
	b, _ := os.ReadFile(filepath.Join(root, "note.md"))
	if string(b) != "original 🌿\r\n" {
		t.Fatal("failed preservation overwrote note")
	}
}
func TestEditorBoundariesAndMove(t *testing.T) {
	w := New(t.TempDir())
	rev, err := w.CreateNote("one.md", "🌿")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.CreateNote("two.md", "second")
	for _, p := range []string{"../escape.md", "/absolute.md", "system/agents/owner.md", ".hidden.md", "wrong.txt"} {
		if _, err = w.CreateNote(p, "x"); err == nil {
			t.Fatalf("allowed %s", p)
		}
	}
	outside := t.TempDir()
	_ = os.Symlink(outside, filepath.Join(w.vault, "link"))
	if _, err = w.CreateNote("link/escape.md", "x"); err == nil {
		t.Fatal("symlink allowed")
	}
	if err = w.MoveNote("one.md", "two.md", rev); !os.IsExist(err) {
		t.Fatal("collision accepted", err)
	}
	_ = os.Mkdir(filepath.Join(w.vault, "drafts"), 0755)
	if err = w.MoveNote("one.md", "drafts/one.md", rev); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(w.vault, "drafts/one.md"))
	if string(b) != "🌿" {
		t.Fatal("move changed bytes")
	}
}
func TestCompetingEditorSaves(t *testing.T) {
	root := t.TempDir()
	w := New(root)
	rev, _ := w.CreateNote("note.md", "base")
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, text := range []string{"one", "two"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := New(root).WriteNoteIfRevision("note.md", text, rev)
			out <- err
		}(text)
	}
	wg.Wait()
	close(out)
	ok, conflicts := 0, 0
	for err := range out {
		if err == nil {
			ok++
		} else {
			var c *Conflict
			if errors.As(err, &c) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("saved=%d conflicts=%d", ok, conflicts)
	}
}
