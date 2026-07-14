package approvals

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/vaultwriter"
)

// craftPending builds the body (evidence + a ````proposed fence) and files a
// pending proposal, returning its id.
func craftPending(t *testing.T, s *Store, p Proposal, proposed string) string {
	t.Helper()
	p.Body = "evidence line\n\n````proposed\n" + proposed + "\n````"
	saved, err := s.Propose(p)
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	return saved.ID
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	harness := t.TempDir()      // the "excalibur root"
	agents := filepath.Join(harness, "artifacts")
	vault := t.TempDir()        // the knowledge vault
	s := NewStore(agents).WithVaultRoot(vault).WithVaultWriter(vaultwriter.New(vault))
	return s, vault
}

func TestConfirmAppendXQueue(t *testing.T) {
	s, vault := newTestStore(t)
	// a hand-edited x posts.md the confirm must byte-preserve
	orig := "# queue\n- existing one\n\n# posted\n- done already\n"
	xp := filepath.Join(vault, "x posts.md")
	os.WriteFile(xp, []byte(orig), 0o644)

	id := craftPending(t, s, Proposal{
		Type: TypeAppendXQueue, Action: "Queue X post: agent affordance",
		Agent: "critic", Ritual: "audit-drafts", ApplyPath: "x posts.md",
	}, "- an agent cannot choose a future its action set does not contain.")

	if err := s.Confirm(id); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	got, _ := os.ReadFile(xp)
	want := "# queue\n- existing one\n- an agent cannot choose a future its action set does not contain.\n\n# posted\n- done already\n"
	if string(got) != want {
		t.Fatalf("append mismatch:\n got %q\nwant %q", got, want)
	}
	// the proposal resolved (pending → approved) atomically with the write
	if len(s.List("pending")) != 0 {
		t.Fatal("proposal should have left pending")
	}
	if len(s.List("approved")) != 1 {
		t.Fatal("proposal should be approved")
	}
}

func TestConfirmUpdateVaultSkill_TuneOnly(t *testing.T) {
	s, vault := newTestStore(t)
	skill := filepath.Join(vault, "skills", "x-content", "SKILL.md")
	os.MkdirAll(filepath.Dir(skill), 0o755)
	os.WriteFile(skill, []byte("old rulebook\n"), 0o644)

	// filed by a non-tune ritual → apply refused, proposal stays pending, file untouched
	bad := craftPending(t, s, Proposal{
		Type: TypeUpdateVaultSkill, Action: "revise skill (bad)", Agent: "critic",
		Ritual: "audit-drafts", ApplyPath: "skills/x-content/SKILL.md",
	}, "new rulebook from a non-tune ritual")
	if err := s.Confirm(bad); err == nil {
		t.Fatal("update-vault-skill from a non-tune ritual must be refused (D15)")
	}
	if b, _ := os.ReadFile(skill); string(b) != "old rulebook\n" {
		t.Fatal("skill file must be untouched after a refused apply")
	}
	if len(s.List("pending")) != 1 {
		t.Fatal("refused proposal must stay pending")
	}

	// filed by a tune ritual → applied
	good := craftPending(t, s, Proposal{
		Type: TypeUpdateVaultSkill, Action: "revise skill (good)", Agent: "scribe",
		Ritual: "tune", ApplyPath: "skills/x-content/SKILL.md",
	}, "new rulebook from tune")
	if err := s.Confirm(good); err != nil {
		t.Fatalf("confirm tune skill: %v", err)
	}
	if b, _ := os.ReadFile(skill); string(b) != "new rulebook from tune\n" {
		t.Fatalf("skill not updated: %q", b)
	}
}

func TestCurrentContent_VaultSkillDiff(t *testing.T) {
	s, vault := newTestStore(t)
	skill := filepath.Join(vault, "skills", "x-content", "references", "voice-benjamin.md")
	os.MkdirAll(filepath.Dir(skill), 0o755)
	os.WriteFile(skill, []byte("current voice\n"), 0o644)

	cur, ok := s.CurrentContent(Proposal{
		Type: TypeUpdateVaultSkill, ApplyPath: "skills/x-content/references/voice-benjamin.md",
	})
	if !ok || cur != "current voice\n" {
		t.Fatalf("CurrentContent for vault-skill = %q, %v", cur, ok)
	}
	// an out-of-allow-list path yields no current content
	if _, ok := s.CurrentContent(Proposal{Type: TypeUpdateVaultSkill, ApplyPath: "skills/evil/x.md"}); ok {
		t.Fatal("out-of-list skill path should not resolve")
	}
}

func TestRejectAppendXQueue_WritesNothing(t *testing.T) {
	s, vault := newTestStore(t)
	xp := filepath.Join(vault, "x posts.md")
	os.WriteFile(xp, []byte("# queue\n- a\n"), 0o644)
	id := craftPending(t, s, Proposal{
		Type: TypeAppendXQueue, Action: "queue something", Agent: "critic",
		Ritual: "audit-drafts", ApplyPath: "x posts.md",
	}, "- b")
	if err := s.Reject(id, "off-voice"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if b, _ := os.ReadFile(xp); string(b) != "# queue\n- a\n" {
		t.Fatal("reject must not touch the x-posts file")
	}
	if len(s.List("rejected")) != 1 {
		t.Fatal("proposal should be rejected")
	}
}
