package vaultindex

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"manifest/record"
)

// ReindexPaths re-reads the given vault-relative markdown paths and updates the
// projection incrementally: a path that no longer exists on disk is removed, one
// that exists is re-parsed and replaced. Entities are re-derived once at the end.
// This keeps Obsidian edits visible without a full Rebuild.
func (ix *Index) ReindexPaths(relPaths []string) error {
	if len(relPaths) == 0 {
		return nil
	}
	tx, err := ix.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	regions := ix.cfg.aiRegions()
	for _, rel := range relPaths {
		rel = filepath.ToSlash(rel)
		if err := deletePath(tx, rel); err != nil {
			return fmt.Errorf("deletePath %s: %w", rel, err)
		}
		abs := filepath.Join(ix.cfg.VaultRoot, filepath.FromSlash(rel))
		content, err := os.ReadFile(abs)
		if err != nil {
			continue // removed or unreadable → stays deleted
		}
		var mtime int64
		if fi, err := os.Stat(abs); err == nil {
			mtime = fi.ModTime().Unix()
		}
		n := ParseNote(rel, content, mtime, regions)
		n.Zone = ix.cfg.zoneOf(n.Path)
		if err := insertNote(tx, n); err != nil {
			return fmt.Errorf("insertNote %s: %w", rel, err)
		}
	}
	if err := deriveEntities(tx); err != nil {
		return fmt.Errorf("deriveEntities: %w", err)
	}
	return tx.Commit()
}

func deletePath(tx *sql.Tx, rel string) error {
	// fts5 rows are deleted by rowid (a column WHERE is not supported), and the
	// rowid is notes.id — so drop the fts row before the notes row.
	var id int64
	if err := tx.QueryRow(`SELECT id FROM notes WHERE path = ?`, rel).Scan(&id); err == nil {
		if _, err := tx.Exec(`DELETE FROM notes_fts WHERE rowid = ?`, id); err != nil {
			return fmt.Errorf("fts delete: %w", err)
		}
	}
	for _, t := range []string{"notes", "note_categories", "note_aliases", "note_emails", "inline_fields", "note_tasks"} {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE path = ?", rel); err != nil {
			return fmt.Errorf("delete %s: %w", t, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM links WHERE src_path = ?`, rel); err != nil { // links key the note as src_path
		return fmt.Errorf("delete links: %w", err)
	}
	return nil
}

// SubscribeWatch registers this index's sink on the kernel's single watch
// (record.Watch — the fsnotify plumbing lives there once). What stays here is
// this engine's POLICY: every zone, every markdown change (case-insensitive
// .md, vault-relative paths) → debounced incremental ReindexPaths; new
// directories are the watch's business, not a reindex trigger. onReindex, if
// non-nil, is called after each flush with the paths touched and any error —
// handy for logging.
func (ix *Index) SubscribeWatch(w *record.Watch, debounce time.Duration, onReindex func(paths []string, err error)) {
	if debounce <= 0 {
		debounce = 400 * time.Millisecond
	}
	var mu sync.Mutex
	var timer *time.Timer
	pending := map[string]bool{}
	flush := func() {
		mu.Lock()
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		pending = map[string]bool{}
		mu.Unlock()
		if len(paths) == 0 {
			return
		}
		err := ix.ReindexPaths(paths)
		if onReindex != nil {
			onReindex(paths, err)
		}
	}
	w.Subscribe(func(e record.Event) {
		if e.DirCreated || !strings.HasSuffix(strings.ToLower(e.Path), ".md") {
			return
		}
		rel, err := filepath.Rel(ix.cfg.VaultRoot, e.Path)
		if err != nil {
			return
		}
		mu.Lock()
		pending[filepath.ToSlash(rel)] = true
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, flush)
		mu.Unlock()
	})
}
