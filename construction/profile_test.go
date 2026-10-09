package construction

import (
	"strings"
	"testing"
)

func TestProfileRefusesWhatCantBeBuilt(t *testing.T) {
	cases := []struct {
		name string
		pts  [][2]float64
		want string
	}{
		{"one point", [][2]float64{{0, 0}}, "2 to 32 points"},
		{"crossing", [][2]float64{{0, 0}, {100, 100}, {100, 0}, {0, 100}}, "crosses the segment"},
		{"closed hem", [][2]float64{{0, 100}, {0, 0}, {0.5, 100}}, "closed hem"},
		{"outside", [][2]float64{{0, 0}, {5000, 0}}, "outside the junction"},
		{"stacked", [][2]float64{{0, 0}, {0, 0.2}, {0, 50}}, "at least 1 mm"},
	}
	for _, c := range cases {
		got := strings.Join(checkProfile("points", c.pts), "; ")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q lacks %q", c.name, got, c.want)
		}
	}
	if got := checkProfile("points", [][2]float64{{-20, 235}, {0, 235}, {0, 160}, {12, 140}, {12, 100}}); len(got) != 0 {
		t.Fatalf("a receiver hooked into a joint is buildable: %v", got)
	}
}

// A custom counterflashing receiver: hooked 20 mm into a joint 235 mm above
// the roof, down the wall face, kicked out to lap the apron's upstand. The
// compiler builds it, cuts the brick to receive it, and measures the lap.
func TestProfiledPartIsBuiltAndMeasured(t *testing.T) {
	s, st, asmID := templateProblem(t)
	cf := st.Assemblies[asmID].firstOf(TypeCounterflashing)
	st = mustExec(t, s, st, asmID, map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-surface-counterflashing", "newComponentIds": map[string]string{"apron": NewID(KindComponent)}})
	id := NewID(KindComponent)
	st = mustExec(t, s, st, asmID, map[string]any{"op": "AddProfiledPart", "newComponentId": id, "name": "Receiver in a cut joint",
		"thickness": 0.6, "points": [][2]float64{{-20, 235}, {0, 235}, {0, 160}, {12, 140}, {12, 100}}, "replaces": cf.ID})
	a := st.Assemblies[asmID]
	if c, _ := a.component(cf.ID); c.Applicability != "inapplicable" {
		t.Fatal("the replaced counterflashing is set aside")
	}
	ir := compile(t, st, asmID)
	var lap *LapFact
	for i := range ir.Facts.Laps {
		if ir.Facts.Laps[i].Over == id {
			lap = &ir.Facts.Laps[i]
		}
	}
	upstand := a.firstOf(TypeApronFlashing).param("upstand")
	if lap == nil || !lap.OK || lap.Lap < upstand-100-1 || lap.Lap > upstand-100+1 {
		t.Fatalf("lap over the upstand measured (upstand %v): %+v", upstand, lap)
	}
	if len(ir.Facts.WallCuts) != 1 || ir.Facts.WallCuts[0].Depth != 20 {
		t.Fatalf("the brick is cut to receive it: %+v", ir.Facts.WallCuts)
	}
	found := false
	for _, p := range ir.Parts {
		if p.Component == id && len(p.Solids) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("the part has geometry")
	}
	keys := map[string]bool{}
	for _, is := range st.Validation[asmID].Issues {
		keys[is.RuleKey] = true
	}
	if !keys["masonry.custom-cut"] {
		t.Fatalf("the cut is an issue to review: %v", keys)
	}
	// redraw it shallower and too short to lap: the lap check speaks
	st = mustExec(t, s, st, asmID, map[string]any{"op": "SetProfile", "componentId": id, "points": [][2]float64{{0, 235}, {0, 170}, {12, 160}, {12, upstand - 10}}})
	keys = map[string]bool{}
	for _, is := range st.Validation[asmID].Issues {
		if is.Status != "resolved" {
			keys[is.RuleKey] = true
		}
	}
	if keys["masonry.custom-cut"] || !keys["flashing.lap.custom-over-upstand"] {
		t.Fatalf("no cut, and a short lap is flagged: %v", keys)
	}
	if _, _, err := exec(t, s, st, asmID, OwnerActor(), map[string]any{"op": "SetProfile", "componentId": id, "points": [][2]float64{{0, 0}, {100, 100}, {100, 0}, {0, 100}}}); err == nil || !strings.Contains(err.Error(), "crosses") {
		t.Fatalf("a crossing profile is refused with where: %v", err)
	}
}

// Each custom part reports its own cut, even when another one cuts deeper.
func TestProfiledPartsReportTheirOwnCuts(t *testing.T) {
	s, st, asmID := templateProblem(t)
	a1, a2 := NewID(KindComponent), NewID(KindComponent)
	st = mustExec(t, s, st, asmID,
		map[string]any{"op": "AddProfiledPart", "newComponentId": a1, "name": "deep", "points": [][2]float64{{-40, 300}, {0, 300}, {0, 200}}},
		map[string]any{"op": "AddProfiledPart", "newComponentId": a2, "name": "shallow", "points": [][2]float64{{-10, 240}, {0, 240}, {0, 180}}})
	ir := compile(t, st, asmID)
	got := map[string]float64{}
	for _, w := range ir.Facts.WallCuts {
		got[w.Component] = w.Depth
	}
	if got[a1] != 40 || got[a2] != 10 {
		t.Fatalf("own depths: %v", got)
	}
}

// A custom part that laps the upstand makes the roof-to-wall water
// transition; one that stops short doesn't.
func TestProfiledPartMakesTheWaterTransition(t *testing.T) {
	s, st, asmID := templateProblem(t)
	cf := st.Assemblies[asmID].firstOf(TypeCounterflashing)
	st = mustExec(t, s, st, asmID, map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-surface-counterflashing", "newComponentIds": map[string]string{"apron": NewID(KindComponent)}})
	id := NewID(KindComponent)
	st = mustExec(t, s, st, asmID, map[string]any{"op": "AddProfiledPart", "newComponentId": id, "name": "receiver", "replaces": cf.ID,
		"points": [][2]float64{{-20, 235}, {0, 235}, {0, 160}, {12, 140}, {12, 100}}})
	water := func() bool {
		for _, is := range st.Validation[asmID].Issues {
			if is.Status != "resolved" && is.RuleKey == "moisture.transition.water" {
				return true
			}
		}
		return false
	}
	if water() {
		t.Fatal("a lapping custom part makes the water transition")
	}
	st = mustExec(t, s, st, asmID, map[string]any{"op": "SetProfile", "componentId": id, "points": [][2]float64{{-20, 235}, {0, 235}, {0, 170}}})
	if !water() {
		t.Fatal("a custom part that stops above the upstand doesn't")
	}
}
