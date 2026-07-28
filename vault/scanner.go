package vault

import (
	"io/fs"

	"manifest/record"
)

// Config controls how the vault is scanned and where new daily notes are created.
type Config struct {
	Root        string   // absolute path to the whole vault
	NewDailyDir string   // dir (relative to Root) where notes are created on save
	GoalsName   string   // filename that marks the goals master file, e.g. "goals.md"
	SkipDirs    []string // directory base names to skip (in addition to dotdirs)
	// SystemRoot is the vault-relative system-zone folder (system-root-plan §1).
	// Daily-note and goals classification apply ONLY in the knowledge zone, so the
	// scanner short-circuits the whole subtree: a date-named file under
	// system/excalibur/ must never be mistaken for a daily note. "" disables.
	SystemRoot string
}

// Snapshot is an immutable index of where things live, produced by one Scan.
type Snapshot struct {
	Daily     map[string]string // "2026-06-29" -> absolute path
	GoalsPath string            // absolute path to goals.md, or ""
}

// Scanner walks the vault and classifies markdown files by convention.
type Scanner struct{ cfg Config }

func NewScanner(cfg Config) *Scanner { return &Scanner{cfg: cfg} }

// Scan walks the vault once and returns a fresh snapshot. Unreadable entries
// are skipped rather than aborting the whole scan. The walk + dir-skip policy
// is the kernel's; the zone short-circuit (no dailies/goals in the system
// zone) is this locator's declaration.
func (s *Scanner) Scan() (*Snapshot, error) {
	snap := &Snapshot{Daily: make(map[string]string)}
	inSystem := func(abs string) bool { return underSystemZone(s.cfg, abs) }
	err := record.WalkMarkdown(s.cfg.Root, record.SkipSet(s.cfg.SkipDirs), inSystem,
		func(path string, d fs.DirEntry) error {
			switch kind, date := classify(d.Name(), path, s.cfg.GoalsName); kind {
			case KindDaily:
				snap.Daily[date] = path
			case KindGoals:
				if snap.GoalsPath == "" {
					snap.GoalsPath = path
				}
			}
			return nil
		})
	return snap, err
}
