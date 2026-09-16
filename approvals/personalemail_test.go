package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonalEmailEffectClaimNeverReplays(t *testing.T) {
	s := NewStore(t.TempDir())
	p := Proposal{ID: strings.Repeat("a", 64), Ritual: PersonalEmailRitual, GmailThreadID: "thread", Type: TypeAppendVaultNote}
	if s.claimPersonalEmailEffect(p, false) == nil {
		t.Fatal("writerless apply admitted")
	}
	if err := s.claimPersonalEmailEffect(p, true); err != nil {
		t.Fatal(err)
	}
	// Reopen just as after a crash; the approval may still be pending. Neither
	// an automatic nor a manual retry can claim a second effect.
	reopened := NewStore(filepath.Dir(s.dir))
	if reopened.claimPersonalEmailEffect(p, true) == nil {
		t.Fatal("effect replayed")
	}
	b, err := os.ReadFile(filepath.Join(s.dir, "email-effects", p.ID+".json"))
	if err != nil || !strings.Contains(string(b), `"replay":false`) {
		t.Fatal(string(b), err)
	}
}

func TestCutoverFenceExcludesCanonicalPublication(t *testing.T) {
	artifacts := t.TempDir()
	s := NewStore(artifacts)
	p, err := s.Propose(Proposal{Action: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	release, err := AcquireDecisionFence(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Propose(Proposal{Action: "other"}); err == nil {
		t.Fatal("proposal bypassed cutover fence")
	}
	if err = s.Confirm(p.ID); err == nil {
		t.Fatal("confirm bypassed cutover fence")
	}
	if err = s.Reject(p.ID, "no"); err == nil {
		t.Fatal("reject bypassed cutover fence")
	}
	release()
	if err = s.Reject(p.ID, "no"); err != nil {
		t.Fatal(err)
	}
}
