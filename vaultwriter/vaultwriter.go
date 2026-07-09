// Package vaultwriter performs the ONE kind of write the app makes into the Obsidian
// knowledge vault: creating an `extrinsic/<title>.md` note when the user clicks
// "Save to vault" on a feed item. It mirrors the vault's own convention
// (categories: [...] frontmatter + a #tag) and is deliberately WRITE-ONCE — it never
// overwrites an existing hand-authored note. Everything else the agents produce stays
// outside the vault. This is user-triggered only.
package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"manifest/mdfm"
)

// Writer targets a single vault root.
type Writer struct {
	vault         string
	systemRoot    string // vault-relative system-zone folder for the write guard ("" → "system")
	extrinsicRoot string // vault-relative extrinsic-zone folder (books/articles) ("" → "extrinsic")
}

// New builds a writer for the given vault path ("" disables saving).
func New(vaultPath string) *Writer { return &Writer{vault: vaultPath} }

// Enabled reports whether a vault is configured.
func (w *Writer) Enabled() bool { return w.vault != "" }

// SaveExtrinsic creates <vault>/extrinsic/<title>.md and returns the vault-relative
// path. If a note with that title already exists it is returned untouched (write-once)
// — we never clobber the user's notes. The path is guarded to stay under extrinsic/.
// The filename is lowercased to match the vault's convention (all person/book
// notes are lowercase; Obsidian resolves [[Links]] case-insensitively).
func (w *Writer) SaveExtrinsic(title, itemType, why, link, source, body string) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	name := strings.ToLower(sanitizeName(title))
	if name == "" {
		return "", errors.New("cannot save an item with an empty title")
	}
	extrinsic := filepath.Join(w.vault, "extrinsic")
	full := filepath.Join(extrinsic, name+".md")
	if !isUnder(full, extrinsic) { // defend against traversal in the title
		return "", errors.New("invalid note path")
	}
	rel := filepath.Join("extrinsic", name+".md")
	if err := w.Guard(filepath.ToSlash(rel), WriteRawUser); err != nil {
		return "", err
	}
	if _, err := os.Stat(full); err == nil {
		return rel, nil // already exists — write-once, keep the user's note
	}
	if err := os.MkdirAll(extrinsic, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, []byte(buildNote(itemType, why, link, source, body)), 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// categoryFor maps a feed item type to the vault's category + tag conventions.
func categoryFor(itemType string) (category, tag string) {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "person", "people":
		return "people", "person"
	case "paper", "papers":
		return "papers", "paper"
	case "company", "companies":
		return "companies", "company"
	case "artifact":
		return "research", "artifact"
	default: // finding and anything else
		return "research", "finding"
	}
}

func buildNote(itemType, why, link, source, body string) string {
	cat, tag := categoryFor(itemType)
	fm := (&mdfm.Writer{}).SetList("categories", []string{cat})

	var b strings.Builder
	b.WriteString("#" + tag + "\n\n")
	if why = strings.TrimSpace(why); why != "" {
		b.WriteString(why + "\n\n")
	}
	if link = strings.TrimSpace(link); link != "" {
		b.WriteString(link + "\n\n")
	}
	if body = strings.TrimSpace(body); body != "" {
		b.WriteString(body + "\n\n")
	}
	if source = strings.TrimSpace(source); source != "" {
		b.WriteString("Source: " + source + "\n")
	}
	return fm.String(strings.TrimRight(b.String(), "\n"))
}

// sanitizeName strips filesystem-unsafe characters from a title while keeping it
// human-readable (spaces + case preserved, matching the vault's own filenames).
func sanitizeName(title string) string {
	title = strings.TrimSpace(title)
	var b strings.Builder
	for _, r := range title {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte(' ')
		default:
			if r >= 0x20 { // drop control chars
				b.WriteRune(r)
			}
		}
	}
	// collapse whitespace and drop pure-dot tokens (".."/"." — never path parts)
	var kept []string
	for _, f := range strings.Fields(b.String()) {
		if strings.Trim(f, ".") == "" {
			continue
		}
		kept = append(kept, f)
	}
	return strings.TrimLeft(strings.Join(kept, " "), ".")
}

func isUnder(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
