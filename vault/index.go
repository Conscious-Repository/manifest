package vault

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Index is a concurrency-safe, rebuildable view of the vault. It is a derived
// cache — never the source of truth — and can be rebuilt from a re-scan at any
// time with identical results.
type Index struct {
	cfg     Config
	scanner *Scanner
	mu      sync.RWMutex
	snap    *Snapshot
}

func NewIndex(cfg Config) (*Index, error) {
	ix := &Index{cfg: cfg, scanner: NewScanner(cfg)}
	if err := ix.Rebuild(); err != nil {
		return nil, err
	}
	return ix, nil
}

// Rebuild re-scans the whole vault and atomically swaps in the new snapshot.
func (ix *Index) Rebuild() error {
	snap, err := ix.scanner.Scan()
	if err != nil {
		return err
	}
	ix.mu.Lock()
	ix.snap = snap
	ix.mu.Unlock()
	return nil
}

// DailyNote resolves a date's note anywhere in the vault. When no note exists it
// returns the path where one WOULD be created (Root/NewDailyDir/<date>.md)
// without creating anything — callers create the file on first save.
func (ix *Index) DailyNote(date string) (string, error) {
	ix.mu.RLock()
	p, ok := ix.snap.Daily[date]
	ix.mu.RUnlock()
	if ok {
		return p, nil
	}
	return filepath.Join(ix.cfg.Root, ix.cfg.NewDailyDir, date+".md"), nil
}

// Lookup is the read-only variant: ("", false) when the date has no note.
func (ix *Index) Lookup(date string) (string, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	p, ok := ix.snap.Daily[date]
	return p, ok
}

// Dates returns all indexed daily-note dates, sorted ascending.
func (ix *Index) Dates() []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]string, 0, len(ix.snap.Daily))
	for d := range ix.snap.Daily {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// GoalsPath returns the indexed goals.md path, or "" if none was found.
func (ix *Index) GoalsPath() string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.snap.GoalsPath
}

// update incrementally folds a single created/modified file into the snapshot.
// Removals and renames go through Rebuild instead (see Watcher). System-zone
// files short-circuit exactly like the scanner: daily/goals classification
// applies only in the knowledge zone.
func (ix *Index) update(path string) {
	if underSystemZone(ix.cfg, path) {
		return
	}
	kind, date := classify(filepath.Base(path), path, ix.cfg.GoalsName)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	switch kind {
	case KindDaily:
		ix.snap.Daily[date] = path
	case KindGoals:
		if ix.snap.GoalsPath == "" {
			ix.snap.GoalsPath = path
		}
	}
}

// underSystemZone reports whether an absolute path is the system-zone root or
// anything inside it, matched by vault-relative path (never by base name).
func underSystemZone(cfg Config, path string) bool {
	if cfg.SystemRoot == "" {
		return false
	}
	rel, err := filepath.Rel(cfg.Root, path)
	if err != nil {
		return false
	}
	r := filepath.ToSlash(rel)
	return r == cfg.SystemRoot || strings.HasPrefix(r, cfg.SystemRoot+"/")
}
