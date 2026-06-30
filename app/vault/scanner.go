package vault

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Config controls how the vault is scanned and where new daily notes are created.
type Config struct {
	Root        string   // absolute path to the whole vault
	NewDailyDir string   // dir (relative to Root) where notes are created on save
	GoalsName   string   // filename that marks the goals master file, e.g. "goals.md"
	SkipDirs    []string // directory base names to skip (in addition to dotdirs)
	CacheDir    string   // absolute path of the calendar cache; its <date>.md mirrors must never be indexed as daily notes
}

// Snapshot is an immutable index of where things live, produced by one Scan.
type Snapshot struct {
	Daily     map[string]string // "2026-06-29" -> absolute path
	GoalsPath string            // absolute path to goals.md, or ""
	Agents    []string          // absolute paths of type:agent notes
}

// Scanner walks the vault and classifies markdown files by convention.
type Scanner struct{ cfg Config }

func NewScanner(cfg Config) *Scanner { return &Scanner{cfg: cfg} }

// Scan walks the vault once and returns a fresh snapshot. Unreadable entries are
// skipped rather than aborting the whole scan.
func (s *Scanner) Scan() (*Snapshot, error) {
	snap := &Snapshot{Daily: make(map[string]string)}
	skip := make(map[string]bool, len(s.cfg.SkipDirs))
	for _, d := range s.cfg.SkipDirs {
		skip[d] = true
	}
	err := filepath.WalkDir(s.cfg.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == s.cfg.Root {
				return nil
			}
			if s.cfg.CacheDir != "" && path == s.cfg.CacheDir {
				return filepath.SkipDir // calendar cache mirrors are not daily notes
			}
			name := d.Name()
			if skip[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		switch kind, date := classify(d.Name(), path, s.cfg.GoalsName); kind {
		case KindDaily:
			snap.Daily[date] = path
		case KindGoals:
			if snap.GoalsPath == "" {
				snap.GoalsPath = path
			}
		case KindAgent:
			snap.Agents = append(snap.Agents, path)
		}
		return nil
	})
	return snap, err
}
