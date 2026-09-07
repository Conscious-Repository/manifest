package vaultwriter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"manifest/record"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Serializes cooperating writers, including distinct Writer instances in one process.
// External editors do not participate: backups preserve the last observed bytes.
var editMu sync.Mutex

func Revision(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type Conflict struct {
	Raw      string `json:"raw"`
	Revision string `json:"revision"`
	Missing  bool   `json:"missing"`
}

func (c *Conflict) Error() string {
	return "note changed outside this editor; review the current version"
}

// SafePath refuses symlinks at every component, including redirects within the
// vault into a different write zone. The configured vault root itself may be a link.
func SafePath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "\\") {
		return "", errors.New("invalid vault path")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("invalid vault path")
	}
	full := root
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == "." {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return "", errors.New("hidden paths are not editable")
		}
		full = filepath.Join(full, part)
		fi, err := os.Lstat(full)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symlink paths are not editable")
		}
	}
	return full, nil
}

func (w *Writer) editorPath(rel string) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	if !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return "", errors.New("a Markdown filename is required")
	}
	if err := w.Guard(rel, WriteRawUser); err != nil {
		return "", err
	}
	zone := record.ZoneOf(filepath.ToSlash(filepath.Clean(rel)), w.systemRootOrDefault(), w.extrinsicRootOrDefault())
	if _, _, err := w.checkCap("note-editor-"+zone, rel); err != nil {
		return "", err
	}
	return SafePath(w.vault, rel)
}

func checkRevision(full, expected string) ([]byte, error) {
	if expected == "" {
		return nil, errors.New("ifRevision is required")
	}
	raw, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return nil, &Conflict{Missing: true}
	}
	if err != nil {
		return nil, err
	}
	if Revision(raw) != expected {
		return nil, &Conflict{Raw: string(raw), Revision: Revision(raw)}
	}
	return raw, nil
}

func atomicBytes(full string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(full), ".manifest-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode.Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), full); err != nil {
		return err
	}
	// A directory-sync failure after rename must not claim the bytes did not land.
	if d, e := os.Open(filepath.Dir(full)); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func exclusiveBytes(full string, data []byte) error {
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		_ = os.Remove(full)
	}
	return err
}

func (w *Writer) WriteNoteIfRevision(rel, raw, expected string) (string, error) {
	editMu.Lock()
	defer editMu.Unlock()
	full, err := w.editorPath(rel)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(raw) || len(raw) > 4<<20 {
		return "", errors.New("note must be UTF-8 and at most 4 MB")
	}
	before, err := checkRevision(full, expected)
	if err != nil {
		return "", err
	}
	if string(before) == raw {
		return expected, nil
	}
	fi, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	// Content-addressed sibling history is independent of the derived index.
	backup := full + ".pre-write-" + Revision(before)
	if err = exclusiveBytes(backup, before); err != nil {
		if !os.IsExist(err) {
			return "", fmt.Errorf("could not preserve previous version: %w", err)
		}
		fi, e := os.Lstat(backup)
		if e != nil || !fi.Mode().IsRegular() {
			return "", errors.New("previous-version backup is not a regular file")
		}
		saved, e := os.ReadFile(backup)
		if e != nil || string(saved) != string(before) {
			return "", errors.New("previous-version backup does not match observed bytes")
		}
	}
	if _, err = checkRevision(full, expected); err != nil {
		return "", err
	}
	if err = atomicBytes(full, []byte(raw), fi.Mode()); err != nil {
		return "", err
	}
	w.traced(w.audit(rel, "note-write", string(ActorUserAction), int64(len(raw)-len(before))))
	return Revision([]byte(raw)), nil
}

func (w *Writer) CreateNote(rel, raw string) (string, error) {
	editMu.Lock()
	defer editMu.Unlock()
	full, err := w.editorPath(rel)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(raw) || len(raw) > 4<<20 {
		return "", errors.New("note must be UTF-8 and at most 4 MB")
	}
	if err = exclusiveBytes(full, []byte(raw)); err != nil {
		return "", err
	}
	w.traced(w.audit(rel, "note-create", string(ActorUserAction), int64(len(raw))))
	return Revision([]byte(raw)), nil
}

