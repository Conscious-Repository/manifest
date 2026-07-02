package vaultindex

import (
	"database/sql"
	"strings"
)

// NoteRef is a note in a result set (the projection of Dataview's file.link).
type NoteRef struct {
	Path  string
	Name  string
	Date  string
	MTime int64
}

// SortOrder mirrors the SORT clause of the vault's Dataview index notes.
type SortOrder int

const (
	SortNameAsc   SortOrder = iota // _index_people: SORT file.name ASC
	SortMtimeDesc                  // _index_syncs:  SORT file.mtime DESC
)

// Category reproduces Dataview's `WHERE contains(categories, value)` over the
// whole vault — the exact semantics of the categories/_index_* notes. Values are
// matched EXACTLY (no normalization); both YAML styles were already unified at
// parse time. AI-authored notes are NOT excluded here (a category index is not
// an interaction timeline) so the result set matches Dataview's `FROM ""`.
func (ix *Index) Category(value string, order SortOrder) ([]NoteRef, error) {
	q := `SELECT n.path, n.name, n.date, n.mtime
	      FROM notes n JOIN note_categories c ON c.path = n.path
	      WHERE c.category = ? `
	switch order {
	case SortMtimeDesc:
		q += `ORDER BY n.mtime DESC, n.name ASC`
	default:
		q += `ORDER BY n.name ASC`
	}
	rows, err := ix.db.Query(q, value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteRef
	for rows.Next() {
		var r NoteRef
		if err := rows.Scan(&r.Path, &r.Name, &r.Date, &r.MTime); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Entity is a resolved node in the graph: a note, or a bare link target.
type Entity struct {
	Key        string
	Display    string
	NotePath   string // "" when no note exists behind the link target
	HasNote    bool
	IsPerson   bool
	AIAuthored bool
}

// Entity looks up one entity by its key (a lowercased name/target).
func (ix *Index) Entity(key string) (Entity, bool) {
	return scanEntity(ix.db.QueryRow(
		`SELECT key, display, note_path, is_person, ai_authored FROM entities WHERE key = ?`,
		strings.ToLower(strings.TrimSpace(key))))
}

// Resolve maps a raw name/nickname/handle to an entity: first a note by name,
// then an alias (alias:/aliases:), then a bare link target. The alias map is
// general (not person-only), per the audit.
func (ix *Index) Resolve(name string) (Entity, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return Entity{}, false
	}
	// 1. a note whose name matches
	if e, ok := scanEntity(ix.db.QueryRow(
		`SELECT e.key,e.display,e.note_path,e.is_person,e.ai_authored
		 FROM entities e JOIN notes n ON n.path=e.note_path
		 WHERE n.name_lower = ? LIMIT 1`, lower)); ok {
		return e, true
	}
	// 2. an alias on some note
	var path string
	if err := ix.db.QueryRow(`SELECT path FROM note_aliases WHERE alias_lower = ? LIMIT 1`, lower).Scan(&path); err == nil {
		if e, ok := scanEntity(ix.db.QueryRow(
			`SELECT key,display,note_path,is_person,ai_authored FROM entities WHERE note_path = ? LIMIT 1`, path)); ok {
			return e, true
		}
	}
	// 3. a bare link-target entity
	return ix.Entity(lower)
}

// Backlink is one note that links an entity, with its date provenance.
type Backlink struct {
	Path       string
	Name       string
	Date       string // "" when undated
	DateSource string // "filename" | "frontmatter" | ""
	AIAuthored bool
}

// Backlinks returns every note linking the entity (AI-authored included),
// newest dated first, undated last. Use Interactions for the human timeline.
func (ix *Index) Backlinks(key string) ([]Backlink, error) {
	return ix.backlinks(key, false)
}

// Interactions is the human interaction timeline: notes linking the entity with
// AI-authored content EXCLUDED (a generated note about a person is not talking
// to them — audit §5), newest dated first.
func (ix *Index) Interactions(key string) ([]Backlink, error) {
	return ix.backlinks(key, true)
}

func (ix *Index) backlinks(key string, excludeAI bool) ([]Backlink, error) {
	q := `SELECT DISTINCT n.path, n.name, n.date, n.date_source, n.ai_authored
	      FROM links l JOIN notes n ON n.path = l.src_path
	      WHERE l.target_key = ? `
	if excludeAI {
		q += `AND n.ai_authored = 0 `
	}
	q += `ORDER BY (n.date = '') ASC, n.date DESC, n.name ASC`
	rows, err := ix.db.Query(q, strings.ToLower(strings.TrimSpace(key)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Backlink
	for rows.Next() {
		var b Backlink
		var ai int
		if err := rows.Scan(&b.Path, &b.Name, &b.Date, &b.DateSource, &ai); err != nil {
			return nil, err
		}
		b.AIAuthored = ai == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

// LastMet returns the most recent DATED interaction with an entity — the max
// date across non-AI, dated backlinks. Undated notes never contribute a date,
// so "last met" derives only from dated sources (audit §0). ok is false when
// there is no dated interaction at all.
func (ix *Index) LastMet(key string) (date, sourcePath string, ok bool) {
	err := ix.db.QueryRow(
		`SELECT n.date, n.path
		 FROM links l JOIN notes n ON n.path = l.src_path
		 WHERE l.target_key = ? AND n.ai_authored = 0 AND n.date != ''
		 ORDER BY n.date DESC, n.name ASC LIMIT 1`,
		strings.ToLower(strings.TrimSpace(key))).Scan(&date, &sourcePath)
	if err != nil {
		return "", "", false
	}
	return date, sourcePath, true
}

// Mentions is the FTS fallback: notes whose body/name matches the query text.
// AI-authored notes are excluded so briefs/digests don't masquerade as sources.
func (ix *Index) Mentions(text string, limit int) ([]NoteRef, error) {
	m := strings.TrimSpace(text)
	if m == "" {
		return nil, nil
	}
	match := `"` + strings.ReplaceAll(m, `"`, `""`) + `"` // phrase match, quote-escaped
	if limit <= 0 {
		limit = 50
	}
	rows, err := ix.db.Query(
		`SELECT n.path, n.name, n.date, n.mtime
		 FROM notes_fts JOIN notes n ON n.id = notes_fts.rowid
		 WHERE notes_fts MATCH ? AND n.ai_authored = 0
		 ORDER BY rank LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteRef
	for rows.Next() {
		var r NoteRef
		if err := rows.Scan(&r.Path, &r.Name, &r.Date, &r.MTime); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanEntity(row *sql.Row) (Entity, bool) {
	var e Entity
	var person, ai int
	if err := row.Scan(&e.Key, &e.Display, &e.NotePath, &person, &ai); err != nil {
		return Entity{}, false
	}
	e.HasNote = e.NotePath != ""
	e.IsPerson = person == 1
	e.AIAuthored = ai == 1
	return e, true
}
