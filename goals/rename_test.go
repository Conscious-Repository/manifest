package goals

import (
	"strings"
	"testing"
)

// The rename-identity contract (kernel identity rule): a rename NEVER moves a
// goal's id — references ([rock::] in to do.md/backlog, daily [goal::],
// serves chains, the portal) store ids, so the id pins on first rename and
// the old name's slug joins the alias vocabulary on rocks/annuals.

const renameSample = `## Aion

### 1-year
- [ ] Prototype shipped [goal:: aion/prototype-shipped]

### Rocks (90-day)
- [ ] Series A 15M [goal:: aion/series-a-15m] [serves:: aion/prototype-shipped]
    - [ ] resources for raise ready
    - [ ] term sheet signed
`

func TestRenameUnpinnedStageKeepsID(t *testing.T) {
	d := Parse(renameSample)
	_, st := d.FindGoal("aion/series-a-15m/resources-for-raise-ready")
	if st == nil {
		t.Fatal("derived stage id not found")
	}
	text := "resources locked in"
	if !d.EditGoal("aion/series-a-15m/resources-for-raise-ready", GoalEdit{Text: &text}) {
		t.Fatal("edit failed")
	}
	// the id must NOT re-derive from the new text
	if _, g := d.FindGoal("aion/series-a-15m/resources-for-raise-ready"); g == nil || g.Text != "resources locked in" {
		t.Fatalf("stage id moved on rename: %+v", g)
	}
	// and the pin must survive serialization → reparse
	out := Serialize(d)
	if !strings.Contains(out, "resources locked in [goal:: aion/series-a-15m/resources-for-raise-ready]") {
		t.Fatalf("pin not emitted:\n%s", out)
	}
	d2 := Parse(out)
	if _, g := d2.FindGoal("aion/series-a-15m/resources-for-raise-ready"); g == nil {
		t.Fatal("pin lost across round-trip")
	}
	// fixpoint holds
	if again := Serialize(Parse(out)); again != out {
		t.Fatalf("not a fixpoint after rename:\n%s", out)
	}
}

func TestRenameRockAddsAliasKeepsID(t *testing.T) {
	d := Parse(renameSample)
	text := "Close the round"
	if !d.EditGoal("aion/series-a-15m", GoalEdit{Text: &text}) {
		t.Fatal("edit failed")
	}
	_, g := d.FindGoal("aion/series-a-15m")
	if g == nil || g.Text != "Close the round" {
		t.Fatalf("rock id moved on rename: %+v", g)
	}
	// the OLD name's slug joins the aliases only when it differs from the id
	// tail — here old slug == "series-a-15m" == tail, so no alias yet
	if len(g.Aliases) != 0 {
		t.Fatalf("redundant alias added: %v", g.Aliases)
	}
	// a SECOND rename records the intermediate name's slug as an alias
	text2 := "Series A closed"
	if !d.EditGoal("aion/series-a-15m", GoalEdit{Text: &text2}) {
		t.Fatal("second edit failed")
	}
	_, g = d.FindGoal("aion/series-a-15m")
	if len(g.Aliases) != 1 || g.Aliases[0] != "close-the-round" {
		t.Fatalf("intermediate-name alias missing: %v", g.Aliases)
	}
	// serves chain still resolves by id
	if !strings.Contains(Serialize(d), "[serves:: aion/prototype-shipped]") {
		t.Fatal("serves chain lost")
	}
}

func TestAreaRenamePinsDescendants(t *testing.T) {
	d := Parse(renameSample)
	if !d.RenameArea("Aion", "Aion Labs") {
		t.Fatal("area rename failed")
	}
	d.assignIDs()
	// every goal keeps its aion/ id — the unpinned stages got pinned before
	// the prefix changed
	for _, id := range []string{
		"aion/prototype-shipped",
		"aion/series-a-15m",
		"aion/series-a-15m/resources-for-raise-ready",
		"aion/series-a-15m/term-sheet-signed",
	} {
		if _, g := d.FindGoal(id); g == nil {
			t.Fatalf("id %q lost after area rename", id)
		}
	}
	out := Serialize(d)
	if !strings.Contains(out, "## Aion Labs") {
		t.Fatalf("area not renamed:\n%s", out)
	}
	if again := Serialize(Parse(out)); again != out {
		t.Fatalf("not a fixpoint after area rename:\n%s", out)
	}
}
