package server

import (
	"strings"
	"testing"

	"manifest/construction"
)

func (f *constructionFix) asmCommand(t *testing.T, id, asm string, ops ...map[string]any) cResp {
	t.Helper()
	cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	return f.do(t, "POST", fixtureBase+"/problems/"+id+"/commands", map[string]any{"schemaVersion": 1, "requestId": cReqID("cmd"), "problemId": id,
		"assemblyId": asm, "expectedAssemblyRevision": viewRev(cur, "assembly:"+asm), "operations": ops})
}

func (f *constructionFix) decisionCommand(t *testing.T, id string, op map[string]any) cResp {
	t.Helper()
	return f.do(t, "POST", fixtureBase+"/problems/"+id+"/decisions", map[string]any{"schemaVersion": 1, "requestId": cReqID("dec"), "problemId": id,
		"operations": []map[string]any{op}})
}

// History, comparison and decisions over the real routes: an assembly's
// own revision history with actors, a structured comparison of exact
// revisions, a decision accepted by the owner only for the exact revision,
// staleness after an edit, and the selection pinned to the accepted revision.
func TestConstructionCommandHistoryCompareAndDecisions(t *testing.T) {
	f := constructionFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-cmd-0001")
	ins := componentID(t, v, asm, construction.TypeInsulation)
	r0 := viewRev(v, "assembly:"+asm)
	if r := f.asmCommand(t, id, asm, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"}); r.Code != 200 {
		t.Fatalf("edit %d %s", r.Code, r.Body)
	}
	h := f.do(t, "GET", fixtureBase+"/problems/"+id+"/assemblies/"+asm+"/history", nil).json(t)["history"].([]any)
	if len(h) != 2 || h[0].(map[string]any)["operations"].([]any)[0] != "SetDimension" || h[0].(map[string]any)["actor"].(map[string]any)["kind"] != "owner" {
		t.Fatalf("history %v", h)
	}
	c := f.do(t, "GET", fixtureBase+"/problems/"+id+"/compare?a="+asm+"@"+r0+"&b="+asm, nil)
	if c.Code != 200 {
		t.Fatalf("compare %d %s", c.Code, c.Body)
	}
	cmp := c.json(t)["comparison"].(map[string]any)
	if cmp["same"] == true || len(cmp["components"].([]any)) != 1 || !strings.Contains(c.json(t)["notice"].(string), "not approved for construction") {
		t.Fatalf("comparison %v", cmp)
	}
	if bad := f.do(t, "GET", fixtureBase+"/problems/"+id+"/compare?a="+asm+"@"+strings.Repeat("a", 64)+"&b="+asm, nil); bad.Code != 404 {
		t.Fatalf("a revision outside the chain is refused: %d", bad.Code)
	}
	dec := construction.NewID("dec")
	if r := f.decisionCommand(t, id, map[string]any{"op": "ProposeDecision", "decisionId": dec, "assemblyId": asm, "title": "Use this alternative", "proposal": "Working detail, conditional."}); r.Code != 200 {
		t.Fatalf("propose %d %s", r.Code, r.Body)
	}
	if r := f.decisionCommand(t, id, map[string]any{"op": "SetLifecycle", "lifecycle": "archived"}); r.Code != 422 {
		t.Fatalf("the decisions route refuses other families: %d", r.Code)
	}
	ds := f.do(t, "GET", fixtureBase+"/problems/"+id+"/decisions", nil).json(t)
	d := ds["decisions"].(map[string]any)[dec].(map[string]any)
	cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	if r := f.decisionCommand(t, id, map[string]any{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": viewRev(cur, "decision:"+dec)}); r.Code != 200 {
		t.Fatalf("approve %d %s", r.Code, r.Body)
	}
	cur = f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	sel := viewProblem(cur)["selectedAssembly"].(map[string]any)
	if sel["revision"] != d["assembly"].(map[string]any)["revision"] || viewProblem(cur)["lifecycle"] != "owner-selected" {
		t.Fatalf("selection %v", sel)
	}
	if st := cur["decisionStates"].(map[string]any)[dec].(map[string]any); st["stale"] != false || st["status"] != "accepted-for-project" {
		t.Fatalf("state %v", st)
	}
	f.asmCommand(t, id, asm, map[string]any{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 160, "unit": "mm"})
	cur = f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	if st := cur["decisionStates"].(map[string]any)[dec].(map[string]any); st["stale"] != true {
		t.Fatalf("a significant edit stales the acceptance: %v", st)
	}
	if viewProblem(cur)["selectedAssembly"].(map[string]any)["revision"] != sel["revision"] {
		t.Fatal("the selection stays pinned to the accepted revision")
	}
	f.assertSourcesUntouched(t)
}
