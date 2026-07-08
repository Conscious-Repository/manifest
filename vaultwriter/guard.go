package vaultwriter

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
)

// The centralized write guard (system-root-plan §3). It extends the existing
// "read-only on the knowledge vault" invariant with the zone line:
//
//   - ENGINE-OWNED regions (<systemRoot>/agents/, <systemRoot>/excalibur/ — plus
//     the legacy vault-root spellings) are never written by the dashboard, under
//     any class. The excalibur engine owns that subtree; the dashboard's spirits
//     editor goes through its own allow-listed store, not this writer.
//   - DATABASE-class writes (the markdown databases: CRMs, home board, aion)
//     are legal only under <systemRoot>/ — a structured record can never land
//     in the knowledge zone.
//   - RAW-USER-class writes (the note editor, contact-note saves, task toggles
//     — the user's own hands on a note they are looking at) remain legal in
//     both zones, exactly as shipped today. Nothing NEW writes knowledge-zone
//     files; the user editing their own prose is not the app writing.

// WriteClass is the kind of write being attempted, which decides where it may land.
type WriteClass int

const (
	// WriteRawUser is an explicit user edit to a note they can see (editor save,
	// checkbox toggle, frontmatter confirm). Legal anywhere except engine-owned.
	WriteRawUser WriteClass = iota
	// WriteDatabase is a structured record write for the system-zone markdown
	// databases. Legal only under the system root (and never engine-owned).
	WriteDatabase
)

// WithSystemRoot sets the vault-relative system-zone folder the guard uses
// (default "system"). Returns the writer for chaining at construction.
func (w *Writer) WithSystemRoot(root string) *Writer {
	w.systemRoot = strings.Trim(filepath.ToSlash(root), "/")
	return w
}

func (w *Writer) systemRootOrDefault() string {
	if w.systemRoot == "" {
		return "system"
	}
	return w.systemRoot
}

// Guard decides whether a vault-relative write is legal for the given class.
// Every write entry point in this package flows through it.
func (w *Writer) Guard(rel string, class WriteClass) error {
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return errors.New("invalid note path")
	}
	sr := w.systemRootOrDefault()
	for _, owned := range []string{sr + "/agents", sr + "/excalibur", "Agents", "excalibur"} {
		if clean == owned || strings.HasPrefix(clean, owned+"/") {
			return errors.New("that path is engine-owned — the dashboard never writes it")
		}
	}
	if class == WriteDatabase {
		if clean != sr && !strings.HasPrefix(clean, sr+"/") {
			return errors.New("database records live under " + sr + "/ — knowledge-zone writes are refused")
		}
	}
	return nil
}
