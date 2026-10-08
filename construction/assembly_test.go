package construction

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// templateProblem creates a problem seeded with the roof-masonry draft.
func templateProblem(t *testing.T) (*Store, *State, string) {
	t.Helper()
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "create-tpl-0001", "title": "Corrugated roof to masonry wall",
		"template": TemplateRoofMasonry})
	req, hash, err := ParseCreate(raw)
	if err != nil {
		t.Fatal(err)
	}
	st, _, err := s.CreateProblem(fixtureProperty, req, hash, OwnerActor(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Problem.Alternatives) != 1 {
		t.Fatalf("alternatives %v", st.Problem.Alternatives)
	}
	return s, st, st.Problem.Alternatives[0]
}

var reqSeq int

// exec runs operations on an assembly with the state's current revisions.
func exec(t *testing.T, s *Store, st *State, asmID string, actor Actor, ops ...map[string]any) (*State, *Receipt, error) {
	t.Helper()
	reqSeq++
	body := map[string]any{"schemaVersion": 1, "requestId": fmt.Sprintf("req-asm-%06d", reqSeq), "problemId": st.Problem.ID,
		"operations": ops, "expectedProblemRevision": st.Revision("problem")}
	if asmID != "" {
		body["assemblyId"] = asmID
		body["expectedAssemblyRevision"] = st.Revision("assembly:" + asmID)
	}
	raw, _ := json.Marshal(body)
	pc, err := ParseCommand(raw)
	if err != nil {
		return nil, nil, err
	}
	return s.ExecuteCommand(fixtureProperty, pc, actor, nil)
}

