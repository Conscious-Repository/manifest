package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// coordination state (P1 Phase 1) over the unified projection: a personal
// line can depend on a property line; blocked derives across files; the
// endpoints write the fields into whichever file owns the line.

func coordPost(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	switch path {
	case "/api/tasks/depends":
		srv.handleTaskDepends(rec, req)
	case "/api/tasks/priority":
		srv.handleTaskPriority(rec, req)
	case "/api/tasks/check":
		srv.handleTaskCheck(rec, req)
	default:
		t.Fatalf("unrouted %s", path)
	}
	return rec
}

func rowByID(t *testing.T, v map[string]any, id string) map[string]any {
	t.Helper()
	for _, r := range v["rows"].([]any) {
		if m := r.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("row %s missing", id)
	return nil
}

func strs(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, x := range list {
		out = append(out, x.(string))
	}
	return out
}

func TestCoordCrossFileBlocked(t *testing.T) {
	srv, vault := unifiedHarness(t)
	const personal, prop = "inbox/loose-personal-thing", "prop:761-maple/rough-in/rough-electrical"

	// the personal line waits on the property line + one id nobody knows
	rec := coordPost(t, srv, "/api/tasks/depends",
		`{"id":"`+personal+`","depends":["`+prop+`","aion:nope"]}`)
	if rec.Code != 200 {
		t.Fatalf("depends: %d %s", rec.Code, rec.Body.String())
	}
	td, _ := os.ReadFile(filepath.Join(vault, "to do.md"))
	if !strings.Contains(string(td), "loose personal thing [added:: 2026-08-01] [depends:: "+prop+", aion:nope]") {
		t.Fatalf("depends not written to tasks.md:\n%s", td)
	}
	// the target got pinned so the reference survives rewording (plan D1)
	pr, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(pr), "Rough electrical [todo:: rough-in/rough-electrical]") {
		t.Fatalf("dependency target not pinned:\n%s", pr)
	}

	v := getTasksView(t, srv)
	me := rowByID(t, v, personal)
	if me["state"] != "blocked" || strings.Join(strs(me["blockedBy"]), "|") != prop ||
		strings.Join(strs(me["unresolved"]), "|") != "aion:nope" {
		t.Fatalf("blocked projection: %v", me)
	}
	// the property row knows what depends on it
	if p := rowByID(t, v, prop); strings.Join(strs(p["dependents"]), "|") != personal || p["state"] != "open" {
		t.Fatalf("dependents projection: %v", p)
	}
	// the doc-local domains view cannot see the property id: it reports the
	// same edge as unresolved rather than guessing
	for _, dom := range v["domains"].([]any) {
		for _, tk := range dom.(map[string]any)["tasks"].([]any) {
			if m := tk.(map[string]any); m["id"] == personal {
				if m["state"] != "open" || strings.Join(strs(m["unresolved"]), "|") != prop+"|aion:nope" {
					t.Fatalf("doc-local projection: %v", m)
				}
			}
		}
	}

	// completing the blocker unblocks — nothing stored changed on the dependent
	if rec := coordPost(t, srv, "/api/tasks/check", `{"id":"`+prop+`","checked":true}`); rec.Code != 200 {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	v = getTasksView(t, srv)
	me = rowByID(t, v, personal)
	if me["state"] != "open" || me["blockedBy"] != nil {
		t.Fatalf("still blocked after the blocker completed: %v", me)
	}
	if strings.Join(strs(me["depends"]), "|") != prop+"|aion:nope" {
		t.Fatalf("depends list must persist untouched: %v", me)
	}

	// remove one edge, add another
	if rec := coordPost(t, srv, "/api/tasks/depends", `{"id":"`+personal+`","remove":"aion:nope","add":"real-estate/call-the-county"}`); rec.Code != 200 {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	v = getTasksView(t, srv)
	me = rowByID(t, v, personal)
	if strings.Join(strs(me["depends"]), "|") != prop+"|real-estate/call-the-county" || me["state"] != "blocked" {
		t.Fatalf("after edit: %v", me)
	}
	// clear
	if rec := coordPost(t, srv, "/api/tasks/depends", `{"id":"`+personal+`","depends":[]}`); rec.Code != 200 {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	td, _ = os.ReadFile(filepath.Join(vault, "to do.md"))
	if strings.Contains(string(td), "[depends::") {
		t.Fatalf("depends not cleared:\n%s", td)
	}
}

func TestCoordPriority(t *testing.T) {
	srv, vault := unifiedHarness(t)
	const prop = "prop:761-maple/rough-in/rough-electrical"
	// a property line takes priority in ITS file (est:: passthrough intact)
	if rec := coordPost(t, srv, "/api/tasks/priority", `{"id":"`+prop+`","priority":"high"}`); rec.Code != 200 {
		t.Fatalf("prop priority: %d %s", rec.Code, rec.Body.String())
	}
	pr, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if !strings.Contains(string(pr), "Rough electrical [added:: 2026-08-05] [priority:: high] [est:: 8000]") {
		t.Fatalf("property priority not written:\n%s", pr)
	}
	// the closed set is enforced — nothing written
	if rec := coordPost(t, srv, "/api/tasks/priority", `{"id":"inbox/loose-personal-thing","priority":"urgent"}`); rec.Code != 400 {
		t.Fatalf("out-of-set priority accepted: %d", rec.Code)
	}
	if rec := coordPost(t, srv, "/api/tasks/priority", `{"id":"inbox/loose-personal-thing","priority":"Medium"}`); rec.Code != 200 {
		t.Fatalf("personal priority: %d %s", rec.Code, rec.Body.String())
	}
	v := getTasksView(t, srv)
	if rowByID(t, v, "inbox/loose-personal-thing")["priority"] != "med" || rowByID(t, v, prop)["priority"] != "high" {
		t.Fatalf("priority projection: %v", v["rows"])
	}
	// rank is untouched by priority
	td, _ := os.ReadFile(filepath.Join(vault, "to do.md"))
	if strings.Contains(string(td), "[rank::") || !strings.Contains(string(td), "[priority:: med]") {
		t.Fatalf("rank/priority separation:\n%s", td)
	}
	// clear
	if rec := coordPost(t, srv, "/api/tasks/priority", `{"id":"`+prop+`","priority":""}`); rec.Code != 200 {
		t.Fatalf("clear: %d", rec.Code)
	}
	pr, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/761-maple.md"))
	if strings.Contains(string(pr), "[priority::") {
		t.Fatalf("priority not cleared:\n%s", pr)
	}
	// backlog items refuse loudly (no field to hold it yet)
	if rec := coordPost(t, srv, "/api/tasks/priority", `{"id":"aion:abc","priority":"high"}`); rec.Code != 400 {
		t.Fatalf("aion priority should be refused: %d", rec.Code)
	}
	// unknown id → 404, not a silent no-op
	if rec := coordPost(t, srv, "/api/tasks/priority", `{"id":"inbox/nope","priority":"high"}`); rec.Code != 404 {
		t.Fatalf("unknown id: %d", rec.Code)
	}
}
