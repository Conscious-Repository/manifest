package vaultindex

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
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
		if err := insertNote(tx, ParseNote(rel, content, mtime, regions)); err != nil {
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
	for _, t := range []string{"notes", "note_categories", "note_aliases", "note_emails", "inline_fields"} {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE path = ?", rel); err != nil {
			return fmt.Errorf("delete %s: %w", t, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM links WHERE src_path = ?`, rel); err != nil { // links key the note as src_path
		return fmt.Errorf("delete links: %w", err)
	}
	return nil
}

// Watch blocks, watching the vault for markdown changes and reindexing affected
// files (debounced). It returns when ctx is cancelled. onReindex, if non-nil, is
// called after each debounced flush with the paths touched and any error — handy
// for logging. New directories are picked up automatically.
func (ix *Index) Watch(ctx context.Context, debounce time.Duration, onReindex func(paths []string, err error)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if debounce <= 0 {
		debounce = 400 * time.Millisecond
	}
	skip := skipSet(ix.cfg.SkipDirs)
	ix.addDirs(w, skip)

	pending := map[string]bool{}
	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(debounce)
		} else {
			timer.Reset(debounce)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			// a newly created directory joins the watch
			if ev.Op&(fsnotify.Create) != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					base := filepath.Base(ev.Name)
					if !strings.HasPrefix(base, ".") && !skip[base] {
						_ = w.Add(ev.Name)
					}
					continue
				}
			}
			if !strings.HasSuffix(strings.ToLower(ev.Name), ".md") {
				continue
			}
			if rel, err := filepath.Rel(ix.cfg.VaultRoot, ev.Name); err == nil {
				pending[filepath.ToSlash(rel)] = true
				arm()
			}
		case <-timerC:
			timerC = nil
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			pending = map[string]bool{}
			err := ix.ReindexPaths(paths)
			if onReindex != nil {
				onReindex(paths, err)
			}
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
		}
	}
}

func (ix *Index) addDirs(w *fsnotify.Watcher, skip map[string]bool) {
	_ = filepath.WalkDir(ix.cfg.VaultRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		base := d.Name()
		if p != ix.cfg.VaultRoot && (strings.HasPrefix(base, ".") || skip[base]) {
			return filepath.SkipDir
		}
		_ = w.Add(p)
		return nil
	})
}
