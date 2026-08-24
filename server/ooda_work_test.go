package server

import (
	"strings"
	"testing"

	"manifest/aion"
	"manifest/realestate"
	"manifest/tasks"
	"manifest/teamportal"
)

// oodaWorkIDs flattens every lane of every group into the set of item ids.
func oodaWorkIDs(groups []oodaWorkGroup) map[string]bool {
	ids := map[string]bool{}
	for _, g := range groups {
		for _, lane := range [][]oodaWorkItem{g.Open, g.Overdue, g.DueThisWeek, g.Decisions, g.Waiting} {
			for _, it := range lane {
				ids[it.ID] = true
			}
		}
	}
	return ids
}

// A member marking a vault backlog item done through the portal writes an
// OVERRIDE (items.ext.json), never the vault — and the first WORK projection
// read only the vault's own status, so the item stayed open forever (brian's
// report, 2026-08-22: item 33e6054b marked done TWICE, never cleared). The
// override is the authoritative runtime write for team-set state, in both
// directions: done/decided closes an open base item, open reopens a done one.
func TestOodaWorkHonorsTeamOverrides(t *testing.T) {
	snap := oodaFixture()
	snap.Backlog = []*aion.BacklogItem{
		{ID: "aion-bl/portal-done", Text: "hang banners in fountain park", Owner: "BA", Kind: "task", Status: aion.StatusOpen},
		{ID: "aion-bl/reopened", Text: "back again", Owner: "BA", Kind: "task", Status: aion.StatusDone},
		{ID: "aion-bl/in-progress", Text: "half way", Owner: "BA", Kind: "task", Status: aion.StatusDone},
		{ID: "aion-bl/weird", Text: "unknown mark", Owner: "BA", Kind: "task", Status: aion.StatusOpen},
		{ID: "aion-bl/due-only", Text: "due patched, status untouched", Owner: "BA", Kind: "task", Status: aion.StatusOpen},
		{ID: "aion-bl/untouched", Text: "no override at all", Owner: "BA", Kind: "task"},
	}
	overrides := map[string]teamportal.Override{
		"aion-bl/portal-done": {Fields: map[string]string{"status": "done", "done_on": "2026-08-22"}},
		"aion-bl/reopened":    {Fields: map[string]string{"status": "open"}},
		"aion-bl/in-progress": {Fields: map[string]string{"status": "in_progress"}},
		// a status nobody has invented yet is still somebody's mark — CLOSED,
		// the same allow-list reading oodaBacklogOpen applies to the vault
		"aion-bl/weird": {Fields: map[string]string{"status": "someday"}},
		// an override that never touched status defers to the base item
		"aion-bl/due-only": {Fields: map[string]string{"due": "2026-09-01"}},
	}
	ids := oodaWorkIDs(buildOodaWork(snap, "2026-08-22", overrides, nil, nil))
	for id, want := range map[string]bool{
		"aion-bl/portal-done": false,
		"aion-bl/reopened":    true,
		"aion-bl/in-progress": true,
		"aion-bl/weird":       false,
		"aion-bl/due-only":    true,
		"aion-bl/untouched":   true,
	} {
		if ids[id] != want {
			t.Errorf("%s: shown = %v, want %v", id, ids[id], want)
		}
	}

	// nil overrides (no team store) must reproduce the base-only projection
	ids = oodaWorkIDs(buildOodaWork(snap, "2026-08-22", nil, nil, nil))
	if !ids["aion-bl/portal-done"] || ids["aion-bl/reopened"] {
		t.Fatalf("nil overrides must fall back to the vault's own status: %v", ids)
	}

	// the dashboard's per-person counts and week lanes ride the same rule
	d := buildOodaDashboard(snap, "2026-08-22", overrides, nil, nil)
	for _, it := range d.Week {
		if it.ID == "aion-bl/portal-done" {
			t.Fatal("a portal-done item leaked into the dashboard week lane")
		}
	}
	for _, o := range d.Owners {
		if o.Owner == "BA" && o.Open != 4 {
			t.Fatalf("BA open = %d, want 4 (reopened, in-progress, due-only, untouched)", o.Open)
		}
	}
}

