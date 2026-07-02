package approvals

import (
	"os"
	"path/filepath"
	"testing"
)

// harnessWithCornerstone builds a throwaway harness tree with one spirit
// cornerstone and an approvals store rooted at <harness>/artifacts.
func harnessWithCornerstone(t *testing.T, cornerstone string) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	csDir := filepath.Join(root, "spirits", "domain-scout")
	if err := os.MkdirAll(csDir, 0o755); err != nil {
		t.Fatal(err)
	}
	csPath := filepath.Join(csDir, "cornerstone.md")
	if err := os.WriteFile(csPath, []byte(cornerstone), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(filepath.Join(root, "artifacts"))
	return s, csPath
}

// fileActionable drops a pending actionable proposal exactly as the engine's
// write_approval cast would: apply-path frontmatter + a 4-backtick `proposed`
// fence carrying the full new file content.
func fileActionable(t *testing.T, s *Store, id, applyPath, proposed string) {
	t.Helper()
	body := "Evidence: 4 of 5 discards were funding-round posts.\n\n````proposed\n" + proposed + "\n````"
	content := "---\ntype: approval\nid: " + id + "\naction: add a skip rule\nagent: domain-scout\ncreated: 2026-07-02T08:00:00Z\napply-path: " + applyPath + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(s.dir, "pending", id+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const baseCornerstone = "---\nportal:: claude-sub\nwritable: [artifacts/feed]\navailable_spellbooks: [web, feed]\n---\n# Cornerstone\n\n- Search first, write second.\n"

func TestConfirmAppliesWithinAllowList(t *testing.T) {
	s, csPath := harnessWithCornerstone(t, baseCornerstone)
	// same frontmatter, one new behavior line — a legal tune
	proposed := "---\nportal:: claude-sub\nwritable: [artifacts/feed]\navailable_spellbooks: [web, feed]\n---\n# Cornerstone\n\n- Search first, write second.\n- Skip funding-round announcements; they are reliably discarded.\n"
	fileActionable(t, s, "aaaaaaaaaaaa", "spirits/domain-scout/cornerstone.md", proposed)

	if err := s.Confirm("aaaaaaaaaaaa"); err != nil {
		t.Fatalf("Confirm should apply, got %v", err)
	}
	got, _ := os.ReadFile(csPath)
	if string(got) != proposed {
		t.Fatalf("cornerstone not rewritten to proposed content.\n got: %q", got)
	}
	if n := len(s.List("approved")); n != 1 {
		t.Fatalf("expected 1 approved, got %d", n)
	}
	if n := len(s.List("pending")); n != 0 {
		t.Fatalf("pending should be empty, got %d", n)
	}
}

func TestConfirmRefusesOutsideAllowList(t *testing.T) {
	s, _ := harnessWithCornerstone(t, baseCornerstone)
	// try to write the vault's daily note — not on the allow-list
	fileActionable(t, s, "bbbbbbbbbbbb", "spirits/domain-scout/identity.md", "malicious")
	err := s.Confirm("bbbbbbbbbbbb")
	if err == nil {
		t.Fatal("Confirm must refuse an out-of-list apply-path")
	}
	if n := len(s.List("pending")); n != 1 {
		t.Fatalf("refused proposal must stay pending, got %d pending", n)
	}
	if n := len(s.List("approved")); n != 0 {
		t.Fatalf("nothing should be approved, got %d", n)
	}
}

func TestConfirmRefusesTraversal(t *testing.T) {
	s, _ := harnessWithCornerstone(t, baseCornerstone)
	fileActionable(t, s, "cccccccccccc", "spirits/../../etc/passwd", "x")
	if err := s.Confirm("cccccccccccc"); err == nil {
		t.Fatal("Confirm must refuse a traversal apply-path")
	}
}

func TestConfirmRefusesFrontmatterChange(t *testing.T) {
	s, csPath := harnessWithCornerstone(t, baseCornerstone)
	// widen writable in the frontmatter — must be refused
	proposed := "---\nportal:: claude-sub\nwritable: [artifacts/feed, spirits/warden]\navailable_spellbooks: [web, feed]\n---\n# Cornerstone\n\n- Search first, write second.\n"
	fileActionable(t, s, "dddddddddddd", "spirits/domain-scout/cornerstone.md", proposed)
	if err := s.Confirm("dddddddddddd"); err == nil {
		t.Fatal("Confirm must refuse a cornerstone frontmatter change")
	}
	got, _ := os.ReadFile(csPath)
	if string(got) != baseCornerstone {
		t.Fatalf("cornerstone must be untouched after refusal, got %q", got)
	}
	if n := len(s.List("pending")); n != 1 {
		t.Fatalf("refused proposal must stay pending, got %d", n)
	}
}

func TestRejectAppliesNothing(t *testing.T) {
	s, csPath := harnessWithCornerstone(t, baseCornerstone)
	proposed := "---\nportal:: claude-sub\nwritable: [artifacts/feed]\navailable_spellbooks: [web, feed]\n---\n# Cornerstone\n\n- New rule.\n"
	fileActionable(t, s, "eeeeeeeeeeee", "spirits/domain-scout/cornerstone.md", proposed)
	if err := s.Reject("eeeeeeeeeeee", "not convinced"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(csPath)
	if string(got) != baseCornerstone {
		t.Fatalf("Reject must not touch the target file, got %q", got)
	}
	if n := len(s.List("rejected")); n != 1 {
		t.Fatalf("expected 1 rejected, got %d", n)
	}
}

func TestApplyPathAllowed(t *testing.T) {
	ok := []string{
		"chargebook.md",
		"spirits/domain-scout/cornerstone.md",
		"spirits/warden/rituals/audit.md",
		"spirits/ea-coordinator/rituals/waiting-on.md",
	}
	bad := []string{
		"", "spirits/domain-scout/identity.md", "spirits/domain-scout/memories/long-term.md",
		"../chargebook.md", "spirits/../chargebook.md", "spirits/domain-scout/cornerstone.md/x",
		"/etc/passwd", "spirits/x/rituals/sub/deep.md", "spirits/x/cornerstone.md ",
		"grimoire/spellbooks/web/spellbook.md", "spirits//cornerstone.md",
		"spirits/x/rituals/notmd.txt", "spirits/domain-scout/rituals/",
	}
	for _, p := range ok {
		if !ApplyPathAllowed(p) {
			t.Errorf("ApplyPathAllowed(%q) = false, want true", p)
		}
	}
	for _, p := range bad {
		if ApplyPathAllowed(p) {
			t.Errorf("ApplyPathAllowed(%q) = true, want false", p)
		}
	}
}

func TestProposedRoundTrips(t *testing.T) {
	// a proposed cornerstone that itself contains a 3-backtick block must survive
	// the 4-backtick outer fence intact.
	s, _ := harnessWithCornerstone(t, baseCornerstone)
	proposed := "---\nportal:: claude-sub\nwritable: [artifacts/feed]\navailable_spellbooks: [web, feed]\n---\n# Cornerstone\n\n- Prefer code like ```go fmt.Println()``` in examples.\n"
	fileActionable(t, s, "ffffffffffff", "spirits/domain-scout/cornerstone.md", proposed)
	p, err := s.parse(filepath.Join(s.dir, "pending", "ffffffffffff.md"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Proposed != proposed {
		t.Fatalf("proposed did not round-trip.\n got: %q\nwant: %q", p.Proposed, proposed)
	}
}
