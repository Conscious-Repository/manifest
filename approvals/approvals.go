// Package approvals is the human-in-the-loop gate for side-effectful agent work.
// ea-coordinator DRAFTS proposals (never sends); the dashboard materializes them
// under <harness>/artifacts/approvals/{pending,approved,rejected}/ — the harness
// tree, outside the vault since the medium split. Confirm/Reject only RECORD the
// human decision (a folder move) — the app itself never sends, pays, or acts.
// The status is the folder the file lives in.
package approvals

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/mdfm"
	"manifest/vaultwriter"
)

// Proposal is one drafted action awaiting the user's decision. When ApplyPath is
// set the proposal is *actionable*: Proposed holds the full new file content and,
// on Confirm, the dashboard writes it to ApplyPath (within the hard allow-list)
// before recording the decision. Plain proposals leave both empty.
type Proposal struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "approval" (default) | "create-vault-note" | aion/re twins | "run-errand"
	Action    string `json:"action"`
	Agent     string `json:"agent"`
	Ritual    string `json:"ritual"`  // the filing ritual
	Section   string `json:"section"` // optional section hint carried in frontmatter (legacy; unused)
	Created   string `json:"created"` // RFC3339
	Status    string `json:"status"`  // pending|approved|rejected (= folder)
	Body      string `json:"body"`
	ApplyPath string `json:"applyPath"` // target (allow-list only), "" if none
	Proposed  string `json:"proposed"`  // full new file content, "" if none
	// run-errand fields (errands-aside §4): the one errand this proposal
	// authorizes. Empty for every other type.
	ErrandText    string `json:"errandText,omitempty"`
	ErrandAccount string `json:"errandAccount,omitempty"`
	ErrandGoal    string `json:"errandGoal,omitempty"`
	// append-vault-note fields (email-sync): the Gmail thread identity the
	// append is anchored to, the auto-apply stamp ("applied <RFC3339>")
	// recorded when AutoApplyAppends confirmed it without a human card, and
	// the optional rename target — the filename carries the thread's date
	// range, so a grown thread extends its end date on apply.
	GmailThreadID string `json:"gmailThreadId,omitempty"`
	Auto          string `json:"auto,omitempty"`
	RenameTo      string `json:"renameTo,omitempty"`
}

// TypeCreateVaultNote is the granola-sync proposal type (plan §4): it writes a
// brand-new dated note at the VAULT ROOT on Confirm, not a harness config file.
const TypeCreateVaultNote = "create-vault-note"

// TypeRunErrand (errands-aside §4) authorizes exactly ONE aside errand:
// approving enqueues it, never a standing grant. Unlike the actionable types
// it writes NO file — Confirm records the decision (the folder move) and the
// SERVER dispatches the enqueue after a successful confirm; ApplyPath stays
// empty so the file-apply machinery is never touched. Nothing emits these
// proposals yet (the EA loop will) — the type exists so the inbox can execute
// errands the day it does.
const TypeRunErrand = "run-errand"

var statuses = []string{"pending", "approved", "rejected"}

