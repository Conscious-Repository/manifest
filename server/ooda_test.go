package server

import (
	"testing"

	"manifest/aion"
	"manifest/realestate"
)

// oodaFixture: three properties spanning the phases + one that is over plan
// and one that is late, so every attention rule has a subject.
func oodaFixture() *oodaSnapshot {
	prop := func(slug, entity, status string, plan, paid, committed float64, work []realestate.WorkStage) realestate.Property {
		p := realestate.Property{
			Slug: slug, Short: slug, Address: slug + " St", Entity: entity,
			Status: status, Control: "owned", Work: work,
			Project: &realestate.ProjectBudget{PlanTotal: plan, Paid: paid, Committed: committed},
		}
		return p
	}
	stage := func(text, doneBy string, checked bool, tasks []*realestate.WorkNode) realestate.WorkStage {
		return realestate.WorkStage{ID: "s-" + text, Text: text, DoneBy: doneBy, Checked: checked, Tasks: tasks}
	}
	return &oodaSnapshot{
		Properties: []realestate.Property{
			// over plan
			prop("over-st", "Garden SPE", "construction", 100000, 120000, 130000,
				[]realestate.WorkStage{stage("Shell", "2030-01-01", false, nil)}),
			// late (its rock was due in the past)
			prop("late-st", "Garden SPE", "construction", 200000, 50000, 60000,
				[]realestate.WorkStage{stage("Roof", "2020-01-01", false, nil)}),
			// stabilized — settled, so never an attention row
			prop("held-st", "Anderson Org", "leased", 90000, 90000, 90000,
				[]realestate.WorkStage{stage("Done", "", true, nil)}),
			// hidden + research tail must not count anywhere
			{Slug: "hidden-st", Hidden: true, Control: "owned"},
			{Slug: "tracked-st", Control: "tracked"},
		},
		Entities: []realestate.Entity{{Slug: "garden-spe", Name: "Garden SPE"}, {Slug: "anderson", Name: "Anderson Org"}},
		Deals:    []realestate.Deal{{Slug: "duo", Name: "Rehab Duo", Status: "under_contract"}},
		Backlog:  []*aion.BacklogItem{},
		People:   []*aion.Person{{Initials: "BA", Name: "benjamin anderson"}},
		Holdings: map[string]realestate.EntityHoldings{},
	}
}

// The dashboard's money must equal the sum of the SAME Project rollup the
// cockpit's SPENT figure reads — the cross-check that keeps the two surfaces
// from ever disagreeing about the portfolio.
func TestOodaDashboardMoneyMatchesTheRollup(t *testing.T) {
	snap := oodaFixture()
	d := buildOodaDashboard(snap, "2026-08-20")
	wantPaid, wantCommitted := oodaLedgerTotals(snap)
	if d.KPIs.Paid != wantPaid || d.KPIs.PaidTotal != wantPaid {
		t.Fatalf("paid = %v/%v, want %v", d.KPIs.Paid, d.KPIs.PaidTotal, wantPaid)
	}
	if d.KPIs.Committed != wantCommitted {
		t.Fatalf("committed = %v, want %v", d.KPIs.Committed, wantCommitted)
	}
	// hidden and research-tail records are outside the shared portfolio
	if d.KPIs.PortfolioSize != 3 {
		t.Fatalf("portfolio size = %d, want 3 (hidden + tracked excluded)", d.KPIs.PortfolioSize)
	}
	// only the unsettled ones are "live projects"
	if d.KPIs.LiveProjects != 2 {
		t.Fatalf("live projects = %d, want 2", d.KPIs.LiveProjects)
	}
	if d.KPIs.OverPlan != 1 {
		t.Fatalf("over plan = %d, want 1", d.KPIs.OverPlan)
	}
	// plan-to-go never counts a NEGATIVE remainder (the over-plan property)
	if d.KPIs.PlanToGo != 150000 {
		t.Fatalf("plan to go = %v, want 150000 (only the under-plan remainder)", d.KPIs.PlanToGo)
	}
}

