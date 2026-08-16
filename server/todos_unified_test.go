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
// with `## work` + `## todos` sections, a live index, and a server whose
// writes flow through declared capabilities — the stage-4 substrate end to end.
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
		"## work",
		"- [ ] Rough-in",
		"    - [ ] Rough electrical [est:: 8000]",
		"",
		"## todos",
		"- [ ] rough electrical [added:: 2026-08-05] [work:: rough-in/rough-electrical]",
		"- [ ] chase gutter bid [added:: 2026-08-06] [owner:: acme-gc]",
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

// The projection: my rows span personal + property files; someone else's item
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
	if !ids["prop:761-maple/rough-electrical"] {
		t.Fatalf("property row missing: %v", ids)
	}
	if ids["prop:761-maple/chase-gutter-bid"] {
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
	if counts["todos"].(float64) != float64(len(rows)) {
		t.Fatal("counts.todos must derive from rows")
	}
}

// Checking a property todo routes to the property file, stamps [done::], and
// DUAL-STAMPS the [work::]-tethered `## work` line (accrual truth).
func TestPropTaskCheckDualStamp(t *testing.T) {
	srv, vault := unifiedHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/check",
		strings.NewReader(`{"id":"prop:761-maple/rough-electrical","checked":true}`))
	srv.handleTaskCheck(rec, req)
	if rec.Code != 200 {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	got := string(raw)
	if !strings.Contains(got, "- [x] rough electrical") || !strings.Contains(got, "[done:: ") {
		t.Fatalf("todo line not checked/stamped:\n%s", got)
	}
	if !strings.Contains(got, "    - [x] Rough electrical [est:: 8000]") {
		t.Fatalf("work line not dual-stamped:\n%s", got)
	}
	// unchecking reverses both
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/tasks/check",
		strings.NewReader(`{"id":"prop:761-maple/rough-electrical","checked":false}`))
	srv.handleTaskCheck(rec, req)
	raw, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	got = string(raw)
	if strings.Contains(got, "- [x] rough electrical") || strings.Contains(got, "    - [x] Rough electrical") {
		t.Fatalf("uncheck did not reverse both stamps:\n%s", got)
	}
}

// Rank order in → [rank:: n] on every listed line, one write per touched file,
// and the view returns in that order.
func TestRankBatch(t *testing.T) {
	srv, vault := unifiedHarness(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/rank", strings.NewReader(
		`{"order":["prop:761-maple/rough-electrical","real-estate/call-the-county","inbox/loose-personal-thing"]}`))
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
	if !strings.Contains(string(prop), "rough electrical [added:: 2026-08-05] [rank:: 1] [work:: rough-in/rough-electrical]") {
		t.Fatalf("property rank not written:\n%s", prop)
	}
	v := getTasksView(t, srv)
	rows := v["rows"].([]any)
	if rows[0].(map[string]any)["id"] != "prop:761-maple/rough-electrical" ||
		rows[1].(map[string]any)["id"] != "real-estate/call-the-county" {
		t.Fatalf("view order does not follow rank: %v", rows)
	}
}

// Adding with a property container lands the line in THAT file — no copy in
// to do.md (one line, one file; surfaces project it).
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
	if !strings.Contains(string(prop), "- [ ] order dumpsters [added:: ") ||
		!strings.Contains(string(prop), "[owner:: acme-gc]") {
		t.Fatalf("property add missing:\n%s", prop)
	}
	td, _ := os.ReadFile(filepath.Join(vault, "to do.md"))
	if strings.Contains(string(td), "order dumpsters") {
		t.Fatal("parallel copy leaked into to do.md")
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

// A property todo files under one of THIS property's stages via {stage} on
// /api/tasks/update — names outside the pipeline are refused, "" clears.
func TestPropTaskStageUpdate(t *testing.T) {
	srv, vault := unifiedHarness(t)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/tasks/update", strings.NewReader(body))
		srv.handleTaskUpdate(rec, req)
		return rec
	}
	if rec := post(`{"id":"prop:761-maple/chase-gutter-bid","stage":"rough-in"}`); rec.Code != 200 {
		t.Fatalf("stage set: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(raw), "chase gutter bid") || !strings.Contains(string(raw), "[stage:: Rough-in]") {
		t.Fatalf("stage not stamped (canonical casing):\n%s", raw)
	}
	// a name outside the property's pipeline is refused
	if rec := post(`{"id":"prop:761-maple/chase-gutter-bid","stage":"Lease-up"}`); rec.Code == 200 {
		t.Fatalf("foreign stage accepted: %s", rec.Body.String())
	}
	// "" clears — the task rides the current stage again
	if rec := post(`{"id":"prop:761-maple/chase-gutter-bid","stage":""}`); rec.Code != 200 {
		t.Fatalf("stage clear: %d %s", rec.Code, rec.Body.String())
	}
	raw, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if strings.Contains(string(raw), "[stage::") {
		t.Fatalf("stage not cleared:\n%s", raw)
	}
}
