package approvals

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"manifest/mdfm"
)

// TypeAppendVaultNote is the email-sync growth proposal type: it appends new
// message sections to an EXISTING confirmed thread note under the vault's log/
// folder. The confirmed create-vault-note is the owner's standing consent for
// its thread, so matching appends auto-apply (AutoApplyAppends) instead of
// rendering a card; any refusal falls back to an ordinary human card.
const TypeAppendVaultNote = "append-vault-note"

// appendNoteRe is the ONLY apply-path shape an append-vault-note may target:
// an existing dated note under log/ ("log/YYYY-MM-DD <title>.md"). Unlike
// create-vault-note the path names the note as it lives on disk (including the
// log/ folder and any owner retitle) — the engine resolves it from the vault
// index at filing time.
var appendNoteRe = regexp.MustCompile(`^log/\d{4}-\d{2}-\d{2} [^/\\]+\.md$`)

// AppendVaultNotePathAllowed is the hard allow-list for append-vault-note
// apply-paths: exactly one dated note under log/, clean, relative, no traversal.
func AppendVaultNotePathAllowed(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, "\\") {
		return false
	}
	if strings.Contains(rel, "..") || filepath.ToSlash(filepath.Clean(rel)) != rel {
		return false
	}
	return appendNoteRe.MatchString(rel)
}

// applyAppendVaultNote appends the proposed message sections to the end of an
// existing thread note. Fail-closed: the target must exist, its on-disk
// gmail-thread-id frontmatter must equal the proposal's (the note-identity
// guard against renames/replacements), and the payload must be body text only —
// a frontmatter-shaped payload is refused so an append can never touch the
// note's frontmatter. When the proposal carries rename-to (the thread's date
// range extended), the grown note lands under the NEW name and the old file is
// removed — the start date and the owner's title are preserved by the engine,
// which only ever extends the end date. With a writer wired, the grown bytes
// land through the vault-note capability and a rename is a capability move
// (both audited); the raw write/remove below is only the writer-less fallback.
func (s *Store) applyAppendVaultNote(p Proposal) error {
	if !AppendVaultNotePathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not a log/ dated note (log/YYYY-MM-DD <title>.md)", p.ApplyPath)
	}
	payload := strings.TrimSpace(p.Proposed)
	if payload == "" {
		return fmt.Errorf("apply refused: append-vault-note %q carries no content", p.ApplyPath)
	}
	if strings.HasPrefix(payload, "---") {
		return fmt.Errorf("apply refused: append-vault-note %q payload is frontmatter-shaped — appends are body text only", p.ApplyPath)
	}
	if strings.TrimSpace(p.GmailThreadID) == "" {
		return fmt.Errorf("apply refused: append-vault-note %q carries no gmail-thread-id", p.ApplyPath)
	}
	if s.vaultRoot == "" {
		return errors.New("apply refused: no vault root configured for append-vault-note")
	}
	lane, err := s.noteLane("append-vault-note")
	if err != nil {
		return err
	}
	logDir := filepath.Join(s.vaultRoot, "log")
	logAbs, _ := filepath.Abs(logDir)
	target := filepath.Join(s.vaultRoot, filepath.FromSlash(p.ApplyPath))
	tgtAbs, _ := filepath.Abs(target)
	if filepath.Dir(tgtAbs) != logAbs {
		return fmt.Errorf("apply refused: %q escapes log/", p.ApplyPath)
	}
	final, finalRel := target, p.ApplyPath
	if rt := strings.TrimSpace(p.RenameTo); rt != "" && rt != p.ApplyPath {
		if !AppendVaultNotePathAllowed(rt) {
			return fmt.Errorf("apply refused: rename target %q is not a log/ dated note", rt)
		}
		final, finalRel = filepath.Join(s.vaultRoot, filepath.FromSlash(rt)), rt
		finAbs, _ := filepath.Abs(final)
		if filepath.Dir(finAbs) != logAbs {
			return fmt.Errorf("apply refused: rename target %q escapes log/", rt)
		}
		if _, err := os.Stat(final); err == nil {
			return fmt.Errorf("apply refused: rename target %q already exists — not overwriting", rt)
		}
	}
	cur, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("apply refused: target note %q is not readable (was it renamed?): %w", p.ApplyPath, err)
	}
	fm, _ := mdfm.Split(string(cur))
	if strings.TrimSpace(fm["gmail-thread-id"]) != strings.TrimSpace(p.GmailThreadID) {
		return fmt.Errorf("apply refused: %q gmail-thread-id does not match the proposal — not the same thread note", p.ApplyPath)
	}
	body := string(cur)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	body += "\n" + payload + "\n"
	if lane {
		// Move first, then grow in place: the note is never doubled, and a
		// failed write leaves the ORIGINAL bytes under the new name (moved back
		// below), never a half-applied copy beside the old one.
		if final != target {
			if err := s.vw.RenameCap(s.noteCap, p.ApplyPath, finalRel); err != nil {
				return fmt.Errorf("apply refused: rename: %w", err)
			}
		}
		if err := s.vw.WriteCap(s.noteCap, finalRel, []byte(body)); err != nil {
			if final != target {
				_ = s.vw.RenameCap(s.noteCap, finalRel, p.ApplyPath) // roll the move back
			}
			return err
		}
		return nil
	}
	if err := os.WriteFile(final, []byte(body), 0o644); err != nil {
		return err
	}
	if final != target {
		if err := os.Remove(target); err != nil {
			_ = os.Remove(final) // roll back the copy so the note isn't doubled
			return fmt.Errorf("apply refused: rename could not remove the old note: %w", err)
		}
	}
	return nil
}

// AppendFinalPath is the vault-relative path the grown note lives at AFTER a
// successful apply (the rename target when the range extended) — the path the
// extraction nudge should name.
func AppendFinalPath(p Proposal) string {
	if rt := strings.TrimSpace(p.RenameTo); rt != "" {
		return rt
	}
	return p.ApplyPath
}

// AutoApplyAppends applies every pending append-vault-note whose target note
// matches its thread id, records each under approved/ with an `auto:` stamp
// (the audit trail — no card ever renders for a clean append), and calls
// notify with the applied vault-relative paths so the extraction sinks
// re-mine the grown notes. Refusals are left pending — they surface as
// ordinary human cards with a diff. Returns (applied, refused) counts.
func (s *Store) AutoApplyAppends(notify func(paths []string)) (int, int) {
	applied, refused := 0, 0
	var paths []string
	for _, p := range s.List("pending") {
		if p.Type != TypeAppendVaultNote {
			continue
		}
		if err := s.applyAppendVaultNote(p); err != nil {
			refused++
			continue
		}
		p.Status = "approved"
		p.Auto = "applied " + time.Now().UTC().Format(time.RFC3339)
		if err := os.WriteFile(filepath.Join(s.dir, "approved", p.ID+".md"), []byte(serialize(p)), 0o644); err != nil {
			refused++
			continue
		}
		_ = os.Remove(filepath.Join(s.dir, "pending", p.ID+".md"))
		applied++
		paths = append(paths, AppendFinalPath(p))
	}
	if len(paths) > 0 && notify != nil {
		notify(paths)
	}
	return applied, refused
}