// The three attention rules, ported verbatim from the cockpit.
func TestOodaAttentionRules(t *testing.T) {
	snap := oodaFixture()
	d := buildOodaDashboard(snap, "2026-08-20")
	got := map[string]oodaFacts{}
	for _, f := range d.Attention {
		got[f.Slug] = f
	}
	if !got["over-st"].Over {
		t.Fatal("over-plan property not flagged")
	}
	if !got["late-st"].Late {
		t.Fatal("late property not flagged")
	}
	if _, ok := got["held-st"]; ok {
		t.Fatal("a settled property must never light attention — every lease would flag forever")
	}
	// a pipeline parcel with NO rocks is 'not started', not 'stalled' — the
	// hasStages arm is the noise cut this rule exists for
	snap.Properties = append(snap.Properties, realestate.Property{
		Slug: "raw-lot", Entity: "Garden SPE", Status: "under_contract", Control: "owned",
		Project: &realestate.ProjectBudget{},
	})
	d2 := buildOodaDashboard(snap, "2026-08-20")
	for _, f := range d2.Attention {
		if f.Slug == "raw-lot" {
			t.Fatal("a parcel with no rock plan must not read as stalled")
		}
	}
}

// The entity board never sums owned and acquiring, and surfaces the
// unassigned row LAST rather than hiding the modelling gap.
func TestOodaEntityBoardKeepsUnassignedVisible(t *testing.T) {
	snap := oodaFixture()
	snap.Properties = append(snap.Properties, realestate.Property{
		Slug: "orphan", Control: "owned", Status: "construction",
		Project: &realestate.ProjectBudget{Paid: 10},
	})
	d := buildOodaDashboard(snap, "2026-08-20")
	if len(d.Entities) == 0 || d.Entities[len(d.Entities)-1].Entity != "" {
		t.Fatalf("the unassigned row must sort last: %+v", d.Entities)
	}
	for _, e := range d.Entities {
		if e.Owned+e.Acquiring == 0 {
			t.Fatalf("entity row with no holdings: %+v", e)
		}
	}
}

// WORK groups by assignee, puts the unassigned group last, and lands each
// item in the right lane.
func TestOodaWorkGroupsAndLanes(t *testing.T) {
	snap := oodaFixture()
	snap.Backlog = []*aion.BacklogItem{
		{ID: "aion-bl/a", Text: "call the bank", Owner: "BA", Kind: "task", Due: "2020-01-01"},
		{ID: "aion-bl/b", Text: "pick a lender", Owner: "BPA", Kind: "decision"},
		{ID: "aion-bl/c", Text: "nobody owns me", Kind: "task"},
		{ID: "aion-bl/done", Text: "finished", Owner: "BA", Kind: "task", Checked: true},
	}
	groups := buildOodaWork(snap, "2026-08-20")
	byOwner := map[string]oodaWorkGroup{}
	for _, g := range groups {
		byOwner[g.Owner] = g
	}
	if len(byOwner["BA"].Overdue) != 1 {
		t.Fatalf("BA overdue = %+v", byOwner["BA"])
	}
	if len(byOwner["BPA"].Decisions) != 1 {
		t.Fatalf("BPA decisions = %+v", byOwner["BPA"])
	}
	if len(byOwner[""].Open) != 1 {
		t.Fatalf("unassigned open = %+v", byOwner[""])
	}
	if groups[len(groups)-1].Owner != "" {
		t.Fatal("the unassigned group must sort LAST — it is the finding")
	}
	// a checked backlog item is not open work
	for _, g := range groups {
		for _, lane := range [][]oodaWorkItem{g.Open, g.Overdue, g.DueThisWeek, g.Decisions, g.Waiting} {
			for _, it := range lane {
				if it.ID == "aion-bl/done" {
					t.Fatal("a done item leaked into open work")
				}
			}
		}
	}
}

// The rock-tree item id round-trips: it is how the assignee lock finds a node.
func TestOodaNodeIDRoundTrip(t *testing.T) {
	slug, work, ok := parseOodaNodeID("prop/748-n-euclid#shell/roof")
	if !ok || slug != "748-n-euclid" || work != "shell/roof" {
		t.Fatalf("parse = %q %q %v", slug, work, ok)
	}
	if _, _, ok := parseOodaNodeID("aion-bl/something"); ok {
		t.Fatal("a backlog id must not parse as a node id")
	}
	if _, _, ok := parseOodaNodeID("prop/no-hash"); ok {
		t.Fatal("a bare property anchor is not a node id")
	}
}
