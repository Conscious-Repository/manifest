package vaultwriter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The X-posts queue ops (content-studio §4). x posts.md is a lightweight vault
// file: a `# queue` section of bullets (nested for threads) and a `# posted`
// archive. Approving an append-x-queue proposal appends a bullet under `# queue`;
// the "mark posted" dashboard action moves a bullet to `# posted`. Both are
// byte-preserving: everything outside the touched line is untouched, so the
// owner's hand-edits survive.

// AppendQueueBullet appends block (one bullet, or a nested thread block) under the
// `# queue` heading in the vault file relFile, guarded and byte-preserving. It
// creates the heading if absent and refuses if an identical lead bullet already
// sits anywhere in the file (queue or posted).
func (w *Writer) AppendQueueBullet(relFile, block string) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	if err := w.Guard(filepath.ToSlash(relFile), WriteRawUser); err != nil {
		return err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(relFile))
	content := ""
	if b, err := os.ReadFile(full); err == nil {
		content = string(b)
	} else if !os.IsNotExist(err) {
		return err
	}
	out, changed := appendBulletToQueue(content, block)
	if !changed {
		return errors.New("that bullet is already in the queue or posted — not re-adding")
	}
	return os.WriteFile(full, []byte(out), 0o644)
}

// MoveBulletToPosted moves an exact queue bullet to `# posted` in relFile,
// guarded and byte-preserving. Errors if the bullet isn't in the queue.
func (w *Writer) MoveBulletToPosted(relFile, bullet string) error {
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
	out, moved := moveBulletToPosted(string(b), bullet)
	if !moved {
		return errors.New("bullet not found in the queue")
	}
	return os.WriteFile(full, []byte(out), 0o644)
}

// --- pure transforms (byte-preserving) ---

// normalizeBullet reduces a line to its bullet text for dedupe/match: strips
// leading indentation, a leading "- ", and surrounding space.
func normalizeBullet(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "- ")
	return strings.TrimSpace(t)
}

func firstNonEmptyLine(block string) string {
	for _, ln := range strings.Split(block, "\n") {
		if strings.TrimSpace(ln) != "" {
			return ln
		}
	}
	return ""
}

// sectionEnd returns the index of the next top-level "# " heading after start,
// or len(lines) if none.
func sectionEnd(lines []string, start int) int {
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "# ") {
			return i
		}
	}
	return len(lines)
}

// headingIndex returns the index of the exact "# <name>" heading (case-insensitive), or -1.
func headingIndex(lines []string, name string) int {
	for i, ln := range lines {
		if strings.EqualFold(strings.TrimSpace(ln), "# "+name) {
			return i
		}
	}
	return -1
}

func appendBulletToQueue(content, block string) (string, bool) {
	block = strings.TrimRight(block, "\n")
	if strings.TrimSpace(block) == "" {
		return content, false
	}
	leadNorm := normalizeBullet(firstNonEmptyLine(block))
	lines := strings.Split(content, "\n")
	// dedupe: identical lead bullet anywhere (queue or posted)
	if leadNorm != "" {
		for _, ln := range lines {
			if normalizeBullet(ln) == leadNorm {
				return content, false
			}
		}
	}
	qi := headingIndex(lines, "queue")
	if qi == -1 {
		out := strings.TrimRight(content, "\n")
		if out != "" {
			out += "\n\n"
		}
		out += "# queue\n\n" + block + "\n"
		return out, true
	}
	end := sectionEnd(lines, qi+1)
	// insert right after the last non-blank line of the queue section
	insertAt := end
	for insertAt > qi+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	blockLines := strings.Split(block, "\n")
	out := make([]string, 0, len(lines)+len(blockLines))
	out = append(out, lines[:insertAt]...)
	out = append(out, blockLines...)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n"), true
}

func moveBulletToPosted(content, bullet string) (string, bool) {
	target := normalizeBullet(bullet)
	if target == "" {
		return content, false
	}
	lines := strings.Split(content, "\n")
	qi := headingIndex(lines, "queue")
	if qi == -1 {
		return content, false
	}
	qEnd := sectionEnd(lines, qi+1)
	bi := -1
	for i := qi + 1; i < qEnd; i++ {
		if normalizeBullet(lines[i]) == target {
			bi = i
			break
		}
	}
	if bi == -1 {
		return content, false
	}
	moved := lines[bi]
	lines = append(lines[:bi], lines[bi+1:]...)
	pi := headingIndex(lines, "posted")
	if pi == -1 {
		out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
		out += "\n\n# posted\n\n" + moved + "\n"
		return out, true
	}
	insertAt := pi + 1 // right below the # posted heading
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, moved)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n"), true
}
