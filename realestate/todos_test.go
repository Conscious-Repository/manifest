package realestate

import (
	"strings"
	"testing"

	"manifest/tasks"
)

// The rock tree is the property's single task home (overhaul §6): the
// task-surface facade projects non-milestone, non-decision nodes and routes
// mutations back into tree nodes.
func TestPropertyTaskListTree(t *testing.T) {
	stages := ParseWork([]string{
		"- [ ] Exterior & structural [done-by:: 2026-10-15]",
		"    - [ ] roofing [milestone::]",
		"        - [ ] choose shingle color [owner:: MCC] [decision::]",
		"        - [ ] schedule crane permit [owner:: me]",
		"    - [ ] pick up fans [todo:: pick-up-fans] [owner:: me]",
		"- [ ] Finishes",
	})
	list := &PropertyTaskList{Stages: stages, Section: "rocks"}

	items := list.Tasks()
	if len(items) != 2 { // milestone + decision excluded
		t.Fatalf("want 2 task-surface rows, got %d: %+v", len(items), items)
	}
	if items[0].ID != "exterior-structural/roofing/schedule-crane-permit" {
		t.Fatalf("derived task id: %q", items[0].ID)
	}
	if items[1].ID != "pick-up-fans" { // explicit [todo::] pin wins
		t.Fatalf("pinned task id: %q", items[1].ID)
	}

	// Find by pin and by work id; mutations flow into the emit
	n := list.Find("pick-up-fans")
	if n == nil {
		t.Fatal("Find by pin")
	}
	n.Task.Checked = true
	n.Task.Done = "2026-08-18"
	if _, byWork := FindWorkNode(list.Stages, "exterior-structural/roofing/schedule-crane-permit"); byWork == nil {
		t.Fatal("Find by work id")
	}
	out := list.Emit()
	if !strings.Contains(out, "- [x] pick up fans [todo:: pick-up-fans] [done:: 2026-08-18] [owner:: me]") {
		t.Fatalf("mutation lost:\n%s", out)
	}

	// Append: under a named rock, under a milestone id, and loose (current rock)
	list.Append(&tasks.Task{Text: "paint trim", Added: "2026-08-18"}, "Finishes")
	list.Append(&tasks.Task{Text: "order shingles"}, "exterior-structural/roofing")
	list.Append(&tasks.Task{Text: "loose one"}, "")
	out = list.Emit()
	if !strings.Contains(out, "- [ ] paint trim [added:: 2026-08-18]") {
		t.Fatalf("append under rock text:\n%s", out)
	}
	if !strings.Contains(out, "        - [ ] order shingles") {
		t.Fatalf("append under milestone must nest at depth 2:\n%s", out)
	}

	// Remove by id
	if !list.Remove("pick-up-fans") {
		t.Fatal("remove by pin")
	}
	if list.Find("pick-up-fans") != nil {
		t.Fatal("removed node still found")
	}
}

// The 3-level grammar: depth roles, milestone/decision flags, est rollup
// through milestones, and byte fixpoint including bare [decision::] markers.
func TestRockTreeGrammar(t *testing.T) {
	src := strings.Join([]string{
		"- [ ] Exterior & structural [done-by:: 2026-10-15]",
		"    - [ ] roofing [milestone::] [est:: 2000]",
		"        - [ ] tear-off [est:: 9000]",
		"        - [ ] choose shingle color [owner:: MCC] [decision::]",
		"    - [ ] point chimney [est:: 3000]",
	}, "\n") + "\n"
	stages := ParseWork(strings.Split(strings.TrimRight(src, "\n"), "\n"))
	if out := EmitWork(stages); out != src {
		t.Fatalf("fixpoint broken:\n--- in ---\n%s--- out ---\n%s", src, out)
	}
	st := stages[0]
	if st.DoneBy != "2026-10-15" {
		t.Fatalf("done-by: %q", st.DoneBy)
	}
	roofing := st.Tasks[0]
	if !roofing.Milestone || roofing.ID != "exterior-structural/roofing" {
		t.Fatalf("milestone flag/id: %+v", roofing)
	}
	if roofing.EstTotal != 11000 { // own 2000 + tear-off 9000
		t.Fatalf("milestone est rollup: %v", roofing.EstTotal)
	}
	if st.EstTotal != 14000 {
		t.Fatalf("rock est rollup: %v", st.EstTotal)
	}
	dec := roofing.Children[1]
	if !dec.Decision || dec.Task.Owner != "MCC" {
		t.Fatalf("decision node: %+v", dec)
	}
	// decisions don't count as unestimated; the open tear-off has an est,
	// point chimney has an est → nothing unestimated
	if st.Unestimated != 0 {
		t.Fatalf("unestimated: %d", st.Unestimated)
	}
	// a childless line with [milestone::] is still a milestone (owner just
	// created it; the explicit field disambiguates)
	lone := ParseWork([]string{"- [ ] R", "    - [ ] framing [milestone::]"})
	if !lone[0].Tasks[0].Milestone {
		t.Fatal("childless [milestone::] must read as milestone")
	}
}

// Money joins recurse: a row tethered to a depth-2 task rolls up through its
// milestone into the rock, draw-aware at each node.
func TestJoinWorkLedgerDepth2(t *testing.T) {
	stages := ParseWork([]string{
		"- [ ] Exterior [work:: exterior]",
		"    - [ ] roofing [milestone::] [work:: exterior/roofing]",
		"        - [ ] tear-off [work:: exterior/roofing/tear-off]",
	})
	JoinWorkLedger(stages, []LedgerRow{
		{Type: "bid", Status: "accepted", Amount: 9000, WorkID: "exterior/roofing/tear-off"},
		{Type: "expense", Status: "paid", Amount: 4000, WorkID: "exterior/roofing/tear-off"},
		{Type: "expense", Status: "paid", Amount: 500, WorkID: "exterior/roofing"},
	}, nil)
	leaf := stages[0].Tasks[0].Children[0]
	if leaf.Committed != 9000 || leaf.Paid != 4000 {
		t.Fatalf("leaf money: %+v", leaf)
	}
	mil := stages[0].Tasks[0]
	if mil.Committed != 9500 || mil.Paid != 4500 {
		t.Fatalf("milestone rollup: committed=%v paid=%v", mil.Committed, mil.Paid)
	}
	if stages[0].Committed != 9500 || stages[0].Paid != 4500 {
		t.Fatalf("rock rollup: committed=%v paid=%v", stages[0].Committed, stages[0].Paid)
	}
}

// Legacy `## tasks` lines still read tolerantly (pre-migration files).
func TestLegacyTasksRead(t *testing.T) {
	legacy := ParsePropertyTasks([]string{
		"- [ ] gutters front + back [added:: 2026-08-01]",
		"a hand-written context line",
		"- [ ] gutters front + back [todo:: gutters-2] [added:: 2026-08-05]",
	})
	list := &PropertyTaskList{Legacy: legacy}
	items := list.Tasks()
	if len(items) != 2 {
		t.Fatalf("want 2 legacy rows, got %d", len(items))
	}
	if items[0].ID != "gutters-front-back" || items[1].ID != "gutters-2" {
		t.Fatalf("legacy ids: %q %q", items[0].ID, items[1].ID)
	}
}
