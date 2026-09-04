package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/record"
	"manifest/teamportal"
	"manifest/threads"
	"manifest/vaultwriter"
)

// panelFixture: a vault with the todo-plans capabilities + all three thread
// stores wired (aion team store included).
func panelFixture(t *testing.T) (*Server, string) {
	t.Helper()
	return panelFixtureAt(t, t.TempDir(), t.TempDir())
}

// panelFixtureAt is panelFixture over caller-owned directories — a second
// call on the same dirs is a fresh process over the same files (a restart).
func panelFixtureAt(t *testing.T, vault, dataDir string) (*Server, string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(vault, "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).
		WithZoneRoots("system", "extrinsic").
		WithAudit(dataDir).
		Grant(
			vaultwriter.Capability{Name: "todo-plans", Zone: record.ZoneSystem,
				Pattern: "system/todo-plans/**", Actor: vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "todo-plans-agent", Zone: record.ZoneSystem,
				Pattern: "system/todo-plans/**", Actor: vaultwriter.ActorApprovedProposal},
		)
	srv := &Server{}
	srv.UseVault(vw)
	srv.UseTaskPlans("system/todo-plans")
	private, err := threads.New(filepath.Join(dataDir, "todo-threads"))
	if err != nil {
		t.Fatal(err)
	}
	reDir := filepath.Join(dataDir, "re-team")
	reStore, err := threads.New(reDir)
	if err != nil {
		t.Fatal(err)
	}
	aionDir := filepath.Join(dataDir, "aion-portal")
	team, err := teamportal.New(aionDir)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := threads.New(aionDir)
	if err != nil {
		t.Fatal(err)
	}
	srv.UseThreads(private, reStore, team, blobs, "benjamin@aion.bio")
	return srv, vault
}

func TestPlanRecordRoundTrip(t *testing.T) {
	srv, vault := panelFixture(t)
	if err := srv.ensurePlanRecord("aion:abc123", "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	rec := srv.readPlanRecord("aion:abc123")
	if !rec.Exists || rec.Assignee != "agent:hermes" || rec.State != "open" {
		t.Fatalf("skeleton: %+v", rec)
	}
	// the plan section lands under the AGENT capability, frontmatter untouched
	if err := srv.writePlanSection("todo-plans-agent", "aion:abc123", "plan", "1. do the thing"); err != nil {
		t.Fatal(err)
	}
	rec = srv.readPlanRecord("aion:abc123")
	if rec.Plan != "1. do the thing" {
		t.Fatalf("sections: %+v", rec)
	}
	// the file is where the capability says it is (slugged, exact id in fm)
	raw, err := os.ReadFile(filepath.Join(vault, "system", "todo-plans", "aion-abc123.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "todo: aion:abc123") {
		t.Fatalf("frontmatter lost the exact id:\n%s", raw)
	}
}

func TestPlanCapabilityViolationLoud(t *testing.T) {
	srv, _ := panelFixture(t)
	// a write outside system/todo-plans/ under the capability must FAIL LOUD
	err := srv.vault.WriteCap("todo-plans", "system/aion/backlog.md", []byte("nope"))
	if err == nil || !strings.Contains(err.Error(), "write-capability violation") {
		t.Fatalf("expected capability violation, got %v", err)
	}
}

func TestThreadRouting(t *testing.T) {
	srv, _ := panelFixture(t)
	// aion → teamportal store (team-visible), identity = admin email
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), "aion:item9", threads.ActComment, "team hello", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	ext := srv.threads.aion.Ext()
	if len(ext.Comments["item9"]) != 1 || ext.Comments["item9"][0].Author != "benjamin@aion.bio" {
		t.Fatalf("aion comment routing: %+v", ext.Comments)
	}
	th := srv.listThread("aion:item9")
	if len(th) != 1 || th[0].Text != "team hello" {
		t.Fatalf("aion thread read-back: %+v", th)
	}
	// prop → RE shared store
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), "prop:bayard/fix-roof", threads.ActComment, "re hello", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := srv.threads.re.Thread("prop:bayard/fix-roof"); len(got) != 1 {
		t.Fatalf("re routing: %+v", got)
	}
	// personal real-estate DOMAIN id also goes to the RE store
	if srv.threadKind("real-estate/gutters-761") != "re" {
		t.Fatal("real-estate/ domain id should route to the RE store")
	}
	// personal → private
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), "inbox/thing", threads.ActComment, "just me", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := srv.threads.private.Thread("inbox/thing"); len(got) != 1 {
		t.Fatalf("private routing: %+v", got)
	}
	// RE store unset → prop falls back to private
	srv.threads.re = nil
	if srv.threadKind("prop:bayard/x") != "private" {
		t.Fatal("prop must fall back to private when RE teamDir unset")
	}
	// structural entries on aion todos stay in the private machine trail
	if _, err := srv.addThreadEntry(threads.Identity{ID: "agent:hermes", Name: "Hermes"}, "aion:item9", threads.ActAssign, "assigned", nil, nil, map[string]any{"assignee": "agent:hermes"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.threads.private.Thread("aion:item9"); len(got) != 1 || got[0].Action != threads.ActAssign {
		t.Fatalf("structural aion entry should stay private: %+v", got)
	}
}
