// Package sharedhome projects one canonical Home section into private planners.
package sharedhome

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type Store struct {
	Path  string
	Write func(string, []byte) error
}
type Snapshot struct {
	Content string
	Err     error
}

// Split preserves every other area verbatim. Home is the only shared heading.
func Split(raw string) (private, home string) {
	lines := strings.Split(raw, "\n")
	in := false
	var p, h []string
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			in = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "## ")), "Home")
		}
		if in {
			h = append(h, line)
		} else {
			p = append(p, line)
		}
	}
	return strings.TrimSpace(strings.Join(p, "\n")) + "\n", strings.TrimSpace(strings.Join(h, "\n"))
}
func (s *Store) Load(raw string) (string, *Snapshot) {
	b, err := os.ReadFile(s.Path)
	snap := &Snapshot{Content: strings.TrimSpace(string(b)), Err: err}
	p, _ := Split(raw)
	return p + "\n" + snap.Content + "\n", snap
}

// Save compares the Home snapshot under an interprocess lock. An unrelated
// private edit never overwrites a newer Home; simultaneous Home edits conflict.
func (s *Store) Save(raw string, snap *Snapshot, savePrivate func(string) error) error {
	if snap == nil || snap.Err != nil {
		return fmt.Errorf("shared Home unavailable; reload before saving")
	}
	p, h := Split(raw)
	if h == "" {
		return fmt.Errorf("Home is shared and cannot be removed or renamed")
	}
	return Locked(s.Path, func() error {
		current, err := os.ReadFile(s.Path)
		if err != nil {
			return err
		}
		if h != snap.Content && strings.TrimSpace(string(current)) != snap.Content {
			return fmt.Errorf("Home changed on another device; reload and try again")
		}
		if err := savePrivate(p); err != nil {
			return err
		}
		if h != snap.Content {
			if err := s.Write(s.Path, []byte(h+"\n")); err != nil {
				return err
			}
			snap.Content = h
		}
		return nil
	})
}
func Locked(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
