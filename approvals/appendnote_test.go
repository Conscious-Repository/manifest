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
