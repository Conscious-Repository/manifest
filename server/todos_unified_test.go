package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/realestate"
	"manifest/record"
	"manifest/tasks"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

// unifiedHarness builds a real temp vault: a `to do.md`, one property record
// whose `## rocks` tree holds the property tasks (overhaul §6), a live index,
// and a server whose writes flow through declared capabilities.
func unifiedHarness(t *testing.T) (*Server, string) {
	t.Helper()
	vault := t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("to do.md", strings.Join([]string{
		"# To Do",
		"",
		"## Inbox",
		"- [ ] loose personal thing [added:: 2026-08-01]",
		"",
		"## Real Estate",
		"- [ ] call the county [added:: 2026-08-02]",
	}, "\n")+"\n")
	write("system/realestate/properties/761-maple.md", strings.Join([]string{
		"---",
		"categories: [property]",
		"address: 761 Maple Ave, Saint Louis, MO",
		"status: construction",
		"control: owned",
		"---",
		"",
		"## rocks",
		"- [ ] Rough-in [done-by:: 2026-11-01]",
		"    - [ ] Rough electrical [added:: 2026-08-05] [est:: 8000]",
		"    - [ ] chase gutter bid [added:: 2026-08-06] [owner:: acme-gc]",
		"- [ ] Finishes",
	}, "\n")+"\n")

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}

	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic").Grant(
		vaultwriter.Capability{Name: "todos", Zone: record.ZoneKnowledge,
			Pattern: "to do*", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorUserAction},
	)

	srv := &Server{index: ix}
	srv.UseVault(vw)
	srv.UseTasks(tasks.NewStore(vault, "to do.md", vw.BindAbs("todos")))
	srv.UseOwner("BA")
	srv.realestate = realestate.New(ix)
	return srv, vault
}

func getTasksView(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.handleTasksGet(rec, httptest.NewRequest("GET", "/api/tasks", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /api/tasks: %d %s", rec.Code, rec.Body.String())
	}
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// The projection: my rows span personal + property trees; someone else's item
// never reaches rows — it appears under Outstanding, grouped by container.
func TestUnifiedProjection(t *testing.T) {
	srv, _ := unifiedHarness(t)
	v := getTasksView(t, srv)
	rows := v["rows"].([]any)
	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.(map[string]any)["id"].(string)] = true
	}
	if !ids["inbox/loose-personal-thing"] || !ids["real-estate/call-the-county"] {
		t.Fatalf("personal rows missing: %v", ids)
	}
	if !ids["prop:761-maple/rough-in/rough-electrical"] {
		t.Fatalf("property tree row missing: %v", ids)
	}
	if ids["prop:761-maple/rough-in/chase-gutter-bid"] {
		t.Fatal("assigned-to-others item leaked into my rows")
	}
	out := v["outstanding"].([]any)
	if len(out) != 1 {
		t.Fatalf("outstanding groups: %v", out)
	}
	g := out[0].(map[string]any)
	if g["count"].(float64) != 1 || g["container"].(map[string]any)["slug"] != "761-maple" {
		t.Fatalf("outstanding group: %v", g)
	}
	counts := v["counts"].(map[string]any)
	if counts["tasks"].(float64) != float64(len(rows)) {
		t.Fatal("counts.todos must derive from rows")
	}
}

// Checking a property task routes to the tree line, stamps [done::], and
// flips the ONE line — the rock's Ready/money state derives from it directly
// (the Rev-3 dual-stamp is gone with the merge).
func TestPropTaskCheckTree(t *testing.T) {
	srv, vault := unifiedHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/check",
		strings.NewReader(`{"id":"prop:761-maple/rough-in/rough-electrical","checked":true}`))
	srv.handleTaskCheck(rec, req)
	if rec.Code != 200 {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	got := string(raw)
	if !strings.Contains(got, "- [x] Rough electrical") || !strings.Contains(got, "[done:: ") {
		t.Fatalf("tree line not checked/stamped:\n%s", got)
	}
	// unchecking reverses it
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/tasks/check",
		strings.NewReader(`{"id":"prop:761-maple/rough-in/rough-electrical","checked":false}`))
	srv.handleTaskCheck(rec, req)
	raw, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	got = string(raw)
	if strings.Contains(got, "- [x] Rough electrical") || strings.Contains(got, "[done:: ") {
		t.Fatalf("uncheck did not reverse:\n%s", got)
	}
}

