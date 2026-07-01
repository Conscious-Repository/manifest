package goals

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/vault"
)

// Store reads and writes the single goals.md master file. The path is resolved
// through the vault Index (so a hand-moved goals.md is still found); a fallback
// at the vault root is used when none has been indexed yet.
type Store struct {
	idx       *vault.Index
	fallback  string
	vaultRoot string
}

func NewStore(idx *vault.Index, vaultRoot, goalsName string) *Store {
	if goalsName == "" {
		goalsName = "goals.md"
	}
	return &Store{idx: idx, fallback: filepath.Join(vaultRoot, goalsName), vaultRoot: vaultRoot}
}

// archivePath is the vault-root file for a quarter's closed Rocks, e.g.
// "<vault>/goals 2026-Q3.md" (space-style, matching the dated-note convention).
func (s *Store) archivePath(quarter string) string {
	return filepath.Join(s.vaultRoot, "goals "+quarter+".md")
}

// CloseGoal moves a Rock out of goals.md and appends it to the archive for the quarter
// it closed in (§6). outcome is "win" or "learn"; note is optional (for a learn).
func (s *Store) CloseGoal(id, outcome, note string, now time.Time) error {
	outcome = strings.ToLower(strings.TrimSpace(outcome))
	if outcome != "win" && outcome != "learn" {
		return fmt.Errorf("outcome must be win or learn, got %q", outcome)
	}
	doc := s.Load()
	area, g := doc.FindGoal(id)
	if area == nil || g == nil {
		return fmt.Errorf("goal %q not found", id)
	}
	if rock := doc.RockOf(id); rock == nil || rock.ID != id {
		return fmt.Errorf("only a Rock can be closed")
	}
	entry := ArchiveEntry{
		Area: area.Name, Text: g.Text, GoalID: g.identity(),
		Outcome: outcome, Closed: now.Format("2006-01-02"),
		Reached: lastStageName(g), Serves: g.Serves, Note: strings.TrimSpace(note),
	}
	doc.DeleteGoal(id)
	if err := s.Save(doc); err != nil {
		return err
	}
	return s.appendArchive(CurrentQuarter(now), entry)
}

// lastStageName is the trail's tip — the last stage under a Rock (completed or current).
func lastStageName(rock *Goal) string {
	if n := len(rock.Children); n > 0 {
		return rock.Children[n-1].Text
	}
	return ""
}

func (s *Store) appendArchive(quarter string, entry ArchiveEntry) error {
	path := s.archivePath(quarter)
	var entries []ArchiveEntry
	if b, err := os.ReadFile(path); err == nil {
		entries = parseArchive(string(b))
	}
	entries = append(entries, entry)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(serializeArchive(quarter, entries)), 0o644)
}

// LoadAllArchives reads every "goals <quarter>.md" at the vault root, newest quarter
// first. Read-only history for the roll-up and the History view.
func (s *Store) LoadAllArchives() []ArchiveQuarter {
	matches, _ := filepath.Glob(filepath.Join(s.vaultRoot, "goals *.md"))
	var out []ArchiveQuarter
	for _, path := range matches {
		q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(filepath.Base(path), ".md"), "goals "))
		if !quarterRe.MatchString(q) {
			continue
		}
		if b, err := os.ReadFile(path); err == nil {
			out = append(out, ArchiveQuarter{Quarter: q, Entries: parseArchive(string(b))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Quarter > out[j].Quarter })
	return out
}

// Path returns the goals.md path: the indexed one, or the root fallback.
func (s *Store) Path() string {
	if p := s.idx.GoalsPath(); p != "" {
		return p
	}
	return s.fallback
}

// Load parses the current goals.md (an empty doc if the file is absent).
func (s *Store) Load() *Doc {
	b, _ := os.ReadFile(s.Path())
	return Parse(string(b))
}

func (s *Store) Save(d *Doc) error {
	p := s.Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(Serialize(d)), 0o644)
}

// Seed creates a starter goals.md with the standard life areas if none exists.
func (s *Store) Seed() error {
	if _, err := os.Stat(s.Path()); err == nil {
		return nil // already exists — never overwrite
	}
	return s.Save(seedDoc())
}

// Migrate performs the silent one-time upgrade from the pre-v2 format (90-day /
// 30-day cascade with due:: dates) to the horizon ladder (§0). It is idempotent:
// already-migrated files pass through untouched. Before the first migrated save it
// writes a one-time "<path>.pre-migration" backup so the change is reversible.
// Returns whether a migration was applied.
func (s *Store) Migrate(now time.Time) (bool, error) {
	path := s.Path()
	b, err := os.ReadFile(path)
	if err != nil {
		return false, nil // no file yet — Seed handles the fresh case
	}
	if !needsMigration(string(b)) {
		return false, nil
	}
	backup := path + ".pre-migration"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err := os.WriteFile(backup, b, 0o644); err != nil {
			return false, err
		}
	}
	doc := Parse(string(b))
	doc.migrateFromLegacy(now)
	if err := s.Save(doc); err != nil {
		return false, err
	}
	return true, nil
}

// Pool returns the 30-day owner==me goals available to pull into a day.
func (s *Store) Pool() []PlateItem {
	return s.Load().Pool()
}

// Promote ensures a goal carries a durable [goal:: id] (so a daily-task backlink
// stays stable across text edits) and returns its text and id. It does not check
// the goal.
func (s *Store) Promote(id string) (text, goalID string, ok bool) {
	doc := s.Load()
	_, g := doc.FindGoal(id)
	if g == nil {
		return "", "", false
	}
	pid := g.explicitID()
	if pid == "" {
		pid = g.ID // the derived id becomes the durable one
		g.Fields = append(g.Fields, Field{Key: "goal", Value: pid})
		if err := s.Save(doc); err != nil {
			return "", "", false
		}
	}
	return g.Text, pid, true
}

func seedDoc() *Doc {
	area := func(name string) *Area {
		return &Area{Name: name, hasAnnual: true, hasRocks: true}
	}
	return &Doc{
		preamble: "# Goals",
		Areas: []*Area{
			area("Aion"),
			area("OODA Group"),
			area("House"),
			area("Personal"),
			area("Sidequests"),
		},
	}
}