// vaultNoteRe is the ONLY apply-path shape a create-vault-note may write:
// a vault-root dated note "YYYY-MM-DD <title>.md" with no subfolder.
var vaultNoteRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} [^/\\]+\.md$`)

// Store is the approvals directory. root is the harness tree that
// "approval"-type apply-paths resolve against (the parent of <agentsDir>);
// vaultRoot is where "create-vault-note" proposals write. "" disables the
// respective applies.
type Store struct {
	dir       string
	root      string
	vaultRoot string
	vw        *vaultwriter.Writer // for guarded vault record writes (aion/re appends); nil disables them
	aionCap   string              // approved-proposal capability for aion applies; "" disables them
	reCap     string              // approved-proposal capability for real-estate applies; "" disables them
	goalsCap  string              // approved-proposal capability for goals placements; "" disables them
}

// NewStore roots the store at <agentsDir>/approvals and creates its subfolders.
// agentsDir is <harness>/artifacts, so the harness root — the base actionable
// apply-paths resolve against — is its parent.
func NewStore(agentsDir string) *Store {
	dir := filepath.Join(agentsDir, "approvals")
	for _, st := range statuses {
		_ = os.MkdirAll(filepath.Join(dir, st), 0o700)
	}
	return &Store{dir: dir, root: filepath.Dir(agentsDir)}
}

// WithVaultRoot sets the vault root that create-vault-note proposals write into
// (the knowledge vault, OUTSIDE the harness). Without it, those applies refuse.
func (s *Store) WithVaultRoot(vaultRoot string) *Store {
	s.vaultRoot = vaultRoot
	return s
}

// WithVaultWriter wires the guarded vault writer used to apply the aion/re
// record appends. Without it, those applies refuse.
func (s *Store) WithVaultWriter(vw *vaultwriter.Writer) *Store {
	s.vw = vw
	return s
}

// CreateVaultNotePathAllowed is the hard allow-list for create-vault-note
// apply-paths: a single vault-root dated note, no directory component, no
// traversal. Anything wider is refused (and is a warden finding).
func CreateVaultNotePathAllowed(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, "\\") {
		return false
	}
	if strings.Contains(rel, "/") || strings.Contains(rel, "..") {
		return false
	}
	return vaultNoteRe.MatchString(rel)
}

// ApplyPathAllowed is the hard allow-list for actionable-approval apply-paths.
// It must stay byte-identical to the engine's audit.ApplyPathAllowed — the two
// are one contract. Only three shapes may ever be written on Confirm:
//
//	chargebook.md
//	spirits/<spirit>/cornerstone.md
//	spirits/<spirit>/rituals/<ritual>.md
//
// The path must be clean, relative, forward-slashed, and each segment a plain
// name (no ".", "..", empty, or nested slashes). Anything else is refused.
func ApplyPathAllowed(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsAny(rel, "\\") {
		return false
	}
	if filepath.ToSlash(filepath.Clean(rel)) != rel {
		return false // not already clean/normalized (catches ., .., //, trailing /)
	}
	segs := strings.Split(rel, "/")
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	switch {
	case rel == "chargebook.md":
		return true
	case len(segs) == 3 && segs[0] == "spirits" && segs[2] == "cornerstone.md":
		return true
	case len(segs) == 4 && segs[0] == "spirits" && segs[2] == "rituals" && strings.HasSuffix(segs[3], ".md"):
		return true
	}
	return false
}

// List returns proposals in a status folder, oldest-first.
func (s *Store) List(status string) []Proposal {
	entries, _ := os.ReadDir(filepath.Join(s.dir, status))
	var out []Proposal
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if p, err := s.parse(filepath.Join(s.dir, status, e.Name())); err == nil {
			p.Status = status
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created < out[j].Created })
	return out
}

// CurrentContent returns the on-disk content of an actionable proposal's target
// (so the UI can diff current-vs-proposed), and whether it was readable within
// the allow-list. Plain proposals — or an unreadable/out-of-list path — yield
// ("", false).
func (s *Store) CurrentContent(p Proposal) (string, bool) {
	switch p.Type {
	case TypeAppendVaultNote:
		// the current thread note, so the fallback card can show where the
		// appended sections land
		if s.vaultRoot == "" || !AppendVaultNotePathAllowed(p.ApplyPath) {
			return "", false
		}
		b, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(p.ApplyPath)))
		if err != nil {
			return "", false
		}
		return string(b), true
	case TypeAionBacklog, TypeAionHeuristic, TypeReBacklog, TypeAionResolve, TypeReResolve, TypeGoalsItem:
		// the current corpus file, so the UI can diff current vs current+line
		if s.vaultRoot == "" ||
			((p.Type == TypeAionBacklog || p.Type == TypeAionResolve) && !AionBacklogPathAllowed(p.ApplyPath)) ||
			(p.Type == TypeAionHeuristic && !AionHeuristicPathAllowed(p.ApplyPath)) ||
			((p.Type == TypeReBacklog || p.Type == TypeReResolve) && !ReBacklogPathAllowed(p.ApplyPath)) ||
			(p.Type == TypeGoalsItem && !GoalsPathAllowed(p.ApplyPath)) {
			return "", false
		}
		b, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(p.ApplyPath)))
		if err != nil {
			return "", false
		}
		return string(b), true
	default:
		if p.ApplyPath == "" || s.root == "" || !ApplyPathAllowed(p.ApplyPath) {
			return "", false
		}
		b, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(p.ApplyPath)))
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

// Counts returns the number of proposals per status (for the sub-tab badge).
func (s *Store) Counts() map[string]int {
	c := map[string]int{}
	for _, st := range statuses {
		c[st] = len(s.List(st))
	}
	return c
}

// Propose writes a new pending proposal (dedupe by id — same action+body won't double up).
func (s *Store) Propose(p Proposal) (Proposal, error) {
	if strings.TrimSpace(p.Action) == "" {
		return Proposal{}, errors.New("proposal action is required")
	}
	if p.Created == "" {
		p.Created = time.Now().UTC().Format(time.RFC3339)
	}
	if p.ID == "" {
		p.ID = proposalID(p)
	}
	p.Status = "pending"
	dest := filepath.Join(s.dir, "pending", p.ID+".md")
	if _, err := os.Stat(dest); err == nil {
		return p, nil // already pending — dedupe
	}
	if err := os.WriteFile(dest, []byte(serialize(p)), 0o644); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

// Confirm records the user's approval: pending → approved. For a plain proposal
// it only moves the file (the app never sends/executes). For an ACTIONABLE
// proposal (apply-path + proposed content) it FIRST writes the proposed content
// to the target — within the hard allow-list, refusing any cornerstone payload
// that alters frontmatter — and only then moves it. If the apply is refused,
// nothing is written or moved and the error surfaces (the proposal stays
// actionable in pending/).
func (s *Store) Confirm(id string) error { return s.confirm(id, ConfirmEdits{}) }

// LoadPending reads one pending proposal (the server's confirm dispatch reads
// the run-errand payload BEFORE confirming; a missing file = already decided).
func (s *Store) LoadPending(id string) (Proposal, error) {
	p, err := s.parse(filepath.Join(s.dir, "pending", id+".md"))
	if err != nil {
		return Proposal{}, err
	}
	p.Status = "pending"
	return p, nil
}

// ConfirmEdits are the owner's pre-confirm edits to a create-vault-note:
// the attendee wikilink line, the filename title (date prefix kept,
// unsafe chars stripped), and the note's frontmatter categories (the
// automation key — an `aion` category makes the freshly-written note
// trigger the extraction pipeline). Edit* flags distinguish "no edit"
// from "cleared to none".
type ConfirmEdits struct {
	Attendees      []string
	EditAttendees  bool
	Title          string
	Categories     []string
	EditCategories bool
}

// ConfirmCreateNote confirms a create-vault-note after the user edited it
// in the dashboard.
func (s *Store) ConfirmCreateNote(id string, e ConfirmEdits) error {
	return s.confirm(id, e)
}

// LoadApproved reads one approved proposal (the post-confirm record — its
// ApplyPath reflects any title edit, so the server's extraction nudge names
// the file that was actually written).
func (s *Store) LoadApproved(id string) (Proposal, error) {
	p, err := s.parse(filepath.Join(s.dir, "approved", id+".md"))
	if err != nil {
		return Proposal{}, err
	}
	p.Status = "approved"
	return p, nil
}

// datePrefixRe splits a "YYYY-MM-DD <title>.md" apply-path into date + title.
// Email thread notes carry a date RANGE ("YYYY-MM-DD - YYYY-MM-DD <title>.md");
// the whole range is the fixed prefix, so a retitle never clobbers the end date.
var datePrefixRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}(?: - \d{4}-\d{2}-\d{2})?) (.+)\.md$`)

