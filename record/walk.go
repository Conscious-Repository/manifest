package record

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// Filename date grammar — the two DISTINCT conventions, both kernel-owned:
//
//   - DailyNoteRe matches a daily-note filename EXACTLY (YYYY-MM-DD.md and
//     nothing more). It must stay strictly anchored: many notes merely START
//     with a date ("2026-01-09 meeting.md") and are NOT daily notes.
//   - DatedPrefixRe matches any dated-prefix name (the index's "this note has
//     a date" claim; also validates ISO date scalars).
var (
	DailyNoteRe   = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)
	DatedPrefixRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
)

// SkipSet is the standard never-walk set (all dot-dirs are skipped anyway;
// these survive even if a config lists them without the dot) plus extras.
func SkipSet(extra []string) map[string]bool {
	m := map[string]bool{".git": true, ".obsidian": true, ".trash": true}
	for _, d := range extra {
		m[d] = true
	}
	return m
}

// skipEntry is the one dir-skip policy: dot-dirs, the skip set, and an
// optional caller predicate (zone short-circuits).
func skipEntry(root, path, base string, skip map[string]bool, skipDir func(abs string) bool) bool {
	if path == root {
		return false
	}
	if strings.HasPrefix(base, ".") || skip[base] {
		return true
	}
	return skipDir != nil && skipDir(path)
}

// WalkMarkdown walks root and calls fn for every markdown file (suffix
// matched case-insensitively), applying the standard dir-skip policy.
// Unreadable entries are skipped, not fatal; an error from fn aborts the walk.
func WalkMarkdown(root string, skip map[string]bool, skipDir func(abs string) bool, fn func(abs string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipEntry(root, path, d.Name(), skip, skipDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		return fn(path, d)
	})
}

// WalkDirs visits every directory under root (same skip policy) — fsnotify
// watch registration for both index engines' watchers.
func WalkDirs(root string, skip map[string]bool, skipDir func(abs string) bool, add func(abs string)) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if skipEntry(root, path, d.Name(), skip, skipDir) {
			return filepath.SkipDir
		}
		add(path)
		return nil
	})
}
