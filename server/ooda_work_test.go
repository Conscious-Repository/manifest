package server

import (
	"testing"

	"manifest/aion"
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
	ids := oodaWorkIDs(buildOodaWork(snap, "2026-08-22", overrides))
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
	ids = oodaWorkIDs(buildOodaWork(snap, "2026-08-22", nil))
	if !ids["aion-bl/portal-done"] || ids["aion-bl/reopened"] {
		t.Fatalf("nil overrides must fall back to the vault's own status: %v", ids)
	}

	// the dashboard's per-person counts and week lanes ride the same rule
	d := buildOodaDashboard(snap, "2026-08-22", overrides)
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