func mustExec(t *testing.T, s *Store, st *State, asmID string, ops ...map[string]any) *State {
	t.Helper()
	next, _, err := exec(t, s, st, asmID, OwnerActor(), ops...)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func issueKeys(r *ValidationReport, sev string) map[string]bool {
	out := map[string]bool{}
	for _, is := range r.Issues {
		if (sev == "" || is.Severity == sev) && is.Status != "resolved" {
			out[is.RuleKey] = true
		}
	}
	return out
}

func compByType(a *Assembly, typ string) *Component { return a.firstOf(typ) }

func compile(t *testing.T, st *State, id string) *GeometryIR {
	t.Helper()
	ir, err := Compile(st.Assemblies[id], st.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

// The seeded draft has every §4 member, illustrative labelled numbers, an
// unknown wall and unresolved orientation, a stored model hash, and the
// conditional issue set — and no blocking finding.
func TestConstructionAssemblyTemplateSeed(t *testing.T) {
	_, st, id := templateProblem(t)
	a := st.Assemblies[id]
	for _, typ := range []string{TypeRafterArray, TypeDecking, TypeControlLayer, TypeInsulation, TypeUnderlayment, TypeBattenArray,
		TypeCorrugatedSheet, TypeProfileClosure, TypeApronFlashing, TypeCounterflashing, TypeSealant, TypeFastenerSet, TypeMasonryWythe} {
		if a.firstOf(typ) == nil {
			t.Fatalf("missing %s", typ)
		}
	}
	if len(a.byType(TypeMasonryWythe)) != 2 || len(a.byType(TypeFastenerSet)) != 2 {
		t.Fatal("two wythes and two fastener sets expected")
	}
	if a.Junction.WallCondition.Value != "unknown" || a.Junction.Orientation != "unresolved" || a.firstOf(TypeCavitySpace) != nil {
		t.Fatal("must start with an unknown wall, unresolved orientation and no assumed cavity")
	}
	for name, q := range a.Parameters {
		if !q.Illustrative || q.Provenance != ProvUserAssumption {
			t.Fatalf("parameter %s must be a labelled illustrative assumption", name)
		}
	}
	if *a.firstOf(TypeInsulation).Shape.Params["thickness"].Value != 100 || a.aparam("pitch") != 10 {
		t.Fatal("fixture geometry")
	}
	ir := compile(t, st, id)
	if a.ModelHash != ir.Hash || a.CompilerVersion != CompilerVersion {
		t.Fatalf("stored model hash %s != compiled %s", a.ModelHash, ir.Hash)
	}
	rep := st.Validation[id]
	if rep == nil || rep.AssemblyRevision != st.Revision("assembly:"+id) || rep.GeometryHash != ir.Hash {
		t.Fatalf("validation report must pin the exact assembly revision and geometry: %+v", rep)
	}
	keys := issueKeys(rep, SevCritical)
	for _, want := range []string{"wall.condition.unknown", "junction.orientation.unresolved", "junction.strategy.unresolved",
		"structure.specification", "moisture.vapour-control", "fastener.specification"} {
		if !keys[want] {
			t.Fatalf("missing critical issue %s in %v", want, keys)
		}
	}
	if rep.Counts.Blocking != 0 {
		t.Fatal("stored report never carries blocking findings")
	}
	for _, k := range []string{"masonry.cavity.drainage", "masonry.reglet.cutting"} {
		if keys[k] {
			t.Fatalf("%s must not appear before a wall type/strategy is chosen", k)
		}
	}
	if a.Applicability.Status != "unknown" {
		t.Fatalf("applicability %+v", a.Applicability)
	}
}

// 100 → 150 mm insulation by a direct dimension edit: same component ids,
// thicker layer, deeper stack, and the fastener review issues appear — the
// fasteners' adequacy is not silently assumed unchanged.
func TestConstructionAssemblyInsulationEditTriggersFastenerReview(t *testing.T) {
	s, st, id := templateProblem(t)
	before := st.Assemblies[id]
	ins := before.firstOf(TypeInsulation)
	idsBefore := []string{}
	for _, c := range before.Components {
		idsBefore = append(idsBefore, c.ID)
	}
	wallIssue := ""
	for _, is := range st.Validation[id].Issues {
		if is.RuleKey == "wall.condition.unknown" {
			wallIssue = is.ID
		}
	}
	st2, rc, err := exec(t, s, st, id, OwnerActor(), map[string]any{"op": "SetDimension", "componentId": ins.ID, "dimension": "thickness", "value": 150, "unit": "mm"})
	if err != nil {
		t.Fatal(err)
	}
	a := st2.Assemblies[id]
	for i, c := range a.Components {
		if c.ID != idsBefore[i] {
			t.Fatal("component ids must survive a parameter edit")
		}
	}
	if string(rc.Operations[0].After) == string(rc.Operations[0].Before) || !strings.Contains(string(rc.Operations[0].After), `"value":150`) {
		t.Fatalf("receipt before/after %s → %s", rc.Operations[0].Before, rc.Operations[0].After)
	}
	if a.ModelHash == before.ModelHash {
		t.Fatal("geometry hash must change")
	}
	ir := compile(t, st2, id)
	if ir.Facts.StackToRafter != 216 {
		t.Fatalf("stack %v", ir.Facts.StackToRafter)
	}
	var structural FastenerFact
	for _, f := range ir.Facts.Fasteners {
		if f.StackBased {
			structural = f
		}
	}
	if structural.Embedment != 14 || !structural.ReachesHost {
		t.Fatalf("embedment after the edit: %+v", structural)
	}
	keys := issueKeys(st2.Validation[id], SevCritical)
	if !keys["structure.fastener.embedment"] || !keys["structure.fastener.stack-changed"] || !keys["structure.specification"] {
		t.Fatalf("fastener review issues missing: %v", keys)
	}
	for _, is := range st2.Validation[id].Issues {
		if is.RuleKey == "wall.condition.unknown" && is.ID != wallIssue {
			t.Fatal("an issue keeps its id across revisions (rule key + target)")
		}
	}
	// re-specifying the screw length for the new stack clears stack-changed,
	// but structural review never clears
	st3 := mustExec(t, s, st2, id, map[string]any{"op": "SetDimension", "componentId": a.byRole("attachment:batten-through-stack").ID, "dimension": "length", "value": 280, "unit": "mm"})
	keys = issueKeys(st3.Validation[id], SevCritical)
	if keys["structure.fastener.stack-changed"] || keys["structure.fastener.embedment"] || !keys["structure.specification"] {
		t.Fatalf("after re-specifying the length: %v", keys)
	}
	resolved := false
	for _, is := range st3.Validation[id].Issues {
		resolved = resolved || (is.RuleKey == "structure.fastener.stack-changed" && is.Status == "resolved")
	}
	if !resolved {
		t.Fatal("the cleared issue is reported once as resolved")
	}
}

// The same canonical command on the same assembly content yields the same
// geometry hash, whoever the actor; compile is pure and repeatable, and a
// reload recompiles to the stored hash.
func TestConstructionCommandsSameOperationSameGeometry(t *testing.T) {
	s, st, id := templateProblem(t)
	ins := st.Assemblies[id].firstOf(TypeInsulation).ID
	human, _, err := exec(t, s, st, id, OwnerActor(), map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"})
	if err != nil {
		t.Fatal(err)
	}
	// an agent applies the identical operation to a copy of the original
	s2, st2b, id2 := templateProblem(t)
	_ = s2
	agentSt, agentRc, err := exec(t, s2, st2b, id2, AgentActor("alfred", "cap-1", ""), map[string]any{"op": "SetDimension", "componentId": st2b.Assemblies[id2].firstOf(TypeInsulation).ID, "dimension": "thickness", "value": 150, "unit": "mm"})
	if err != nil {
		t.Fatal(err)
	}
	if agentRc.Actor.Kind != ActorAgent || agentRc.Actor.Agent != "alfred" {
		t.Fatalf("receipt actor %+v", agentRc.Actor)
	}
	// different random ids → compare geometry with ids normalised: same
	// triangle count, bounds and facts
	a, b := compile(t, human, id), compile(t, agentSt, id2)
	if a.Triangles != b.Triangles || a.Bounds != b.Bounds || a.Facts.StackToRafter != b.Facts.StackToRafter {
		t.Fatal("same operation must give the same geometry")
	}
	// on the SAME content the hash is identical, repeatedly and after reload
	again := compile(t, human, id)
	if again.Hash != a.Hash {
		t.Fatal("compile is not deterministic")
	}
	reloaded, err := s.Load(fixtureProperty, st.Problem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if compile(t, reloaded, id).Hash != human.Assemblies[id].ModelHash {
		t.Fatal("reload must recompile to the stored model hash")
	}
}

// Units convert once and deterministically; unknown units, NaN-like and
// out-of-range values are refused with the prior assembly intact.
func TestConstructionCommandsUnitsAndRanges(t *testing.T) {
	s, st, id := templateProblem(t)
	ins := st.Assemblies[id].firstOf(TypeInsulation).ID
	st2 := mustExec(t, s, st, id, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 6, "unit": "in"})
	if v := *st2.Assemblies[id].firstOf(TypeInsulation).Shape.Params["thickness"].Value; v != 152.4 {
		t.Fatalf("6 in → %v mm", v)
	}
	before := headBytes(t, s, fixtureProperty, st.Problem.ID)
	for _, bad := range []map[string]any{
		{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 100, "unit": "furlong"},
		{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 0, "unit": "mm"},
		{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": -5, "unit": "mm"},
		{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 1e9, "unit": "mm"},
		{"op": "SetDimension", "componentId": ins, "dimension": "density", "value": 10, "unit": "mm"},
		{"op": "SetPitch", "value": 75, "unit": "deg"},
		{"op": "SetPitch", "value": 10, "unit": "rad"},
	} {
		if _, _, err := exec(t, s, st2, id, OwnerActor(), bad); StatusOf(err) != 422 {
			t.Fatalf("%v: %v", bad, err)
		}
	}
	if raw := []byte(`{"schemaVersion":1,"requestId":"req-nan-0001","problemId":"` + st.Problem.ID + `","assemblyId":"` + id +
		`","expectedAssemblyRevision":"` + st2.Revision("assembly:"+id) + `","operations":[{"op":"SetDimension","componentId":"` + ins +
		`","dimension":"thickness","value":NaN,"unit":"mm"}]}`); true {
		if _, err := ParseCommand(raw); StatusOf(err) != 422 {
			t.Fatalf("NaN literal: %v", err)
		}
	}
	if string(headBytes(t, s, fixtureProperty, st.Problem.ID)) == string(before) {
		// st2 moved the head once; the refused writes must not have moved it again
	}
	cur, _ := s.Load(fixtureProperty, st.Problem.ID)
	if cur.Head.Generation != st2.Head.Generation {
		t.Fatal("a refused command moved the head")
	}
	// unknown value keeps a labelled placeholder for geometry
	st3 := mustExec(t, s, st2, id, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": nil, "unit": "mm"})
	q := st3.Assemblies[id].firstOf(TypeInsulation).Shape.Params["thickness"]
	if q.Value != nil || q.Placeholder == nil || *q.Placeholder != 152.4 || q.Label() != "unresolved" {
		t.Fatalf("unknown quantity %+v", q)
	}
	if compile(t, st3, id).Labels[ins+".thickness"] != "unresolved" {
		t.Fatal("drawings must label the unknown dimension unresolved")
	}
}

// Wall-condition branches: cavity (assumed) adds a cavity space with an
// unknown width; a through-wall tray needs the cavity; solid/bonded with a
// reglet raises the masonry-cutting review; mismatches are inapplicable.
func TestConstructionValidationWallBranches(t *testing.T) {
	s, st, id := templateProblem(t)
	// through-wall before a cavity is refused, not drawn into solid masonry
	if _, _, err := exec(t, s, st, id, OwnerActor(), map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-through-wall-flashing",
		"newComponentIds": map[string]string{"through-wall": NewID(KindComponent), "weeps": NewID(KindComponent), "end-dams": NewID(KindComponent)}}); StatusOf(err) != 422 {
		t.Fatalf("through-wall without a cavity: %v", err)
	}
	cav := NewID(KindComponent)
	st2 := mustExec(t, s, st, id, map[string]any{"op": "SetWallCondition", "value": "cavity", "provenance": "user-assumption",
		"note": "conditional alternative assumes a cavity", "newComponentIds": map[string]string{"cavity": cav}})
	a := st2.Assemblies[id]
	if c := a.firstOf(TypeCavitySpace); c == nil || c.ID != cav || c.Shape.Params["width"].Value != nil {
		t.Fatal("cavity space with an unknown width expected")
	}
	keys := issueKeys(st2.Validation[id], SevCritical)
	for _, want := range []string{"wall.condition.unverified", "masonry.cavity.drainage", "masonry.cavity.width-unknown"} {
		if !keys[want] {
			t.Fatalf("cavity branch missing %s: %v", want, keys)
		}
	}
	ir := compile(t, st2, id)
	if ir.Facts.CavityWidth != 50 {
		t.Fatalf("placeholder cavity width %v", ir.Facts.CavityWidth)
	}
	tw, weeps, dams := NewID(KindComponent), NewID(KindComponent), NewID(KindComponent)
	st3 := mustExec(t, s, st2, id, map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-through-wall-flashing",
		"newComponentIds": map[string]string{"through-wall": tw, "weeps": weeps, "end-dams": dams}})
	a = st3.Assemblies[id]
	if a.firstOf(TypeCounterflashing).Applicability != "inapplicable" || a.firstOf(TypeThroughWall) == nil {
		t.Fatal("the tray replaces the counterflashing role")
	}
	keys = issueKeys(st3.Validation[id], SevCritical)
	if keys["masonry.cavity.drainage"] || keys["masonry.cavity.component-missing"] {
		t.Fatalf("cavity drainage now addressed: %v", keys)
	}
	ir = compile(t, st3, id)
	var roof, masonry, iface int
	for _, o := range ir.Overlays {
		switch o.Network {
		case "roof":
			roof++
		case "masonry":
			masonry++
		case "interface":
			iface++
		}
	}
	if roof == 0 || masonry == 0 || iface != 1 {
		t.Fatalf("roof and masonry drainage must be separate networks with one explicit interface: roof=%d masonry=%d interface=%d", roof, masonry, iface)
	}
	parts := map[string]IRPart{}
	for _, p := range ir.Parts {
		parts[p.Component] = p
	}
	if len(parts[weeps].Solids) == 0 || len(parts[dams].Solids) != 2 || !parts[cav].Void {
		t.Fatal("weeps, two end dams and a void cavity expected")
	}
	if got := len(parts[a.byRole("masonry:outer").ID].Solids); got < 3 {
		t.Fatalf("the outer wythe splits around the tray and weep course: %d solids", got)
	}
	// a reglet on a cavity wall is inapplicable
	st4 := mustExec(t, s, st3, id, map[string]any{"op": "SetJunctionStrategy", "orientation": "headwall", "strategy": "apron-reglet-counterflashing"})
	keys = issueKeys(st4.Validation[id], SevCritical)
	if !keys["junction.strategy.inapplicable"] || !keys["masonry.cavity.drainage"] || st4.Assemblies[id].Applicability.Status != "inapplicable" {
		t.Fatalf("reglet on a cavity wall: %v %+v", keys, st4.Assemblies[id].Applicability)
	}
	// solid/bonded: reglet is conditional, cutting needs review, no cavity issues
	st5 := mustExec(t, s, st4, id, map[string]any{"op": "SetWallCondition", "value": "solid-bonded", "provenance": "user-assumption"})
	keys = issueKeys(st5.Validation[id], SevCritical)
	if !keys["masonry.reglet.cutting"] || keys["masonry.cavity.drainage"] || keys["junction.strategy.inapplicable"] {
		t.Fatalf("solid branch: %v", keys)
	}
	if st5.Assemblies[id].Applicability.Status != "conditional" {
		t.Fatalf("an assumed solid wall leaves the reglet alternative conditional: %+v", st5.Assemblies[id].Applicability)
	}
	if st5.Assemblies[id].firstOf(TypeCavitySpace).Applicability != "inapplicable" {
		t.Fatal("the cavity space is switched off, not deleted")
	}
	ir = compile(t, st5, id)
	outerSolids := 0
	for _, p := range ir.Parts {
		if p.Component == st5.Assemblies[id].byRole("masonry:outer").ID {
			outerSolids = len(p.Solids)
		}
	}
	if outerSolids != 3 {
		t.Fatalf("the reglet groove splits the outer wythe into 3 solids, got %d", outerSolids)
	}
}

// Sidewall: the headwall apron is switched off, never rotated; a sidewall
// flashing is created with the caller's id; step flashing and the stepped
// cavity tray are refused as not modelled for corrugated metal.
func TestConstructionValidationSidewallBranch(t *testing.T) {
	s, st, id := templateProblem(t)
	for _, strat := range []string{"step-flashing", "sidewall-through-wall-flashing"} {
		if _, _, err := exec(t, s, st, id, OwnerActor(), map[string]any{"op": "SetJunctionStrategy", "orientation": "sidewall", "strategy": strat,
			"newComponentIds": map[string]string{"sidewall-flashing": NewID(KindComponent)}}); StatusOf(err) != 422 {
			t.Fatalf("%s must be refused: %v", strat, err)
		}
	}
	if _, _, err := exec(t, s, st, id, OwnerActor(), map[string]any{"op": "SetJunctionStrategy", "orientation": "sidewall", "strategy": "apron-surface-counterflashing"}); StatusOf(err) != 422 {
		t.Fatalf("a headwall strategy at a sidewall: %v", err)
	}
	side := NewID(KindComponent)
	st2 := mustExec(t, s, st, id, map[string]any{"op": "SetJunctionStrategy", "orientation": "sidewall", "strategy": "sidewall-surface-counterflashing",
		"newComponentIds": map[string]string{"sidewall-flashing": side}})
	a := st2.Assemblies[id]
	if a.firstOf(TypeApronFlashing).Applicability != "inapplicable" || a.firstOf(TypeSidewallFlash).ID != side || a.Junction.Type != "roof-sidewall" {
		t.Fatal("sidewall parts")
	}
	ir := compile(t, st2, id)
	for _, p := range ir.Parts {
		if p.Type == TypeApronFlashing {
			t.Fatal("the headwall apron must not be drawn at a sidewall")
		}
	}
	// the roof now falls along +X (down the wall), not away from it
	ins := a.firstOf(TypeInsulation).ID
	for _, p := range ir.Parts {
		if p.Component != ins {
			continue
		}
		pos := p.Solids[0].Positions
		zAt := func(x float64) float64 {
			best := math.Inf(-1)
			for i := 0; i+2 < len(pos); i += 3 {
				if math.Abs(pos[i]-x) < 1e-6 {
					best = math.Max(best, pos[i+2])
				}
			}
			return best
		}
		if drop := zAt(0) - zAt(2400); math.Abs(drop-2400*math.Tan(10*math.Pi/180)) > 0.01 {
			t.Fatalf("sidewall roof drop along the wall %.3f", drop)
		}
	}
	if !issueKeys(st2.Validation[id], SevInfo)["geometry.part-inapplicable"] || a.firstOf(TypeProfileClosure).Applicability != "inapplicable" {
		t.Fatal("the headwall closure is switched off (and reported) at a sidewall")
	}
	for _, p := range ir.Parts {
		if p.Type == TypeProfileClosure && len(p.Solids) > 0 {
			t.Fatal("a switched-off part has no geometry")
		}
	}
}

// Blocking geometry is refused with the prior head intact: a reverse lap,
// a layer gap, a disconnected part, a host cycle, unsupported transforms,
// tombstone reuse, and a triangle budget overrun.
func TestConstructionValidationBlockingRefusals(t *testing.T) {
	s, st, id := templateProblem(t)
	a := st.Assemblies[id]
	n := roofNormal(a)
	apron, ins, sealant, battens := a.firstOf(TypeApronFlashing).ID, a.firstOf(TypeInsulation).ID, a.firstOf(TypeSealant).ID, a.firstOf(TypeBattenArray).ID
	gen := st.Head.Generation
	refuse := func(name string, want int, ops ...map[string]any) {
		t.Helper()
		_, _, err := exec(t, s, st, id, OwnerActor(), ops...)
		if StatusOf(err) != want {
			t.Fatalf("%s: want %d, got %v", name, want, err)
		}
		cur, _ := s.Load(fixtureProperty, st.Problem.ID)
		if cur.Head.Generation != gen {
			t.Fatalf("%s moved the head", name)
		}
	}
	down := []float64{-n[0] * 10, -n[1] * 10, -n[2] * 10}
	refuse("reverse lap", 422, map[string]any{"op": "SetTransform", "componentId": apron, "translation": down, "unit": "mm"})
	up := []float64{n[0] * 5, n[1] * 5, n[2] * 5}
	refuse("layer gap", 422, map[string]any{"op": "SetTransform", "componentId": ins, "translation": up, "unit": "mm"})
	refuse("layer in-plane move", 422, map[string]any{"op": "SetTransform", "componentId": ins, "translation": []float64{50, 0, 0}, "unit": "mm"})
	refuse("disconnected", 422, map[string]any{"op": "SetTransform", "componentId": sealant, "translation": []float64{0, 300, 0}, "unit": "mm"})
	refuse("array move", 422, map[string]any{"op": "SetTransform", "componentId": battens, "translation": []float64{0, 0, 10}, "unit": "mm"})
	x, y := NewID(KindComponent), NewID(KindComponent)
	mk := func(id, host string) map[string]any {
		return map[string]any{"op": "AddComponent", "component": map[string]any{"id": id, "type": TypeSealant, "role": "seal:test", "name": "cycle test",
			"hostId": host, "shape": map[string]any{"kind": "sealant-bead", "params": map[string]any{"size": map[string]any{"value": 10, "unit": "mm", "state": "assumed", "provenance": "user-assumption"}}},
			"transform": map[string]any{"translation": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}}, "attachments": []string{}, "applicability": "applicable"}}
	}
	refuse("host cycle", 422, mk(x, y), mk(y, x))
	// remove (detach) then try to reuse the tombstoned id
	st2 := mustExec(t, s, st, id, map[string]any{"op": "RemoveComponent", "componentId": sealant, "dependents": "detach"})
	gen = st2.Head.Generation
	st = st2
	refuse("tombstone reuse", 422, mk(sealant, ""))
	// triangle budget: a very large detail with dense fasteners
	refuse("triangle budget", 422,
		map[string]any{"op": "SetDimension", "parameter": "widthAlongWall", "value": 6000, "unit": "mm"},
		map[string]any{"op": "SetDimension", "parameter": "depthFromWall", "value": 6000, "unit": "mm"},
		map[string]any{"op": "SetDimension", "componentId": battens, "dimension": "spacing", "value": 100, "unit": "mm"},
		map[string]any{"op": "SetDimension", "componentId": a.byRole("attachment:sheet-to-batten").ID, "dimension": "every", "value": 1, "unit": "count"})
}

// Removing a part others depend on is refused unless dependents are detached
// explicitly; removing the exposed rafters raises the timber requirement.
func TestConstructionCommandsRemoveDependents(t *testing.T) {
	s, st, id := templateProblem(t)
	raf := st.Assemblies[id].firstOf(TypeRafterArray).ID
	_, _, err := exec(t, s, st, id, OwnerActor(), map[string]any{"op": "RemoveComponent", "componentId": raf})
	var e *Error
	if !errors.As(err, &e) || e.Status != 409 || len(e.Problems) == 0 {
		t.Fatalf("remove with dependents: %v", err)
	}
	st2 := mustExec(t, s, st, id, map[string]any{"op": "RemoveComponent", "componentId": raf, "dependents": "detach"})
	a := st2.Assemblies[id]
	if c, _ := a.component(raf); c != nil || len(a.Tombstones) != 1 || a.Tombstones[0].ID != raf {
		t.Fatal("rafters tombstoned")
	}
	keys := issueKeys(st2.Validation[id], SevCritical)
	if !keys["timber.exposure"] || !keys["attachment.host-missing"] {
		t.Fatalf("removing the exposed rafters must raise timber and attachment issues: %v", keys)
	}
}

// Variants copy with an explicit derivedFrom pin and retained ids; restore
// makes an earlier revision current as a NEW revision; a token from another
// assembly is refused; acknowledgements are owner-only and not copied.
func TestConstructionCommandsVariantRestoreAcknowledge(t *testing.T) {
	s, st, id := templateProblem(t)
	ins := st.Assemblies[id].firstOf(TypeInsulation).ID
	rev100 := st.Revision("assembly:" + id)
	st2 := mustExec(t, s, st, id, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"})
	st3 := mustExec(t, s, st2, id, map[string]any{"op": "AcknowledgeIssue", "ruleKey": "roof.minimum-pitch", "target": st2.Assemblies[id].firstOf(TypeCorrugatedSheet).ID, "reason": "will check against the sourced detail"})
	acked := false
	for _, is := range st3.Validation[id].Issues {
		acked = acked || (is.RuleKey == "roof.minimum-pitch" && is.Status == "acknowledged" && is.Severity == SevAdvisory)
	}
	if !acked {
		t.Fatal("acknowledged issue stays visible as acknowledged")
	}
	if _, _, err := exec(t, s, st3, id, AgentActor("alfred", "cap", ""), map[string]any{"op": "AcknowledgeIssue", "ruleKey": "roof.minimum-pitch", "target": "x", "reason": "agent"}); StatusOf(err) != 403 {
		t.Fatalf("agent acknowledgement: %v", err)
	}
	vid := NewID(KindAssembly)
	st4 := mustExec(t, s, st3, id, map[string]any{"op": "CreateVariant", "newAssemblyId": vid, "name": "Cavity-wall drainage (conditional)"})
	v := st4.Assemblies[vid]
	if v.DerivedFrom == nil || v.DerivedFrom.ID != id || v.DerivedFrom.Revision != st3.Revision("assembly:"+id) || len(v.IssueStates) != 0 {
		t.Fatalf("variant pin %+v", v.DerivedFrom)
	}
	if v.Components[3].ID != st3.Assemblies[id].Components[3].ID || len(st4.Problem.Alternatives) != 2 || st4.Problem.Lifecycle != LifecycleAlternatives {
		t.Fatal("variant keeps ids within the problem and is listed")
	}
	st5 := mustExec(t, s, st4, id, map[string]any{"op": "RestoreRevision", "revision": rev100})
	a := st5.Assemblies[id]
	if *a.firstOf(TypeInsulation).Shape.Params["thickness"].Value != 100 || a.RevisionNumber != st4.Assemblies[id].RevisionNumber+1 || a.ParentRevision != st4.Revision("assembly:"+id) {
		t.Fatalf("restore must be a new revision with the old content: rev %d parent %s", a.RevisionNumber, a.ParentRevision)
	}
	if _, _, err := exec(t, s, st5, id, OwnerActor(), map[string]any{"op": "RestoreRevision", "revision": st5.Revision("assembly:" + vid)}); StatusOf(err) != 404 {
		t.Fatalf("a token from another assembly: %v", err)
	}
	hist, err := s.History(fixtureProperty, st.Problem.ID, 0)
	if err != nil || len(hist) != int(st5.Head.Generation) {
		t.Fatalf("history is append-only: %d entries for generation %d (%v)", len(hist), st5.Head.Generation, err)
	}
}

// An agent edits drafts through the same reducer; it cannot select the
// working variant or touch owner-only problem facts.
func TestConstructionCommandsAgentBounds(t *testing.T) {
	s, st, id := templateProblem(t)
	agent := AgentActor("zeck", "cap-agent", "run-x")
	st2, rc, err := exec(t, s, st, id, agent, map[string]any{"op": "SetDimension", "componentId": st.Assemblies[id].firstOf(TypeInsulation).ID, "dimension": "thickness", "value": 120, "unit": "mm"})
	if err != nil || rc.Actor.Principal != "agent:zeck" || st2.Assemblies[id].Actor.Kind != ActorAgent {
		t.Fatalf("agent draft edit: %v %+v", err, rc)
	}
	for _, op := range []map[string]any{{"op": "SelectVariant", "assemblyId": id}, {"op": "SetProblemText", "title": "agent title"}} {
		if _, _, err := exec(t, s, st2, "", agent, op); StatusOf(err) != 403 {
			t.Fatalf("agent %v: %v", op["op"], err)
		}
	}
}
