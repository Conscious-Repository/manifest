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

// reTestStore mirrors aionTestStore for the real-estate domain: a temp vault
// with system/realestate/backlog.md seeded, the realestate-approved
// capability granted, and the store wired with WithReCapability.
func reTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	harness := t.TempDir()
	vault := t.TempDir()
	dataDir := t.TempDir()
	vw := vaultwriter.New(vault).
		WithZoneRoots("system", "extrinsic").
		WithAudit(dataDir).
		Grant(vaultwriter.Capability{
			Name: "realestate-approved", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorApprovedProposal,
		})
	if err := os.MkdirAll(filepath.Join(vault, "system", "realestate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "system", "realestate", "backlog.md"), []byte(aion.REBacklogSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(filepath.Join(harness, "artifacts")).
		WithVaultRoot(vault).
		WithVaultWriter(vw).
		WithReCapability("realestate-approved")
	return s, vault, dataDir
}

func reProposal(payload aion.ProposalPayload) Proposal {
	body := "candidate from a transcript\n\n> \"" + payload.Quote + "\"\n\n" +
		aion.RenderPayloadFenceIn(aion.REPayloadFence, payload)
	return Proposal{
		Type: TypeReBacklog, Action: "re: " + payload.Kind + " — " + payload.Title,
		Agent: "extractor", Ritual: "real-estate", Body: body, ApplyPath: ReBacklogPath,
	}
}

// Confirm appends exactly one grammar line to the RE decision log under the
// realestate-approved capability, review fields never persist.
func TestReAcceptWritesExactlyOneLine(t *testing.T) {
	s, vault, dataDir := reTestStore(t)
	payload := aion.ProposalPayload{
		Kind: "decision", Title: "Take the across-street lab BATNA at $17k/mo",
		Owner: "BA", Status: "open", Sources: []string{"2026-08-12 brian sync"},
		Captured: "2026-08-12", Confidence: 0.9, Quote: "let's lock the BATNA",
	}
	p, err := s.Propose(reProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(vault, "system", "realestate", "backlog.md")
	before, _ := os.ReadFile(target)
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(target)
	if strings.Count(string(after), "\n") != strings.Count(string(before), "\n")+1 {
		t.Fatalf("expected exactly one added line:\n%s", after)
	}
	if !strings.Contains(string(after), "Take the across-street lab BATNA at $17k/mo [id:: aion-bl/take-the-across-street-lab-batna-at-17k-mo] [kind:: decision] [owner:: BA] [source:: [[2026-08-12 brian sync]]] [captured:: 2026-08-12] [status:: open]") {
		t.Fatalf("line wrong:\n%s", after)
	}
	if strings.Contains(string(after), "let's lock the BATNA") || strings.Contains(string(after), "0.9") {
		t.Fatal("proposal review fields leaked into the record")
	}
	audit, _ := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if !strings.Contains(string(audit), "system/realestate/backlog.md\trealestate-approved\tapproved-proposal\t") {
		t.Fatalf("audit line missing/wrong actor:\n%s", audit)
	}
}

// Wrong apply-path is refused: an re-backlog proposal may never write the
// aion backlog (or anything else).
func TestReWrongPathRefused(t *testing.T) {
	s, vault, _ := reTestStore(t)
	payload := aion.ProposalPayload{Kind: "task", Title: "sneaky", Status: "open", Captured: "2026-08-12"}
	p := reProposal(payload)
	p.ApplyPath = "system/aion/backlog.md"
	created, err := s.Propose(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(created.ID); err == nil || !strings.Contains(err.Error(), "not the real-estate backlog") {
		t.Fatalf("wrong path accepted: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(vault, "system", "aion", "backlog.md")); len(b) > 0 {
		t.Fatal("aion backlog written by an re proposal")
	}
}

// The heuristic kind is refused for real estate at edit AND at apply.
func TestReHeuristicKindRefused(t *testing.T) {
	s, _, _ := reTestStore(t)
	// at apply: a proposal whose fence carries kind=heuristic
	payload := aion.ProposalPayload{Kind: aion.KindHeuristic, Title: "always buy corners", Captured: "2026-08-12"}
	p, err := s.Propose(reProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "no heuristics file") {
		t.Fatalf("heuristic kind accepted at apply: %v", err)
	}
	// at edit: SetAionPayload refuses the kind flip
	task := aion.ProposalPayload{Kind: "task", Title: "call the bank", Status: "open", Captured: "2026-08-12"}
	p2, err := s.Propose(reProposal(task))
	if err != nil {
		t.Fatal(err)
	}
	flip := task
	flip.Kind = aion.KindHeuristic
	flip.Heuristic.Mode = "new" // pass generic payload validation so OUR guard is what fires
	if err := s.SetAionPayload(p2.ID, flip); err == nil || !strings.Contains(err.Error(), "no heuristics file") {
		t.Fatalf("heuristic flip accepted at edit: %v", err)
	}
}

// No write without the realestate-approved capability.
func TestReNoWriteWithoutCapability(t *testing.T) {
	harness := t.TempDir()
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "system", "realestate"), 0o755); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic")
	s := NewStore(filepath.Join(harness, "artifacts")).
		WithVaultRoot(vault).
		WithVaultWriter(vw) // NO WithReCapability
	payload := aion.ProposalPayload{Kind: "task", Title: "no cap", Status: "open", Captured: "2026-08-12"}
	p, err := s.Propose(reProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("write allowed without capability: %v", err)
	}
}

// A DECISION carries its rock through the whole approve→vault lane (the
// payload dropped it on the decision branch, so an RE decision could never be
// tethered to a rock/property/deal from the card).
func TestReDecisionKeepsItsRock(t *testing.T) {
	s, vault, _ := reTestStore(t)
	payload := aion.ProposalPayload{
		Kind: "decision", Title: "Refinance 4032 Page before the rate reset",
		Owner: "BA", Status: "decided", Decided: "2026-08-14", Outcome: "locked at 6.5%",
		Rock: "4032-page", Sources: []string{"2026-08-14 lender call"}, Captured: "2026-08-14",
	}
	p, err := s.Propose(reProposal(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(vault, "system", "realestate", "backlog.md"))
	if !strings.Contains(string(after), "[rock:: 4032-page]") {
		t.Fatalf("decision landed without its rock:\n%s", after)
	}
}
