package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// x posts.md structure ops (content-studio iteration-2 §1/§3). The file has three
// sections:
//
//	# drafts   ← the owner's scratch ideas (seed material)
//	# queue    ← approved, ready to post (the pipeline appends here)
//	# posted   ← posted, newest at top
//
// A bullet is a top-level "- …" line plus any indented continuation lines (a
// thread block). All ops are byte-preserving outside the touched lines so the
// owner's hand-edits survive, and the queue-tab edits are conflict-safe
// (replace-by-exact-match; a changed file → clean refusal, never a blind write).

// Bullet is one editable item: the full verbatim block (lead line + any indented
// children), and its normalized lead for matching/dedupe.
type Bullet struct {
	Text string `json:"text"`
	Lead string `json:"lead"`
}

// XPostsDoc is the parsed three-section view for display (studio.ledger, Queue tab).
type XPostsDoc struct {
	Drafts        []Bullet `json:"drafts"`
	Queue         []Bullet `json:"queue"`
	Posted        []Bullet `json:"posted"`
	NeedsMigration bool    `json:"needsMigration"` // old shape: # queue present, # drafts absent
}

// ParseXPosts parses the file into its three sections. A pre-migration file
// (# queue + no # drafts) parses its current # queue as Queue and flags migration.
func ParseXPosts(content string) XPostsDoc {
	lines := strings.Split(content, "\n")
	di := headingIndex(lines, "drafts")
	qi := headingIndex(lines, "queue")
	pi := headingIndex(lines, "posted")
	var doc XPostsDoc
	doc.NeedsMigration = qi >= 0 && di < 0
	section := func(start int) []Bullet {
		if start < 0 {
			return nil
		}
		return parseSectionBullets(lines, start+1, sectionEnd(lines, start+1))
	}
	doc.Drafts = section(di)
	doc.Queue = section(qi)
	doc.Posted = section(pi)
	return doc
}

func parseSectionBullets(lines []string, start, end int) []Bullet {
	var out []Bullet
	i := start
	for i < end {
		if strings.HasPrefix(lines[i], "- ") {
			block := []string{lines[i]}
			i++
			for i < end && (strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t")) {
				block = append(block, lines[i])
				i++
			}
			text := strings.Join(block, "\n")
			out = append(out, Bullet{Text: text, Lead: normalizeBullet(block[0])})
		} else {
			i++
		}
	}
	return out
}

// --- migration (§1) ---

// MigrateXPosts renames the current # queue → # drafts and inserts a fresh empty
// # queue before # posted, byte-preserving everything else. Idempotent: a file
// already carrying # drafts is returned unchanged.
func (w *Writer) MigrateXPosts(relFile string) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	if err := w.Guard(filepath.ToSlash(relFile), WriteRawUser); err != nil {
		return err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(relFile))
	b, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	out, changed := migrateXPosts(string(b))
	if !changed {
		return errors.New("already migrated (# drafts present) — nothing to do")
	}
	return os.WriteFile(full, []byte(out), 0o644)
}

// PreviewMigration returns the post-migration content (for the old→new review),
// and whether a migration would change anything.
func PreviewMigration(content string) (string, bool) { return migrateXPosts(content) }

func migrateXPosts(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	if headingIndex(lines, "drafts") >= 0 {
		return content, false // already migrated
	}
	qi := headingIndex(lines, "queue")
	if qi < 0 {
		return content, false // nothing to migrate
	}
	// rename the queue heading to drafts, preserving its exact leading form
	lines[qi] = strings.Replace(lines[qi], "queue", "drafts", 1)
	// insert a fresh empty "# queue" section before "# posted" (or at end)
	insertAt := headingIndex(lines, "posted")
	if insertAt < 0 {
		out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
		return out + "\n\n# queue\n", true
	}
	// back up over blank lines so the new section sits cleanly before # posted
	for insertAt > 0 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	nl := make([]string, 0, len(lines)+3)
	nl = append(nl, lines[:insertAt]...)
	nl = append(nl, "", "# queue", "")
	nl = append(nl, lines[insertAt:]...)
	return strings.Join(nl, "\n"), true
}

