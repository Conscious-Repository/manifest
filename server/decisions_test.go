package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/decisions"
	"manifest/graph"
	"manifest/ledger"
)

// P3 decisions over the server: the note is written through the injected
// writer, the entity lands in the graph, every lifecycle step is a ledger
// line under the decision, the record's refs are derived edges, and the
// backlog's decisions coexist read-only.

func decisionFixture(t *testing.T) (*Server, string, *ledger.Store, *decisions.Store) {
	t.Helper()
	srv, vault, led, _ := graphFixture(t)
	write := func(abs string, data []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, data, 0o644)
	}
	ds := decisions.NewStore(vault, "system/decisions", write)
	srv.UseDecisions(ds)
	return srv, vault, led, ds
}

func TestDecisionLifecycleWritesFileGraphAndLedger(t *testing.T) {
	srv, vault, led, ds := decisionFixture(t)
	const task = "inbox/loose-personal-thing"
	// refused: no title; a bad status; nothing written, nothing ledgered
	if code, r := artifactsDo(t, srv, "POST", "/api/decisions/create", `{"why":"x"}`); code != 400 || !strings.Contains(r["error"].(string), "needs a title") {
		t.Fatalf("no title: %d %+v", code, r)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/decisions/create", `{"title":"x","status":"maybe"}`); code != 400 {
		t.Fatal("bad status")
	}
	if len(ds.List()) != 0 || len(allLedgerEntries(t, led)) != 0 {
		t.Fatal("a refused decision must leave no trace")
	}

	// create: evidence names an artifact and a heuristic; downstream names the task
	code, r := artifactsDo(t, srv, "POST", "/api/decisions/create", `{"title":"Pick the vendor","owner":"BA","why":"the current vendor churns",
		"evidence":[{"ref":"artifact:1f2e3d4c5b6a7980","note":"the brief"},{"ref":"heuristic:h1a2b3c4","note":"read first"},{"ref":"[[vendor call]]"}],
		"alternatives":[{"option":"stay","tradeoff":"churn"}],"expectedOutcome":"churn halves",
		"downstream":[{"ref":"task:`+task+`","note":"the order"}],"sources":["meeting 2026-09-01"],"actor":"agent:hermes"}`)
	if code != 200 || r["created"] != true {
		t.Fatalf("create: %d %+v", code, r)
	}
	d := r["decision"].(map[string]any)
	if d["id"] != "pick-the-vendor" || d["status"] != "open" || d["editable"] != true || d["source"] != "agent:hermes" ||
		d["open"].(map[string]any)["note"] != "system/decisions/pick-the-vendor.md" {
		t.Fatalf("created view: %+v", d)
	}
	// the file is the truth
	raw, err := os.ReadFile(filepath.Join(vault, "system", "decisions", "pick-the-vendor.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "---\ntitle: Pick the vendor\nowner: BA\nstatus: open\ncaptured: ") ||
		!strings.Contains(string(raw), "## why\nthe current vendor churns\n\n## evidence\n- the brief [ref:: artifact:1f2e3d4c5b6a7980]\n- read first [ref:: heuristic:h1a2b3c4]\n- [[vendor call]]\n\n## alternatives\n- stay [tradeoff:: churn]\n") ||
		!strings.Contains(string(raw), "## downstream\n- the order [ref:: task:"+task+"]\n\n## sources\n- [[meeting 2026-09-01]]\n") {
		t.Fatalf("note:\n%s", raw)
	}
	// an open twin is a 400
	if code, r := artifactsDo(t, srv, "POST", "/api/decisions/create", `{"title":"pick the vendor"}`); code != 400 || !strings.Contains(r["error"].(string), "already in the ledger") {
		t.Fatalf("twin: %d %+v", code, r)
	}

	// the graph: registered as an entity (once), and the refs are derived edges
	code, r = artifactsDo(t, srv, "GET", "/api/graph", "")
	if code != 200 || len(r["entities"].([]any)) != 1 {
		t.Fatalf("entities: %d %+v", code, r)
	}
	if e := r["entities"].([]any)[0].(map[string]any); e["kind"] != "decision" || e["id"] != "pick-the-vendor" || e["ref"] != "system/decisions/pick-the-vendor.md" || e["source"] != "decisions" {
		t.Fatalf("entity: %+v", e)
	}
	code, r = artifactsDo(t, srv, "GET", "/api/graph/neighbors?ref=decision:pick-the-vendor", "")
	if code != 200 {
		t.Fatalf("neighbors: %d %+v", code, r)
	}
	var hops []string
	for _, h := range r["neighbors"].([]any) {
		hop := h.(map[string]any)
		e := hop["edge"].(map[string]any)
		hops = append(hops, hop["direction"].(string)+" "+e["kind"].(string)+" "+hop["node"].(map[string]any)["kind"].(string)+":"+hop["node"].(map[string]any)["id"].(string)+" "+e["source"].(string)+" derived="+boolStr(e["derived"].(bool)))
	}
	want := []string{
		"in informs artifact:1f2e3d4c5b6a7980 decisions derived=true",
		"in supports heuristic:h1a2b3c4 decisions derived=true",
		"in depends_on task:" + task + " decisions derived=true",
	}
	if strings.Join(hops, "|") != strings.Join(want, "|") {
		t.Fatalf("derived edges:\n got %v\nwant %v", hops, want)
	}
	// … so "what depends on this decision" is the task, and the wikilink was not an edge
	if code, r = artifactsDo(t, srv, "GET", "/api/graph/deps?ref=decision:pick-the-vendor&dir=down", ""); code != 200 || r["count"].(float64) != 1 ||
		r["reach"].([]any)[0].(map[string]any)["node"].(map[string]any)["id"] != task {
		t.Fatalf("downstream: %d %+v", code, r)
	}
	// a stored claim with the same key wins over the derived one (P2's rule holds)
	if code, r = artifactsDo(t, srv, "POST", "/api/graph/edges", `{"from":"task:`+task+`","to":"decision:pick-the-vendor","kind":"depends_on","basis":"stated"}`); code != 200 || r["added"] != true {
		t.Fatalf("stored claim: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "GET", "/api/graph/edges?ref=decision:pick-the-vendor&kind=depends_on", ""); code != 200 || r["count"].(float64) != 1 || r["edges"].([]any)[0].(map[string]any)["derived"] != false {
		t.Fatalf("stored wins: %d %+v", code, r)
	}

	// update: a no-op writes nothing; an outcome DECIDES; an actual outcome REVISITS
	if code, r = artifactsDo(t, srv, "POST", "/api/decisions/update", `{"id":"pick-the-vendor","why":"the current vendor churns"}`); code != 200 || r["changed"] != false {
		t.Fatalf("no-op: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "POST", "/api/decisions/update", `{"id":"pick-the-vendor","outcome":"beta, 12 months","actor":"owner"}`); code != 200 || r["transition"] != "decided" || r["decision"].(map[string]any)["status"] != "decided" {
		t.Fatalf("decide: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "POST", "/api/decisions/update", `{"id":"pick-the-vendor","actualOutcome":"churn fell 30%"}`); code != 200 || r["transition"] != "revisited" {
		t.Fatalf("revisit: %d %+v", code, r)
	}
	if code, _ = artifactsDo(t, srv, "POST", "/api/decisions/update", `{"id":"nope","why":"x"}`); code != 404 {
		t.Fatalf("unknown id: %d", code)
	}
	if code, _ = artifactsDo(t, srv, "POST", "/api/decisions/update", `{"id":"pick-the-vendor","status":"undecided"}`); code != 400 {
		t.Fatalf("bad status: %d", code)
	}
	if code, _ = artifactsDo(t, srv, "POST", "/api/decisions/update", `{"id":"aion-bl/x","why":"x"}`); code != 400 {
		t.Fatalf("backlog decisions are not edited here: %d", code)
	}

	// the ledger: created · entity.added · edge.added (P2) · updated+decided · updated+revisited, all under the decision
	var kinds []string
	for _, e := range allLedgerEntries(t, led) {
		if e.Object == (ledger.Object{Kind: "decision", ID: "pick-the-vendor"}) {
			kinds = append(kinds, e.Kind)
		}
	}
	if strings.Join(kinds, ",") != "decision.created,graph.entity.added,decision.updated,decision.decided,decision.updated,decision.revisited" {
		t.Fatalf("ledger kinds: %v", kinds)
	}
	es := allLedgerEntries(t, led)
	if es[0].Source != "decision" || es[0].Actor != "agent:hermes" || es[0].Task != task || es[0].Ref != "system/decisions/pick-the-vendor.md" || es[0].Meta["evidence"] != float64(3) {
		t.Fatalf("created event: %+v", es[0])
	}
	// … and the view reconstructs it: history + graph
	code, r = artifactsDo(t, srv, "GET", "/api/decisions/get?id=pick-the-vendor", "")
	if code != 200 || r["status"] != "revisited" || r["outcome"] != "beta, 12 months" || r["actualOutcome"] != "churn fell 30%" || r["decided"] == "" || r["revisited"] == "" {
		t.Fatalf("view: %d %+v", code, r)
	}
	h := r["history"].(map[string]any)
	if len(h["entries"].([]any)) != 6 || !containsStr(strs(h["kinds"]), "decision.revisited") {
		t.Fatalf("view history: %+v", h)
	}
	if len(r["graph"].([]any)) != 3 {
		t.Fatalf("view graph: %+v", r["graph"])
	}
	// the task's own history carries the decision's lifecycle (related ref)
	if code, r = artifactsDo(t, srv, "GET", "/api/ledger/history?object="+task+"&objectKind=task", ""); code != 200 || len(r["entries"].([]any)) < 5 {
		t.Fatalf("task history: %d %+v", code, r)
	}
	if code, _ = artifactsDo(t, srv, "GET", "/api/decisions/get?id=nope", ""); code != 404 {
		t.Fatal("unknown decision")
	}
}

func TestDecisionsListCoexistsWithBacklogDecisions(t *testing.T) {
	srv, vault, led, _ := decisionFixture(t)
	// a backlog with one decision, read through the same aion store the server holds
	aionRoot := filepath.Join(vault, "system", "aion")
	if err := os.MkdirAll(aionRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	backlog := "## Decisions\n- Drop the old CRM [id:: aion-bl/drop-crm] [kind:: decision] [owner:: HZ] [captured:: 2026-08-01] [source:: [[crm thread]]]\n"
	if err := os.WriteFile(filepath.Join(aionRoot, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(abs string, data []byte) error { return os.WriteFile(abs, data, 0o644) }
	srv.UseAion(aion.NewStore(vault, "system/aion", write), "", "", "", t.TempDir())
	srv.aionLive = nil // the plain store path

	if code, r := artifactsDo(t, srv, "POST", "/api/decisions/create", `{"title":"Pick the vendor","owner":"BA"}`); code != 200 {
		t.Fatalf("create: %d %+v", code, r)
	}
	code, r := artifactsDo(t, srv, "GET", "/api/decisions", "")
	if code != 200 || r["count"].(float64) != 2 || r["configured"] != true {
		t.Fatalf("list: %d %+v", code, r)
	}
	ledgerOne, aionOne := r["decisions"].([]any)[0].(map[string]any), r["decisions"].([]any)[1].(map[string]any)
	if ledgerOne["id"] != "pick-the-vendor" || ledgerOne["editable"] != true {
		t.Fatalf("ledger row: %+v", ledgerOne)
	}
	if aionOne["id"] != "aion-bl/drop-crm" || aionOne["source"] != "aion" || aionOne["editable"] != false || aionOne["open"] != nil || strs(aionOne["sources"])[0] != "crm thread" {
		t.Fatalf("aion row: %+v", aionOne)
	}
	if code, r = artifactsDo(t, srv, "GET", "/api/decisions?source=aion", ""); code != 200 || r["count"].(float64) != 1 {
		t.Fatalf("source filter: %d %+v", code, r)
	}
	if code, r = artifactsDo(t, srv, "GET", "/api/decisions?owner=hz", ""); code != 200 || r["count"].(float64) != 1 {
		t.Fatalf("owner filter: %d %+v", code, r)
	}
	// the backlog decision resolves by id, read-only, with the decision view
	if code, r = artifactsDo(t, srv, "GET", "/api/decisions/get?id=aion-bl/drop-crm", ""); code != 200 || r["title"] != "Drop the old CRM" || r["editable"] != false {
		t.Fatalf("aion view: %d %+v", code, r)
	}
	// deciding it through the backlog route ledgers decision.decided under decision:aion-bl/drop-crm …
	if code, r = artifactsDo(t, srv, "POST", "/api/aion/backlog/decide/aion-bl/drop-crm", `{"outcome":"moved to attio"}`); code != 200 {
		t.Fatalf("aion decide: %d %+v", code, r)
	}
	var hit *ledger.Entry
	for _, e := range allLedgerEntries(t, led) {
		if e.Kind == "decision.decided" && e.Object == (ledger.Object{Kind: "decision", ID: "aion-bl/drop-crm"}) {
			e := e
			hit = &e
		}
	}
	if hit == nil || hit.Meta["outcome"] != "moved to attio" || hit.Meta["source"] != "aion" || hit.Ref != "" {
		t.Fatalf("aion decide event: %+v", hit)
	}
	// … and the backlog file kept its shape (aion's fixpoint, untouched by the ledger)
	raw, _ := os.ReadFile(filepath.Join(aionRoot, "backlog.md"))
	if !strings.Contains(string(raw), "[status:: decided] [decided:: ") || !strings.Contains(string(raw), "[outcome:: moved to attio]") {
		t.Fatalf("backlog:\n%s", raw)
	}
	if code, r = artifactsDo(t, srv, "GET", "/api/decisions?status=decided", ""); code != 200 || r["count"].(float64) != 1 || r["decisions"].([]any)[0].(map[string]any)["id"] != "aion-bl/drop-crm" {
		t.Fatalf("status filter after decide: %d %+v", code, r)
	}
}

func TestDecisionsWithoutStoreListsBacklogAndRefusesWrites(t *testing.T) {
	srv, _, _, _ := graphFixture(t)
	if code, r := artifactsDo(t, srv, "GET", "/api/decisions", ""); code != 200 || r["configured"] != false || r["count"].(float64) != 0 {
		t.Fatalf("list: %d %+v", code, r)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/decisions/create", `{"title":"x"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("write without a store: %d", code)
	}
	// the graph still answers (no decision edges to derive)
	if code, r := artifactsDo(t, srv, "GET", "/api/graph/neighbors?ref=decision:x", ""); code != 200 || r["count"].(float64) != 0 {
		t.Fatalf("graph without decisions: %d %+v", code, r)
	}
	_ = graph.KindDecision
}
