package goals

import (
	"strings"
	"testing"
)

const moveFixture = `# Goals

## Aion
### 1-year — 2026
- [ ] Human prototype MRI [goal:: aion/mri]

### Rocks (90-day)
- [ ] Human scale spec [goal:: aion/spec] [quarter:: 2026-Q3] [serves:: aion/mri]
    - [ ] Prototype [owner:: YS]
- [ ] Orphan idea [goal:: aion/orphan] [quarter:: 2026-Q3]
- [ ] EPR go/no-go [goal:: aion/epr] [quarter:: 2026-Q3]
`

func TestMoveGoalReparentsAcrossRocks(t *testing.T) {
	d := Parse(moveFixture)
	d.assignIDs()

	// a 30-day item moves between rocks
	if !d.MoveGoal("aion/spec/prototype", "aion/epr") {
		t.Fatal("stage move refused")
	}
	out := Serialize(d)
	if !strings.Contains(out, "- [ ] EPR go/no-go") {
		t.Fatalf("target rock lost:\n%s", out)
	}
	// the moved line now nests under EPR (indented, owner preserved)
	epr := strings.SplitN(out, "EPR go/no-go", 2)[1]
	if !strings.Contains(epr, "    - [ ] Prototype") || !strings.Contains(epr, "[owner:: YS]") {
		t.Fatalf("prototype not under EPR with owner:\n%s", out)
	}

	// an orphan top-level rock nests under a rock (becomes its 30-day item)
	d2 := Parse(out)
	d2.assignIDs()
	if !d2.MoveGoal("aion/orphan", "aion/spec") {
		t.Fatal("orphan nest refused")
	}
	out2 := Serialize(d2)
	spec := strings.SplitN(out2, "Human scale spec", 2)[1]
	if !strings.Contains(strings.SplitN(spec, "EPR", 2)[0], "Orphan idea") {
		t.Fatalf("orphan not under spec:\n%s", out2)
	}

	// promote back to top level
	d3 := Parse(out2)
	d3.assignIDs()
	if !d3.MoveGoal("aion/orphan", "") {
		t.Fatal("promote refused")
	}
	out3 := Serialize(d3)
	if !strings.Contains(out3, "\n- [ ] Orphan idea") {
		t.Fatalf("orphan not top-level:\n%s", out3)
	}

	// round-trips stay fixpoints after every move
	for _, o := range []string{out, out2, out3} {
		if Serialize(Parse(o)) != o {
			t.Fatalf("post-move doc is not a fixpoint:\n%s", o)
		}
	}
}

func TestMoveGoalRefusals(t *testing.T) {
	d := Parse(moveFixture)
	d.assignIDs()
	if d.MoveGoal("aion/spec", "aion/spec") {
		t.Fatal("self-move accepted")
	}
	if d.MoveGoal("aion/spec", "aion/spec/prototype") {
		t.Fatal("cycle (move under own child) accepted")
	}
	if d.MoveGoal("aion/epr", "aion/spec/prototype") {
		t.Fatal("move under a NON-top-level target accepted")
	}
	if d.MoveGoal("aion/nope", "aion/spec") {
		t.Fatal("unknown id accepted")
	}
	if d.MoveGoal("aion/spec", "") {
		t.Fatal("promoting an already-top-level rock accepted")
	}
	// annuals may not gain children via move (only rocks may)
	if d.MoveGoal("aion/orphan", "aion/mri") {
		t.Fatal("move under an ANNUAL accepted")
	}
}

// TestMultiServes: 1:many rock→annual links — repeated [serves::] fields
// round-trip byte-identically; a comma list canonicalizes; EditGoal replaces
// the full list and dedupes.
func TestMultiServes(t *testing.T) {
	doc := `# Goals

## Aion

### 1-year — 2026
- [ ] MRI [goal:: aion/mri]
- [ ] Write [goal:: aion/write]

### Rocks (90-day)
- [ ] Series A 15M [goal:: aion/series-a] [quarter:: 2026-Q3] [serves:: aion/mri] [serves:: aion/write]
`
	d := Parse(doc)
	if out := Serialize(d); out != doc {
		t.Fatalf("multi-serves not a fixpoint:\n%s", out)
	}
	rock := d.Areas[0].Rocks[0]
	if len(rock.Serves) != 2 || rock.Serves[0] != "aion/mri" || rock.Serves[1] != "aion/write" {
		t.Fatalf("serves: %v", rock.Serves)
	}
	// comma form canonicalizes to repeated fields on save
	d2 := Parse(strings.ReplaceAll(doc, "[serves:: aion/mri] [serves:: aion/write]", "[serves:: aion/mri, aion/write]"))
	if got := d2.Areas[0].Rocks[0].Serves; len(got) != 2 {
		t.Fatalf("comma parse: %v", got)
	}
	if !strings.Contains(Serialize(d2), "[serves:: aion/mri] [serves:: aion/write]") {
		t.Fatalf("comma form did not canonicalize:\n%s", Serialize(d2))
	}
	// EditGoal replaces the list, deduping
	d.assignIDs()
	if !d.EditGoal("aion/series-a", GoalEdit{Serves: &[]string{"aion/write", "aion/write", "aion/mri"}}) {
		t.Fatal("edit refused")
	}
	if got := d.Areas[0].Rocks[0].Serves; len(got) != 2 || got[0] != "aion/write" {
		t.Fatalf("edit result: %v", got)
	}
	// clearing
	if !d.EditGoal("aion/series-a", GoalEdit{Serves: &[]string{}}) {
		t.Fatal("clear refused")
	}
	if len(d.Areas[0].Rocks[0].Serves) != 0 {
		t.Fatal("clear failed")
	}
}

// TestAliases: repeated [alias::] fields round-trip byte-identically and
// EditGoal full-replaces + dedups (the portal-matcher vocabulary).
func TestAliases(t *testing.T) {
	doc := `# Goals

## Aion

### Rocks (90-day)
- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3] [serves:: aion/mri] [alias:: fundraising] [alias:: fundraise]
`
	d := Parse(doc)
	if out := Serialize(d); out != doc {
		t.Fatalf("alias not a fixpoint:\n%s", out)
	}
	r := d.Areas[0].Rocks[0]
	if len(r.Aliases) != 2 || r.Aliases[0] != "fundraising" || r.Aliases[1] != "fundraise" {
		t.Fatalf("aliases: %v", r.Aliases)
	}
	d.assignIDs()
	if !d.EditGoal("aion/series-a-15m", GoalEdit{Aliases: &[]string{"fundraise", "fundraise", "series-a-raise"}}) {
		t.Fatal("edit refused")
	}
	if got := d.Areas[0].Rocks[0].Aliases; len(got) != 2 || got[0] != "fundraise" || got[1] != "series-a-raise" {
		t.Fatalf("edit result: %v", got)
	}
	// serves survives an aliases-only edit (independent lists)
	if len(d.Areas[0].Rocks[0].Serves) != 1 {
		t.Fatalf("serves clobbered by alias edit: %v", d.Areas[0].Rocks[0].Serves)
	}
}
