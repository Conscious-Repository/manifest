package recruiting

import (
	"strings"
	"time"
)

// PASSED — a decision that survives the run it was made in.
//
// Passing a draft used to mark one row in ONE run's cache. Sweep the same lab
// next week and the same people came back as `new`, with no memory that they
// had already been looked at and declined. That is how a queue silts up: the
// work of deciding is thrown away, and the queue grows back.
//
// The fix is the one every sourcing tool converged on — LinkedIn's hidden
// candidates, SeekOut's hidden filters, Gem's "candidates are only added if
// you mark them Yes": a rejection SUPPRESSES AT SEARCH TIME, not just in a
// folder. What lands here is a TOMBSTONE, never a record: an id, a name key, a
// reason and a date. No profile, no evidence, no PII beyond the name that has
// to be matched on — a person you declined does not earn a file in the vault.
//
// The tombstone is what `candidateIndex` consults, so the next run marks them
// `passed` instead of offering them again, and undoing a pass lifts the stone.

// passedKeys is the closed row vocabulary of passed.md.
var passedKeys = []string{"key", "name", "reason", "source", "at"}

func passedRecognized(r *Row) bool { return r.Has("key") }

// Passed is one tombstone.
type Passed struct {
	// Key is the match key — the same normalized name (or `source:externalId`)
	// the duplicate check uses, so suppression and dedupe agree by
	// construction rather than by two lists that drift.
	Key     string  `json:"key"`
	Name    string  `json:"name,omitempty"`
	Reason  string  `json:"reason,omitempty"`
	Source  string  `json:"source,omitempty"`
	At      string  `json:"at,omitempty"`
	Unknown []Field `json:"unknown,omitempty"`
}

// PassedDoc is passed.md.
type PassedDoc struct {
	DocFM
	Lines []Line
}

// ParsePassed reads passed.md.
func ParsePassed(content string) *PassedDoc {
	d := &PassedDoc{}
	d.DocFM, d.Lines = parseRows(content, passedRecognized)
	return d
}

// SerializePassed is the fixpoint emitter for passed.md.
func SerializePassed(d *PassedDoc) string { return serializeRows(d.DocFM, d.Lines) }

// Passed collects the rows in order.
func (d *PassedDoc) Passed() []Passed {
	out := []Passed{}
	for _, ln := range d.Lines {
		if ln.Row == nil {
			continue
		}
		r := ln.Row
		out = append(out, Passed{
			Key: r.Get("key"), Name: r.Get("name"), Reason: r.Get("reason"),
			Source: r.Get("source"), At: r.Get("at"),
			Unknown: unknownFields(r, passedKeys...),
		})
	}
	return out
}

// Add records one pass, or refreshes the reason and date of one already
// recorded — passing the same person twice is not two facts.
func (d *PassedDoc) Add(p Passed) (Passed, error) {
	p.Key = strings.TrimSpace(p.Key)
	if p.Key == "" {
		return Passed{}, errf("a pass needs the key it suppresses")
	}
	if row := findRow(d.Lines, "key", p.Key); row != nil {
		setOrRemove(row, "reason", strings.TrimSpace(p.Reason))
		setOrRemove(row, "at", strings.TrimSpace(p.At))
		return p, nil
	}
	r := newRow("key", p.Key)
	for _, kv := range [][2]string{{"name", p.Name}, {"reason", p.Reason},
		{"source", p.Source}, {"at", p.At}} {
		if strings.TrimSpace(kv[1]) != "" {
			r.Set(kv[0], kv[1])
		}
	}
	d.Lines = append(d.Lines, Line{Row: r})
	return p, nil
}

// Remove lifts one tombstone — the undo behind `undo pass`.
func (d *PassedDoc) Remove(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	kept := d.Lines[:0]
	gone := false
	for _, ln := range d.Lines {
		if ln.Row != nil && strings.TrimSpace(ln.Row.Get("key")) == key {
			gone = true
			continue
		}
		kept = append(kept, ln)
	}
	d.Lines = kept
	return gone
}

// PassedKey is the suppression key for a draft: its external id when the
// source gave one (exact, and immune to a name spelled two ways), else the
// normalized name — the same two rules, in the same order, that
// candidateIndex.match uses to spot a duplicate.
func PassedKey(source, externalID, name string) string {
	if ext := strings.TrimSpace(externalID); ext != "" && strings.TrimSpace(source) != "" {
		return strings.TrimSpace(source) + ":" + ext
	}
	return normalizeKey(name)
}

// ---- store ----

func (s *Store) LoadPassed() *PassedDoc { return ParsePassed(s.raw("passed.md")) }

func (s *Store) SavePassed(d *PassedDoc) error { return s.save("passed.md", SerializePassed(d)) }

// PassedSet is every suppressed key, for the run path to consult.
func (s *Store) PassedSet() map[string]Passed {
	out := map[string]Passed{}
	for _, p := range s.LoadPassed().Passed() {
		if k := strings.TrimSpace(p.Key); k != "" {
			out[k] = p
		}
	}
	return out
}

// AddPassed records a pass.
func (s *Store) AddPassed(p Passed, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(p.At) == "" {
		p.At = now.UTC().Format("2006-01-02")
	}
	doc := s.LoadPassed()
	if _, err := doc.Add(p); err != nil {
		return err
	}
	return s.SavePassed(doc)
}

// RemovePassed lifts a pass. A key that was never recorded is not an error:
// undo runs after a pass that predates this file, and refusing there would
// make an old queue impossible to correct.
func (s *Store) RemovePassed(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadPassed()
	if !doc.Remove(key) {
		return nil
	}
	return s.SavePassed(doc)
}
