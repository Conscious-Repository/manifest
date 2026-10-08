package server

import (
	"encoding/json"
	"testing"
	"time"

	"manifest/construction"
)

// A request body is exactly one JSON value (construction.DecodeRequest). A
// body carrying a second document, or trailing bytes after the first —
// including a stray ']' or '}', which a Decoder.More check does not see — is
// refused with 422 before anything happens: no steward run is dispatched, no
// export is retained, no receipt is committed, and the request id is not
// consumed (the same id with a clean body then succeeds).
func TestConstructionConcatenatedBodiesRefused(t *testing.T) {
	f, out := nativeFixture(t)
	v, id, asm := f.createTemplate(t, fixtureBase, "create-trail-01")
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	state := func() (int64, int) {
		st, err := f.srv.construction.store.Load(sub, id)
		if err != nil {
			t.Fatal(err)
		}
		return st.Head.Generation, len(st.Derived.Artifacts)
	}
	gen0, derived0 := state()
	raw := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	rev := viewRev(v, "assembly:"+asm)
	ins := componentID(t, v, asm, construction.TypeInsulation)
	routes := []struct{ name, path, body string }{
		{"steward request", fixtureBase + "/problems/" + id + "/agent/requests",
			raw(map[string]any{"schemaVersion": 1, "requestId": "steward-trail-01", "text": "Increase the insulation to 150 mm.", "assemblyId": asm})},
		{"export", fixtureBase + "/problems/" + id + "/assemblies/" + asm + "/exports",
			raw(map[string]any{"schemaVersion": 1, "requestId": "export-trail-01", "revision": rev, "format": "svg"})},
		{"command", fixtureBase + "/problems/" + id + "/commands",
			raw(map[string]any{"schemaVersion": 1, "requestId": "cmd-trail-0001", "problemId": id, "assemblyId": asm, "expectedAssemblyRevision": rev,
				"operations": []map[string]any{{"op": "SetDimension", "componentId": ins, "dimension": "thickness", "value": 150, "unit": "mm"}}})},
	}
	for _, rt := range routes {
		for _, body := range []string{rt.body + rt.body, rt.body + " " + rt.body, rt.body + "]", rt.body + "}", rt.body + "]" + rt.body,
			rt.body + "}" + rt.body, rt.body + "x", rt.body + "null"} {
			if r := f.do(t, "POST", rt.path, body); r.Code != 422 {
				t.Fatalf("%s with a trailing value (%d bytes): want 422, got %d %s", rt.name, len(body), r.Code, r.Body)
			}
		}
	}
	if gen, derived := state(); gen != gen0 || derived != derived0 {
		t.Fatalf("a refused body committed something: generation %d → %d, derived %d → %d", gen0, gen, derived0, derived)
	}
	if _, ok := f.srv.construction.stewards.get("steward-trail-01"); ok {
		t.Fatal("a refused steward body was recorded")
	}
	if calls := stubCalls(t, out); len(calls) != 0 {
		t.Fatalf("a refused steward body dispatched the agent: %+v", calls)
	}
	// the same request ids with clean bodies succeed: nothing was consumed
	for _, rt := range routes[1:] {
		if r := f.do(t, "POST", rt.path, rt.body); r.Code != 200 {
			t.Fatalf("%s with a clean body: %d %s", rt.name, r.Code, r.Body)
		}
	}
	r := f.do(t, "POST", routes[0].path, routes[0].body)
	if r.Code != 200 {
		t.Fatalf("steward request with a clean body: %d %s", r.Code, r.Body)
	}
	req := r.json(t)["request"].(map[string]any)
	for deadline := time.Now().Add(30 * time.Second); req["state"] == "pending"; {
		if time.Now().After(deadline) {
			t.Fatal("steward request did not finish")
		}
		time.Sleep(50 * time.Millisecond)
		req = f.do(t, "GET", fixtureBase+"/problems/"+id+"/agent/requests/"+req["id"].(string), nil).json(t)["request"].(map[string]any)
	}
	if calls := stubCalls(t, out); len(calls) != 1 {
		t.Fatalf("exactly the clean steward request reached the agent: %+v", calls)
	}
	f.assertSourcesUntouched(t)
}
