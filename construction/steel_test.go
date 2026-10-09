package construction

import (
	"strings"
	"testing"
)

// The 761 framing: 2C3×3.5 rafters at 16 in on a W8×13 beam 6 in off the wall.
func TestSteelFraming(t *testing.T) {
	s, st, asmID := templateProblem(t)
	beam := NewID(KindComponent)
	st = mustExec(t, s, st, asmID,
		map[string]any{"op": "UseSteelRafters", "size": "C3x3.5", "spacing": 406.4, "note": "A2.07"},
		map[string]any{"op": "AddSteelBeam", "newComponentId": beam, "size": "W8x13", "position": 152.4, "note": "A2.07 grid A.2"})
	a := st.Assemblies[asmID]
	r := a.firstOf(TypeRafterArray)
	if r.param("depth") != 76.2 || r.param("width") != 69.6 || r.param("spacing") != 406.4 || !strings.Contains(r.Name, "Steel") {
		t.Fatalf("rafters: %s %v", r.Name, r.Shape.Params)
	}
	ir := compile(t, st, asmID)
	solids := map[string]int{}
	for _, p := range ir.Parts {
		solids[p.Component] = len(p.Solids)
	}
	if solids[r.ID] == 0 || solids[r.ID]%3 != 0 || solids[beam] != 3 {
		t.Fatalf("channels are three strips each, the beam three plates: %v", solids)
	}
	for _, id := range ir.Facts.Disconnected {
		if id == beam || id == r.ID {
			t.Fatalf("the steel touches the assembly: %v", ir.Facts.Disconnected)
		}
	}
	steelFact := false
	for _, ff := range ir.Facts.Fasteners {
		if ff.Host == r.ID && ff.HostSteel && ff.HostFlange == 6.9 {
			steelFact = true
		}
	}
	if !steelFact {
		t.Fatalf("screws into the rafters are measured against the steel flange: %+v", ir.Facts.Fasteners)
	}
	keys := map[string]bool{}
	for _, is := range st.Validation[asmID].Issues {
		if is.Status != "resolved" {
			keys[is.RuleKey] = true
		}
	}
	if !keys["steel.protection"] || keys["geometry.disconnected"] || keys["structure.fastener.embedment"] {
		t.Fatalf("steel review, nothing loose, no timber embedment rule: %v", keys)
	}
	sec, err := Section(ir, st.Catalog, SectionPlane{Origin: [3]float64{300, 0, 0}, Normal: [3]float64{1, 0, 0}, Up: [3]float64{0, 0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, p := range sec.Parts {
		if p.Component == beam {
			seen = true
		}
	}
	if !seen {
		t.Fatal("the beam is in the section")
	}
	if _, _, err := exec(t, s, st, asmID, OwnerActor(), map[string]any{"op": "AddSteelBeam", "newComponentId": NewID(KindComponent), "size": "W99x1", "position": 150}); err == nil || !strings.Contains(err.Error(), "W8x13") {
		t.Fatalf("an unknown size names the ones there are: %v", err)
	}
}
