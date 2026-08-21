package server

// The §12 amendment's pin (ooda-portal email plan, phase 5):
// TestPortalApprovalConfirmIsTheOnlyVaultCrossing. A portal member's approval
// decision is now the ONE portal write that may reach the vault — through
// approvalsFor(id).Confirm, under oodaApprovalAccess, into the typed apply
// lanes' hard path allow-lists, audited by vaultwriter. This test proves all
// three arms: an in-entity confirm writes exactly the allow-listed records;
// an out-of-entity confirm is 403 with the vault byte-identical; reject never
// writes.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/record"
	"manifest/vaultwriter"
)

// oodaFeedFixture extends the portal fixture with a live approvals lane: a
// harness store whose applies cross vaultwriter under the RE capability —
// production's wiring, in miniature.
func oodaFeedFixture(t *testing.T) (*oodaPortalHandles, *approvals.Store) {
	t.Helper()
	f := oodaPortalFixtureFull(t)
	vault := f.vault
	write := func(rel, content string) {
		full := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// the ownership graph: 748 N Euclid sits on garden-spe, which OLGA owns
	// into THROUGH a holding entity (the real Garden SPE shape); 4924 Fountain
	// sits on anderson-org, which she does not.
	write("system/realestate/entities/garden-spe.md", `---
categories: [entity]
name: Garden SPE LLC
owners: ["[[sobkiv holdings]] 60%", "[[brian anderson]] 40%"]
---
`)
	write("system/realestate/entities/sobkiv-holdings.md", `---
categories: [entity]
name: sobkiv holdings
owners: ["[[olga sobkiv]] 100%"]
---
`)
	write("system/realestate/entities/anderson-org.md", `---
categories: [entity]
name: Anderson Organization LLC
owners: ["[[benjamin anderson]] 100%"]
---
`)
	write("system/realestate/properties/4924-fountain.md", `---
categories: [property]
address: 4924 Fountain Ave, St. Louis
entity: Anderson Organization LLC
status: pre_development
control: owned
---

## rocks
- [ ] Pre-development [work:: pre-development]
`)
	// 748's entity slug must resolve against the entity record's NAME — the
	// fixture property says "garden-spe", the record is "Garden SPE LLC"; use
	// the slug form the chain walker also accepts.
	write("system/realestate/backlog.md", aion.REBacklogSeed)
	write("system/realestate/contractors/olga-sobkiv.md", "---\ncategories: [contractor]\nname: olga sobkiv\n---\n")
	if _, err := f.srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	// the live snapshot composed before these records existed — recompose
	// (refresh(false) is 1s-throttled, so force it)
	if err := f.live.refresh(true); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	vw := vaultwriter.New(vault).
		WithZoneRoots("system", "extrinsic").
		WithAudit(dataDir).
		Grant(vaultwriter.Capability{
			Name: "realestate-approved", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorApprovedProposal,
		})
	ap := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts")).
		WithVaultRoot(vault).
		WithVaultWriter(vw).
		WithReCapability("realestate-approved")
	f.srv.UseHarnesses([]Harness{{Name: "test", Approvals: ap}})
	return f, ap
}

func reContractProposal(t *testing.T, ap *approvals.Store, slug, property string, total float64) approvals.Proposal {
	t.Helper()
	payload := `{"kind":"bid","contractor":"olga-sobkiv","name":"permit set — ` + property + `",` +
		`"total":` + jsonNum(total) + `,"date":"2026-08-21",` +
		`"allocations":[{"property":"` + property + `","node":"pre-development","amount":` + jsonNum(total) + `}]}`
	p, err := ap.Propose(approvals.Proposal{
		Type:   "re-contract",
		Action: "re: bid — permit set, " + property,
		Agent:  "extractor", Ritual: "ooda-email",
		ApplyPath: "system/realestate/contracts/" + slug + ".md",
		Body: "portal email extraction\n\n> \"permit set quoted\" — portal email sha256:x (confidence 0.9)\n\n" +
			"````re-contract\n" + payload + "\n````",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func jsonNum(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestPortalApprovalConfirmIsTheOnlyVaultCrossing(t *testing.T) {
	f, ap := oodaFeedFixture(t)
	olga, _ := f.auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())

	// --- arm 1: in-entity confirm crosses, and only into allow-listed paths ---
	in := reContractProposal(t, ap, "olga-permit-748", "748-n-euclid", 5500)
	before := vaultFingerprint(t, f.vault)
	beforeFiles := vaultFileHashes(t, f.vault)
	rec := oodaDo(t, f.h, olga, "POST", "/api/ooda/approval/"+in.ID, `{"action":"confirm"}`)
	if rec.Code != 200 {
		t.Fatalf("in-entity confirm: %d %s", rec.Code, rec.Body)
	}
	contract := filepath.Join(f.vault, "system/realestate/contracts/olga-permit-748.md")
	if _, err := os.Stat(contract); err != nil {
		t.Fatalf("confirm did not write the contract record: %v", err)
	}
	after := vaultFingerprint(t, f.vault)
	if before == after {
		t.Fatal("in-entity confirm changed nothing — the apply lane did not run")
	}
	// every changed FILE sits inside the RE apply lanes' allow-lists
	for _, path := range changedFiles(t, f.vault, beforeFiles) {
		rel := strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), strings.ReplaceAll(f.vault, "\\", "/")+"/")
		if !strings.HasPrefix(rel, "system/realestate/contracts/") &&
			!strings.HasPrefix(rel, "system/realestate/properties/") &&
			!strings.HasPrefix(rel, "system/realestate/contractors/") &&
			rel != "system/realestate/backlog.md" {
			t.Fatalf("confirm touched a file outside the apply lanes: %s", rel)
		}
	}

	// --- arm 2: out-of-entity confirm is 403 and the vault is untouched ---
	out := reContractProposal(t, ap, "olga-permit-4924", "4924-fountain", 4400)
	before = vaultFingerprint(t, f.vault)
	rec = oodaDo(t, f.h, olga, "POST", "/api/ooda/approval/"+out.ID, `{"action":"confirm"}`)
	if rec.Code != 403 {
		t.Fatalf("out-of-entity confirm: %d, want 403 (%s)", rec.Code, rec.Body)
	}
	if vaultFingerprint(t, f.vault) != before {
		t.Fatal("a REFUSED portal approval still reached the vault")
	}

	// --- arm 3: reject never writes (use a proposal olga may decide) ---
	rej := reContractProposal(t, ap, "olga-permit-748-b", "748-n-euclid", 900)
	before = vaultFingerprint(t, f.vault)
	rec = oodaDo(t, f.h, olga, "POST", "/api/ooda/approval/"+rej.ID, `{"action":"reject","reason":"stale quote"}`)
	if rec.Code != 200 {
		t.Fatalf("reject: %d %s", rec.Code, rec.Body)
	}
	if vaultFingerprint(t, f.vault) != before {
		t.Fatal("reject touched the vault")
	}
}

// vaultFileHashes snapshots every file's content for a per-FILE diff.
func vaultFileHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[path] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// changedFiles names every file added or modified since the snapshot.
func changedFiles(t *testing.T, root string, before map[string]string) []string {
	t.Helper()
	var out []string
	for path, content := range vaultFileHashes(t, root) {
		if prev, ok := before[path]; !ok || prev != content {
			out = append(out, path)
		}
	}
	return out
}

// The predicate's table test: who sees and who decides, across both types.
func TestOodaApprovalAccessTable(t *testing.T) {
	f, ap := oodaFeedFixture(t)
	snap := f.live.Snapshot()
	if snap == nil {
		t.Fatal("no snapshot")
	}
	contract := reContractProposal(t, ap, "olga-permit-748-c", "748-n-euclid", 1000)
	rows := f.srv.approvalRows(nil)
	var row approvalRow
	for _, rr := range rows {
		if rr.ID == contract.ID {
			row = rr
		}
	}
	if row.ID == "" {
		t.Fatal("proposal row not found")
	}
	cases := []struct {
		email     string
		admin     bool
		visible   bool
		decidable bool
	}{
		{"ben@ooda.group", true, true, true},         // admin
		{"me@olgasobkiv.com", false, true, true},     // owns into garden-spe via sobkiv holdings
		{"bpabbassa@att.net", false, true, true},     // Brian ANDERSON — direct 40% owner
		{"brian@ooda.group", false, false, false},    // Brian FROMAL — a seat, but no ownership
		{"stranger@ooda.group", false, false, false}, // domain member, no ownership
	}
	for _, c := range cases {
		v, d := f.srv.oodaApprovalAccess(c.email, c.admin, PortalOptions{Store: f.store, AdminEmail: "ben@ooda.group"}, row, snap)
		if v != c.visible || d != c.decidable {
			t.Fatalf("%s: visible=%v decidable=%v, want %v/%v", c.email, v, d, c.visible, c.decidable)
		}
	}
	// re-backlog: owner initials decide it
	bl, err := ap.Propose(approvals.Proposal{
		Type: "re-backlog", Action: "re: task — Olga to deliver 748 permit set",
		Agent: "extractor", Ritual: "ooda-email",
		ApplyPath: "system/realestate/backlog.md",
		Body: "from the thread\n\n> \"I'll deliver Friday\" — portal email sha256:x (confidence 0.9)\n\n" +
			aion.RenderPayloadFenceIn(aion.REPayloadFence, aion.ProposalPayload{Kind: "task", Title: "deliver 748 permit set", Owner: "OS"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	rows = f.srv.approvalRows(nil)
	for _, rr := range rows {
		if rr.ID != bl.ID {
			continue
		}
		if v, d := f.srv.oodaApprovalAccess("me@olgasobkiv.com", false, PortalOptions{Store: f.store}, rr, snap); !v || !d {
			t.Fatalf("task owner must see+decide her own task (v=%v d=%v)", v, d)
		}
		if v, _ := f.srv.oodaApprovalAccess("brian@ooda.group", false, PortalOptions{Store: f.store}, rr, snap); v {
			t.Fatal("a task owned by OS must not be visible to BPA")
		}
	}
}
