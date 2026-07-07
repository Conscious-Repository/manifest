package vaultindex

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// memSeq names each in-memory database uniquely so separate indexes stay isolated.
var memSeq int64

// Config controls how the vault is indexed.
type Config struct {
	VaultRoot string   // absolute path to the whole vault
	DBPath    string   // where the SQLite projection lives (outside the vault; "" = in-memory)
	AIRegions []string // vault-relative path prefixes tagged AI-authored (default: Agents/, excalibur/)
	SkipDirs  []string // directory base names never walked (default: dotdirs + .trash)
}

func (c Config) aiRegions() []string {
	if len(c.AIRegions) == 0 {
		return []string{"Agents/", "excalibur/"}
	}
	return c.AIRegions
}

// Index is the rebuildable SQLite projection of the vault. It owns only its own
// cache file; it never writes the vault.
type Index struct {
	db  *sql.DB
	cfg Config
}

const schema = `
CREATE TABLE IF NOT EXISTS notes (
  id          INTEGER PRIMARY KEY,   -- rowid, shared with notes_fts for delete-by-rowid
  path        TEXT UNIQUE NOT NULL,
  name        TEXT NOT NULL,
  name_lower  TEXT NOT NULL,
  date        TEXT NOT NULL DEFAULT '',
  date_source TEXT NOT NULL DEFAULT '',
  ai_authored INTEGER NOT NULL DEFAULT 0,
  transcript  INTEGER NOT NULL DEFAULT 0,
  granola_id  TEXT NOT NULL DEFAULT '',
  mtime       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_notes_name_lower ON notes(name_lower);
CREATE INDEX IF NOT EXISTS idx_notes_date       ON notes(date);
CREATE INDEX IF NOT EXISTS idx_notes_granola    ON notes(granola_id);

CREATE TABLE IF NOT EXISTS note_categories (path TEXT NOT NULL, category TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_cat_value ON note_categories(category);
CREATE INDEX IF NOT EXISTS idx_cat_path  ON note_categories(path);

CREATE TABLE IF NOT EXISTS note_aliases (path TEXT NOT NULL, alias TEXT NOT NULL, alias_lower TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_alias_lower ON note_aliases(alias_lower);

CREATE TABLE IF NOT EXISTS note_emails (path TEXT NOT NULL, email TEXT NOT NULL, email_lower TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_email_lower ON note_emails(email_lower);

CREATE TABLE IF NOT EXISTS links (src_path TEXT NOT NULL, target_key TEXT NOT NULL, display TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_key);
CREATE INDEX IF NOT EXISTS idx_links_src    ON links(src_path);

CREATE TABLE IF NOT EXISTS inline_fields (path TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL DEFAULT '');
CREATE INDEX IF NOT EXISTS idx_if_key ON inline_fields(key);

CREATE TABLE IF NOT EXISTS note_tasks (path TEXT NOT NULL, line INTEGER NOT NULL, text TEXT NOT NULL, checked INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL DEFAULT 'checkbox');
CREATE INDEX IF NOT EXISTS idx_task_path ON note_tasks(path);

CREATE TABLE IF NOT EXISTS entities (
  key         TEXT PRIMARY KEY,
  display     TEXT NOT NULL DEFAULT '',
  note_path   TEXT NOT NULL DEFAULT '',
  is_person   INTEGER NOT NULL DEFAULT 0,
  ai_authored INTEGER NOT NULL DEFAULT 0
);

-- rowid matches notes.id, so FTS rows are deleted by rowid (fts5 cannot filter a DELETE by a column)
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(name, body, tokenize='porter unicode61');
`

// Open opens (creating if needed) the SQLite projection and ensures the schema.
// A "" DBPath yields a private in-memory index (used by tests) — each call gets
// its OWN in-memory database (a unique shared-cache name), so concurrent indexes
// never bleed into each other.
func Open(cfg Config) (*Index, error) {
	dsn := fmt.Sprintf("file:vaultindex-mem-%d?mode=memory&cache=shared", atomic.AddInt64(&memSeq, 1))
	if cfg.DBPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
			return nil, err
		}
		dsn = "file:" + cfg.DBPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; the projection is small and single-process
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Index{db: db, cfg: cfg}, nil
}

// schemaVersion bumps whenever the table shapes change. Because the index is a
// disposable projection (Rebuild reproduces it from the vault), a version
// mismatch simply drops every table and recreates — a stale on-disk index from
// an older build upgrades itself with no migration, losslessly.
const schemaVersion = 4

var allTables = []string{"notes", "note_categories", "note_aliases", "note_emails", "links", "inline_fields", "note_tasks", "entities", "notes_fts"}

