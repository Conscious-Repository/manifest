package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const appendTestNote = `---
categories:
  - sync
gmail-thread-id: thread-abc123
---
[[Jane Doe]]

## 2026-08-10 — Jane Doe
first message body
`

// appendTestStore seeds a temp vault with one confirmed thread note under log/.
func appendTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	harness := t.TempDir()
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "log", "2026-08-10 roof bid.md"), []byte(appendTestNote), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewStore(filepath.Join(harness, "artifacts")).WithVaultRoot(vault), vault
}

func appendProposal(applyPath, threadID, sections string) Proposal {
	return Proposal{
		Type: TypeAppendVaultNote, Action: "Append 1 message(s) to " + applyPath,
		Agent: "ea-coordinator", Ritual: "email-sync",
		Body:      "New message(s) on the thread — appended below the existing note.\n\n````proposed\n" + sections + "\n````",
		ApplyPath: applyPath, Proposed: sections, GmailThreadID: threadID,
	}
}

func TestAppendVaultNotePathAllowed(t *testing.T) {
	for rel, want := range map[string]bool{
		"log/2026-08-10 roof bid.md":  true,
		"log/2026-08-10 a.md":         true,
		"2026-08-10 roof bid.md":      false, // no log/ prefix
		"log/roof bid.md":             false, // no date
		"log/../secret.md":            false,
		"log/2026-08-10 roof bid.txt": false,
		"log/sub/2026-08-10 a.md":     false,
		"/log/2026-08-10 a.md":        false,
		"":                            false,
	} {
		if got := AppendVaultNotePathAllowed(rel); got != want {
			t.Errorf("AppendVaultNotePathAllowed(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestAppendVaultNoteAppendsByteExactly(t *testing.T) {
	s, vault := appendTestStore(t)
	sections := "## 2026-08-12 — Benjamin Anderson\nreply body here"
	p, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", sections))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(vault, "log", "2026-08-10 roof bid.md"))
	want := appendTestNote + "\n" + sections + "\n"
	if string(after) != want {
		t.Fatalf("append not byte-exact:\n---got---\n%s\n---want---\n%s", after, want)
	}
}

func TestAppendVaultNoteRefusals(t *testing.T) {
	s, _ := appendTestStore(t)
	cases := []struct {
		name string
		p    Proposal
		msg  string
	}{
		{"missing note", appendProposal("log/2026-08-10 nope.md", "thread-abc123", "## x\nbody"), "not readable"},
		{"thread-id mismatch", appendProposal("log/2026-08-10 roof bid.md", "other-thread", "## x\nbody"), "does not match"},
		{"no thread id", appendProposal("log/2026-08-10 roof bid.md", "", "## x\nbody"), "no gmail-thread-id"},
		{"frontmatter payload", appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "---\nevil: true\n---"), "frontmatter-shaped"},
		{"empty payload", appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", ""), "no content"},
		{"outside log", appendProposal("2026-08-10 roof bid.md", "thread-abc123", "## x\nbody"), "not a log/ dated note"},
	}
	for _, tc := range cases {
		err := s.applyAppendVaultNote(tc.p)
		if err == nil || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: got %v, want error containing %q", tc.name, err, tc.msg)
		}
	}
}

func TestAppendVaultNoteRenamesRange(t *testing.T) {
	s, vault := appendTestStore(t)
	sections := "## 2026-08-14 — Jane Doe\nlatest word"
	p := appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", sections)
	p.RenameTo = "log/2026-08-10 - 2026-08-14 roof bid.md"
	saved, err := s.Propose(p)
	if err != nil {
		t.Fatal(err)
	}
	var notified []string
	applied, refused := s.AutoApplyAppends(func(paths []string) { notified = append(notified, paths...) })
	if applied != 1 || refused != 0 {
		t.Fatalf("applied=%d refused=%d", applied, refused)
	}
	// the note lives under the range name; the old file is gone
	after, err := os.ReadFile(filepath.Join(vault, "log", "2026-08-10 - 2026-08-14 roof bid.md"))
	if err != nil {
		t.Fatalf("renamed note missing: %v", err)
	}
	if !strings.Contains(string(after), "latest word") || !strings.Contains(string(after), "first message body") {
		t.Fatalf("content wrong after rename:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(vault, "log", "2026-08-10 roof bid.md")); err == nil {
		t.Fatal("old note still present after rename")
	}
	// the extraction nudge names the NEW path
	if len(notified) != 1 || notified[0] != "log/2026-08-10 - 2026-08-14 roof bid.md" {
		t.Fatalf("notify paths = %v", notified)
	}
	if _, err := s.LoadApproved(saved.ID); err != nil {
		t.Fatalf("approved record: %v", err)
	}
	// a rename target that already exists refuses (falls back to a card)
	if err := os.WriteFile(filepath.Join(vault, "log", "2026-08-10 collision.md"), []byte(appendTestNote), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "log", "2026-08-10 - 2026-08-15 collision.md"), []byte("taken"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := appendProposal("log/2026-08-10 collision.md", "thread-abc123", "## x\nbody")
	q.RenameTo = "log/2026-08-10 - 2026-08-15 collision.md"
	if err := s.applyAppendVaultNote(q); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing rename target: got %v", err)
	}
}

func TestRetitleKeepsDateRange(t *testing.T) {
	got, ok := retitleApplyPath("2026-08-10 - 2026-08-14 roof bid.md", "jane roofing saga")
	if !ok || got != "2026-08-10 - 2026-08-14 jane roofing saga.md" {
		t.Fatalf("range retitle = %q,%v", got, ok)
	}
	got, ok = retitleApplyPath("2026-08-10 roof bid.md", "jane roofing saga")
	if !ok || got != "2026-08-10 jane roofing saga.md" {
		t.Fatalf("single retitle = %q,%v", got, ok)
	}
}

func TestAutoApplyAppends(t *testing.T) {
	s, vault := appendTestStore(t)
	good, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "thread-abc123", "## 2026-08-12 — Jane Doe\nsecond message"))
	if err != nil {
		t.Fatal(err)
	}
	bad, err := s.Propose(appendProposal("log/2026-08-10 roof bid.md", "wrong-thread", "## 2026-08-12 — Jane Doe\nimpostor"))
	if err != nil {
		t.Fatal(err)
	}
	var notified []string
	applied, refused := s.AutoApplyAppends(func(paths []string) { notified = append(notified, paths...) })
	if applied != 1 || refused != 1 {
		t.Fatalf("applied=%d refused=%d, want 1/1", applied, refused)
	}
	if len(notified) != 1 || notified[0] != "log/2026-08-10 roof bid.md" {
		t.Fatalf("notify paths = %v", notified)
	}
	// the good append landed and is recorded approved with an auto stamp
	after, _ := os.ReadFile(filepath.Join(vault, "log", "2026-08-10 roof bid.md"))
	if !strings.Contains(string(after), "second message") || strings.Contains(string(after), "impostor") {
		t.Fatalf("wrong content landed:\n%s", after)
	}
	approved, err := s.LoadApproved(good.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(approved.Auto, "applied ") {
		t.Fatalf("approved record missing auto stamp: %q", approved.Auto)
	}
	// the bad one stays pending (the human-card fallback)
	if _, err := s.LoadPending(bad.ID); err != nil {
		t.Fatalf("refused append should stay pending: %v", err)
	}
	// idempotent second sweep: nothing new applies, the bad one still refuses
	applied, refused = s.AutoApplyAppends(nil)
	if applied != 0 || refused != 1 {
		t.Fatalf("second sweep applied=%d refused=%d, want 0/1", applied, refused)
	}
}
