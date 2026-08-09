package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/record"
	"manifest/vaultwriter"
)

// aionTestStore builds a store over a temp harness + temp vault with the
// aion-approved capability granted — the §4 approved-proposal lane end to
// end (vaultwriter chokepoint + audit log included).
func aionTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	harness := t.TempDir()
	vault := t.TempDir()
	dataDir := t.TempDir()
	vw := vaultwriter.New(vault).
		WithZoneRoots("system", "extrinsic").
		WithAudit(dataDir).
		Grant(vaultwriter.Capability{
			Name: "aion-approved", Zone: record.ZoneSystem,
			Pattern: "system/aion/**", Actor: vaultwriter.ActorApprovedProposal,
		})
	if err := os.MkdirAll(filepath.Join(vault, "system", "aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, seed := range aion.SeedFiles {
		if err := os.WriteFile(filepath.Join(vault, "system", "aion", name), []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(filepath.Join(harness, "artifacts")).
		WithVaultRoot(vault).
		WithVaultWriter(vw).
		WithAionCapability("aion-approved")
	return s, vault, dataDir
}

func aionProposal(payload aion.ProposalPayload) Proposal {
	typ, path := TypeAionBacklog, AionBacklogPath
	if payload.Kind == aion.KindHeuristic {
		typ, path = TypeAionHeuristic, AionHeuristicPath
	}
	body := "candidate from a transcript\n\n> \"" + payload.Quote + "\"\n\n" +
		aion.RenderPayloadFence(payload)
	return Proposal{
		Type: typ, Action: "aion: " + payload.Kind + " — " + payload.Title,
		Agent: "aion-extractor", Ritual: "extract", Body: body, ApplyPath: path,
	}
}

func TestAionAcceptWritesExactlyOneLine(t *testing.T) {
	s, vault, dataDir := aionTestStore(t)
	payload := aion.ProposalPayload{
		Kind: "task", Title: "Secure the Deep Tech Week venue", Owner: "JR",
		Status: "open", Sources: []string{"2026-07-31 jack ruhl sync"},
		Captured: "2026-07-31", Confidence: 0.89, Quote: "finding a venue",
	}
	p, err := s.Propose(aionProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(vault, "system", "aion", "backlog.md")
	before, _ := os.ReadFile(target)
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(target)
	if strings.Count(string(after), "\n") != strings.Count(string(before), "\n")+1 {
		t.Fatalf("expected exactly one added line:\n%s", after)
	}
	if !strings.Contains(string(after), "- [ ] Secure the Deep Tech Week venue [kind:: task] [owner:: JR] [source:: [[2026-07-31 jack ruhl sync]]] [captured:: 2026-07-31] [status:: open]") {
		t.Fatalf("line wrong:\n%s", after)
	}
	// quote/confidence never persisted
	if strings.Contains(string(after), "finding a venue") || strings.Contains(string(after), "0.89") {
		t.Fatal("proposal review fields leaked into the record")
	}
	// audit log actor is approved-proposal — the §4 lane's first live use
	audit, _ := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if !strings.Contains(string(audit), "system/aion/backlog.md\taion-approved\tapproved-proposal\t") {
		t.Fatalf("audit line missing/wrong actor:\n%s", audit)
	}
	// the proposal moved to approved/
	if len(s.List("approved")) != 1 || len(s.List("pending")) != 0 {
		t.Fatal("proposal not archived to approved/")
	}
}

func TestAionRejectNeverResurfaces(t *testing.T) {
	s, vault, _ := aionTestStore(t)
	payload := aion.ProposalPayload{Kind: "decision", Title: "Choose the regulatory path",
		Owner: "BA/RT", Captured: "2026-07-02"}
	p, err := s.Propose(aionProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reject(p.ID, "not yet"); err != nil {
		t.Fatal(err)
	}
	// vault untouched
	b, _ := os.ReadFile(filepath.Join(vault, "system", "aion", "backlog.md"))
	if strings.Contains(string(b), "regulatory path") {
		t.Fatal("reject wrote to the vault")
	}
	// the same extraction re-proposed: Propose dedupes only pending — the
	// engine-side dedupe spans folders; here we pin the app-side equivalent
	// (Materialize's decidedElsewhere) for the same id
	if !s.decidedElsewhere(p.ID) {
		t.Fatal("rejected id not visible to the dedupe")
	}
}

func TestAionEditKeepsIDAndFlipsType(t *testing.T) {
	s, _, _ := aionTestStore(t)
	payload := aion.ProposalPayload{Kind: "task", Title: "Morale is the most valuable resource",
		Captured: "2026-07-02"}
	p, err := s.Propose(aionProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	// flip to a heuristic (mode new) — the card's kind flip
	payload.Kind = aion.KindHeuristic
	payload.Heuristic = aion.HeuristicIntent{Mode: aion.HeuristicModeNew}
	if err := s.SetAionPayload(p.ID, payload); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadPending(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID {
		t.Fatal("edit changed the id")
	}
	if got.Type != TypeAionHeuristic || got.ApplyPath != AionHeuristicPath {
		t.Fatalf("type/path not flipped: %s %s", got.Type, got.ApplyPath)
	}
	re, ok := AionPayload(got)
	if !ok || re.Kind != aion.KindHeuristic || re.Heuristic.Mode != aion.HeuristicModeNew {
		t.Fatalf("payload not persisted: %+v", re)
	}
}

func TestAionSecretsRefusedAtEveryGate(t *testing.T) {
	s, vault, _ := aionTestStore(t)
	// THE fixture shape from the corpus reality (a live AWS key in
	// `cellular wattage project.md`) — sanitized to the canonical example
	leaky := aion.ProposalPayload{
		Kind: "task", Title: "rotate AKIAIOSFODNN7EXAMPLE before backfill",
		Captured: "2026-08-07",
	}
	p, err := s.Propose(aionProposal(leaky))
	if err != nil {
		t.Fatal(err)
	}
	// gate 1: the edit path refuses
	if err := s.SetAionPayload(p.ID, leaky); err == nil || !strings.Contains(err.Error(), "aws-access-key") {
		t.Fatalf("edit gate: %v", err)
	}
	// gate 2: the apply path refuses, names the class, never the value
	err = s.Confirm(p.ID)
	if err == nil {
		t.Fatal("apply gate accepted a secret")
	}
	if !strings.Contains(err.Error(), "aws-access-key") {
		t.Fatalf("apply error should name the class: %v", err)
	}
	if strings.Contains(err.Error(), "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("apply error leaked the value: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(vault, "system", "aion", "backlog.md"))
	if strings.Contains(string(b), "AKIA") {
		t.Fatal("secret reached the vault")
	}
	// refusal leaves the proposal PENDING (still decidable)
	if len(s.List("pending")) != 1 {
		t.Fatal("refused proposal should stay pending")
	}
}

func TestAionHeuristicReinforceEndToEnd(t *testing.T) {
	s, vault, _ := aionTestStore(t)
	// land a heuristic first
	first := aion.ProposalPayload{
		Kind: aion.KindHeuristic, Title: "Take the longer path that gets you there faster",
		Sources: []string{"aion biosciences"}, Captured: "2026-07-02",
		Heuristic: aion.HeuristicIntent{Mode: aion.HeuristicModeNew},
	}
	p1, _ := s.Propose(aionProposal(first))
	if err := s.Confirm(p1.ID); err != nil {
		t.Fatal(err)
	}
	// reinforce it from a later transcript
	re := aion.ProposalPayload{
		Kind: aion.KindHeuristic, Title: "the long way is the short way",
		Sources: []string{"2026-07-27 derya ii"}, Captured: "2026-07-27",
		Heuristic: aion.HeuristicIntent{Mode: aion.HeuristicModeReinforce,
			Target: "Take the longer path that gets you there faster"},
	}
	p2, _ := s.Propose(aionProposal(re))
	if err := s.Confirm(p2.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(vault, "system", "aion", "heuristics.md"))
	if !strings.Contains(string(b), "- Take the longer path that gets you there faster [first:: 2026-07-02]") ||
		!strings.Contains(string(b), "    - [[2026-07-27 derya ii]] [date:: 2026-07-27]") {
		t.Fatalf("reinforce landed wrong:\n%s", b)
	}
	// a reinforce whose target vanished refuses loudly and stays pending
	gone := re
	gone.Title = "another expression"
	gone.Heuristic.Target = "A statement nobody wrote"
	p3, _ := s.Propose(aionProposal(gone))
	if err := s.Confirm(p3.ID); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing target: %v", err)
	}
	if len(s.List("pending")) != 1 {
		t.Fatal("refused reinforce should stay pending")
	}
}

// TestAionNoWriteWithoutCapability pins the §4 property: with no granted
// capability the apply refuses and the vault stays untouched.
func TestAionNoWriteWithoutCapability(t *testing.T) {
	harness := t.TempDir()
	vault := t.TempDir()
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic")
	s := NewStore(filepath.Join(harness, "artifacts")).WithVaultRoot(vault).WithVaultWriter(vw)
	// note: no WithAionCapability
	p, _ := s.Propose(aionProposal(aion.ProposalPayload{Kind: "task", Title: "x", Captured: "2026-08-07"}))
	if err := s.Confirm(p.ID); err == nil {
		t.Fatal("apply without capability accepted")
	}
	if _, err := os.Stat(filepath.Join(vault, "system", "aion", "backlog.md")); err == nil {
		t.Fatal("file appeared without a capability")
	}
}
