package homeplan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"manifest/sharedhome"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store is system/home/plan.json. Writes take the same interprocess lock the
// shared Home files use, so the owner's Manifest, Olga's and the CLI can all
// write without one silently replacing another's newer edit.
type Store struct {
	Path  string
	Write func(path string, data []byte) error // the caller's vaultwriter capability
}

// HistoryDir holds every replaced revision: the plan's undo. Nothing here
// deletes them; every file goes through the caller's vaultwriter.
func (s *Store) HistoryDir() string {
	return filepath.Join(filepath.Dir(s.Path), "plan-history")
}

// Revision is the SHA-256 of the file bytes; "" means no plan yet.
func Revision(raw []byte) string {
	if raw == nil {
		return ""
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// ConflictError: the caller's revision is not the file's.
type ConflictError struct{ Current string }

func (e *ConflictError) Error() string {
	return "the plan changed since you loaded it; reload and apply your change again"
}

var ErrNoPlan = errors.New("no shared plan yet")

// Read returns the plan, its raw bytes and revision.
func (s *Store) Read() (*Plan, []byte, string, error) {
	raw, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil, nil, "", ErrNoPlan
	}
	if err != nil {
		return nil, nil, "", err
	}
	p, err := Decode(raw)
	if err != nil {
		return nil, raw, Revision(raw), fmt.Errorf("plan.json is not a valid plan: %w", err)
	}
	return p, raw, Revision(raw), nil
}

// Apply merges patch into the plan at revision, validates the whole result
// and writes it, keeping the replaced bytes in plan-history. revision "" with
// no plan creates one (the patch is then the whole document); revision ""
// with a plan present is a conflict, which makes a seed safe to re-run.
func (s *Store) Apply(revision string, patch []byte, known func(string) bool, now time.Time) (*Plan, string, error) {
	var out *Plan
	var rev string
	err := sharedhome.Locked(s.Path, func() error {
		raw, err := os.ReadFile(s.Path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if os.IsNotExist(err) {
			raw = nil
		}
		if cur := Revision(raw); cur != revision {
			return &ConflictError{Current: cur}
		}
		merged, err := MergePatch(raw, patch)
		if err != nil {
			return err
		}
		p, err := Decode(merged)
		if err != nil {
			return &ValidationError{[]string{err.Error()}}
		}
		var before *Plan
		if raw != nil {
			if before, err = Decode(raw); err != nil {
				before = nil
			}
		}
		var added []string
		for id := range p.Tasks {
			if before == nil || !hasTask(before, id) {
				added = append(added, id)
			}
		}
		sort.Strings(added)
		if err := Validate(p, known, added); err != nil {
			return err
		}
		next, err := Encode(p)
		if err != nil {
			return err
		}
		if raw != nil {
			if string(next) == string(raw) {
				out, rev = p, Revision(raw)
				return nil // nothing changed: no history entry, same revision
			}
			name := now.UTC().Format("20060102T150405.000000000Z") + "-" + Revision(raw)[:12] + ".json"
			if err := s.Write(filepath.Join(s.HistoryDir(), name), raw); err != nil {
				return err
			}
		}
		if err := s.Write(s.Path, next); err != nil {
			return err
		}
		out, rev = p, Revision(next)
		return nil
	})
	return out, rev, err
}

func hasTask(p *Plan, id string) bool { _, ok := p.Tasks[id]; return ok }

// History lists saved revisions, newest first.
func (s *Store) History() []string {
	entries, _ := os.ReadDir(s.HistoryDir())
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			out = append(out, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// Restore replaces the plan with a history entry, as an ordinary
// revision-checked write (so the replaced plan lands in history too).
func (s *Store) Restore(revision, name string, known func(string) bool, now time.Time) (string, error) {
	if strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".json") {
		return "", fmt.Errorf("bad history name")
	}
	old, err := os.ReadFile(filepath.Join(s.HistoryDir(), name))
	if err != nil {
		return "", err
	}
	var rev string
	err = sharedhome.Locked(s.Path, func() error {
		raw, err := os.ReadFile(s.Path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if os.IsNotExist(err) {
			raw = nil
		}
		if cur := Revision(raw); cur != revision {
			return &ConflictError{Current: cur}
		}
		p, err := Decode(old)
		if err != nil {
			return err
		}
		if err := Validate(p, nil, nil); err != nil {
			return err
		}
		if raw != nil {
			bak := now.UTC().Format("20060102T150405.000000000Z") + "-" + Revision(raw)[:12] + ".json"
			if err := s.Write(filepath.Join(s.HistoryDir(), bak), raw); err != nil {
				return err
			}
		}
		if err := s.Write(s.Path, old); err != nil {
			return err
		}
		rev = Revision(old)
		return nil
	})
	return rev, err
}
