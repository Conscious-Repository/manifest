package approvals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reContractProposal(t *testing.T, payload ReContractPayload, applyPath string) Proposal {
	t.Helper()
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body := "intake: parsed from the uploaded document\n\n````" + ReContractFence + "\n" + string(raw) + "\n````"
	return Proposal{
		Type: TypeReContract, Action: "re: contract — " + payload.Name,
		Agent: "extractor", Ritual: "re-intake", Body: body, ApplyPath: applyPath,
	}
}

func seedProperty(t *testing.T, vault, slug, rocks string) {
	t.Helper()
	dir := filepath.Join(vault, "system", "realestate", "properties")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ncategories: [property]\naddress: " + slug + "\n---\n\n# " + slug + "\n\n## rocks\n" + rocks
	if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Confirm on a two-property contract intake writes: the new contractor, a
// new milestone under each property's named rock, the extracted decision,
// and the contract record — all under the approved-proposal capability.
func TestReContractAcceptWritesEverything(t *testing.T) {
	s, vault, dataDir := reTestStore(t)
	seedProperty(t, vault, "751-bayard-ave", "- [ ] Exterior & structural\n- [ ] Finishes\n")
	seedProperty(t, vault, "753-bayard-ave", "- [ ] Exterior & structural\n")
	payload := ReContractPayload{
		Kind: "contract", ContractorCreate: "Twisted Brick",
		Name:  "Twisted Brick — masonry, 751 + 753 Bayard",
		Total: 25500, Date: "2026-08-16", Expires: "2026-11-14", Doc: "sha256:ab3f",
		Allocations: []ReContractAllocation{
			{Property: "751-bayard-ave", Node: "exterior-structural/masonry", Amount: 12750, Reason: "parapet work is on 751"},
			{Property: "753-bayard-ave", Node: "exterior-structural/masonry", Amount: 12750},
		},
		NewMilestones: []ReContractMilestone{
			{Property: "751-bayard-ave", Rock: "exterior-structural", Name: "masonry"},
			{Property: "753-bayard-ave", Rock: "Exterior & structural", Name: "masonry"},
		},
		Tasks: []ReContractTask{
			{Property: "751-bayard-ave", Parent: "exterior-structural/masonry", Text: "confirm mortar color", Decision: true},
			{Property: "751-bayard-ave", Text: "owner to clear rear access"},
		},
		Terms:     []string{"50% deposit, balance at completion"},
		RiskItems: []string{"$10/lf decking"},
	}
	p, err := s.Propose(reContractProposal(t, payload, "system/realestate/contracts/twisted-brick-masonry.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	// contractor created
	ctr, err := os.ReadFile(filepath.Join(vault, "system/realestate/contractors/twisted-brick.md"))
	if err != nil || !strings.Contains(string(ctr), "name: Twisted Brick") {
		t.Fatalf("contractor record: %v\n%s", err, ctr)
	}
	// contract record written, accepted (kind=contract), both allocations
	con, err := os.ReadFile(filepath.Join(vault, "system/realestate/contracts/twisted-brick-masonry.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"status: accepted", "total: 25500", "expires: 2026-11-14", "doc: \"sha256:ab3f\"",
		`"751-bayard-ave | exterior-structural/masonry | 12750"`,
		`"753-bayard-ave | exterior-structural/masonry | 12750"`,
		"## terms", "50% deposit", "## risk items",
	} {
		if !strings.Contains(string(con), want) {
			t.Fatalf("contract record missing %q:\n%s", want, con)
		}
	}
	// trees gained the milestone + the decision + the loose task
	p751, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/751-bayard-ave.md"))
	for _, want := range []string{
		"    - [ ] masonry [milestone::]",
		"        - [ ] confirm mortar color", "[decision::]",
		"- [ ] owner to clear rear access",
	} {
		if !strings.Contains(string(p751), want) {
			t.Fatalf("751 tree missing %q:\n%s", want, p751)
		}
	}
	p753, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/753-bayard-ave.md"))
	if !strings.Contains(string(p753), "    - [ ] masonry [milestone::]") {
		t.Fatalf("753 tree missing the milestone:\n%s", p753)
	}
	// audited under the approved-proposal actor
	audit, _ := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if !strings.Contains(string(audit), "system/realestate/contracts/twisted-brick-masonry.md\trealestate-approved\tapproved-proposal\t") {
		t.Fatalf("audit line missing:\n%s", audit)
	}
}

// The allow-list holds: a re-contract proposal may only write a fresh record
// directly under the contracts folder, and Σ allocations must equal total.
func TestReContractRefusals(t *testing.T) {
	s, vault, _ := reTestStore(t)
	seedProperty(t, vault, "751-bayard-ave", "- [ ] Exterior & structural\n")
	good := ReContractPayload{
		Kind: "bid", ContractorCreate: "MCC",
		Name: "MCC roof", Total: 22500,
		Allocations: []ReContractAllocation{{Property: "751-bayard-ave", Node: "exterior-structural/roof", Amount: 22500}},
	}
	// wrong path
	p1, err := s.Propose(reContractProposal(t, good, "system/realestate/backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p1.ID); err == nil || !strings.Contains(err.Error(), "contracts-folder") {
		t.Fatalf("wrong path accepted: %v", err)
	}
	// Σ mismatch
	bad := good
	bad.Name = "MCC roof drifted"
	bad.Allocations = []ReContractAllocation{{Property: "751-bayard-ave", Node: "exterior-structural/roof", Amount: 20000}}
	p2, err := s.Propose(reContractProposal(t, bad, "system/realestate/contracts/mcc-roof-drift.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p2.ID); err == nil || !strings.Contains(err.Error(), "must equal total") {
		t.Fatalf("Σ mismatch accepted: %v", err)
	}
	// unknown property
	ghost := good
	ghost.Name = "MCC roof ghost"
	ghost.Allocations = []ReContractAllocation{{Property: "999-nowhere", Node: "exterior-structural/roof", Amount: 22500}}
	p3, err := s.Propose(reContractProposal(t, ghost, "system/realestate/contracts/mcc-roof-ghost.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p3.ID); err == nil || !strings.Contains(err.Error(), "no property record") {
		t.Fatalf("unknown property accepted: %v", err)
	}
	// a bid arrives proposed, never accepted (distinct payload — proposals
	// dedupe by action+body)
	fresh := good
	fresh.Name = "MCC roof fresh"
	p4, err := s.Propose(reContractProposal(t, fresh, "system/realestate/contracts/mcc-roof-751-test.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p4.ID); err != nil {
		t.Fatal(err)
	}
	con, _ := os.ReadFile(filepath.Join(vault, "system/realestate/contracts/mcc-roof-751-test.md"))
	if !strings.Contains(string(con), "status: proposed") {
		t.Fatalf("bid must arrive proposed:\n%s", con)
	}
}

// The owner's edit rewrites the fence (adjust-amounts) and re-validates.
func TestReContractPayloadEdit(t *testing.T) {
	s, vault, _ := reTestStore(t)
	seedProperty(t, vault, "751-bayard-ave", "- [ ] Exterior & structural\n")
	payload := ReContractPayload{
		Kind: "bid", ContractorCreate: "MCC", Name: "MCC roof", Total: 22500,
		Allocations: []ReContractAllocation{{Property: "751-bayard-ave", Node: "exterior-structural/roof", Amount: 22500}},
	}
	p, err := s.Propose(reContractProposal(t, payload, "system/realestate/contracts/mcc-roof-edit.md"))
	if err != nil {
		t.Fatal(err)
	}
	edited := payload
	edited.Total = 24000
	edited.Allocations = []ReContractAllocation{{Property: "751-bayard-ave", Node: "exterior-structural/roof", Amount: 24000}}
	if err := s.SetReContractPayload(p.ID, edited); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	con, _ := os.ReadFile(filepath.Join(vault, "system/realestate/contracts/mcc-roof-edit.md"))
	if !strings.Contains(string(con), "total: 24000") {
		t.Fatalf("edited total not applied:\n%s", con)
	}
	// an invalid edit is refused before it lands
	broken := edited
	broken.Total = 1
	if err := s.SetReContractPayload(p.ID, broken); err == nil {
		t.Fatal("Σ-mismatch edit accepted")
	}
}