// A member marking a ROCK-TREE task done writes an override keyed
// prop/<slug>#<node-id> — the rock walk must honor it exactly as the backlog
// loop honors its ids, in both directions. It never did (the audit's B3), so
// "mark done" on rock work looked accepted and never cleared from WORK or the
// dashboard — re-arming brian's original complaint on a second id family.
func TestOodaRockWorkHonorsTeamOverrides(t *testing.T) {
	snap := oodaFixture()
	node := func(id, text string, checked bool) *realestate.WorkNode {
		return &realestate.WorkNode{ID: id, Task: &tasks.Task{Text: text, Owner: "BA", Checked: checked}}
	}
	snap.Properties = append(snap.Properties, realestate.Property{
		Slug: "rock-st", Short: "rock-st", Entity: "Garden SPE", Status: "construction", Control: "owned",
		Work: []realestate.WorkStage{{ID: "s1", Text: "Shell", Tasks: []*realestate.WorkNode{
			node("shell/done-via-portal", "hang the joists", false),
			node("shell/reopened", "regrade the pad", true),
			node("shell/untouched", "order lumber", false),
		}}},
	})
	overrides := map[string]teamportal.Override{
		"prop/rock-st#shell/done-via-portal": {Fields: map[string]string{"status": "done", "done_on": "2026-08-22"}},
		"prop/rock-st#shell/reopened":        {Fields: map[string]string{"status": "open"}},
	}
	ids := oodaWorkIDs(buildOodaWork(snap, "2026-08-22", overrides, nil, nil))
	for id, want := range map[string]bool{
		"prop/rock-st#shell/done-via-portal": false,
		"prop/rock-st#shell/reopened":        true,
		"prop/rock-st#shell/untouched":       true,
	} {
		if ids[id] != want {
			t.Errorf("%s: shown = %v, want %v", id, ids[id], want)
		}
	}
	// the dashboard's per-person counts ride the same walk
	d := buildOodaDashboard(snap, "2026-08-22", overrides, nil, nil)
	for _, o := range d.Owners {
		if o.Owner == "BA" && o.Open != 2 {
			t.Fatalf("BA open = %d, want 2 (reopened + untouched)", o.Open)
		}
	}
}

// oodaWorkLaneOf names the lane one item landed in ("" when absent).
func oodaWorkLaneOf(groups []oodaWorkGroup, id string) string {
	for _, g := range groups {
		for lane, items := range map[string][]oodaWorkItem{
			"open": g.Open, "overdue": g.Overdue, "dueThisWeek": g.DueThisWeek,
			"decisions": g.Decisions, "waiting": g.Waiting,
		} {
			for _, it := range items {
				if it.ID == id {
					return lane
				}
			}
		}
	}
	return ""
}

