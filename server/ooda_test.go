package server

import (
	"testing"

	"manifest/aion"
	"manifest/realestate"
	"manifest/tasks"
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
	d := buildOodaDashboard(snap, "2026-08-20", nil, nil)
	wantPaid, wantCommitted := oodaLedgerTotals(snap)
	if d.KPIs.Paid != wantPaid || d.KPIs.PaidTotal != wantPaid {
		t.Fatalf("paid = %v/%v, want %v", d.KPIs.Paid, d.KPIs.PaidTotal, wantPaid)
	}
	// CONTRACTED is the Σ-of-signed-contracts figure that must match the
	// cockpit. COMMITTED is a different question (capital we are on the hook
	// for) and is asserted separately, in TestOodaCommittedIsCapitalOnTheHook.
	if d.KPIs.Contracted != wantCommitted {
		t.Fatalf("contracted = %v, want %v", d.KPIs.Contracted, wantCommitted)
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
// The Garden SPE shape, which is what made the old numbers wrong: every parcel
// carries `control: owned` while most are still under contract. The board must
// count them as ACQUIRING, and their purchase prices must land in committed
// and to-close — never in paid.
func TestOodaCommittedIsCapitalOnTheHook(t *testing.T) {
	acqOnly := func(price float64) *realestate.ProjectBudget {
		return &realestate.ProjectBudget{
			PlanTotal:  price,
			Categories: []realestate.BudgetCatRow{{Key: realestate.CatAcquisition, Budget: price}},
		}
	}
	snap := &oodaSnapshot{
		Properties: []realestate.Property{
			// signed but not closed: $20k to close, nothing spent
			{Slug: "uc-1", Short: "742 Bayard", Entity: "Garden SPE",
				Control: "owned", Status: "under_contract", Project: acqOnly(20000)},
			{Slug: "uc-2", Short: "744 Bayard", Entity: "Garden SPE",
				Control: "owned", Status: "under_contract", Project: acqOnly(15000)},
			// closed and building: committed to the WHOLE budget
			{Slug: "own-1", Short: "751 Bayard", Entity: "Garden SPE",
				Control: "owned", Status: "construction",
				Project: &realestate.ProjectBudget{PlanTotal: 300000, Paid: 29000, Committed: 57000}},
			// still negotiating: no obligation at all
			{Slug: "pipe-1", Short: "760 Page", Entity: "Garden SPE",
				Control: "owned", Status: "negotiating", Project: acqOnly(18000)},
		},
		Entities: []realestate.Entity{{Slug: "garden-spe", Name: "Garden SPE"}},
		Backlog:  []*aion.BacklogItem{},
	}
	d := buildOodaDashboard(snap, "2026-08-21", nil, nil)

	if d.KPIs.Owned != 1 || d.KPIs.UnderContract != 2 || d.KPIs.Pipeline != 1 {
		t.Fatalf("counts = owned %d / under-contract %d / pipeline %d, want 1/2/1",
			d.KPIs.Owned, d.KPIs.UnderContract, d.KPIs.Pipeline)
	}
	// committed = the closed project's total budget + the two budgets to close
	if d.KPIs.Committed != 335000 {
		t.Fatalf("committed = %v, want 335000 (300000 owned total + 35000 to close)", d.KPIs.Committed)
	}
	if d.KPIs.ToClose != 35000 {
		t.Fatalf("to close = %v, want 35000", d.KPIs.ToClose)
	}
	// paid is cash only — the unclosed purchase prices are NOT spend
	if d.KPIs.Paid != 29000 {
		t.Fatalf("paid = %v, want 29000 (the one real ledger figure)", d.KPIs.Paid)
	}
	// plan to go = owned remaining (271000) + under-contract totals (35000)
	if d.KPIs.PlanToGo != 306000 {
		t.Fatalf("plan to go = %v, want 306000", d.KPIs.PlanToGo)
	}
	if len(d.Entities) != 1 {
		t.Fatalf("entities = %+v", d.Entities)
	}
	if e := d.Entities[0]; e.Owned != 1 || e.Acquiring != 2 || e.Pipeline != 1 {
		t.Fatalf("Garden SPE row = %+v, want owned 1 / acquiring 2 / pipeline 1", e)
	}
}

// A backlog rock is a machine slug; the WORK tab must render a place and a
// rock, not "ooda-group/operations-health".
func TestOodaRockLabel(t *testing.T) {
	shorts := map[string]string{"4924-fountain-ave": "4924 Fountain Ave"}
	for _, tc := range []struct{ in, container, label string }{
		{"ooda-group/operations-health", "Ooda group", "Operations health"},
		{"4924-fountain-ave", "4924 Fountain Ave", ""},
		{"4924-fountain-ave/roof-tear-off", "4924 Fountain Ave", "Roof tear off"},
		{"", "", ""},
		{"  ", "", ""},
	} {
		c, l := oodaRockLabel(tc.in, shorts)
		if c != tc.container || l != tc.label {
			t.Errorf("oodaRockLabel(%q) = %q / %q, want %q / %q", tc.in, c, l, tc.container, tc.label)
		}
	}
}

// THIS WEEK and WAITING ON are cross-person rollups the dashboard grew so a
// partner can see the schedule and the blockages without opening WORK and
// expanding every person. Both flatten lanes buildOodaWork already computes.
func TestOodaDashboardWeekAndWaiting(t *testing.T) {
	// two people, one rock each, with a mix of overdue / this-week / far-off
	// and one task explicitly waiting on somebody
	stageWith := func(id, text, doneBy string, nodes ...*realestate.WorkNode) realestate.WorkStage {
		return realestate.WorkStage{ID: id, Text: text, DoneBy: doneBy, Tasks: nodes}
	}
	node := func(id, text, owner, waiting string) *realestate.WorkNode {
		return &realestate.WorkNode{ID: id, Task: &tasks.Task{
			Text: text, Owner: owner, Waiting: waiting}}
	}
	snap := &oodaSnapshot{
		Properties: []realestate.Property{
			{Slug: "a-st", Short: "1 A St", Entity: "E", Control: "owned", Status: "construction",
				Work: []realestate.WorkStage{
					stageWith("s1", "Roof", "2020-01-01", // long overdue
						node("n1", "strip the deck", "SM", "")),
					stageWith("s2", "Shell", "2030-01-01", // far future
						node("n2", "order steel", "OS", ""),
						node("n3", "await the survey", "SM", "the surveyor")),
				}},
		},
		Backlog: []*aion.BacklogItem{},
		People:  []*aion.Person{{Initials: "SM", Name: "Stephen"}, {Initials: "OS", Name: "Olga"}},
	}
	d := buildOodaDashboard(snap, "2026-08-22", nil, nil)

	// the waiting task must be in WAITING and NOT double-counted into the week
	if len(d.Waiting) != 1 {
		t.Fatalf("waiting = %d, want 1: %+v", len(d.Waiting), d.Waiting)
	}
	if w := d.Waiting[0]; w.WaitingOn != "the surveyor" {
		t.Fatalf("waitingOn = %q, want %q — the flag alone says nothing actionable",
			w.WaitingOn, "the surveyor")
	}
	// week = overdue + due-this-week across everyone; "order steel" is due in
	// 2030 so it is open, not this week, and the waiting one is in its own lane
	if len(d.Week) != 1 || d.Week[0].Title != "strip the deck" {
		t.Fatalf("week = %+v, want just the overdue roof task", d.Week)
	}
	// overdue is surfaced per person too — the field existed and never rendered
	var sm *oodaOwnerRow
	for i := range d.Owners {
		if d.Owners[i].Owner == "SM" {
			sm = &d.Owners[i]
		}
	}
	if sm == nil || sm.Overdue != 1 {
		t.Fatalf("SM row = %+v, want overdue 1", sm)
	}
	// never nil — the client calls .length on both
	if d.Week == nil || d.Waiting == nil {
		t.Fatal("week/waiting must marshal as [] not null")
	}
}

// An item with no due date cannot be ranked against one that has a date, and
// must not sort above it — otherwise undated work crowds out the actual week.
func TestOodaWeekSortsUndatedLast(t *testing.T) {
	d := oodaDashboard{Week: []oodaWorkItem{
		{Title: "no date", Due: ""},
		{Title: "later", Due: "2026-08-30"},
		{Title: "sooner", Due: "2026-08-23"},
	}}
	sortOodaWeek(d.Week)
	got := []string{d.Week[0].Title, d.Week[1].Title, d.Week[2].Title}
	want := []string{"sooner", "later", "no date"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("week order = %v, want %v", got, want)
		}
	}
}

func TestOodaAttentionRules(t *testing.T) {
	snap := oodaFixture()
	d := buildOodaDashboard(snap, "2026-08-20", nil, nil)
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
	d2 := buildOodaDashboard(snap, "2026-08-20", nil, nil)
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
	d := buildOodaDashboard(snap, "2026-08-20", nil, nil)
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
	groups := buildOodaWork(snap, "2026-08-20", nil, nil)
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

// The WORK tab showed 56 settled decisions as live work (owner report,
// 2026-08-21). Root cause: the open/closed test was a DENY-list ("not checked
// and not done"), and RE decisions are not checkboxes — they close with
// `[status:: decided]`. Every unrecognized status therefore read as open.
// The rule is now an allow-list matching the AION portal's isOpen.
func TestOodaBacklogOpenIsAnAllowList(t *testing.T) {
	openCases := []*aion.BacklogItem{
		{Status: aion.StatusOpen, Text: "open task"},
		{Status: aion.StatusInProgress, Text: "in progress"},
		{Status: "", Text: "no status at all"},
		{Status: "OPEN", Text: "case-insensitive"},
		{Status: " open ", Text: "whitespace"},
	}
	for _, it := range openCases {
		if !oodaBacklogOpen(it) {
			t.Fatalf("%q (status %q) should be open", it.Text, it.Status)
		}
	}
	closedCases := []*aion.BacklogItem{
		{Status: aion.StatusDone, Text: "done task"},
		{Status: aion.StatusDecided, Kind: "decision", Text: "THE REGRESSION: a decided decision"},
		{Status: "DECIDED", Text: "case-insensitive decided"},
		{Checked: true, Text: "checked box, no status"},
		{Status: aion.StatusOpen, Decided: "2026-03-20", Text: "carries its resolution date"},
		{Status: aion.StatusOpen, Outcome: "went with option B", Text: "carries its outcome"},
		{Status: "archived", Text: "a status nobody has invented yet must read CLOSED"},
		nil,
	}
	for _, it := range closedCases {
		if oodaBacklogOpen(it) {
			t.Fatalf("%q (status %q) should be closed", it.Text, it.Status)
		}
	}
}

// End to end through the grouping: a decided decision never reaches a lane.
func TestOodaWorkExcludesDecidedDecisions(t *testing.T) {
	snap := oodaFixture()
	snap.Backlog = []*aion.BacklogItem{
		{ID: "aion-bl/live", Text: "still open", Owner: "BA", Kind: "decision", Status: aion.StatusOpen},
		{ID: "aion-bl/settled", Text: "already decided", Owner: "BA", Kind: "decision",
			Status: aion.StatusDecided, Decided: "2026-03-20"},
		{ID: "aion-bl/donetask", Text: "finished", Owner: "BA", Kind: "task", Status: aion.StatusDone},
	}
	for _, g := range buildOodaWork(snap, "2026-08-21", nil, nil) {
		for _, lane := range [][]oodaWorkItem{g.Open, g.Overdue, g.DueThisWeek, g.Decisions, g.Waiting} {
			for _, it := range lane {
				if it.ID != "aion-bl/live" {
					t.Fatalf("closed item %q surfaced as work in %s's list", it.ID, g.Owner)
				}
			}
		}
	}
	// and the dashboard's per-person counts inherit the same rule
	d := buildOodaDashboard(snap, "2026-08-21", nil, nil)
	for _, o := range d.Owners {
		if o.Owner == "BA" && o.Decisions != 1 {
			t.Fatalf("BA decisions = %d, want 1 (only the undecided one)", o.Decisions)
		}
	}
}
