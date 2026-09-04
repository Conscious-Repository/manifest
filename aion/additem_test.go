package aion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A DONE (or decided) namesake must not block a new add — recurring work
// re-adds titles; only an OPEN twin is a duplicate (owner report 2026-09-04).
func TestAddItemDuplicateScopedToOpen(t *testing.T) {
	dir := t.TempDir()
	root := "system/aion"
	if err := os.MkdirAll(filepath.Join(dir, root), 0o755); err != nil {
		t.Fatal(err)
	}
	st := NewStore(dir, root, func(p string, b []byte) error { return os.WriteFile(p, b, 0o644) })

	if err := st.AddItem(&BacklogItem{Kind: KindTask, Text: "weekly investor sync", Owner: "BA"}); err != nil {
		t.Fatal(err)
	}
	// an open twin refuses
	if err := st.AddItem(&BacklogItem{Kind: KindTask, Text: "Weekly Investor Sync", Owner: "BA"}); err == nil ||
		!strings.Contains(err.Error(), "already in backlog") {
		t.Fatalf("open duplicate should refuse, got %v", err)
	}
	// mark it done → the same title adds cleanly
	doc := st.LoadBacklog()
	for _, it := range doc.AllItems() {
		it.Status = StatusDone
	}
	if err := st.SaveBacklog(doc); err != nil {
		t.Fatal(err)
	}
	if err := st.AddItem(&BacklogItem{Kind: KindTask, Text: "weekly investor sync", Owner: "BA"}); err != nil {
		t.Fatalf("done namesake must not block a re-add: %v", err)
	}
}
