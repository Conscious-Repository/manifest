package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"manifest/spirits"
	"manifest/tasks"
)

// fireFixture: assignFixture + a REAL hermes spirits store (temp dir) so
// spooling and the phase-aware index run against actual trace files.
func fireFixture(t *testing.T) *Server {
	t.Helper()
	srv, _ := panelFixture(t)
	dir := t.TempDir()
	st := tasks.NewStore(dir, "to do.md", testWriteAbs)
	if err := os.WriteFile(st.Path(), []byte("# To Do\n\n## Inbox\n- [ ] paint the fence [added:: 2026-08-14]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.tasksStore = st
	hermes := spirits.NewStore(t.TempDir())
	srv.UseHarnesses([]Harness{{Name: "excalibur"}, {Name: "hermes", Spirits: hermes}})
	return srv
}

func TestAssignSpoolsPlanPhase(t *testing.T) {
	srv := fireFixture(t)
	req := httptest.NewRequest("POST", "/api/tasks/assign",
		strings.NewReader(`{"id":"inbox/paint-the-fence","owner":"agent:hermes"}`))
	w := httptest.NewRecorder()
	srv.handleTaskAssign(w, req)
	if w.Code != 200 {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}
	// the work order is in hermes' spool, carrying text + both tokens
	q := srv.eachHarness()[1].Spirits.Queued()
	if len(q) != 1 {
		t.Fatalf("spool: %+v", q)
	}
	for _, want := range []string{"paint the fence", "[todo:: inbox/paint-the-fence]", "[phase:: plan]", "PROTOCOL:"} {
		if !strings.Contains(q[0].Request, want) {
			t.Fatalf("work order missing %q:\n%s", want, q[0].Request)
		}
	}
	// the index reads it back as plan-queued with the phase recovered
	d, ok := srv.delegationIndex()["inbox/paint-the-fence"]
	if !ok || d.State != "plan-queued" || d.Phase != "plan" || d.Harness != "hermes" {
		t.Fatalf("index: %+v", d)
	}
}

func TestFireGuards(t *testing.T) {
	srv := fireFixture(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/tasks/fire", strings.NewReader(body))
		w := httptest.NewRecorder()
		srv.handleTaskFire(w, req)
		return w
	}
	// no record/plan yet → 400
	if w := post(`{"id":"inbox/paint-the-fence"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("fire without plan: %d", w.Code)
	}
	// a plan but a PERSON assignee → 400 (fire is the agent lane)
	if _, ok := srv.pinTaskID("inbox/paint-the-fence"); !ok {
		t.Fatal("pin")
	}
	if err := srv.setPlanAssignee("inbox/paint-the-fence", "RT"); err != nil {
		t.Fatal(err)
	}
	if err := srv.writePlanSection("todo-plans", "inbox/paint-the-fence", "plan", "1. sand\n2. paint"); err != nil {
		t.Fatal(err)
	}
	if w := post(`{"id":"inbox/paint-the-fence"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("fire without agent: %d %s", w.Code, w.Body.String())
	}
	// agent-assigned → fires: go-phase spool carries the PLAN BYTES + token
	if err := srv.setPlanAssignee("inbox/paint-the-fence", "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	if w := post(`{"id":"inbox/paint-the-fence"}`); w.Code != 200 {
		t.Fatalf("fire: %d %s", w.Code, w.Body.String())
	}
	q := srv.eachHarness()[1].Spirits.Queued()
	if len(q) != 1 {
		t.Fatalf("spool: %+v", q)
	}
	for _, want := range []string{"[phase:: go]", "1. sand", "APPROVED PLAN", "[todo:: inbox/paint-the-fence]"} {
		if !strings.Contains(q[0].Request, want) {
			t.Fatalf("go order missing %q:\n%s", want, q[0].Request)
		}
	}
	// the fire is on the thread, replayable
	th := srv.listThread("inbox/paint-the-fence")
	found := false
	for _, c := range th {
		if c.Action == "fire" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no fire entry: %+v", th)
	}
}
