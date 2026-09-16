package transcriptsync

import "database/sql"

// Index borrows the application index. It never owns or closes the database.
type Index struct{ db *sql.DB }

func NewIndex(db *sql.DB) *Index { return &Index{db: db} }

// PathsByGranolaID returns vault-relative paths of notes carrying granola-id.
func (i *Index) PathsByGranolaID(id string) ([]string, error) {
	if id == "" {
		return nil, nil
	}
	return i.queryStrings(`SELECT path FROM notes WHERE granola_id = ?`, id)
}

// PathsByPocketID returns vault-relative paths of notes carrying pocket-id.
// Tolerant of an index predating the pocket_id column (manifest M3): the
// query error surfaces to the caller, whose err==nil guard degrades to the
// filename/date dedupe legs.
func (i *Index) PathsByPocketID(id string) ([]string, error) {
	if id == "" {
		return nil, nil
	}
	return i.queryStrings(`SELECT path FROM notes WHERE pocket_id = ?`, id)
}

// NamesByDate returns note basenames (no .md) whose date equals date (YYYY-MM-DD).
func (i *Index) NamesByDate(date string) ([]string, error) {
	if date == "" {
		return nil, nil
	}
	return i.queryStrings(`SELECT name FROM notes WHERE date = ?`, date)
}

// ResolveName resolves a candidate attendee name to a canonical display form.
// It checks entities by key (lowercased name), then note aliases. resolved is
// false when nothing matches (the caller then links the bare name).
func (i *Index) ResolveName(name string) (string, bool) {
	lower := lc(name)
	if lower == "" {
		return name, false
	}
	var display string
	err := i.db.QueryRow(`SELECT display FROM entities WHERE key = ?`, lower).Scan(&display)
	if err == nil && display != "" {
		return display, true
	}
	// alias → note name
	var path string
	if err := i.db.QueryRow(`SELECT path FROM note_aliases WHERE alias_lower = ? LIMIT 1`, lower).Scan(&path); err == nil && path != "" {
		var nm string
		if err := i.db.QueryRow(`SELECT name FROM notes WHERE path = ?`, path).Scan(&nm); err == nil && nm != "" {
			return nm, true
		}
	}
	return name, false
}

// ResolvePerson resolves a name-ish token from a meeting title to a canonical
// vault PERSON, for attendee seeding. It matches exactly (entity key), else a
// unique first-name prefix ("austin" → "Austin Tunnell"). Person-only and
// unique-only so a title word never links a company or an ambiguous name.
func (i *Index) ResolvePerson(name string) (string, bool) {
	lower := lc(name)
	if lower == "" {
		return "", false
	}
	var display string
	if err := i.db.QueryRow(`SELECT display FROM entities WHERE key = ? AND is_person = 1 AND display != ''`, lower).Scan(&display); err == nil && display != "" {
		return display, true
	}
	// unique first-name (or leading-token) prefix among people
	rows, err := i.db.Query(`SELECT DISTINCT display FROM entities WHERE is_person = 1 AND display != '' AND (key = ? OR key LIKE ?)`, lower, lower+" %")
	if err != nil {
		return "", false
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil {
			matches = append(matches, d)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func (i *Index) queryStrings(q string, args ...any) ([]string, error) {
	rows, err := i.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func lc(s string) string {
	b := []rune(s)
	for i, r := range b {
		if r >= 'A' && r <= 'Z' {
			b[i] = r + ('a' - 'A')
		}
	}
	// trim spaces
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t') {
		end--
	}
	return string(b[start:end])
}