// filenameUnsafe are the characters banned from a note filename (mirrors the
// engine's granola/pocket noteFilename sanitizer).
var filenameUnsafe = regexp.MustCompile(`[\[\]<>:"/\\|?*]`)

// titleDateFragmentRe strips date fragments a client may have leaked into the
// editable title (a stale title editor once showed "- YYYY-MM-DD <title>" as
// the title portion of a range-named note — prefixing the range again doubled
// the end date). The date prefix is server-owned; a title never carries one.
var titleDateFragmentRe = regexp.MustCompile(`^(?:[-–—\s]*\d{4}-\d{2}-\d{2})+[-–—\s]*`)

// retitleApplyPath rebuilds a create-vault-note apply-path with a new title,
// keeping the original date prefix (a date RANGE for email thread notes).
// Returns (old, false) if the path isn't a dated note or the sanitized title
// is empty (so a bad edit is a no-op, never a widened write). The result
// still satisfies CreateVaultNotePathAllowed.
func retitleApplyPath(old, title string) (string, bool) {
	m := datePrefixRe.FindStringSubmatch(old)
	if m == nil {
		return old, false
	}
	title = strings.TrimSuffix(strings.TrimSpace(title), ".md")
	title = titleDateFragmentRe.ReplaceAllString(title, "")
	title = strings.Join(strings.Fields(filenameUnsafe.ReplaceAllString(title, "")), " ")
	if title == "" {
		return old, false
	}
	return m[1] + " " + title + ".md", true
}

