package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/record"
	"manifest/vaultwriter"
)

// noteLaneStore is the approvals store as main.go wires it for dated notes:
// a vault writer with the vault-note-approved capability + write-audit.log
// under dataDir. The vault carries one confirmed thread note under log/ so the
// append/rename paths have something to grow.
func noteLaneStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	harness, vault, dataDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "log", "2026-08-10 roof bid.md"), []byte(appendTestNote), 0o644); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic").WithAudit(dataDir).Grant(
		vaultwriter.Capability{Name: "vault-note-approved", Zone: record.ZoneKnowledge,
			Pattern: "log/????-??-?? *.md", Actor: vaultwriter.ActorApprovedProposal})
	s := NewStore(filepath.Join(harness, "artifacts")).WithVaultRoot(vault).
		WithVaultWriter(vw).WithVaultNoteCapability("vault-note-approved")
	return s, vault, dataDir
}

func auditLines(t *testing.T, dataDir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if err != nil {
		t.Fatalf("no write-audit.log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// A confirmed create-vault-note lands through the capability lane: the note
// is on disk under log/ AND write-audit.log carries exactly one line naming
// the path, the capability and the approved-proposal actor.
func TestCreateVaultNoteWritesThroughCapabilityLane(t *testing.T) {
	s, vault, dataDir := noteLaneStore(t)
	content := "---\ncategories:\n  - sync\ngranola-id: not_abc\n---\n[[jane doe]]\n\n## Transcript\n\n**Benjamin:** hi\n"
	fileCreateNote(t, s, "a1a1a1a1a1a1", "2026-07-02 Aion Sync.md", content)
	if err := s.Confirm("a1a1a1a1a1a1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "log", "2026-07-02 aion sync.md"))
	if err != nil {
		t.Fatalf("note not written: %v", err)
	}
	if string(got) != content {
		t.Fatalf("note content:\n%s", got)
	}
	lines := auditLines(t, dataDir)
	if len(lines) != 1 || !strings.Contains(lines[0], "log/2026-07-02 aion sync.md\tvault-note-approved\tapproved-proposal\t+") {
		t.Fatalf("audit line: %q", lines)
	}
	// never overwrite still holds inside the lane: a second create of the same
	// note refuses, the bytes stay, and no second audit line appears
	fileCreateNote(t, s, "b2b2b2b2b2b2", "2026-07-02 aion sync.md", "CLOBBER")
	if err := s.Confirm("b2b2b2b2b2b2"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second create: %v", err)
	}
	again, _ := os.ReadFile(filepath.Join(vault, "log", "2026-07-02 aion sync.md"))
	if string(again) != content {
		t.Fatalf("existing note clobbered:\n%s", again)
	}
	if n := len(auditLines(t, dataDir)); n != 1 {
		t.Fatalf("refused create must not audit a write; %d lines", n)
	}
}

// An append with rename-to moves the note under the capability (audited
// rename) and grows it in place (audited write): exactly one note remains,
// under the new name, with both the old and the new sections.
func TestAppendVaultNoteRenameThroughLaneLeavesOneNote(t *testing.T) {
	s, vault, dataDir := noteLaneStore(t)
	sections := "## 2026-08-14 — Jane Doe\nlatest word"
	p := appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", sections)
	p.RenameTo = "log/2026-08-10 - 2026-08-14 roof bid.md"
	if _, err := s.Propose(p); err != nil {
		t.Fatal(err)
	}
	applied, refused := s.AutoApplyAppends(nil)
	if applied != 1 || refused != 0 {
		t.Fatalf("applied=%d refused=%d", applied, refused)
	}
	after, err := os.ReadFile(filepath.Join(vault, "log", "2026-08-10 - 2026-08-14 roof bid.md"))
	if err != nil {
		t.Fatalf("renamed note missing: %v", err)
	}
	if string(after) != appendTestNote+"\n"+sections+"\n" {
		t.Fatalf("grown note not byte-exact:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(vault, "log", "2026-08-10 roof bid.md")); err == nil {
		t.Fatal("old note still present — the note is doubled")
	}
	entries, _ := os.ReadDir(filepath.Join(vault, "log"))
	if len(entries) != 1 {
		t.Fatalf("log/ holds %d notes, want 1", len(entries))
	}
	lines := auditLines(t, dataDir)
	if len(lines) != 2 ||
		!strings.Contains(lines[0], "log/2026-08-10 roof bid.md -> log/2026-08-10 - 2026-08-14 roof bid.md\tvault-note-approved\tapproved-proposal (rename)") ||
		!strings.Contains(lines[1], "log/2026-08-10 - 2026-08-14 roof bid.md\tvault-note-approved\tapproved-proposal\t+") {
		t.Fatalf("audit lines: %q", lines)
	}
}

// A plain append (no rename) through the lane grows the note and audits once.
func TestAppendVaultNoteThroughLaneAudits(t *testing.T) {
	s, vault, dataDir := noteLaneStore(t)
	p, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## 2026-08-12 — Jane Doe\nsecond"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(vault, "log", "2026-08-10 roof bid.md"))
	if !strings.HasSuffix(string(after), "\n## 2026-08-12 — Jane Doe\nsecond\n") {
		t.Fatalf("append did not land:\n%s", after)
	}
	if lines := auditLines(t, dataDir); len(lines) != 1 || !strings.Contains(lines[0], "log/2026-08-10 roof bid.md\tvault-note-approved\tapproved-proposal\t+") {
		t.Fatalf("audit lines: %q", lines)
	}
}

// A writer wired WITHOUT the capability is a dark lane: both applies refuse
// loudly and write nothing (never a silent fall-through to a raw write).
func TestVaultNoteAppliesRefuseWithoutCapability(t *testing.T) {
	s, vault, _ := noteLaneStore(t)
	s.noteCap = ""
	fileCreateNote(t, s, "c3c3c3c3c3c3", "2026-07-02 x.md", "content")
	if err := s.Confirm("c3c3c3c3c3c3"); err == nil || !strings.Contains(err.Error(), "no vault-note capability") {
		t.Fatalf("create without capability: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "log", "2026-07-02 x.md")); err == nil {
		t.Fatal("create wrote despite the dark lane")
	}
	err := s.applyAppendVaultNote(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## x\nbody"))
	if err == nil || !strings.Contains(err.Error(), "no vault-note capability") {
		t.Fatalf("append without capability: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(vault, "log", "2026-08-10 roof bid.md"))
	if string(after) != appendTestNote {
		t.Fatalf("append wrote despite the dark lane:\n%s", after)
	}
}

// With no writer at all (tests, disabled deployments) the pre-lane raw write
// still works — the existing createvaultnote/appendnote tests cover the bulk;
// this pins the fallback explicitly next to the lane tests.
func TestVaultNoteAppliesFallBackWithoutWriter(t *testing.T) {
	s, vault := appendTestStore(t) // WithVaultRoot only, vw == nil
	fileCreateNote(t, s, "d4d4d4d4d4d4", "2026-07-02 x.md", "content")
	if err := s.Confirm("d4d4d4d4d4d4"); err != nil {
		t.Fatalf("fallback create: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(vault, "log", "2026-07-02 x.md")); string(got) != "content\n" {
		t.Fatalf("fallback create content: %q", got)
	}
	if err := s.applyAppendVaultNote(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## x\nbody")); err != nil {
		t.Fatalf("fallback append: %v", err)
	}
}
