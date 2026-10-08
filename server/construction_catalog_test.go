package server

import (
	"net/http"
	"path/filepath"
	"testing"

	"manifest/construction"
)

// researchedFixture runs the fixture research through the API so the
// fixture sources and passages exist for products to cite.
func (f *constructionFix) researchedFixture(t *testing.T, requestID string) (map[string]any, string, string) {
	t.Helper()
	v, id, asm := f.createTemplate(t, fixtureBase, requestID)
	r := f.startRun(t, fixtureBase, id, viewRev(v, "problem"), nil)
	if r.Code != 200 {
		t.Fatalf("run %d %s", r.Code, r.Body)
	}
	done := f.waitRun(t, fixtureBase, id, runOf(r.json(t))["id"].(string))
	if runOf(done)["state"] != "completed" {
		t.Fatalf("research %v", runOf(done)["state"])
	}
	return f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t), id, asm
}

// Materials and products through the typed owner routes; substitution is
// previewed (diff + validation on a copy) before the same SetProduct commit
// pins the product; route families refuse foreign operations; another
// property cannot see the catalog.
func TestConstructionCatalogProductsEndpoints(t *testing.T) {
	f := constructionFixture(t, withFixtureSources)
	v, id, asm := f.researchedFixture(t, "create-cat-0001")
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	st, err := f.srv.construction.store.Load(sub, id)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := construction.CatalogFixtureOps(filepath.Join("..", "construction", "testdata", "roof-wall"), st.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	post := func(path string, asmID string, op map[string]any) cResp {
		body := map[string]any{"schemaVersion": 1, "requestId": cReqID("cat"), "problemId": id, "operations": []map[string]any{op}}
		if asmID != "" {
			cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
			body["assemblyId"], body["expectedAssemblyRevision"] = asmID, viewRev(cur, "assembly:"+asmID)
		}
		return f.do(t, "POST", fixtureBase+"/problems/"+id+path, body)
	}
	for _, k := range []string{"corrugated", "board-120"} {
		if r := post("/products", "", ops[k]); r.Code != 200 {
			t.Fatalf("add product %s: %d %s", k, r.Code, r.Body)
		}
	}
	if r := post("/materials", "", ops["board-mismatch"]); r.Code != 422 {
		t.Fatalf("the materials route refuses product operations: %d", r.Code)
	}
	ins := componentID(t, v, asm, construction.TypeInsulation)
	if r := post("/materials", "", map[string]any{"op": "SetMaterialProperty", "materialId": construction.GenericMaterials()[5].ID, "property": "thermalConductivity",
		"value": 0.022, "unit": "W/(m·K)", "testCondition": "10 °C mean (synthetic owner assumption)", "provenance": "user-assumption"}); r.Code != 200 {
		t.Fatalf("material property %d %s", r.Code, r.Body)
	}
	pr := f.do(t, "GET", fixtureBase+"/problems/"+id+"/products", nil).json(t)
	if len(pr["products"].([]any)) != 2 {
		t.Fatalf("products %v", pr["products"])
	}
	mats := f.do(t, "GET", fixtureBase+"/problems/"+id+"/materials", nil).json(t)
	if len(mats["families"].([]any)) < 13 {
		t.Fatalf("families %v", mats["families"])
	}
	b120 := ops["board-120"]["id"].(string)
	before := viewRev(f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t), "assembly:"+asm)
	sp := f.do(t, "GET", fixtureBase+"/problems/"+id+"/assemblies/"+asm+"/substitution?component="+ins+"&product="+b120, nil)
	if sp.Code != 200 {
		t.Fatalf("substitution preview %d %s", sp.Code, sp.Body)
	}
	spj := sp.json(t)
	subst := spj["substitution"].(map[string]any)
	prev := spj["preview"].(map[string]any)
	if prev["tentative"] != true || prev["blocking"] == true || len(subst["dimensions"].([]any)) == 0 {
		t.Fatalf("preview %v / %v", prev, subst)
	}
	keys := map[string]bool{}
	for _, fd := range prev["findings"].([]any) {
		keys[fd.(map[string]any)["ruleKey"].(string)] = true
	}
	if !keys["structure.fastener.embedment"] || !keys["product.dimension-unverified"] {
		t.Fatalf("the preview re-runs fastener and product checks: %v", keys)
	}
	if viewRev(f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t), "assembly:"+asm) != before {
		t.Fatal("a substitution preview writes nothing")
	}
	r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/assemblies/"+asm+"/commands", map[string]any{"schemaVersion": 1, "requestId": cReqID("sub"), "problemId": id,
		"assemblyId": asm, "expectedAssemblyRevision": before, "operations": []map[string]any{{"op": "SetProduct", "componentId": ins, "productId": b120, "applyDimensions": true}}})
	if r.Code != 200 {
		t.Fatalf("apply substitution %d %s", r.Code, r.Body)
	}
	after := r.json(t)["view"].(map[string]any)
	a := after["assemblies"].(map[string]any)[asm].(map[string]any)
	for _, c := range a["components"].([]any) {
		cm := c.(map[string]any)
		if cm["id"] == ins {
			if cm["shape"].(map[string]any)["params"].(map[string]any)["thickness"].(map[string]any)["value"].(float64) != 120 {
				t.Fatal("the applied substitution set 120 mm on the same component")
			}
		}
	}
	if g := f.do(t, "GET", "/api/properties/fixture-second-house/construction/problems/"+id+"/products", nil); g.Code != 404 {
		t.Fatalf("cross-project catalog %d", g.Code)
	}
	// a non-owner principal is refused on the owner routes
	f.srv.construction.principal = func(*http.Request) (construction.Actor, error) {
		return construction.AgentActor("alfred", "cap", ""), nil
	}
	if r := post("/products", "", ops["board-mismatch"]); r.Code != 403 {
		t.Fatalf("an agent principal on an owner route: %d", r.Code)
	}
	f.srv.construction.principal = nil
	f.assertSourcesUntouched(t)
}