// --- per-bullet edits (§3), conflict-safe ---

// ReplaceBullet replaces an exact original bullet block with a new one in relFile.
// Fail-closed on a stale match (the file changed underneath) so an Obsidian edit
// is never silently clobbered. section is drafts|queue (posted is not edited here).
func (w *Writer) ReplaceBullet(relFile, section, original, replacement string) error {
	return w.editBullets(relFile, section, func(content string) (string, error) {
		out, ok := replaceBullet(content, original, replacement)
		if !ok {
			return "", errors.New("that bullet no longer matches the file (it changed underneath) — reload and retry")
		}
		return out, nil
	})
}

// DeleteBullet removes an exact bullet block from relFile.
func (w *Writer) DeleteBullet(relFile, section, block string) error {
	return w.editBullets(relFile, section, func(content string) (string, error) {
		out, ok := deleteBullet(content, block)
		if !ok {
			return "", errors.New("that bullet no longer matches the file — reload and retry")
		}
		return out, nil
	})
}

// AddBullet appends a bullet to a section (drafts or queue).
func (w *Writer) AddBullet(relFile, section, block string) error {
	return w.editBullets(relFile, section, func(content string) (string, error) {
		out, ok := addBulletToSection(content, section, block)
		if !ok {
			return "", errors.New("that bullet is already present")
		}
		return out, nil
	})
}

func (w *Writer) editBullets(relFile, section string, fn func(string) (string, error)) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	if section != "drafts" && section != "queue" {
		return errors.New("only # drafts and # queue bullets are editable here")
	}
	if err := w.Guard(filepath.ToSlash(relFile), WriteRawUser); err != nil {
		return err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(relFile))
	b, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	out, err := fn(string(b))
	if err != nil {
		return err
	}
	return os.WriteFile(full, []byte(out), 0o644)
}

// replaceBullet swaps an exact original block (verbatim, joined by \n) for a new
// one; ok=false when the original isn't found exactly.
func replaceBullet(content, original, replacement string) (string, bool) {
	original = strings.TrimRight(original, "\n")
	replacement = strings.TrimRight(replacement, "\n")
	if !strings.Contains(content, original) {
		return content, false
	}
	return strings.Replace(content, original, replacement, 1), true
}

func deleteBullet(content, block string) (string, bool) {
	block = strings.TrimRight(block, "\n")
	idx := strings.Index(content, block)
	if idx < 0 {
		return content, false
	}
	// remove the block plus a single trailing newline if present
	end := idx + len(block)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:idx] + content[end:], true
}

// addBulletToSection appends a bullet under the given section's heading (creating
// it if absent), byte-preserving the rest; ok=false if an identical lead already
// exists in that section.
func addBulletToSection(content, section, block string) (string, bool) {
	if section == "queue" {
		return appendBulletToQueue(content, block)
	}
	block = strings.TrimRight(block, "\n")
	if strings.TrimSpace(block) == "" {
		return content, false
	}
	lead := normalizeBullet(firstNonEmptyLine(block))
	lines := strings.Split(content, "\n")
	hi := headingIndex(lines, section)
	if hi < 0 {
		out := strings.TrimRight(content, "\n")
		if out != "" {
			out += "\n\n"
		}
		return out + "# " + section + "\n\n" + block + "\n", true
	}
	end := sectionEnd(lines, hi+1)
	for i := hi + 1; i < end; i++ {
		if lead != "" && normalizeBullet(lines[i]) == lead {
			return content, false
		}
	}
	insertAt := end
	for insertAt > hi+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	blockLines := strings.Split(block, "\n")
	out := make([]string, 0, len(lines)+len(blockLines))
	out = append(out, lines[:insertAt]...)
	out = append(out, blockLines...)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n"), true
}