func ensureSchema(db *sql.DB) error {
	var v int
	_ = db.QueryRow("PRAGMA user_version").Scan(&v)
	if v != schemaVersion {
		for _, t := range allTables {
			if _, err := db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
				return err
			}
		}
		if _, err := db.Exec(schema); err != nil {
			return err
		}
		_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
		return err
	}
	_, err := db.Exec(schema) // fresh DB already at the right version
	return err
}

// Close releases the database handle.
func (ix *Index) Close() error { return ix.db.Close() }

// DB exposes the handle for callers that want raw read queries (read-only).
func (ix *Index) DB() *sql.DB { return ix.db }

// Rebuild wipes and repopulates the projection from a fresh walk of the vault.
// It is lossless: deleting the DB and calling Rebuild reproduces the same index.
func (ix *Index) Rebuild() (int, error) {
	tx, err := ix.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, t := range []string{"notes", "note_categories", "note_aliases", "note_emails", "links", "inline_fields", "note_tasks", "entities", "notes_fts"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return 0, err
		}
	}

	count := 0
	regions := ix.cfg.aiRegions()
	skip := skipSet(ix.cfg.SkipDirs)
	err = filepath.WalkDir(ix.cfg.VaultRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if p != ix.cfg.VaultRoot && (strings.HasPrefix(base, ".") || skip[base]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, err := filepath.Rel(ix.cfg.VaultRoot, p)
		if err != nil {
			return nil
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return nil // unreadable file is skipped, not fatal
		}
		var mtime int64
		if fi, err := d.Info(); err == nil {
			mtime = fi.ModTime().Unix()
		}
		if err := insertNote(tx, ParseNote(filepath.ToSlash(rel), content, mtime, regions)); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := deriveEntities(tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

// insertNote writes one parsed note and all its projected rows within tx. The
// caller guarantees the path is not already present (Rebuild starts empty;
// ReindexPaths deletes first), so the note gets a fresh rowid shared with its
// notes_fts row.
func insertNote(tx *sql.Tx, n Note) error {
	ai := b2i(n.AIAuthored)
	res, err := tx.Exec(`INSERT INTO notes(path,name,name_lower,date,date_source,ai_authored,transcript,granola_id,mtime) VALUES(?,?,?,?,?,?,?,?,?)`,
		n.Path, n.Name, strings.ToLower(n.Name), n.Date, n.DateSource, ai, b2i(n.HasTranscript), n.GranolaID, n.MTime)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, c := range n.Categories {
		if _, err := tx.Exec(`INSERT INTO note_categories(path,category) VALUES(?,?)`, n.Path, c); err != nil {
			return err
		}
	}
	for _, a := range n.Aliases {
		if _, err := tx.Exec(`INSERT INTO note_aliases(path,alias,alias_lower) VALUES(?,?,?)`, n.Path, a, strings.ToLower(a)); err != nil {
			return err
		}
	}
	for _, em := range n.Emails {
		if _, err := tx.Exec(`INSERT INTO note_emails(path,email,email_lower) VALUES(?,?,?)`, n.Path, em, strings.ToLower(em)); err != nil {
			return err
		}
	}
	for _, l := range n.Links {
		if _, err := tx.Exec(`INSERT INTO links(src_path,target_key,display) VALUES(?,?,?)`, n.Path, l.Key, l.Display); err != nil {
			return err
		}
	}
	for _, f := range n.InlineFields {
		if _, err := tx.Exec(`INSERT INTO inline_fields(path,key,value) VALUES(?,?,?)`, n.Path, f.Key, f.Value); err != nil {
			return err
		}
	}
	for _, t := range n.Tasks {
		if _, err := tx.Exec(`INSERT INTO note_tasks(path,line,text,checked,kind) VALUES(?,?,?,?,?)`, n.Path, t.Line, t.Text, b2i(t.Checked), t.Kind); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO notes_fts(rowid,name,body) VALUES(?,?,?)`, id, n.Name, n.Body); err != nil {
		return err
	}
	return nil
}

// deriveEntities materializes the entity table: every note is an entity, and so
// is every link TARGET even when no note exists behind it (audit §0). Note-backed
// entities are inserted first so they win over link-only rows.
func deriveEntities(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM entities`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO entities(key, display, note_path, is_person, ai_authored)
		SELECT n.name_lower, n.name, n.path,
		       (SELECT COUNT(*) FROM note_categories c WHERE c.path=n.path AND c.category='people') > 0,
		       n.ai_authored
		FROM notes n`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO entities(key, display, note_path, is_person, ai_authored)
		SELECT target_key, MIN(display), '', 0, 0 FROM links GROUP BY target_key`); err != nil {
		return err
	}
	return nil
}

func skipSet(extra []string) map[string]bool {
	m := map[string]bool{".git": true, ".obsidian": true, ".trash": true}
	for _, d := range extra {
		m[d] = true
	}
	return m
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