// PatchFields accepts due/needed_by — and the projection must PROJECT them.
// An override's due was accepted by the API, stored in the overlay, and then
// never merged into the item, so the overdue/this-week lanes kept sorting on
// the base date (audit B12). The overlay's date wins for all three sources:
// backlog, rock-tree, and team items.
func TestOodaWorkProjectsDueOverrides(t *testing.T) {
	snap := oodaFixture()
	snap.Backlog = []*aion.BacklogItem{
		{ID: "aion-bl/pulled-in", Text: "was comfortable, now overdue", Owner: "BA", Kind: "task", Status: aion.StatusOpen, Due: "2026-10-01"},
		{ID: "aion-bl/needed-by", Text: "no base due, needed_by set", Owner: "BA", Kind: "task", Status: aion.StatusOpen},
	}
	snap.Properties = append(snap.Properties, realestate.Property{
		Slug: "due-st", Short: "due-st", Entity: "Garden SPE", Status: "construction", Control: "owned",
		Work: []realestate.WorkStage{{ID: "s1", Text: "Shell", DoneBy: "2030-01-01", Tasks: []*realestate.WorkNode{
			{ID: "shell/late", Task: &tasks.Task{Text: "rock date far out, override near", Owner: "BA"}},
		}}},
	})
	team := []teamportal.TeamItem{
		{ID: "team/pushed-out", Kind: "task", Title: "was this week, now pushed", Owner: "BA", Status: "open", Due: "2026-08-23"},
	}
	overrides := map[string]teamportal.Override{
		"aion-bl/pulled-in":      {Fields: map[string]string{"due": "2026-08-20"}},
		"aion-bl/needed-by":      {Fields: map[string]string{"needed_by": "2026-08-25"}},
		"prop/due-st#shell/late": {Fields: map[string]string{"due": "2026-08-01"}},
		"team/pushed-out":        {Fields: map[string]string{"due": "2026-10-15"}},
	}
	groups := buildOodaWork(snap, "2026-08-22", overrides, team, nil)
	for id, want := range map[string]string{
		"aion-bl/pulled-in":      "overdue",     // base 2026-10-01, override pulls it past due
		"aion-bl/needed-by":      "dueThisWeek", // needed_by projects exactly like due
		"prop/due-st#shell/late": "overdue",     // the rock's done-by defers to the overlay
		"team/pushed-out":        "open",        // override pushes it OUT of the week too
	} {
		if got := oodaWorkLaneOf(groups, id); got != want {
			t.Errorf("%s: lane = %q, want %q", id, got, want)
		}
	}
	// no due override → the base date still decides (the defers-to-base case
	// TestOodaWorkHonorsTeamOverrides pins stays intact)
	groups = buildOodaWork(snap, "2026-08-22", nil, team, nil)
	if got := oodaWorkLaneOf(groups, "aion-bl/pulled-in"); got != "open" {
		t.Errorf("no override: lane = %q, want open (base due 2026-10-01)", got)
	}
	if got := oodaWorkLaneOf(groups, "team/pushed-out"); got != "dueThisWeek" {
		t.Errorf("no override: lane = %q, want dueThisWeek (base due 2026-08-23)", got)
	}
}

// Portal-created team items fold into the WORK groups (source:"team"), on the
// same status allow-list and the same overrides as everything else. They used
// to ship in a dead `teamItems` field no client read, so portal-created work
// was invisible everywhere.
func TestOodaWorkIncludesTeamItems(t *testing.T) {
	snap := oodaFixture()
	team := []teamportal.TeamItem{
		{ID: "team/walk-roof", Kind: "task", Title: "walk the roof", Owner: "BA", Status: "open", Captured: "2026-08-20"},
		{ID: "team/pick-lender", Kind: "decision", Title: "pick a lender", Owner: "BA", Status: "open"},
		{ID: "team/already-done", Kind: "task", Title: "done directly", Owner: "BA", Status: "done"},
		{ID: "team/done-via-override", Kind: "task", Title: "done via patch", Owner: "BA", Status: "open"},
	}
	overrides := map[string]teamportal.Override{
		"team/done-via-override": {Fields: map[string]string{"status": "done"}},
	}
	groups := buildOodaWork(snap, "2026-08-22", overrides, team, nil)
	ids := oodaWorkIDs(groups)
	for id, want := range map[string]bool{
		"team/walk-roof":         true,
		"team/pick-lender":       true,
		"team/already-done":      false,
		"team/done-via-override": false,
	} {
		if ids[id] != want {
			t.Errorf("%s: shown = %v, want %v", id, ids[id], want)
		}
	}
	for _, g := range groups {
		if g.Owner != "BA" {
			continue
		}
		if len(g.Decisions) != 1 || g.Decisions[0].ID != "team/pick-lender" {
			t.Fatalf("the team decision must land in the decisions lane: %+v", g.Decisions)
		}
		for _, lane := range [][]oodaWorkItem{g.Open, g.Decisions} {
			for _, it := range lane {
				if strings.HasPrefix(it.ID, "team/") && it.Source != "team" {
					t.Fatalf("team item missing its source flag: %+v", it)
				}
			}
		}
	}
}
