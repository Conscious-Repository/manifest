package vaultindex

import (
	"regexp"
	"strconv"
	"strings"
)

// Contact-layer graph queries (plans/contacts-feature.md). All read-only.

// meetingCats are the categories that mark a note as a meeting/interaction.
var meetingCats = map[string]bool{"sync": true, "first-meeting": true, "meeting": true, "discussion": true}

// dailyNameRe matches a daily-note name — a bare date with no topic (audit §0.6:
// classify dailies by filename). A dated note WITH a topic is a meeting note.
var dailyNameRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// meetingCtxSQL is the SQL predicate (over alias n) for "this note is a
// meeting-context note": it carries a meeting category, or it is a dated-filename
// note that is NOT a bare daily. Note-less targets seen in such notes are
// auto-contacts; targets seen only elsewhere (dailies) go to triage.
const meetingCtxSQL = `(
  EXISTS(SELECT 1 FROM note_categories c WHERE c.path=n.path AND c.category IN ('sync','first-meeting','meeting','discussion'))
  OR (n.date_source='filename' AND NOT (n.name GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'))
)`

// knowledgeSrcSQL is the SQL predicate (over alias n) for "this note can
// evidence a human interaction" (system-root-plan §3): knowledge zone only —
// a [[name]] in a system-zone record (CRM, agent file) never creates contacts,
// timeline entries, or triage items. The ai_authored belt stays for layouts
// where the AI regions sit at the vault root (pre-reorg).
const knowledgeSrcSQL = `(n.zone = 'knowledge' AND n.ai_authored = 0)`

// PersonSeed is a note-backed person entity (categories: [people]).
type PersonSeed struct {
	Key, Display, NotePath string
}

