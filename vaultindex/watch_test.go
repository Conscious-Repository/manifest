package vaultindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchReindexesOnChange(t *testing.T) {
	ix, root := fixture(t)

	got := make(chan []string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ix.Watch(ctx, 50*time.Millisecond, func(paths []string, err error) {
		if err == nil {
			got <- paths
		}
	})
	time.Sleep(100 * time.Millisecond) // let the watcher register directories

	// a brand-new person note should appear without a manual Rebuild
	if err := os.WriteFile(filepath.Join(root, "carol.md"),
		[]byte("---\ncategories: [people]\n---\nnew person\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-got:
			people, _ := ix.Category("people", SortNameAsc)
			if contains(names(people), "carol") {
				return // reindexed on change — success
			}
		case <-deadline:
			t.Fatal("watcher did not reindex the new note within 3s")
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