func (s *Store) confirm(id string, e ConfirmEdits) error {
	src := filepath.Join(s.dir, "pending", id+".md")
	p, err := s.parse(src)
	if err != nil {
		return err
	}
	if e.EditAttendees && p.Type == TypeCreateVaultNote {
		p.Proposed = replaceAttendeeLine(p.Proposed, e.Attendees)
		p.Body = rebuildProposedBody(p.Body, p.Proposed)
	}
	if e.EditCategories && p.Type == TypeCreateVaultNote {
		p.Proposed = replaceCategories(p.Proposed, e.Categories)
		p.Body = rebuildProposedBody(p.Body, p.Proposed)
	}
	// Owner-edited title: retitle the filename (date prefix preserved).
	if title := strings.TrimSpace(e.Title); title != "" && p.Type == TypeCreateVaultNote {
		if np, ok := retitleApplyPath(p.ApplyPath, title); ok && np != p.ApplyPath {
			p.Action = strings.ReplaceAll(p.Action, p.ApplyPath, np) // keep the label in sync
			p.ApplyPath = np
		}
	}
	if p.ApplyPath != "" {
		if err := s.apply(p); err != nil {
			return err
		}
	}
	// Record the approval with the (possibly edited) content, then drop pending.
	p.Status = "approved"
	if err := os.WriteFile(filepath.Join(s.dir, "approved", id+".md"), []byte(serialize(p)), 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// replaceAttendeeLine rewrites a converted note's attendee wikilink line to
// exactly names (as [[name]] links), keeping the frontmatter and sections
// intact. It anchors on the FIRST "## " section heading — "## Transcript"
// for granola/pocket notes, the first "## <date> — <sender>" message section
// for email thread notes — so it works whether or not an attendee line was
// present. Unexpected shapes are returned unchanged.
func replaceAttendeeLine(content string, names []string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return content
	}
	fmClose := 4 + end + len("\n---")
	nl := strings.IndexByte(content[fmClose:], '\n')
	if nl < 0 {
		return content
	}
	head := content[:fmClose+nl+1] // through the frontmatter's closing "---\n"
	body := content[fmClose+nl+1:]
	anchor := -1
	if strings.HasPrefix(body, "## ") {
		anchor = 0
	} else if i := strings.Index(body, "\n## "); i >= 0 {
		anchor = i + 1
	}
	if anchor < 0 {
		return content // no section heading — leave it alone
	}
	rest := body[anchor:]

	var links []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" {
			links = append(links, "[["+n+"]]")
		}
	}
	if len(links) == 0 {
		return head + "\n" + rest
	}
	return head + strings.Join(links, " ") + "\n\n" + rest
}

