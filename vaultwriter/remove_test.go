package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/record"
)

func TestRemoveCapBoundaryAndAudit(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	w := New(root).WithAudit(data).Grant(Capability{Name: "todo-plans", Zone: record.ZoneSystem, Pattern: "system/todo-plans/**", Actor: ActorUserAction})
	const rel = "system/todo-plans/x.md"
	if err := w.WriteCap("todo-plans", rel, []byte("plan")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ cap, rel string }{{"missing", rel}, {"todo-plans", "system/aion/x.md"}, {"todo-plans", "../outside.md"}} {
		if err := w.RemoveCap(tt.cap, tt.rel, nil); err == nil {
			t.Fatalf("allowed removal: %+v", tt)
		}
	}
	refused := errors.New("wrong identity")
	if err := w.RemoveCap("todo-plans", rel, func([]byte) error { return refused }); !errors.Is(err, refused) {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, rel)); err != nil || string(raw) != "plan" {
		t.Fatalf("refused removal changed file: %v", err)
	}
	if err := w.RemoveCap("todo-plans", rel, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
		t.Fatalf("file survived: %v", err)
	}
	if err := w.RemoveCap("todo-plans", rel, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(data, "write-audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSpace(string(raw)), "\n")) != 2 || !strings.Contains(string(raw), rel+"\ttodo-plans\tuser-action (remove)\t-4") {
		t.Fatalf("audit: %s", raw)
	}
	// A link must not make the capability a deletion route outside its tree.
	target := filepath.Join(t.TempDir(), "target.md")
	if err := os.WriteFile(target, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	if err := w.RemoveCap("todo-plans", rel, nil); err == nil {
		t.Fatal("symlink removal allowed")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
}