// PeopleNotes returns every note-backed person entity. Knowledge zone only:
// a system-zone record can never be a person (contacts live in your language).
func (ix *Index) PeopleNotes() ([]PersonSeed, error) {
	rows, err := ix.db.Query(`
		SELECT e.key, e.display, e.note_path FROM entities e
		JOIN notes n ON n.path = e.note_path
		WHERE e.is_person=1 AND ` + knowledgeSrcSQL + ` ORDER BY e.display`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonSeed
	for rows.Next() {
		var p PersonSeed
		if err := rows.Scan(&p.Key, &p.Display, &p.NotePath); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// NoteLessTarget is a link-target entity with no note behind it.
type NoteLessTarget struct {
	Key, Display     string
	RefCount         int  // distinct non-AI notes linking it
	InMeetingContext bool // linked from ≥1 meeting-context note → auto-contact
	LinkedFromPeople int  // distinct PEOPLE notes linking it → org/firm signal (people link their firms)
}

// NoteLessTargets returns every note-less, non-AI link target with its ref count
// and whether it was ever seen in a meeting-context note. The caller (contacts
// service) splits these into auto-contacts vs triage using InMeetingContext.
func (ix *Index) NoteLessTargets() ([]NoteLessTarget, error) {
	rows, err := ix.db.Query(`
		SELECT e.key, e.display,
		  (SELECT COUNT(DISTINCT l.src_path) FROM links l JOIN notes n ON n.path=l.src_path
		     WHERE l.target_key=e.key AND ` + knowledgeSrcSQL + `),
		  EXISTS(SELECT 1 FROM links l JOIN notes n ON n.path=l.src_path
		     WHERE l.target_key=e.key AND ` + knowledgeSrcSQL + ` AND ` + meetingCtxSQL + `),
		  (SELECT COUNT(DISTINCT l.src_path) FROM links l
		     JOIN notes n ON n.path=l.src_path
		     JOIN note_categories c ON c.path=n.path
		     WHERE l.target_key=e.key AND ` + knowledgeSrcSQL + ` AND c.category='people')
		FROM entities e
		WHERE e.note_path='' AND e.ai_authored=0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteLessTarget
	for rows.Next() {
		var t NoteLessTarget
		var mc int
		if err := rows.Scan(&t.Key, &t.Display, &t.RefCount, &mc, &t.LinkedFromPeople); err != nil {
			return nil, err
		}
		t.InMeetingContext = mc == 1
		if t.RefCount > 0 { // ignore targets only referenced by AI-authored notes
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// TimelineEntry is one non-AI backlink for a contact's timeline.
type TimelineEntry struct {
	Path, Name   string
	Date         string // "" when undated → renders as a mention, not a dated event
	SourceType   string // sync|first-meeting|meeting|discussion|daily|note|mention
	IsTranscript bool
}

// Timeline returns every non-AI note linking the entity, newest dated first then
// undated, each classified by source type. last-met = the first dated entry.
func (ix *Index) Timeline(key string) ([]TimelineEntry, error) {
	rows, err := ix.db.Query(`
		SELECT n.path, n.name, n.date, n.date_source, n.transcript,
		  COALESCE((SELECT group_concat(c.category, '|') FROM note_categories c WHERE c.path=n.path), '')
		FROM links l JOIN notes n ON n.path=l.src_path
		WHERE l.target_key=? AND `+knowledgeSrcSQL+`
		ORDER BY (n.date='') ASC, n.date DESC, n.name ASC`, strings.ToLower(strings.TrimSpace(key)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		var dateSource, cats string
		var tr int
		if err := rows.Scan(&e.Path, &e.Name, &e.Date, &dateSource, &tr, &cats); err != nil {
			return nil, err
		}
		e.IsTranscript = tr == 1
		e.SourceType = classifySource(strings.Split(cats, "|"), e.Name, dateSource)
		out = append(out, e)
	}
	return out, rows.Err()
}

// classifySource labels a linking note by how it evidences an interaction.
func classifySource(cats []string, name, dateSource string) string {
	for _, c := range cats {
		if meetingCats[c] {
			return c
		}
	}
	if dailyNameRe.MatchString(name) {
		return "daily"
	}
	if dateSource == "filename" {
		return "note" // dated root note with a topic
	}
	return "mention" // undated
}

// EntityAliases returns the display aliases for an entity: its note's
// alias:/aliases: frontmatter (when a note exists) plus distinct [[target|display]]
// variants that differ from the canonical key.
func (ix *Index) EntityAliases(key string) ([]string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] || strings.ToLower(s) == key {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	// frontmatter aliases (if a note exists behind the entity)
	arows, err := ix.db.Query(`
		SELECT a.alias FROM note_aliases a
		JOIN entities e ON e.note_path = a.path
		WHERE e.key = ?`, key)
	if err == nil {
		for arows.Next() {
			var a string
			if arows.Scan(&a) == nil {
				add(a)
			}
		}
		arows.Close()
	}
	// display variants across links
	drows, err := ix.db.Query(`SELECT DISTINCT display FROM links WHERE target_key = ?`, key)
	if err == nil {
		for drows.Next() {
			var d string
			if drows.Scan(&d) == nil {
				add(d)
			}
		}
		drows.Close()
	}
	return out, nil
}

// LinkedFirms returns org-ish entities the person's own note links to (audit §3:
// an org is inferred as a note a person links to that isn't itself a person or a
// dated interaction). Empty when the entity has no note.
func (ix *Index) LinkedFirms(key string) ([]Entity, error) {
	rows, err := ix.db.Query(`
		SELECT e2.key, e2.display, e2.note_path, e2.is_person, e2.ai_authored
		FROM entities e
		JOIN links l ON l.src_path = e.note_path
		JOIN entities e2 ON e2.key = l.target_key
		LEFT JOIN notes n2 ON n2.path = e2.note_path
		WHERE e.key = ? AND e.note_path != ''
		  AND e2.is_person = 0 AND e2.ai_authored = 0 AND e2.key != e.key
		  AND (n2.path IS NULL OR n2.date = '')     -- not a dated interaction note
		ORDER BY e2.display`, strings.ToLower(strings.TrimSpace(key)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		var person, ai int
		if err := rows.Scan(&e.Key, &e.Display, &e.NotePath, &person, &ai); err != nil {
			return nil, err
		}
		e.HasNote = e.NotePath != ""
		out = append(out, e)
	}
	return out, rows.Err()
}

// SearchRef is a create-flow candidate: an existing entity a typed name might
// bind to, with how many notes reference it.
type SearchRef struct {
	Key, Display, NotePath string
	IsPerson               bool
	HasNote                bool
	RefCount               int
}

// Search finds existing entities whose key or alias matches the query — the
// "bind, don't duplicate" surface (§5). Ordered by reference count.
func (ix *Index) Search(query string) ([]SearchRef, error) {
	q := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	rows, err := ix.db.Query(`
		SELECT DISTINCT e.key, e.display, e.note_path, e.is_person,
		  (SELECT COUNT(DISTINCT l.src_path) FROM links l JOIN notes n ON n.path=l.src_path
		     WHERE l.target_key=e.key AND `+knowledgeSrcSQL+`)
		FROM entities e
		LEFT JOIN note_aliases a ON a.path = e.note_path
		WHERE e.ai_authored=0 AND (e.key LIKE ? OR a.alias_lower LIKE ?)
		  AND (e.note_path='' OR EXISTS(SELECT 1 FROM notes n WHERE n.path=e.note_path AND `+knowledgeSrcSQL+`))
		ORDER BY 5 DESC, e.display ASC
		LIMIT 25`, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchRef
	for rows.Next() {
		var r SearchRef
		var person int
		if err := rows.Scan(&r.Key, &r.Display, &r.NotePath, &person, &r.RefCount); err != nil {
			return nil, err
		}
		r.IsPerson = person == 1
		r.HasNote = r.NotePath != ""
		out = append(out, r)
	}
	return out, rows.Err()
}

// OpenLoop is one unchecked task/next-step surfaced from a meeting-context note.
type OpenLoop struct {
	Path, Name, Date, Text, Kind string
	Line                         int
}

// OpenLoops returns the unchecked tasks + next-step lines from meeting-context
// notes that link the entity (any of keys), newest note first. A loop in a
// multi-person note surfaces for each linked person — the caller does not dedupe.
func (ix *Index) OpenLoops(keys []string) ([]OpenLoop, error) {
	seen := map[string]bool{}
	var out []OpenLoop
	for _, key := range keys {
		rows, err := ix.db.Query(`
			SELECT t.path, n.name, n.date, t.line, t.text, t.kind
			FROM note_tasks t
			JOIN notes n ON n.path = t.path
			JOIN links l ON l.src_path = t.path
			WHERE l.target_key = ? AND t.checked = 0 AND `+knowledgeSrcSQL+` AND `+meetingCtxSQL+`
			ORDER BY n.date DESC, t.line ASC`, strings.ToLower(strings.TrimSpace(key)))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var l OpenLoop
			if err := rows.Scan(&l.Path, &l.Name, &l.Date, &l.Line, &l.Text, &l.Kind); err != nil {
				rows.Close()
				return nil, err
			}
			sig := l.Path + "\x00" + strconv.Itoa(l.Line)
			if !seen[sig] {
				seen[sig] = true
				out = append(out, l)
			}
		}
		rows.Close()
	}
	return out, nil
}

// OpenLoopCounts returns, per link-target key, how many unchecked meeting-context
// loops reference it — the contacts-list rollup (one query for all contacts).
func (ix *Index) OpenLoopCounts() (map[string]int, error) {
	rows, err := ix.db.Query(`
		SELECT l.target_key, COUNT(*)
		FROM note_tasks t
		JOIN notes n ON n.path = t.path
		JOIN links l ON l.src_path = t.path
		WHERE t.checked = 0 AND ` + knowledgeSrcSQL + ` AND ` + meetingCtxSQL + `
		GROUP BY l.target_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var c int
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, rows.Err()
}

// InteractionDatesByKey returns, per link-target key, the distinct dates of its
// non-AI dated interactions (ascending) — the input to the neglect computation.
func (ix *Index) InteractionDatesByKey() (map[string][]string, error) {
	rows, err := ix.db.Query(`
		SELECT l.target_key, n.date
		FROM links l JOIN notes n ON n.path = l.src_path
		WHERE ` + knowledgeSrcSQL + ` AND n.date != ''
		GROUP BY l.target_key, n.date
		ORDER BY l.target_key, n.date`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var k, d string
		if err := rows.Scan(&k, &d); err != nil {
			return nil, err
		}
		out[k] = append(out[k], d)
	}
	return out, rows.Err()
}

// Emails returns the confirmed emails on an entity's note (frontmatter email:).
func (ix *Index) Emails(key string) ([]string, error) {
	rows, err := ix.db.Query(`
		SELECT em.email FROM note_emails em
		JOIN entities e ON e.note_path = em.path
		WHERE e.key = ?`, strings.ToLower(strings.TrimSpace(key)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// ResolveEmail returns the entity key whose note carries the given email, or ""
// — the exact match used once a contact's email is confirmed (§6). Knowledge
// zone only: an email field on a system-zone record is data, not a contact.
func (ix *Index) ResolveEmail(email string) string {
	var k string
	if err := ix.db.QueryRow(`
		SELECT e.key FROM note_emails em
		JOIN entities e ON e.note_path = em.path
		JOIN notes n ON n.path = em.path
		WHERE em.email_lower = ? AND `+knowledgeSrcSQL+` LIMIT 1`,
		strings.ToLower(strings.TrimSpace(email))).Scan(&k); err != nil {
		return ""
	}
	return k
}

// NoteZone returns a note's zone ("knowledge" | "system"), defaulting to
// knowledge for unknown paths — the note view shows a quiet SYSTEM badge.
func (ix *Index) NoteZone(rel string) string {
	var z string
	if err := ix.db.QueryRow(`SELECT zone FROM notes WHERE path = ?`, rel).Scan(&z); err != nil || z == "" {
		return "knowledge"
	}
	return z
}

// VaultRoot exposes the indexed vault root (the contacts service reads note
// bodies directly for the raw-markdown editor).
func (ix *Index) VaultRoot() string { return ix.cfg.VaultRoot }