// replaceCategories rewrites the proposed note's frontmatter `categories:`
// key to exactly cats — consuming EITHER existing shape (inline
// `categories: [a, b]` or block `categories:\n  - a`), emitting the
// kernel-canonical inline form (vaultwriter SetList precedent; the index
// parses both), inserting as the first key when absent, and removing the
// key when cats is empty. Every other frontmatter line passes through
// byte-intact; content without a frontmatter block is returned unchanged.
func replaceCategories(content string, cats []string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	lines := strings.Split(content, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" || lines[i] == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return content
	}
	var clean []string
	for _, c := range cats {
		if c = strings.TrimSpace(c); c != "" {
			clean = append(clean, c)
		}
	}
	catLine := ""
	if len(clean) > 0 {
		catLine = "categories: [" + strings.Join(clean, ", ") + "]"
	}
	out := []string{lines[0]}
	replaced := false
	for i := 1; i < end; i++ {
		ln := lines[i]
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ln)), "categories:") {
			// consume the block-list continuation lines ("  - x")
			for i+1 < end && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") &&
				strings.HasPrefix(lines[i+1], " ") {
				i++
			}
			if catLine != "" {
				out = append(out, catLine)
			}
			replaced = true
			continue
		}
		out = append(out, ln)
	}
	if !replaced && catLine != "" {
		out = append(out[:1], append([]string{catLine}, out[1:]...)...)
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// rebuildProposedBody swaps the content inside a proposal body's ````proposed
// fence for the edited note, preserving the human-facing message above it.
func rebuildProposedBody(body, proposed string) string {
	i := strings.Index(body, "````proposed")
	if i < 0 {
		return body
	}
	head := strings.TrimRight(body[:i], "\n")
	fence := "````proposed\n" + strings.TrimRight(proposed, "\n") + "\n````"
	if head == "" {
		return fence
	}
	return head + "\n\n" + fence
}

// apply writes an actionable proposal's content to its target file. A
// create-vault-note writes a NEW dated note under the vault's log/ folder; every
// other type writes a harness config file within the hard allow-list. Any violation
// returns an error and writes nothing.
func (s *Store) apply(p Proposal) error {
	switch p.Type {
	case TypeCreateVaultNote:
		return s.applyCreateVaultNote(p)
	case TypeAppendVaultNote:
		return s.applyAppendVaultNote(p)
	case TypeReContract:
		return s.applyReContract(p)
	case TypeReBacklog:
		return s.applyReBacklog(p)
	case TypeAionBacklog:
		return s.applyAionBacklog(p)
	case TypeAionHeuristic:
		return s.applyAionHeuristic(p)
	case TypeAionResolve:
		return s.applyAionResolve(p)
	case TypeReResolve:
		return s.applyReResolve(p)
	case TypeGoalsItem:
		return s.applyGoalsItem(p)
	}
	if !ApplyPathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is outside the allow-list (spirits/*/cornerstone.md, spirits/*/rituals/*.md, chargebook.md)", p.ApplyPath)
	}
	if strings.TrimSpace(p.Proposed) == "" {
		return fmt.Errorf("apply refused: proposal declares apply-path %q but carries no proposed content", p.ApplyPath)
	}
	if s.root == "" {
		return errors.New("apply refused: approvals store has no harness root configured")
	}
	target := filepath.Join(s.root, filepath.FromSlash(p.ApplyPath))
	rootAbs, _ := filepath.Abs(s.root)
	tgtAbs, _ := filepath.Abs(target)
	if tgtAbs != rootAbs && !strings.HasPrefix(tgtAbs, rootAbs+string(filepath.Separator)) {
		return fmt.Errorf("apply refused: %q escapes the harness root", p.ApplyPath)
	}
	// A cornerstone's frontmatter (portal::/writable:/available_spellbooks:) is
	// off-limits — tuning proposes behavior prose only. Require it byte-identical.
	if strings.HasSuffix(p.ApplyPath, "/cornerstone.md") {
		cur, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("apply refused: cannot read current %s: %w", p.ApplyPath, err)
		}
		if rawFrontmatter(string(cur)) != rawFrontmatter(p.Proposed) {
			return fmt.Errorf("apply refused: proposed content changes the cornerstone frontmatter (portal::/writable:/available_spellbooks:) — behavior prose only")
		}
	}
	body := p.Proposed
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return os.WriteFile(target, []byte(body), 0o644)
}

// applyCreateVaultNote writes a granola/pocket-sync proposal as a NEW dated note
// under the vault's log/ folder (the dated-note home since 2026-07-30). It still
// refuses anything but a bare dated filename (the unchanged apply-path contract),
// refuses when the note already exists (never overwrite), and refuses if no vault
// root is configured. This is the ONLY way an approval writes outside the harness.
func (s *Store) applyCreateVaultNote(p Proposal) error {
	if !CreateVaultNotePathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not a vault-root dated note (YYYY-MM-DD <title>.md)", p.ApplyPath)
	}
	if strings.TrimSpace(p.Proposed) == "" {
		return fmt.Errorf("apply refused: create-vault-note %q carries no content", p.ApplyPath)
	}
	if s.vaultRoot == "" {
		return errors.New("apply refused: no vault root configured for create-vault-note")
	}
	// Lowercase the filename to match the vault convention (the engine may
	// propose a title-cased "2026-07-08 Some Meeting.md"; save it lowercase).
	// The date digits are unaffected; Obsidian resolves [[links]] case-blind.
	rel := strings.ToLower(p.ApplyPath)
	// Dated transcript notes live under log/ (the vault's dated-note home since
	// the 2026-07-30 knowledge-OS split). The apply-path shape is still a bare
	// dated filename — the ONLY allowed shape, byte-identical to the engine's
	// audit contract — so the security boundary is unchanged; only the write
	// folder moved off root. The engine keeps emitting bare filenames; manifest
	// alone decides the destination on Confirm.
	logDir := filepath.Join(s.vaultRoot, "log")
	target := filepath.Join(logDir, filepath.FromSlash(rel))
	logAbs, _ := filepath.Abs(logDir)
	tgtAbs, _ := filepath.Abs(target)
	if filepath.Dir(tgtAbs) != logAbs {
		return fmt.Errorf("apply refused: %q escapes log/", rel)
	}
	if err := os.MkdirAll(logAbs, 0o755); err != nil {
		return fmt.Errorf("apply refused: cannot ensure log/ dir: %w", err)
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("apply refused: %q already exists — not overwriting", rel)
	}
	body := p.Proposed
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return os.WriteFile(target, []byte(body), 0o644)
}

// rawFrontmatter returns the verbatim text between the leading `---` fences (the
// same slice mdfm.Split parses), or "" when there is no frontmatter block. Used
// to compare a cornerstone's frontmatter exactly, without lossy re-parsing.
func rawFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	idx := strings.Index(content, "\n---")
	if idx < 0 {
		return ""
	}
	return content[4:idx]
}

// Reject records rejection (with an optional reason appended): pending → rejected.
// It applies NOTHING — an actionable proposal's target file is left untouched.
func (s *Store) Reject(id, reason string) error { return s.move(id, "rejected", reason) }

// Materialize parses ea-coordinator's proposed actions (the last fenced JSON array of
// {action, body}) into pending proposals. Returns the newly created ones.
func (s *Store) Materialize(raw, agent string, now time.Time) ([]Proposal, error) {
	arr, ok := mdfm.ExtractJSONArray(raw)
	if !ok {
		return nil, nil
	}
	var raws []struct {
		Action string `json:"action"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(arr), &raws); err != nil {
		return nil, err
	}
	var created []Proposal
	for _, r := range raws {
		if strings.TrimSpace(r.Action) == "" {
			continue
		}
		p := Proposal{Action: strings.TrimSpace(r.Action), Body: r.Body, Agent: agent,
			Created: now.UTC().Format(time.RFC3339)}
		p.ID = proposalID(p)
		if _, err := os.Stat(filepath.Join(s.dir, "pending", p.ID+".md")); err == nil {
			continue // already pending
		}
		// Skip if already decided (approved/rejected) so re-runs don't resurrect it.
		if s.decidedElsewhere(p.ID) {
			continue
		}
		saved, err := s.Propose(p)
		if err != nil {
			return created, err
		}
		created = append(created, saved)
	}
	return created, nil
}

// ---- internals ----

func (s *Store) decidedElsewhere(id string) bool {
	for _, st := range []string{"approved", "rejected"} {
		if _, err := os.Stat(filepath.Join(s.dir, st, id+".md")); err == nil {
			return true
		}
	}
	return false
}

func (s *Store) move(id, to, reason string) error {
	src := filepath.Join(s.dir, "pending", id+".md")
	p, err := s.parse(src)
	if err != nil {
		return err
	}
	if reason != "" {
		p.Body = strings.TrimRight(p.Body, "\n") + "\n\n> rejected: " + reason
	}
	p.Status = to
	dest := filepath.Join(s.dir, to, id+".md")
	if err := os.WriteFile(dest, []byte(serialize(p)), 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func (s *Store) parse(path string) (Proposal, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Proposal{}, err
	}
	fm, body := mdfm.Split(string(b))
	body = strings.TrimSpace(body)
	proposed, _ := mdfm.ExtractFencedBlock(body, "proposed")
	typ := strings.TrimSpace(fm["type"])
	if typ == "" {
		typ = "approval"
	}
	return Proposal{
		ID:        fm["id"],
		Type:      typ,
		Action:    fm["action"],
		Agent:     fm["agent"],
		Ritual:    strings.TrimSpace(fm["ritual"]),
		Section:   strings.TrimSpace(fm["section"]),
		Created:   fm["created"],
		Body:      body, // keeps the ````proposed fence, so the record round-trips
		ApplyPath: strings.TrimSpace(fm["apply-path"]),
		Proposed:  proposed,
		// run-errand payload (errands-aside §4)
		ErrandText:    strings.TrimSpace(fm["errand-text"]),
		ErrandAccount: strings.TrimSpace(fm["errand-account"]),
		ErrandGoal:    strings.TrimSpace(fm["errand-goal"]),
		// append-vault-note payload (email-sync)
		GmailThreadID: strings.TrimSpace(fm["gmail-thread-id"]),
		Auto:          strings.TrimSpace(fm["auto"]),
		RenameTo:      strings.TrimSpace(fm["rename-to"]),
	}, nil
}

func serialize(p Proposal) string {
	typ := p.Type
	if typ == "" {
		typ = "approval"
	}
	return (&mdfm.Writer{}).
		SetRaw("type", typ).
		Set("id", p.ID).
		Set("action", p.Action).
		Set("agent", p.Agent).
		Set("ritual", p.Ritual). // omitted when empty (Set skips blanks)
		Set("section", p.Section).
		Set("created", p.Created).
		Set("apply-path", p.ApplyPath).
		Set("errand-text", p.ErrandText).
		Set("errand-account", p.ErrandAccount).
		Set("errand-goal", p.ErrandGoal).
		Set("gmail-thread-id", p.GmailThreadID).
		Set("auto", p.Auto).
		Set("rename-to", p.RenameTo).
		String(strings.TrimSpace(p.Body))
}

func proposalID(p Proposal) string {
	h := sha1.Sum([]byte(strings.ToLower(p.Action + "|" + p.Body)))
	return hex.EncodeToString(h[:])[:12]
}
