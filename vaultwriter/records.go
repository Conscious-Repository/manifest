package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Structured-record writes (reading-plan §3, and the coming CRM/home databases):
// DATABASE-class writes guarded to the structured zones (system/, extrinsic/).
// These are user actions taken through the dashboard — creating a book record,
// marking one read — never AI authoring, never the knowledge zone.

// CreateRecord writes a new structured record at rel with the given full file
// content. Write-once: an existing file is returned untouched. Guarded to the
// database zones. rel is vault-relative (forward-slash).
func (w *Writer) CreateRecord(rel, content string) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	rel = filepath.ToSlash(rel)
	if err := w.Guard(rel, WriteDatabase); err != nil {
		return "", err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(rel))
	if !isUnder(full, w.vault) {
		return "", errors.New("invalid record path")
	}
	if _, err := os.Stat(full); err == nil {
		return rel, nil // write-once — never clobber
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// SetFrontmatterField upserts a SCALAR frontmatter field on an existing record
// (e.g. status/date-read/rating on "finish"), preserving the body byte-for-byte
// and leaving other frontmatter lines untouched. Database-class, guarded.
func (w *Writer) SetFrontmatterField(rel, key, value string) error {
	full, err := w.resolveRecord(rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	next, err := upsertFMScalar(string(raw), key, value)
	if err != nil {
		return err
	}
	return os.WriteFile(full, []byte(next), 0o644)
}

// resolveRecord is resolve() for database-class edits.
func (w *Writer) resolveRecord(rel string) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	if err := w.Guard(rel, WriteDatabase); err != nil {
		return "", err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(rel))
	if !isUnder(full, w.vault) {
		return "", errors.New("invalid record path")
	}
	return full, nil
}

// upsertFMScalar replaces `key:`'s value in the frontmatter block, or appends
// the line when the key is absent (creating the block if the note has none).
// Only the target line changes; the body is preserved verbatim.
func upsertFMScalar(raw, key, value string) (string, error) {
	line := key + ": " + value
	if !strings.HasPrefix(raw, "---\n") {
		// no frontmatter → prepend a block, keep the body
		return "---\n" + line + "\n---\n\n" + strings.TrimLeft(raw, "\n"), nil
	}
	end := strings.Index(raw[4:], "\n---")
	if end < 0 {
		return "", errors.New("unterminated frontmatter")
	}
	fmEnd := 4 + end // index of the '\n' before closing ---
	block, rest := raw[4:fmEnd], raw[fmEnd:]
	lines := strings.Split(block, "\n")
	replaced := false
	for i, l := range lines {
		k := strings.TrimSpace(strings.SplitN(l, ":", 2)[0])
		if strings.EqualFold(k, key) {
			lines[i] = line
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, line)
	}
	return "---\n" + strings.Join(lines, "\n") + rest, nil
}
