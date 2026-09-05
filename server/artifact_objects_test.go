package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/ledger"
	"manifest/spirits"
)

// P1 artifacts over the server: create by ref through the spirits read path,
// revise with the chain intact, bind to a task line by reference, and the
// ledger carrying each step under the artifact object.

func artifactFixture(t *testing.T) (*Server, string, *ledger.Store) {
	t.Helper()
	srv, vault := unifiedHarness(t)
	primary := spirits.NewStore(t.TempDir())
	hermes := spirits.NewStore(t.TempDir())
	srv.UseHarnesses([]Harness{{Name: "excalibur", Spirits: primary}, {Name: "hermes", Spirits: hermes}})
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	srv.UseLedger(led)
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	srv.UseArtifactRegistry(reg)
	return srv, vault, led
}

func writeBrief(t *testing.T, st *spirits.Store, name, content string) {
	t.Helper()
	dir := filepath.Join(st.Root(), "artifacts", "library")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func artifactsDo(t *testing.T, srv *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	srv.Handler().ServeHTTP(rec, req)
	var out map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: bad json: %v\n%s", method, path, err, rec.Body.String())
		}
	} else {
		out = map[string]any{"error": rec.Body.String()}
	}
	return rec.Code, out
}

func TestArtifactCreateReviseAndLedger(t *testing.T) {
	srv, _, led := artifactFixture(t)
	primary := srv.eachHarness()[0].Spirits
	const ref = "artifacts/library/2026-08-12-uae.md"
	v1 := "---\ntitle: UAE brief\nrun: r1\n---\nfindings v1\n"
	writeBrief(t, primary, "2026-08-12-uae.md", v1)

	// create by ref: the bytes come through the spirits read allow-list
	code, r := artifactsDo(t, srv, "POST", "/api/artifacts/create",
		`{"kind":"brief","title":"UAE brief","ref":"`+ref+`","task":"inbox/loose-personal-thing","run":"r1","source":"run","actor":"agent:hermes"}`)
	if code != 200 || r["created"] != true || r["changed"] != true {
		t.Fatalf("create: %d %+v", code, r)
	}
	a := r["artifact"].(map[string]any)
	id := a["id"].(string)
	if !artifacts.ValidID(id) || a["kind"] != "brief" || a["ref"] != ref || a["harness"] != "excalibur" || a["head"] != artifacts.Hash([]byte(v1)) {
		t.Fatalf("artifact: %+v", a)
	}
	// it opens through the one harness read path (primary → no tag)
	if open, _ := a["open"].(map[string]any); open == nil || open["path"] != ref || open["harness"] != nil {
		t.Fatalf("open link: %+v", a["open"])
	}
	if links := a["links"].(map[string]any); strings.Join(strs(links["producers"]), ",") != "inbox/loose-personal-thing" {
		t.Fatalf("provenance task must read as a producer: %+v", links)
	}
	// the ledger carries artifact.created under the artifact object, with the task as a related ref
	es := allLedgerEntries(t, led)
	if len(es) != 1 || es[0].Kind != "artifact.created" || es[0].Object != (ledger.Object{Kind: "artifact", ID: id}) ||
		es[0].Task != "inbox/loose-personal-thing" || es[0].Run != "r1" || es[0].Actor != "agent:hermes" || es[0].Ref != ref ||
		es[0].Meta["version"].(float64) != 1 {
		t.Fatalf("ledger after create: %+v", es)
	}
	// unreadable refs never register: off the allow-list, or absent
	if code, _ := artifactsDo(t, srv, "POST", "/api/artifacts/create", `{"ref":"spirits/x/secret.md"}`); code != 400 {
		t.Fatalf("off-list ref accepted: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/artifacts/create", `{"ref":"artifacts/library/nope.md"}`); code != 400 {
		t.Fatalf("missing ref accepted: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/artifacts/create", `{"kind":"note"}`); code != 400 {
		t.Fatalf("empty create accepted: %d", code)
	}

	// the same ref registered again with the same bytes: nothing changes
	code, r = artifactsDo(t, srv, "POST", "/api/artifacts/create", `{"ref":"`+ref+`"}`)
	if code != 200 || r["created"] != false || r["changed"] != false || r["artifact"].(map[string]any)["id"] != id {
		t.Fatalf("replay: %d %+v", code, r)
	}

	// revise with inline content: version 2, parent = v1, old bytes still read
	v2 := "---\ntitle: UAE brief\nrun: r2\n---\nfindings v2\n"
	code, r = artifactsDo(t, srv, "POST", "/api/artifacts/revise", `{"id":"`+id+`","content":`+jsonString(v2)+`,"note":"second pass","run":"r2"}`)
	if code != 200 || r["created"] != false || r["changed"] != true {
		t.Fatalf("revise: %d %+v", code, r)
	}
	rev := r["revision"].(map[string]any)
	if rev["n"].(float64) != 2 || rev["parent"] != artifacts.Hash([]byte(v1)) || rev["hash"] != artifacts.Hash([]byte(v2)) {
		t.Fatalf("revision: %+v", rev)
	}
	a = r["artifact"].(map[string]any)
	if len(a["revisions"].([]any)) != 2 || a["head"] != artifacts.Hash([]byte(v2)) {
		t.Fatalf("chain: %+v", a)
	}
	if prov := a["provenance"].(map[string]any); prov["task"] != "inbox/loose-personal-thing" || prov["run"] != "r1" {
		t.Fatalf("provenance must never unlearn: %+v", prov)
	}
	code, r = artifactsDo(t, srv, "GET", "/api/artifacts/get?id="+id+"&content=1&rev="+artifacts.Hash([]byte(v1)), "")
	if code != 200 || r["content"] != v1 {
		t.Fatalf("old revision content: %d %q", code, r["content"])
	}
	code, r = artifactsDo(t, srv, "GET", "/api/artifacts/get?id="+id+"&content=1", "")
	if code != 200 || r["content"] != v2 {
		t.Fatalf("head content: %d %q", code, r["content"])
	}
	if code, _ := artifactsDo(t, srv, "GET", "/api/artifacts/get?id="+id+"&content=1&rev="+strings.Repeat("0", 64), ""); code != 404 {
		t.Fatalf("unknown rev: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "GET", "/api/artifacts/get?id=../../x", ""); code != 404 {
		t.Fatalf("bad id: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/artifacts/revise", `{"id":"`+strings.Repeat("a", 16)+`","content":"x"}`); code != 404 {
		t.Fatalf("revise unknown: %d", code)
	}

	// the object's history reconstructs from the ledger alone
	code, h := artifactsDo(t, srv, "GET", "/api/ledger/history?objectKind=artifact&object="+id, "")
	if code != 200 || strings.Join(strs(h["kinds"]), ",") != "artifact.created,artifact.revised" {
		t.Fatalf("history: %d %+v", code, h)
	}
	// …and the task's history sees the artifact it produced
	code, h = artifactsDo(t, srv, "GET", "/api/ledger/history?objectKind=task&object=inbox/loose-personal-thing", "")
	if code != 200 || len(h["entries"].([]any)) != 2 {
		t.Fatalf("task history: %d %+v", code, h)
	}

	// list + filters, newest change first
	code, l := artifactsDo(t, srv, "GET", "/api/artifacts?task=inbox/loose-personal-thing", "")
	if code != 200 || l["count"].(float64) != 1 {
		t.Fatalf("list by task: %d %+v", code, l)
	}
	if _, l = artifactsDo(t, srv, "GET", "/api/artifacts?kind=plan", ""); l["count"].(float64) != 0 {
		t.Fatalf("kind filter: %+v", l)
	}
}

func TestTaskArtifactBinding(t *testing.T) {
	srv, vault, led := artifactFixture(t)
	primary := srv.eachHarness()[0].Spirits
	writeBrief(t, primary, "memo.md", "memo v1\n")
	_, r := artifactsDo(t, srv, "POST", "/api/artifacts/create", `{"kind":"document","title":"memo","ref":"artifacts/library/memo.md"}`)
	memo := r["artifact"].(map[string]any)["id"].(string)
	const personal, prop = "inbox/loose-personal-thing", "prop:761-maple/rough-in/rough-electrical"

	// the personal line consumes the memo; the property line produced it
	if code, out := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"`+personal+`","addInput":"`+memo+`"}`); code != 200 {
		t.Fatalf("addInput: %d %+v", code, out)
	}
	if code, out := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"`+prop+`","outputs":["`+memo+`","0123456789abcdef"]}`); code != 200 {
		t.Fatalf("outputs: %d %+v", code, out)
	}
	td, _ := os.ReadFile(filepath.Join(vault, "to do.md"))
	if !strings.Contains(string(td), "loose personal thing [added:: 2026-08-01] [inputs:: "+memo+"]") {
		t.Fatalf("inputs not written by reference:\n%s", td)
	}
	pr, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(pr), "Rough electrical [added:: 2026-08-05] [outputs:: "+memo+", 0123456789abcdef] [est:: 8000]") {
		t.Fatalf("outputs not written to the property tree:\n%s", pr)
	}

	// the rows carry the ids; the artifact derives its producers/consumers
	v := getTasksView(t, srv)
	if strings.Join(strs(rowByID(t, v, personal)["inputs"]), ",") != memo || strings.Join(strs(rowByID(t, v, prop)["outputs"]), ",") != memo+",0123456789abcdef" {
		t.Fatalf("rows: %+v", v["rows"])
	}
	_, g := artifactsDo(t, srv, "GET", "/api/artifacts/get?id="+memo, "")
	links := g["links"].(map[string]any)
	if strings.Join(strs(links["producers"]), ",") != prop || strings.Join(strs(links["consumers"]), ",") != personal {
		t.Fatalf("links: %+v", links)
	}
	// the panel answers "what did this task produce / consume" — an id the
	// registry doesn't know lists as unknown, never dropped
	rec := httptest.NewRecorder()
	srv.handleTaskPanel(rec, httptest.NewRequest("GET", "/api/tasks/panel?id="+prop, nil))
	var panel map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	outs := panel["artifacts"].(map[string]any)["outputs"].([]any)
	if len(outs) != 2 || outs[0].(map[string]any)["title"] != "memo" || outs[1].(map[string]any)["unknown"] != true {
		t.Fatalf("panel outputs: %+v", outs)
	}
	// the ledger saw both edges land under the artifact object
	es := allLedgerEntries(t, led)
	if countKind(es, "artifact.bound") != 3 {
		t.Fatalf("bound events: %+v", es)
	}
	// remove: the edge lifts, the ledger records it, unknown ids still refuse nothing
	if code, _ := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"`+prop+`","removeOutput":"0123456789abcdef"}`); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if countKind(allLedgerEntries(t, led), "artifact.unbound") != 1 {
		t.Fatal("unbound event missing")
	}
	// a failed write logs nothing: unknown task → 404 and no event
	before := len(allLedgerEntries(t, led))
	if code, _ := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"inbox/nope","addOutput":"`+memo+`"}`); code != 404 {
		t.Fatalf("unknown task: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"aion:abc","addOutput":"`+memo+`"}`); code != 400 {
		t.Fatalf("backlog binding should be refused: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"`+personal+`"}`); code != 400 {
		t.Fatalf("empty edit accepted: %d", code)
	}
	if got := len(allLedgerEntries(t, led)); got != before {
		t.Fatalf("a refused write ledgered: %d → %d", before, got)
	}
}

// The run writer: a completed delegated run's brief registers itself with
// provenance {run, task} on the ledger sweep; the delegation row names the
// object; a second sweep changes nothing. Rows without a registry entry keep
// working exactly as before (backward compatibility with ArtifactRef).
func TestRunBriefRegistersOnSweep(t *testing.T) {
	srv, vault, led := artifactFixture(t)
	hermes := srv.eachHarness()[1].Spirits
	if err := os.MkdirAll(filepath.Join(hermes.Root(), "artifacts", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := "---\nrun: r1\nspirit: hermes\nritual: delegate\nrequest: \"research zoning [todo:: inbox/loose-personal-thing]\"\n" +
		"started: 2026-08-15T05:00:00Z\nfinished: 2026-08-15T05:01:00Z\noutcome: completed\n---\nran\n"
	if err := os.WriteFile(filepath.Join(hermes.Root(), "artifacts", "runs", "2026-08-15-hermes-r1.md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	brief := "---\ntitle: Zoning brief\nrun: r1\ndate: 2026-08-15T05:01:00Z\n---\nthe brief [todo:: inbox/loose-personal-thing]\n"
	writeBrief(t, hermes, "2026-08-15-zoning.md", brief)

	// before the sweep: the row resolves its ref the legacy way, no object yet
	d, ok := srv.delegationIndex()["inbox/loose-personal-thing"]
	if !ok || d.ArtifactRef != "artifacts/library/2026-08-15-zoning.md" || d.ArtifactID != "" {
		t.Fatalf("legacy row: %+v", d)
	}

	srv.ledgerSweep()
	// the run ref is the report stem — the same id the ledger's run object uses
	list := srv.artifactReg.List(artifacts.Filter{Run: "2026-08-15-hermes-r1"})
	if len(list) != 1 {
		t.Fatalf("brief not registered: %+v", list)
	}
	a := list[0]
	if a.Kind != artifacts.KindBrief || a.Title != "Zoning brief" || a.Harness != "hermes" || a.Ref != "artifacts/library/2026-08-15-zoning.md" ||
		a.Provenance.Task != "inbox/loose-personal-thing" || a.Provenance.Source != "run" || a.Actor != "hermes" ||
		!a.Created.Equal(time.Date(2026, 8, 15, 5, 1, 0, 0, time.UTC)) || a.Head != artifacts.Hash([]byte(brief)) {
		t.Fatalf("registered brief: %+v", a)
	}
	es := allLedgerEntries(t, led)
	if countKind(es, "run.completed") != 1 || countKind(es, "artifact.created") != 1 {
		t.Fatalf("sweep ledger: %+v", es)
	}
	// the delegation row now names the object — and still opens the same ref
	d = srv.delegationIndex()["inbox/loose-personal-thing"]
	if d.ArtifactID != a.ID || d.ArtifactRef != a.Ref {
		t.Fatalf("row after sweep: %+v", d)
	}
	// the artifact opens through the hermes tree (tagged, non-primary)
	_, g := artifactsDo(t, srv, "GET", "/api/artifacts/get?id="+a.ID, "")
	if open := g["open"].(map[string]any); open["path"] != a.Ref || open["harness"] != "hermes" {
		t.Fatalf("open: %+v", open)
	}
	// "what did the task produce" answers from provenance alone — nothing was
	// written onto the task line
	_, l := artifactsDo(t, srv, "GET", "/api/artifacts?task=inbox/loose-personal-thing", "")
	if l["count"].(float64) != 1 {
		t.Fatalf("by task: %+v", l)
	}
	if td, _ := os.ReadFile(filepath.Join(vault, "to do.md")); strings.Contains(string(td), "[outputs::") {
		t.Fatalf("the sweep must not write the task line:\n%s", td)
	}
	// re-registering the same brief is a no-op (idempotent writer)
	res, err := srv.registerRunBrief(srv.findHarness("hermes"), "2026-08-15-hermes-r1", "inbox/loose-personal-thing", "hermes", time.Now())
	if err != nil || res.Changed || res.Created {
		t.Fatalf("re-register: %+v %v", res, err)
	}
	if got := len(allLedgerEntries(t, led)); got != len(es) {
		t.Fatalf("re-register ledgered: %d → %d", len(es), got)
	}
}

// Without a registry every surface degrades to what it was: list is empty,
// writes are 503, delegation rows carry their ref and no id.
func TestArtifactsWithoutRegistry(t *testing.T) {
	srv, _ := unifiedHarness(t)
	code, l := artifactsDo(t, srv, "GET", "/api/artifacts", "")
	if code != 200 || l["count"].(float64) != 0 {
		t.Fatalf("list: %d %+v", code, l)
	}
	if code, _ := artifactsDo(t, srv, "POST", "/api/artifacts/create", `{"content":"x"}`); code != 503 {
		t.Fatalf("create without registry: %d", code)
	}
	if code, _ := artifactsDo(t, srv, "GET", "/api/artifacts/get?id=x", ""); code != 503 {
		t.Fatalf("get without registry: %d", code)
	}
	// binding still writes the line: the ids are references, the registry is optional
	if code, _ := artifactsDo(t, srv, "POST", "/api/tasks/artifacts", `{"id":"inbox/loose-personal-thing","addOutput":"0123456789abcdef"}`); code != 200 {
		t.Fatalf("bind without registry: %d", code)
	}
	rec := httptest.NewRecorder()
	srv.handleTaskPanel(rec, httptest.NewRequest("GET", "/api/tasks/panel?id=inbox/loose-personal-thing", nil))
	var panel map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	if outs := panel["artifacts"].(map[string]any)["outputs"].([]any); len(outs) != 1 || outs[0].(map[string]any)["unknown"] != true {
		t.Fatalf("panel without registry: %+v", panel["artifacts"])
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
