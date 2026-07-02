package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"manifest/mdfm"
)

// Contact-layer vault writes (plans/contacts-feature.md §3.5/§5/§6). Every one of
// these is an EXPLICIT user action taken through the dashboard UI — never AI
// authoring. All writes are confined under the vault root.

// CreatePersonNote creates <vault>/<name>.md with `categories: [people]` (plus
// `alias:` when display variants are given), body = exactly what the user typed.
// Write-once: an existing note is returned untouched (never clobbered). Returns
// the vault-relative path.
func (w *Writer) CreatePersonNote(name string, aliases []string, body string) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	// The vault's person-note convention is a lowercase filename (all 88 existing
	// people notes are lowercase); Obsidian resolves [[Links]] to it case-insensitively.
	base := strings.ToLower(sanitizeName(name))
	if base == "" {
		return "", errors.New("cannot create a person note with an empty name")
	}
	full := filepath.Join(w.vault, base+".md")
	if !isUnder(full, w.vault) {
		return "", errors.New("invalid note path")
	}
	rel := base + ".md"
	if _, err := os.Stat(full); err == nil {
		return rel, nil // write-once — keep the user's existing note
	}
	fm := (&mdfm.Writer{}).SetList("categories", []string{"people"})
	if clean := dedupeAliases(aliases); len(clean) > 0 {
		fm.SetList("alias", clean)
	}
	if err := os.WriteFile(full, []byte(fm.String(strings.TrimSpace(body))), 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

// ReplaceBody rewrites an existing note's body while preserving its frontmatter
// block verbatim — the note-pane "save" on a contact that already has a note.
func (w *Writer) ReplaceBody(rel, body string) error {
	full, err := w.resolve(rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	head := frontmatterBlock(string(raw))
	out := head
	if head != "" {
		out += "\n"
	}
	if b := strings.TrimSpace(body); b != "" {
		out += b + "\n"
	}
	return os.WriteFile(full, []byte(out), 0o644)
}

// AddFrontmatterValue adds value to a frontmatter list key (creating the block or
// the key when absent), idempotently. Used to record an alias on bind (§5) and an
// email on confirm (§6). Returns nil (no-op) when the value is already present.
func (w *Writer) AddFrontmatterValue(rel, key, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty value")
	}
	full, err := w.resolve(rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	next, changed := upsertFMValue(string(raw), key, value)
	if !changed {
		return nil
	}
	return os.WriteFile(full, []byte(next), 0o644)
}

// WriteNote overwrites a note with the exact raw content the user typed in the
// note view's raw-markdown editor (frontmatter + body verbatim). A user write.
func (w *Writer) WriteNote(rel, raw string) error {
	full, err := w.resolve(rel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(full); err != nil {
		return errors.New("note does not exist") // the note view only edits existing files
	}
	if !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	return os.WriteFile(full, []byte(raw), 0o644)
}

var taskMarkRe = regexp.MustCompile(`^(\s*[-*]\s+\[)[ xX](\].*)$`)

// ToggleTask flips the checkbox on the given 0-based line of a note to `want`
// (checked/unchecked) — the "check it off from the dashboard" write. It verifies
// the line is actually a checkbox (optimistic-concurrency guard against drift).
func (w *Writer) ToggleTask(rel string, line int, want bool) error {
	full, err := w.resolve(rel)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if line < 0 || line >= len(lines) {
		return errors.New("line out of range (note changed — reload)")
	}
	if !taskMarkRe.MatchString(strings.TrimRight(lines[line], "\r")) {
		return errors.New("that line is no longer a task (note changed — reload)")
	}
	mark := " "
	if want {
		mark = "x"
	}
	lines[line] = taskMarkRe.ReplaceAllString(strings.TrimRight(lines[line], "\r"), "${1}"+mark+"${2}")
	return os.WriteFile(full, []byte(strings.Join(lines, "\n")), 0o644)
}

func (w *Writer) resolve(rel string) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	full := filepath.Join(w.vault, filepath.FromSlash(rel))
	if !isUnder(full, w.vault) {
		return "", errors.New("invalid note path")
	}
	return full, nil
}

// frontmatterBlock returns the leading `---\n…\n---\n` fence (inclusive) or "".
func frontmatterBlock(raw string) string {
	if !strings.HasPrefix(raw, "---\n") {
		return ""
	}
	if i := strings.Index(raw, "\n---"); i >= 0 {
		rest := raw[i+4:]
		nl := strings.IndexByte(rest, '\n')
		if nl < 0 {
			return raw[:i+4] + "\n"
		}
		return raw[:i+4+nl+1]
	}
	return ""
}

// upsertFMValue inserts value into the frontmatter list under key, tolerating
// inline (`key: [a, b]`), block (`key:\n  - a`), scalar, and absent forms.
// Returns (newRaw, changed); changed is false when value is already present.
func upsertFMValue(raw, key, value string) (string, bool) {
	if !strings.HasPrefix(raw, "---\n") {
		return "---\n" + key + ": [" + value + "]\n---\n\n" + raw, true
	}
	lines := strings.Split(raw, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return raw, false
	}
	head := append([]string{}, lines[1:end]...)
	tail := lines[end:] // includes the closing --- and body

	ci := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	for i := 0; i < len(head); i++ {
		line := head[i]
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			continue
		}
		k, rest, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		rest = strings.TrimSpace(rest)
		switch {
		case strings.HasPrefix(rest, "["):
			items := splitInline(rest)
			for _, it := range items {
				if ci(it) == ci(value) {
					return raw, false
				}
			}
			items = append(items, value)
			head[i] = key + ": [" + strings.Join(items, ", ") + "]"
		case rest == "":
			j := i + 1
			for j < len(head) {
				t := strings.TrimSpace(head[j])
				if t == "" {
					j++
					continue
				}
				if !strings.HasPrefix(t, "- ") {
					break
				}
				if ci(strings.TrimPrefix(t, "- ")) == ci(value) {
					return raw, false
				}
				j++
			}
			ins := append([]string{"  - " + value}, head[j:]...)
			head = append(head[:j], ins...)
		default: // scalar → promote to list
			if ci(rest) == ci(value) {
				return raw, false
			}
			head[i] = key + ": [" + rest + ", " + value + "]"
		}
		return "---\n" + strings.Join(head, "\n") + "\n" + strings.Join(tail, "\n"), true
	}
	// key absent → append a new inline-list line
	head = append(head, key+": ["+value+"]")
	return "---\n" + strings.Join(head, "\n") + "\n" + strings.Join(tail, "\n"), true
}

func splitInline(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	var out []string
	for _, p := range strings.Split(v, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dedupeAliases(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x == "" || seen[strings.ToLower(x)] {
			continue
		}
		seen[strings.ToLower(x)] = true
		out = append(out, x)
	}
	return out
}
