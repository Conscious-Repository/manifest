package server

import (
	"testing"

	"manifest/construction"
)

func (f *constructionFix) createTemplate(t *testing.T, base, requestID string) (map[string]any, string, string) {
	t.Helper()
	r := f.do(t, "POST", base+"/problems", map[string]any{"schemaVersion": 1, "requestId": requestID,
		"title": "Corrugated roof to masonry wall", "template": construction.TemplateRoofMasonry, "scope": map[string]any{"workId": "roof"}})
	if r.Code != 200 {
		t.Fatalf("create %d %s", r.Code, r.Body)
	}
	v := r.json(t)["view"].(map[string]any)
	p := viewProblem(v)
	return v, p["id"].(string), p["alternatives"].([]any)[0].(string)
}

func componentID(t *testing.T, v map[string]any, asmID, typ string) string {
	t.Helper()
	a := v["assemblies"].(map[string]any)[asmID].(map[string]any)
	for _, c := range a["components"].([]any) {
		cm := c.(map[string]any)
		if cm["type"] == typ {
			return cm["id"].(string)
		}
	}
	t.Fatalf("no %s", typ)
	return ""
}

// The draft opens with its compiled geometry and a same-version GLB; a typed
// edit through the assembly route moves both; the previous revision's
// geometry stays retrievable exactly; preview evaluates without writing.
func TestConstructionAssemblyEndpoints(t *testing.T) {
	f := constructionFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-asm-0001")
	base := fixtureBase + "/problems/" + id + "/assemblies/" + asm
	g := f.do(t, "GET", base+"/geometry", nil)
	if g.Code != 200 {
		t.Fatalf("geometry %d %s", g.Code, g.Body)
	}
	gj := g.json(t)
	geo := gj["geometry"].(map[string]any)
	hash0 := geo["hash"].(string)
	if gj["modelHash"] != hash0 || gj["generatorChanged"] != false || gj["assemblyRevision"] != viewRev(v, "assembly:"+asm) {
		t.Fatalf("geometry must be the stored model of the exact revision: %v vs %v", gj["modelHash"], hash0)
	}
	glb := f.do(t, "GET", base+"/glb", nil)
	sum, err := construction.ParseGLB(glb.Body)
	if glb.Code != 200 || err != nil || sum.Extras["geometryHash"] != hash0 || sum.ExternalURIs != 0 || glb.Hdr.Get("Content-Type") != "model/gltf-binary" {
		t.Fatalf("glb %d %v %+v", glb.Code, err, sum)
	}
	val := f.do(t, "GET", base+"/validation", nil).json(t)
	rep := val["report"].(map[string]any)
	if rep["geometryHash"] != hash0 || rep["assemblyRevision"] != viewRev(v, "assembly:"+asm) {
		t.Fatalf("validation report %v", rep)
	}
	ins := componentID(t, v, asm, construction.TypeInsulation)
	cmd := map[string]any{"schemaVersion": 1, "requestId": "cmd-ins-150-a", "problemId": id, "assemblyId": asm,
		"expectedAssemblyRevision": viewRev(v, "assembly:"+asm),
		"operations":               []any{map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"}}}
	// preview first: tentative, nothing written
	pv := f.do(t, "POST", base+"/preview?geometry=1", cmd)
	if pv.Code != 200 {
		t.Fatalf("preview %d %s", pv.Code, pv.Body)
	}
	pr := pv.json(t)["preview"].(map[string]any)
	if pr["tentative"] != true || pr["geometryHash"] == hash0 || pr["blocking"] != false {
		t.Fatalf("preview %v", pr)
	}
	if again := f.do(t, "GET", base+"/geometry", nil).json(t)["geometry"].(map[string]any)["hash"]; again != hash0 {
		t.Fatal("a preview must not change the model")
	}
	r := f.do(t, "POST", base+"/commands", cmd)
	if r.Code != 200 {
		t.Fatalf("commit %d %s", r.Code, r.Body)
	}
	v2 := r.json(t)["view"].(map[string]any)
	g2 := f.do(t, "GET", base+"/geometry", nil).json(t)["geometry"].(map[string]any)
	if g2["hash"] != pr["geometryHash"] {
		t.Fatal("the committed geometry is exactly the previewed geometry")
	}
	old := f.do(t, "GET", base+"/geometry?revision="+viewRev(v, "assembly:"+asm), nil).json(t)["geometry"].(map[string]any)
	if old["hash"] != hash0 {
		t.Fatal("the previous revision's geometry must stay retrievable exactly")
	}
	oldA := f.do(t, "GET", base+"?revision="+viewRev(v, "assembly:"+asm), nil).json(t)
	if oldA["revision"] != viewRev(v, "assembly:"+asm) {
		t.Fatal("exact old revision")
	}
	oldVal := f.do(t, "GET", base+"/validation?revision="+viewRev(v, "assembly:"+asm), nil).json(t)["report"].(map[string]any)
	if oldVal["geometryHash"] != hash0 {
		t.Fatal("the old revision's report")
	}
	// reverse lap preview reports blocking; the commit is refused
	apron := componentID(t, v2, asm, construction.TypeApronFlashing)
	bad := map[string]any{"schemaVersion": 1, "requestId": "cmd-revlap-01", "problemId": id, "assemblyId": asm,
		"expectedAssemblyRevision": viewRev(v2, "assembly:"+asm),
		"operations":               []any{map[string]any{"op": "SetTransform", "componentId": apron, "translation": []float64{0, -2, -10}, "unit": "mm"}}}
	if pr := f.do(t, "POST", base+"/preview", bad).json(t)["preview"].(map[string]any); pr["blocking"] != true {
		t.Fatalf("reverse lap preview %v", pr)
	}
	if r := f.do(t, "POST", base+"/commands", bad); r.Code != 422 {
		t.Fatalf("reverse lap commit %d %s", r.Code, r.Body)
	}
	// URL/body mismatch and cross-project access
	cmd["requestId"] = "cmd-mismatch-1"
	cmd["assemblyId"] = construction.NewID(construction.KindAssembly)
	if r := f.do(t, "POST", base+"/commands", cmd); r.Code != 422 {
		t.Fatalf("assembly mismatch %d", r.Code)
	}
	other := "/api/properties/fixture-second-house/construction/problems/" + id + "/assemblies/" + asm
	for _, p := range []string{other, other + "/geometry", other + "/glb", other + "/validation"} {
		if r := f.do(t, "GET", p, nil); r.Code != 404 {
			t.Fatalf("%s %d", p, r.Code)
		}
	}
	if r := f.do(t, "GET", base+"/geometry?revision="+construction.Token([]byte("nope")), nil); r.Code != 404 {
		t.Fatalf("unknown revision %d", r.Code)
	}
	f.assertSourcesUntouched(t)
}
