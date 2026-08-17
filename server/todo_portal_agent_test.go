package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/spirits"
	"manifest/tasks"
)

// kairosFixture: panelFixture + personas + a THREE-harness federation where
// kairos is team-surface (portal roster) and hermes stays personal, plus an
// aion backlog holding one item (returned id) so aion: pins resolve.
func kairosFixture(t *testing.T) (*Server, string) {
	t.Helper()
	srv, vault := panelFixture(t)
	dir := t.TempDir()
	st := tasks.NewStore(dir, "to do.md", testWriteAbs)
	if err := os.WriteFile(st.Path(), []byte("# To Do\n\n## Inbox\n- [ ] research zoning [added:: 2026-08-14]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.tasksStore = st
	if err := os.MkdirAll(filepath.Join(vault, "system", "aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv.aion = aion.NewStore(vault, "system/aion", testWriteAbs)
	it := &aion.BacklogItem{Kind: aion.KindTask, Text: "canonize the zoning memo", Status: aion.StatusOpen}
	if err := srv.aion.AddItem(it); err != nil {
		t.Fatal(err)
	}
	srv.UseHarnesses([]Harness{
		{Name: "excalibur"},
		{Name: "hermes", Spirits: spirits.NewStore(t.TempDir())},
		{Name: "kairos", Surface: "team", Spirits: spirits.NewStore(t.TempDir())},
	})
	return seedPersonasInto(t, srv), it.ID
}

func TestSurfaceScopedRosters(t *testing.T) {
	srv, _ := kairosFixture(t)
	personal := srv.agentRoster()
	if len(personal) != 1 || personal[0]["id"] != "agent:hermes" {
		t.Fatalf("personal roster must exclude team-surface agents: %+v", personal)
	}
	team := srv.teamAgentRoster()
	if len(team) != 1 || team[0]["id"] != "agent:kairos" {
		t.Fatalf("team roster must be kairos only: %+v", team)
	}
	// resolution spans BOTH surfaces — relays/assigns must recognize kairos
	if h := srv.agentHarness("agent:kairos"); h != "kairos" {
		t.Fatalf("agentHarness must resolve team agents: %q", h)
	}
	if h := srv.agentHarness("agent:hermes"); h != "hermes" {
		t.Fatalf("agentHarness must resolve personal agents: %q", h)
	}
}

func TestPortalMentionAutoAssignsKairos(t *testing.T) {
	srv, item := kairosFixture(t)
	// the portal hook: a member mentions @Kairos::brief on a published item →
	// the aion: todo auto-assigns to the BARE token and a persona-tagged
	// comment-phase order spools into the kairos tree
	srv.AionThreadHook(item, []string{"agent:kairos::brief"}, "what's the zoning setback?")
	kairos := srv.eachHarness()[2].Spirits
	q := kairos.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[persona:: brief]") ||
		!strings.Contains(q[0].Request, "[todo:: aion:"+item+"]") {
		t.Fatalf("portal mention must spool into kairos: %+v", q)
	}
	if got := srv.readPlanRecord("aion:" + item).Assignee; got != "agent:kairos" {
		t.Fatalf("auto-assign must land the bare token: %q", got)
	}
}

func TestAionPanelAndFireBridges(t *testing.T) {
	srv, item := kairosFixture(t)
	if err := srv.AionAssign(item, "agent:kairos", "member@aion.bio", "Team Member"); err != nil {
		t.Fatal(err)
	}
	// the assign entry is attributed to the MEMBER, not the owner
	attributed := false
	for _, c := range srv.threads.private.Thread("aion:" + item) {
		if c.Action == "assign" && c.Author == "member@aion.bio" {
			attributed = true
		}
	}
	if !attributed {
		t.Fatalf("assign must be attributed to the member: %+v", srv.threads.private.Thread("aion:"+item))
	}
	// no plan yet → fire refuses
	if err := srv.AionFire(item, "member@aion.bio", "Team Member"); err == nil {
		t.Fatal("fire without a plan must refuse")
	}
	if err := srv.writePlanSection("todo-plans", "aion:"+item, "plan", "1. do the thing"); err != nil {
		t.Fatal(err)
	}
	// drain the assign's plan spool so fire's go spool isn't blocked
	kairos := srv.eachHarness()[2].Spirits
	for _, f := range mustGlob(t, filepath.Join(kairos.Root(), "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}
	if err := srv.AionFire(item, "member@aion.bio", "Team Member"); err != nil {
		t.Fatal(err)
	}
	q := kairos.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: go]") {
		t.Fatalf("fire must spool the go order: %+v", q)
	}
	panel := srv.AionPanel(item)
	if panel["assignee"] != "agent:kairos" || !strings.Contains(panel["plan"].(string), "do the thing") {
		t.Fatalf("panel payload: %+v", panel)
	}
	if _, ok := panel["delegation"]; !ok {
		t.Fatalf("panel must carry delegation state: %+v", panel)
	}
	// the fire is recorded in the team activity trail (→ the FEED bridge)
	found := false
	for _, e := range srv.threads.aion.Activity(time.Time{}) {
		if e.Action == "agent-fire" && e.Actor == "member@aion.bio" {
			found = true
		}
	}
	if !found {
		t.Fatal("fire must land in the team activity trail")
	}
}

func TestAionPanelV2AndActivityAndPlanWrite(t *testing.T) {
	srv, item := kairosFixture(t)
	// plan-section writes from the portal (both sections), then the extended panel
	if err := srv.AionPlanWrite(item, "plan", "1. draft the memo\n2. circulate"); err != nil {
		t.Fatal(err)
	}
	if err := srv.AionPlanWrite(item, "description", "why this matters"); err != nil {
		t.Fatal(err)
	}
	if err := srv.AionPlanWrite(item, "bogus", "x"); err == nil {
		t.Fatal("unknown section must refuse")
	}
	p := srv.AionPanel(item)
	if p["exists"] != true || p["description"] != "why this matters" ||
		!strings.Contains(p["plan"].(string), "draft the memo") {
		t.Fatalf("extended panel: %+v", p)
	}
	if p["planLines"] != 2 {
		t.Fatalf("planLines: %+v", p["planLines"])
	}
	if at, _ := p["planAt"].(string); at == "" {
		t.Fatalf("planAt missing: %+v", p)
	}
	if rel, _ := p["rel"].(string); !strings.Contains(rel, "todo-plans/aion-") {
		t.Fatalf("rel: %+v", p["rel"])
	}
	// activity: an assign lands in the trail for THIS item only
	if err := srv.AionAssign(item, "agent:kairos", "member@aion.bio", "Team Member"); err != nil {
		t.Fatal(err)
	}
	acts := srv.AionActivity(item)
	found := false
	for _, a := range acts {
		if a["action"] == "agent-assign" && a["actor"] == "member@aion.bio" {
			found = true
		}
	}
	if !found {
		t.Fatalf("assign missing from item activity: %+v", acts)
	}
	if other := srv.AionActivity("some-other-item"); len(other) != 0 {
		t.Fatalf("activity must filter by item: %+v", other)
	}
	// held flips with an agent assignee
	if p2 := srv.AionPanel(item); p2["held"] != true {
		t.Fatalf("agent assignee must read as held: %+v", p2)
	}
}