// Rank order in → [rank:: n] on every listed line, one write per touched file,
// and the view returns in that order.
func TestRankBatch(t *testing.T) {
	srv, vault := unifiedHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/rank", strings.NewReader(
		`{"order":["prop:761-maple/rough-in/rough-electrical","real-estate/call-the-county","inbox/loose-personal-thing"]}`))
	srv.handleTasksRank(rec, req)
	if rec.Code != 200 {
		t.Fatalf("rank: %d %s", rec.Code, rec.Body.String())
	}
	td, _ := os.ReadFile(filepath.Join(vault, "to do.md"))
	if !strings.Contains(string(td), "call the county [added:: 2026-08-02] [rank:: 2]") ||
		!strings.Contains(string(td), "loose personal thing [added:: 2026-08-01] [rank:: 3]") {
		t.Fatalf("personal ranks not written:\n%s", td)
	}
	prop, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(prop), "Rough electrical [added:: 2026-08-05] [rank:: 1] [est:: 8000]") {
		t.Fatalf("property rank not written:\n%s", prop)
	}
	v := getTasksView(t, srv)
	rows := v["rows"].([]any)
	if rows[0].(map[string]any)["id"] != "prop:761-maple/rough-in/rough-electrical" ||
		rows[1].(map[string]any)["id"] != "real-estate/call-the-county" {
		t.Fatalf("view order does not follow rank: %v", rows)
	}
}

// Adding with a property container lands the line IN the tree — under the
// current rock — no copy in to do.md (one line, one file; surfaces project it).
func TestAddToProperty(t *testing.T) {
	srv, vault := unifiedHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/item", strings.NewReader(
		`{"text":"order dumpsters","container":{"kind":"property","slug":"761-maple"},"owner":"acme-gc"}`))
	srv.handleTaskAdd(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	prop, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(prop), "    - [ ] order dumpsters [added:: ") ||
		!strings.Contains(string(prop), "[owner:: acme-gc]") {
		t.Fatalf("tree add missing:\n%s", prop)
	}
	td, _ := os.ReadFile(filepath.Join(vault, "to do.md"))
	if strings.Contains(string(td), "order dumpsters") {
		t.Fatal("parallel copy leaked into to do.md")
	}
}

// The "decision:" capture sugar converts to a [decision::] node in the tree.
func TestAddDecisionSugar(t *testing.T) {
	srv, vault := unifiedHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/item", strings.NewReader(
		`{"text":"decision: pick shingle color","container":{"kind":"property","slug":"761-maple"}}`))
	srv.handleTaskAdd(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	prop, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(prop), "- [ ] pick shingle color [added:: ") ||
		!strings.Contains(string(prop), "[decision::]") {
		t.Fatalf("decision sugar missing:\n%s", prop)
	}
}

func TestSplitPropID(t *testing.T) {
	for _, c := range []struct{ in, slug, line string }{
		{"prop:761-maple/gutters", "761-maple", "gutters"},
		{"prop:761-maple/rough-in/electrical", "761-maple", "rough-in/electrical"},
		{"prop:bad", "", ""},
		{"prop:/x", "", ""},
		{"prop:x/", "", ""},
	} {
		slug, line := splitPropID(c.in)
		if slug != c.slug || line != c.line {
			t.Fatalf("%q → %q %q", c.in, slug, line)
		}
	}
}

// {stage} on /api/tasks/update re-parents the task under the named rock (the
// tree IS the placement) — names outside the pipeline are refused, "" no-ops.
func TestPropTaskMove(t *testing.T) {
	srv, vault := unifiedHarness(t)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/tasks/update", strings.NewReader(body))
		srv.handleTaskUpdate(rec, req)
		return rec
	}
	if rec := post(`{"id":"prop:761-maple/rough-in/chase-gutter-bid","stage":"Finishes"}`); rec.Code != 200 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	got := string(raw)
	fin := strings.Index(got, "- [ ] Finishes")
	bid := strings.Index(got, "chase gutter bid")
	if fin < 0 || bid < fin {
		t.Fatalf("task did not move under Finishes:\n%s", got)
	}
	// a name outside the property's pipeline is refused
	if rec := post(`{"id":"prop:761-maple/finishes/chase-gutter-bid","stage":"Lease-up"}`); rec.Code == 200 {
		t.Fatalf("foreign rock accepted: %s", rec.Body.String())
	}
	// "" is a no-op (position is home)
	if rec := post(`{"id":"prop:761-maple/finishes/chase-gutter-bid","stage":""}`); rec.Code != 200 {
		t.Fatalf("empty stage no-op: %d %s", rec.Code, rec.Body.String())
	}
}
