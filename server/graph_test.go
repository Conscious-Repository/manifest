package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/graph"
	"manifest/ledger"
)

// P2 graph over the server: derived edges from the task lines and the
// artifact registry, stored claims through the API with their ledger event,
// the validator at the HTTP boundary, and the query surface.

func graphFixture(t *testing.T) (*Server, string, *ledger.Store, *graph.Store) {
	t.Helper()
	srv, vault, led := artifactFixture(t)
	write := func(abs string, data []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, data, 0o644)
	}
	gs := graph.NewStore(vault, "system/graph", write)
	if err := gs.Ensure(); err != nil {
		t.Fatal(err)
	}
	srv.UseGraph(gs)
	return srv, vault, led, gs
}

func TestGraphDerivedEdgesFromBindings(t *testing.T) {
	srv, _, _, _ := graphFixture(t)
	const personal, prop = "inbox/loose-personal-thing", "prop:761-maple/rough-in/rough-electrical"
	// a cross-file dependency + an output binding on the task line
	if code, r := artifactsDo(t, srv, "POST", "/api/tasks/depends", `{"id":"`+personal+`","depends":["`+prop+`"]}`); code != 200 {
		t.Fatalf("depends: %d %+v", code, r)
	}
	if code, r := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"`+personal+`","outputs":["1f2e3d4c5b6a7980"]}`); code != 200 {
		t.Fatalf("outputs: %d %+v", code, r)
	}
	// an artifact whose provenance names the property task
	if _, err := srv.artifactReg.Put(artifacts.Put{Kind: "brief", Title: "rough-in brief", Content: []byte("hi"), At: time.Now(),
		Provenance: artifacts.Provenance{Source: "run", Task: prop, Run: "r1"}}); err != nil {
		t.Fatal(err)
	}

	code, r := artifactsDo(t, srv, "GET", "/api/graph/neighbors?ref=task:"+personal, "")
	if code != 200 {
		t.Fatalf("neighbors: %d %+v", code, r)
	}
	var got []string
	for _, h := range r["neighbors"].([]any) {
		hop := h.(map[string]any)
		e := hop["edge"].(map[string]any)
		got = append(got, hop["direction"].(string)+" "+e["kind"].(string)+" "+hop["node"].(map[string]any)["kind"].(string)+":"+hop["node"].(map[string]any)["id"].(string)+" derived="+boolStr(e["derived"].(bool)))
	}
	want := []string{
		"out produced artifact:1f2e3d4c5b6a7980 derived=true",
		"out depends_on task:" + prop + " derived=true",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("derived neighbors:\n got %v\nwant %v", got, want)
	}
	if deg := r["degree"].(map[string]any); deg["out"].(float64) != 2 || deg["in"].(float64) != 0 {
		t.Fatalf("degree: %+v", deg)
	}
	// the reverse edge: the property task's downstream is the personal task
	code, r = artifactsDo(t, srv, "GET", "/api/graph/deps?ref=task:"+prop+"&dir=down", "")
	if code != 200 || r["count"].(float64) != 1 {
		t.Fatalf("downstream: %d %+v", code, r)
	}
	if reach := r["reach"].([]any)[0].(map[string]any); reach["node"].(map[string]any)["id"] != personal || reach["depth"].(float64) != 1 {
		t.Fatalf("downstream reach: %+v", reach)
	}
	// the artifact registry's provenance is a produced edge
	code, r = artifactsDo(t, srv, "GET", "/api/graph/edges?ref=task:"+prop+"&kind=produced", "")
	if code != 200 || r["count"].(float64) != 1 {
		t.Fatalf("provenance edge: %d %+v", code, r)
	}
	if e := r["edges"].([]any)[0].(map[string]any); e["source"] != "artifact" || e["derived"] != true || e["to"].(map[string]any)["kind"] != "artifact" {
		t.Fatalf("provenance edge: %+v", e)
	}
	// a path across the derived edges: artifact ← personal → prop → its brief
	code, r = artifactsDo(t, srv, "GET", "/api/graph/paths?from=artifact:1f2e3d4c5b6a7980&to=task:"+prop, "")
	if code != 200 || r["count"].(float64) != 1 {
		t.Fatalf("paths: %d %+v", code, r)
	}
	if p := r["paths"].([]any)[0].(map[string]any); p["hops"].(float64) != 2 || p["inferred"] != false ||
		p["path"] != "artifact:1f2e3d4c5b6a7980 > task:"+personal+" > task:"+prop {
		t.Fatalf("path: %+v", p)
	}
	// who bridges the artifact and the property task: the personal task
	code, r = artifactsDo(t, srv, "GET", "/api/graph/bridges?a=artifact:1f2e3d4c5b6a7980&b=task:"+prop, "")
	if code != 200 || r["count"].(float64) != 1 || r["bridges"].([]any)[0].(map[string]any)["node"].(map[string]any)["id"] != personal {
		t.Fatalf("bridges: %d %+v", code, r)
	}
	// nothing was written to the graph file for any of this
	code, r = artifactsDo(t, srv, "GET", "/api/graph", "")
	if code != 200 || r["stored"].(float64) != 0 || r["derived"].(float64) != 3 || r["configured"] != true {
		t.Fatalf("summary: %d %+v", code, r)
	}
}

func TestGraphStoredClaimsValidateAndLedger(t *testing.T) {
	srv, vault, led, gs := graphFixture(t)
	const task = "inbox/loose-personal-thing"
	// no basis → 400, nothing written, nothing ledgered
	code, r := artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:`+task+`","to":"decision:aion-bl/pick-the-vendor","kind":"depends_on","source":"owner"}`)
	if code != 400 || !strings.Contains(r["error"].(string), "needs a basis") {
		t.Fatalf("basis-less claim: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:`+task+`","to":"idea:x","kind":"depends_on","basis":"b"}`); code != 400 || !strings.Contains(r["error"].(string), "entity kind") {
		t.Fatalf("unknown entity kind: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:`+task+`","to":"decision:d","kind":"friend","basis":"b"}`); code != 400 || !strings.Contains(r["error"].(string), "closed set") {
		t.Fatalf("unknown edge kind: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"`+task+`","to":"decision:d","kind":"depends_on","basis":"b"}`); code != 400 {
		t.Fatalf("kindless ref: %d %+v", code, r)
	}
	if len(gs.LoadEdges().Edges()) != 0 || len(allLedgerEntries(t, led)) != 0 {
		t.Fatal("a refused claim must leave no trace")
	}

	// a task depends on a decision; a heuristic supports it (inferred)
	code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:`+task+`","to":"decision:aion-bl/pick-the-vendor","kind":"depends_on","basis":"the vendor call gates the order","confidence":"0.9","evidence":"thread:t1","actor":"agent:hermes"}`)
	if code != 200 || r["added"] != true {
		t.Fatalf("add: %d %+v", code, r)
	}
	e := r["edge"].(map[string]any)
	if e["source"] != "owner" || e["inferred"] != false || e["observed"] == "" {
		t.Fatalf("defaults: %+v", e)
	}
	code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"heuristic:h1a2b3c4","to":"decision:aion-bl/pick-the-vendor","kind":"supports","basis":"solve the read problem first","inferred":true,"source":"ooda"}`)
	if code != 200 || r["added"] != true {
		t.Fatalf("add 2: %d %+v", code, r)
	}
	// replay: not a second claim, not a second ledger line
	if code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:`+task+`","to":"decision:aion-bl/pick-the-vendor","kind":"depends_on","basis":"restated"}`); code != 200 || r["added"] != false {
		t.Fatalf("replay: %d %+v", code, r)
	}
	// the file is the truth, byte-shaped like the recruiting network rows
	raw, err := os.ReadFile(filepath.Join(vault, "system", "graph", "edges.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "- [from:: task:"+task+"] [to:: decision:aion-bl/pick-the-vendor] [kind:: depends_on] [basis:: the vendor call gates the order] [confidence:: 0.9] [inferred:: false] [source:: owner] [evidence:: thread:t1] [observed:: ") ||
		!strings.Contains(string(raw), "[kind:: supports] [basis:: solve the read problem first] [inferred:: true] [source:: ooda]") ||
		strings.Contains(string(raw), "restated") {
		t.Fatalf("edges.md:\n%s", raw)
	}
	// the ledger: one line per NEW claim, under the from-entity, the task as related ref
	es := allLedgerEntries(t, led)
	if len(es) != 2 {
		t.Fatalf("ledger: %+v", es)
	}
	if es[0].Kind != "graph.edge.added" || es[0].Source != "graph" || es[0].Actor != "agent:hermes" ||
		es[0].Object != (ledger.Object{Kind: "task", ID: task}) || es[0].Task != task ||
		es[0].Meta["to"] != "decision:aion-bl/pick-the-vendor" || es[0].Meta["edgeKind"] != "depends_on" || es[0].Meta["confidence"] != "0.9" {
		t.Fatalf("edge event: %+v", es[0])
	}
	if es[1].Object != (ledger.Object{Kind: "heuristic", ID: "h1a2b3c4"}) || es[1].Task != "" || es[1].Meta["inferred"] != true {
		t.Fatalf("second event: %+v", es[1])
	}
	// … so the task's history carries the claim, and the decision's does too through the object query
	code, r = artifactsDo(t, srv, "GET", "/api/ledger/history?object="+task+"&objectKind=task", "")
	if code != 200 || len(r["entries"].([]any)) != 1 {
		t.Fatalf("task history: %d %+v", code, r)
	}

	// queries see the stored claims (flagged derived=false) and inference
	code, r = artifactsDo(t, srv, "GET", "/api/graph/neighbors?ref=decision:aion-bl/pick-the-vendor&dir=in", "")
	if code != 200 || r["count"].(float64) != 2 {
		t.Fatalf("decision neighbors: %d %+v", code, r)
	}
	for _, h := range r["neighbors"].([]any) {
		if h.(map[string]any)["edge"].(map[string]any)["derived"] != false {
			t.Fatalf("stored claims must not read as derived: %+v", h)
		}
	}
	if code, r = artifactsDo(t, srv, "GET", "/api/graph/neighbors?ref=decision:aion-bl/pick-the-vendor&facts=1", ""); code != 200 || r["count"].(float64) != 1 {
		t.Fatalf("facts only: %d %+v", code, r)
	}
	// what depends on the decision (reverse edge) — the task
	if code, r = artifactsDo(t, srv, "GET", "/api/graph/deps?ref=decision:aion-bl/pick-the-vendor&dir=down", ""); code != 200 || r["count"].(float64) != 1 ||
		r["reach"].([]any)[0].(map[string]any)["node"].(map[string]any)["id"] != task {
		t.Fatalf("downstream of the decision: %d %+v", code, r)
	}
	// the cross-domain path: heuristic → decision ← task, marked inferred
	code, r = artifactsDo(t, srv, "GET", "/api/graph/paths?from=heuristic:h1a2b3c4&to=task:"+task, "")
	if code != 200 || r["count"].(float64) != 1 || r["paths"].([]any)[0].(map[string]any)["inferred"] != true {
		t.Fatalf("triad path: %d %+v", code, r)
	}

	// entities: registered once, ledgered under themselves
	code, r = artifactsDo(t, srv, "POST", "/api/graph/entities", `{"id":"aion-bl/pick-the-vendor","kind":"decision","title":"Pick the vendor","source":"aion"}`)
	if code != 200 || r["added"] != true {
		t.Fatalf("entity: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "POST", "/api/graph/entities", `{"id":"aion-bl/pick-the-vendor","kind":"decision"}`); code != 200 || r["added"] != false {
		t.Fatalf("entity replay: %d %+v", code, r)
	}
	if code, _ = artifactsDo(t, srv, "POST", "/api/graph/entities", `{"id":"x","kind":"idea"}`); code != 400 {
		t.Fatalf("entity kind outside the set: %d", code)
	}
	es = allLedgerEntries(t, led)
	if len(es) != 3 || es[2].Kind != "graph.entity.added" || es[2].Object != (ledger.Object{Kind: "decision", ID: "aion-bl/pick-the-vendor"}) {
		t.Fatalf("entity event: %+v", es)
	}
	if code, r = artifactsDo(t, srv, "GET", "/api/graph", ""); code != 200 || r["stored"].(float64) != 2 || len(r["entities"].([]any)) != 1 {
		t.Fatalf("summary: %d %+v", code, r)
	}
	// vocabulary on the wire: closed set, the triad first
	if v := r["vocabulary"].(map[string]any); strs(v["entityKinds"])[0] != "task" || strs(v["entityKinds"])[1] != "decision" || !containsStr(strs(v["edgeKinds"]), "coauthor") {
		t.Fatalf("vocabulary: %+v", v)
	}
}

func TestGraphWithoutStoreReadsDerivedAndRefusesWrites(t *testing.T) {
	srv, _, _ := artifactFixture(t)
	if code, r := artifactsDo(t, srv, "GET", "/api/graph", ""); code != 200 || r["configured"] != false {
		t.Fatalf("summary: %d %+v", code, r)
	}
	if code, _ := artifactsDo(t, srv, "GET", "/api/graph/neighbors?ref=task:inbox/loose-personal-thing", ""); code != 200 {
		t.Fatalf("derived read: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:a","to":"task:b","kind":"depends_on","basis":"b"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("write without a store: %d", code)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
