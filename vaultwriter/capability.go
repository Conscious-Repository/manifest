package vaultwriter

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"manifest/record"
)

// The write-capability layer (kernel-extraction Pass A §A3, owner decision
// 2026-07-28: NO exemptions — knowledge-zone files route through here too).
// A capability is declared at construction: {zone, path pattern, actor}. Every
// capability write is checked against its declaration and appends one line to
// the append-only audit log. A violation is a loud error, never a silent skip.

// Actor is WHO authorized a write: the owner acting directly in the UI, an
// approved proposal being applied (§4 — agents never write directly), or the
// bank-feed auto-apply lane (bank-accounts plan §7 — machine ledger appends
// whose standing authorization is the account link + the matching rule).
type Actor string

const (
	ActorUserAction       Actor = "user-action"
	ActorApprovedProposal Actor = "approved-proposal"
	ActorBankFeed         Actor = "bank-feed"
	// ActorPortalMember is a signed-in TEAM member's edit reaching the vault
	// through the materialization lane (ARCHITECTURE §12, 2026-08-24). It is
	// deliberately not ActorApprovedProposal: that word means the owner
	// approved this one write, and here nobody did — the standing consent is
	// the capability itself. The audit log should be able to say which of the
	// two moved the bytes.
	ActorPortalMember Actor = "portal-member"
)

// Capability is one declared write permission.
type Capability struct {
	Name    string // audit label, e.g. "goals", "todos", "daily"
	Zone    string // record.ZoneKnowledge | ZoneSystem | ZoneExtrinsic
	Pattern string // vault-relative pattern — see matches()
	Actor   Actor
}

// matches reports whether a vault-relative (slash, clean) path is inside the
// capability's pattern. Three forms:
//
//	"dir/**"        — the whole subtree under dir/
//	"goals*.md*"    — no slash: matched against the BASENAME, anywhere in zone
//	"a/b/c.md"      — otherwise: path.Match segment-wise on the full path
func (c Capability) matches(rel string) bool {
	p := c.Pattern
	if strings.HasSuffix(p, "/**") {
		root := strings.TrimSuffix(p, "/**")
		return rel == root || strings.HasPrefix(rel, root+"/")
	}
	if !strings.Contains(p, "/") {
		ok, err := path.Match(p, path.Base(rel))
		return err == nil && ok
	}
	ok, err := path.Match(p, rel)
	return err == nil && ok
}

// Grant declares capabilities on the writer (chainable, construction-time).
func (w *Writer) Grant(caps ...Capability) *Writer {
	if w.caps == nil {
		w.caps = map[string]Capability{}
	}
	for _, c := range caps {
		w.caps[c.Name] = c
	}
	return w
}

// WithAudit points the append-only audit log at <dataDir>/write-audit.log
// (chainable). Without it, capability writes still guard but do not log.
func (w *Writer) WithAudit(dataDir string) *Writer {
	if strings.TrimSpace(dataDir) != "" {
		w.auditPath = filepath.Join(dataDir, "write-audit.log")
	}
	return w
}

// checkCap resolves capName and verifies rel against its declaration:
// traversal-clean, not engine-owned, pattern match, zone match.
func (w *Writer) checkCap(capName, rel string) (Capability, string, error) {
	c, ok := w.caps[capName]
	if !ok {
		return c, "", fmt.Errorf("write-capability violation: no capability %q declared", capName)
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if err := w.Guard(clean, WriteRawUser); err != nil { // traversal + engine-owned
		return c, "", err
	}
	if !c.matches(clean) {
		return c, "", fmt.Errorf("write-capability violation: %q is outside capability %q (%s)", clean, c.Name, c.Pattern)
	}
	if zone := record.ZoneOf(clean, w.systemRootOrDefault(), w.extrinsicRootOrDefault()); zone != c.Zone {
		return c, "", fmt.Errorf("write-capability violation: %q is in the %s zone; capability %q is declared for %s", clean, zone, c.Name, c.Zone)
	}
	return c, clean, nil
}

// WriteCap is the capability-checked vault write: check, mkdir, write, audit.
func (w *Writer) WriteCap(capName, rel string, data []byte) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	c, clean, err := w.checkCap(capName, rel)
	if err != nil {
		return err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	var oldLen int64
	if fi, err := os.Stat(full); err == nil {
		oldLen = fi.Size()
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return err
	}
	w.audit(clean, c.Name, string(c.Actor), int64(len(data))-oldLen)
	return nil
}

// RenameCap is a capability-checked move: the DESTINATION must satisfy the
// capability (that's where content lands); the source only passes the base
// guard (traversal + engine-owned) — a user-invoked move may relocate a
// knowledge-zone note into a structured zone (goodreads re-filing books).
func (w *Writer) RenameCap(capName, relOld, relNew string) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	cleanOld := path.Clean(filepath.ToSlash(relOld))
	if err := w.Guard(cleanOld, WriteRawUser); err != nil {
		return err
	}
	c, cleanNew, err := w.checkCap(capName, relNew)
	if err != nil {
		return err
	}
	if err := os.Rename(
		filepath.Join(w.vault, filepath.FromSlash(cleanOld)),
		filepath.Join(w.vault, filepath.FromSlash(cleanNew))); err != nil {
		return err
	}
	w.audit(cleanOld+" -> "+cleanNew, c.Name, string(c.Actor)+" (rename)", 0)
	return nil
}

// BindAbs adapts a capability to the store lifecycle: stores hold ABSOLUTE
// paths (vault-index-resolved), so the bound func re-derives the vault-relative
// path, then runs the full capability check + audit. Stores take this injected
// from main — no store touches os.WriteFile itself (the §A3 acceptance grep).
func (w *Writer) BindAbs(capName string) func(absPath string, data []byte) error {
	return func(absPath string, data []byte) error {
		rel, err := filepath.Rel(w.vault, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("write-capability violation: %q is outside the vault", absPath)
		}
		return w.WriteCap(capName, filepath.ToSlash(rel), data)
	}
}

// audit appends one line per vault write: time, path, capability, actor, byte
// delta. Append-only by construction; no UI reads it yet — it accrues.
func (w *Writer) audit(rel, capName, actor string, delta int64) {
	if w.auditPath == "" {
		return
	}
	w.auditMu.Lock()
	defer w.auditMu.Unlock()
	f, err := os.OpenFile(w.auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return // auditing never blocks the owner's write
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%+d\n",
		time.Now().UTC().Format(time.RFC3339), rel, capName, actor, delta)
}

// commit is the shared tail for the writer's own guarded methods (contacts,
// records, ledgers, queue, xposts, extrinsic saves): mkdir, write, audit.
// full is absolute (under the vault); label names the entry point.
func (w *Writer) commit(full, label string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	var oldLen int64
	if fi, err := os.Stat(full); err == nil {
		oldLen = fi.Size()
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return err
	}
	rel := full
	if r, err := filepath.Rel(w.vault, full); err == nil {
		rel = filepath.ToSlash(r)
	}
	w.audit(rel, label, string(ActorUserAction), int64(len(data))-oldLen)
	return nil
}
