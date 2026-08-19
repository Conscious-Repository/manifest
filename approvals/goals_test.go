package approvals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/goals"
	"manifest/record"
	"manifest/vaultwriter"
)

// goalsTestStore builds the §12 goals-approved lane end to end: temp harness,
// temp vault with a canonical goals.md, the knowledge-zone capability, the
// audit log.
func goalsTestStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	harness := t.TempDir()
	vault := t.TempDir()
	dataDir := t.TempDir()
	vw := vaultwriter.New(vault).
		WithZoneRoots("system", "extrinsic").
		WithAudit(dataDir).
		Grant(vaultwriter.Capability{
			Name: "goals-approved", Zone: record.ZoneKnowledge,
			Pattern: "goals.md", Actor: vaultwriter.ActorApprovedProposal,
		})
	seed := goals.Serialize(goals.Parse("# Goals\n\n## Home\n\n### Rocks (90-day)\n" +
		"- [ ] Backyard [quarter:: 2026-Q3]\n" +
		"    - [ ] Metal up\n"))
	if err := os.WriteFile(filepath.Join(vault, "goals.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(filepath.Join(harness, "artifacts")).
		WithVaultRoot(vault).
		WithVaultWriter(vw).
		WithGoalsCapability("goals-approved")
	return s, vault, dataDir
}

func goalsProposal(t *testing.T, payload goals.PlacementPayload) Proposal {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Proposal{
		Type:   TypeGoalsItem,
		Action: "goals: " + payload.Mode + " " + payload.Level + " — " + payload.Title,
		Agent:  "hermes", Ritual: "goals",
		Body:      "> \"back pad complete\" — the owner, on Telegram\n\n```goals\n" + string(b) + "\n```\n",
		ApplyPath: GoalsPath,
	}
}

func TestGoalsAcceptPlacesExactlyOneLine(t *testing.T) {
	s, vault, dataDir := goalsTestStore(t)
	p, err := s.Propose(goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelMilestone, Area: "Home",
		ParentID: "home/backyard", Title: "Back pad complete",
	}))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(vault, "goals.md")
	before, _ := os.ReadFile(target)
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(target)
	if strings.Count(string(after), "\n") != strings.Count(string(before), "\n")+1 {
		t.Fatalf("expected exactly one added line:\n%s", after)
	}
	if !strings.Contains(string(after), "    - [ ] Back pad complete") {
		t.Fatalf("milestone missing:\n%s", after)
	}
	// the owner's words in the evidence line never reach goals.md
	if strings.Contains(string(after), "Telegram") {
		t.Fatal("proposal evidence leaked into goals.md")
	}
	audit, _ := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if !strings.Contains(string(audit), "goals.md\tgoals-approved\tapproved-proposal\t") {
		t.Fatalf("audit line missing/wrong actor:\n%s", audit)
	}
}

func TestGoalsWrongPathRefused(t *testing.T) {
	s, vault, _ := goalsTestStore(t)
	pr := goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelRock, Area: "Home", Title: "Sneaky",
	})
	pr.ApplyPath = "notes/goals.md" // any non-root path — the basename capability would match it
	p, err := s.Propose(pr)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "vault-root goals.md") {
		t.Fatalf("non-root path must refuse, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "notes", "goals.md")); err == nil {
		t.Fatal("a refused apply wrote a file")
	}
	// the proposal stays pending after a refusal
	if got := s.List("pending"); len(got) != 1 {
		t.Fatalf("refused proposal should stay pending, got %d", len(got))
	}
}

func TestGoalsMissingFileRefuses(t *testing.T) {
	s, vault, _ := goalsTestStore(t)
	if err := os.Remove(filepath.Join(vault, "goals.md")); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Propose(goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelRock, Area: "Home", Title: "X",
	}))
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing goals.md must refuse (a proposal never creates it), got %v", err)
	}
}

func TestGoalsNoCapabilityRefuses(t *testing.T) {
	s, _, _ := goalsTestStore(t)
	s.goalsCap = "" // the lane is dark
	p, _ := s.Propose(goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelRock, Area: "Home", Title: "X",
	}))
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("no capability must refuse, got %v", err)
	}
}

func TestGoalsSecretsRefused(t *testing.T) {
	s, vault, _ := goalsTestStore(t)
	p, _ := s.Propose(goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelRock, Area: "Home",
		Title: "rotate sk-ant-api03-abcdefghij0123456789abcdefghij0123456789abcdefghij0123456789",
	}))
	if err := s.Confirm(p.ID); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret must refuse, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(vault, "goals.md"))
	if strings.Contains(string(after), "sk-ant") {
		t.Fatal("secret reached goals.md")
	}
}

func TestGoalsEditLaneKeepsID(t *testing.T) {
	s, _, _ := goalsTestStore(t)
	p, _ := s.Propose(goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelMilestone, Area: "Home",
		ParentID: "home/backyard", Title: "Back pad complete",
	}))
	if err := s.SetGoalsPayload(p.ID, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelMilestone, Area: "Home",
		ParentID: "home/backyard", Title: "Back pad poured and cured",
	}); err != nil {
		t.Fatal(err)
	}
	got := s.List("pending")
	if len(got) != 1 || got[0].ID != p.ID {
		t.Fatalf("edit must keep the id, got %+v", got)
	}
	pay, ok := GoalsPayload(got[0])
	if !ok || pay.Title != "Back pad poured and cured" {
		t.Fatalf("edited payload not persisted: %+v", pay)
	}
	// an invalid edit refuses
	if err := s.SetGoalsPayload(p.ID, goals.PlacementPayload{Mode: "merge"}); err == nil {
		t.Fatal("invalid payload edit must refuse")
	}
}

func TestGoalsRejectNeverResurrects(t *testing.T) {
	s, _, _ := goalsTestStore(t)
	pr := goalsProposal(t, goals.PlacementPayload{
		Mode: goals.ModeAdd, Level: goals.LevelRock, Area: "Home", Title: "Mini split upstairs",
	})
	p, _ := s.Propose(pr)
	if err := s.Reject(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	_, disposition, err := s.ProposeBackfill(pr)
	if err != nil {
		t.Fatal(err)
	}
	if disposition != "decided" {
		t.Fatalf("a rejected placement must not resurrect, got %q", disposition)
	}
}
