package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"manifest/construction"
	"manifest/graph"
)

// Owner sources and passages through the typed command routes: a retained
// document's quote is verified against its page; a URL is metadata only and
// never fetched; route families refuse foreign operations; the evidence
// graph is served as validated edges with a clickable path.
func TestConstructionSourcesEvidenceAndGraph(t *testing.T) {
	f := constructionFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-src-0001")
	doc := []byte("SYNTHETIC FIXTURE owner note.\fPage 2: the apron upstand is covered by a counterflashing (fictional).")
	v = f.upload(t, fixtureBase, id, viewRev(v, "problem"), "upload-doc-01", "note.txt", doc, "document")
	in := viewProblem(v)["inputs"].([]any)[0].(map[string]any)
	src := construction.NewID("src")
	cmd := func(path string, asmID string, ops ...map[string]any) cResp {
		body := map[string]any{"schemaVersion": 1, "requestId": cReqID("ev"), "problemId": id, "operations": ops}
		if asmID != "" {
			cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
			body["assemblyId"], body["expectedAssemblyRevision"] = asmID, viewRev(cur, "assembly:"+asmID)
		}
		return f.do(t, "POST", fixtureBase+"/problems/"+id+path, body)
	}
	r := cmd("/sources", "", map[string]any{"op": "AddSource", "id": src, "title": "Owner note (synthetic)", "class": "owner-document", "access": "open",
		"input": map[string]any{"id": in["artifactId"], "revision": in["revision"]}})
	if r.Code != 200 {
		t.Fatalf("add source %d %s", r.Code, r.Body)
	}
	evd, clm := construction.NewID("evd"), construction.NewID("clm")
	if r := cmd("/evidence", "", map[string]any{"op": "AddSource", "id": construction.NewID("src"), "title": "x", "class": "secondary"}); r.Code != 422 {
		t.Fatalf("the evidence route refuses source operations: %d", r.Code)
	}
	r = cmd("/evidence", "", map[string]any{"op": "AddEvidence", "id": evd, "claimId": clm, "sourceId": src, "page": 2,
		"quote": "the apron upstand is covered by a counterflashing", "claim": "The apron upstand is covered by a counterflashing (owner note).", "relation": "supports", "confidence": 0.5})
	if r.Code != 200 {
		t.Fatalf("add evidence %d %s", r.Code, r.Body)
	}
	if r := cmd("/evidence", "", map[string]any{"op": "AddEvidence", "id": construction.NewID("evd"), "claimId": construction.NewID("clm"), "sourceId": src, "page": 2,
		"quote": "the apron upstand is 900 mm", "claim": "made up", "relation": "supports", "confidence": 0.5}); r.Code != 422 {
		t.Fatalf("a quote absent from the page is refused: %d", r.Code)
	}
	var hits atomic.Int32
	ext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer ext.Close()
	if r := cmd("/sources", "", map[string]any{"op": "AddSource", "id": construction.NewID("src"), "title": "Manufacturer page", "class": "manufacturer", "url": ext.URL + "/sheet"}); r.Code != 200 {
		t.Fatalf("url source %d %s", r.Code, r.Body)
	}
	if hits.Load() != 0 {
		t.Fatal("a source URL is never fetched")
	}
	cur := f.do(t, "GET", fixtureBase+"/problems/"+id, nil).json(t)
	jct := cur["assemblies"].(map[string]any)[asm].(map[string]any)["junction"].(map[string]any)["id"].(string)
	if r := cmd("/evidence", asm, map[string]any{"op": "LinkEvidence", "target": jct, "evidenceId": evd, "relation": "supports"}); r.Code != 200 {
		t.Fatalf("link %d %s", r.Code, r.Body)
	}
	g := f.do(t, "GET", fixtureBase+"/problems/"+id+"/evidence", nil).json(t)
	e := g["evidence"].(map[string]any)
	if len(e["sources"].([]any)) != 2 || len(e["evidence"].([]any)) != 1 || e["evidence"].([]any)[0].(map[string]any)["verification"] != "verified" {
		t.Fatalf("evidence snapshot %v", e)
	}
	p := f.do(t, "GET", fixtureBase+"/problems/"+id+"/evidence/paths?assembly="+asm+"&target="+jct, nil).json(t)
	paths := p["paths"].([]any)
	if len(paths) != 1 || len(paths[0].([]any)) != 5 {
		t.Fatalf("one five-node path: %v", paths)
	}
	gr := f.do(t, "GET", fixtureBase+"/problems/"+id+"/graph?assembly="+asm, nil).json(t)
	for _, e := range gr["edges"].([]any) {
		em := e.(map[string]any)
		from, to := em["from"].(map[string]any), em["to"].(map[string]any)
		edge := graph.Edge{From: graph.R(from["kind"].(string), from["id"].(string)), To: graph.R(to["kind"].(string), to["id"].(string)), Kind: em["kind"].(string),
			Basis: em["basis"].(string), Source: em["source"].(string)}
		if err := graph.Validate(edge, construction.ConstructionVocabulary()); err != nil {
			t.Fatalf("graph edge invalid: %v", err)
		}
	}
	if len(gr["nodes"].([]any)) < 5 {
		t.Fatalf("graph nodes %v", gr["nodes"])
	}
	// another property cannot see this evidence
	if r := f.do(t, "GET", "/api/properties/fixture-second-house/construction/problems/"+id+"/evidence", nil); r.Code != 404 {
		t.Fatalf("cross-project evidence %d", r.Code)
	}
	if !strings.Contains(string(f.do(t, "GET", fixtureBase+"/problems/"+id+"/sources", nil).Body), "URL recorded as metadata only") {
		t.Fatal("the URL source says it was not fetched")
	}
	f.assertSourcesUntouched(t)
}