// MoveNote uses an exclusive hard link then unlinks the old name. Destination
// collisions cannot overwrite another note. Same-vault filesystems are required.
func (w *Writer) MoveNote(from, to, expected string) error {
	editMu.Lock()
	defer editMu.Unlock()
	old, err := w.editorPath(from)
	if err != nil {
		return err
	}
	next, err := w.editorPath(to)
	if err != nil {
		return err
	}
	if _, err = checkRevision(old, expected); err != nil {
		return err
	}
	if old == next {
		return nil
	}
	if err = os.Link(old, next); err != nil {
		return err
	}
	if err = os.Remove(old); err != nil {
		_ = os.Remove(next)
		return err
	}
	w.traced(w.audit(from+" -> "+to, "note-move", string(ActorUserAction), 0))
	return nil
}

// UpdateCap serializes a record transform and preserves its latest observed
// bytes. The callback receives the full record, including unknown fields.
func (w *Writer) UpdateCap(capName, rel string, transform func([]byte) ([]byte, error)) error {
	editMu.Lock()
	defer editMu.Unlock()
	c, clean, err := w.checkCap(capName, rel)
	if err != nil {
		return err
	}
	full, err := SafePath(w.vault, clean)
	if err != nil {
		return err
	}
	before, err := os.ReadFile(full)
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		return err
	}
	after, err := transform(before)
	if err != nil {
		return err
	}
	if string(before) == string(after) {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	if missing {
		err = exclusiveBytes(full, after)
	} else {
		if _, err = checkRevision(full, Revision(before)); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if fi, e := os.Stat(full); e == nil {
			mode = fi.Mode()
		}
		err = atomicBytes(full, after, mode)
	}
	if err != nil {
		return err
	}
	w.traced(w.audit(clean, c.Name, string(c.Actor), int64(len(after)-len(before))))
	return nil
}

// MoveNoteWithRecord moves a document and its conversation companion. All
// destination creation is exclusive. The original note is removed last, so an
// interrupted move can leave duplicate names but cannot destroy the only copy.
// Unlike a database transaction, this does not promise cross-file atomicity.
func (w *Writer) MoveNoteWithRecord(from, to, expected, recordFrom, recordTo string, before, after []byte) error {
	if len(before) == 0 {
		return w.MoveNote(from, to, expected)
	}
	editMu.Lock()
	defer editMu.Unlock()
	old, err := w.editorPath(from)
	if err != nil {
		return err
	}
	next, err := w.editorPath(to)
	if err != nil {
		return err
	}
	if old == next {
		return nil
	}
	if _, err = checkRevision(old, expected); err != nil {
		return err
	}
	c, _, err := w.checkCap("writing", recordFrom)
	if err != nil {
		return err
	}
	if _, _, err = w.checkCap("writing", recordTo); err != nil {
		return err
	}
	rf, err := SafePath(w.vault, recordFrom)
	if err != nil {
		return err
	}
	rt, err := SafePath(w.vault, recordTo)
	if err != nil {
		return err
	}
	if _, err = checkRevision(rf, Revision(before)); err != nil {
		return err
	}
	if err = exclusiveBytes(rt, after); err != nil {
		return err
	}
	if err = os.Link(old, next); err != nil {
		_ = os.Remove(rt)
		return err
	}
	if err = os.Remove(old); err != nil {
		_ = os.Remove(next)
		_ = os.Remove(rt)
		return err
	}
	// Keep the old companion as an explicit move receipt outside .md indexing.
	// A failure to archive it leaves a recoverable duplicate, not a failed move.
	archive := rf + ".moved-" + Revision(after)
	_ = os.Rename(rf, archive)
	w.traced(w.audit(from+" -> "+to, "note-move", string(ActorUserAction), 0))
	w.traced(w.audit(recordFrom+" -> "+recordTo, c.Name, string(c.Actor), int64(len(after)-len(before))))
	return nil
}

func (w *Writer) ToggleTaskIfRevision(rel string, line int, want bool, expected string) (string, error) {
	full, err := w.editorPath(rel)
	if err != nil {
		return "", err
	}
	raw, err := checkRevision(full, expected)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(raw), "\n")
	if line < 0 || line >= len(lines) {
		return "", errors.New("line out of range")
	}
	original := lines[line]
	plain := strings.TrimSuffix(original, "\r")
	if !taskMarkRe.MatchString(plain) {
		return "", errors.New("that line is no longer a task")
	}
	mark := " "
	if want {
		mark = "x"
	}
	lines[line] = taskMarkRe.ReplaceAllString(plain, "${1}"+mark+"${2}")
	if strings.HasSuffix(original, "\r") {
		lines[line] += "\r"
	}
	return w.WriteNoteIfRevision(rel, strings.Join(lines, "\n"), expected)
}
