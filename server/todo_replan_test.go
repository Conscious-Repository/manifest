package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/threads"
)

func TestPlanCtxHashStability(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	h1 := srv.planCtxHash(id)
	if h1 == "" || len(h1) != 12 {
		t.Fatalf("hash shape: %q", h1)
	}
	if h2 := srv.planCtxHash(id); h2 != h1 {
		t.Fatalf("same inputs must hash the same: %q vs %q", h1, h2)
	}
	if err := srv.writePlanSection("todo-plans", id, "description", "the deadline moved to Friday"); err != nil {
		t.Fatal(err)
	}
	if h3 := srv.planCtxHash(id); h3 == h1 {
		t.Fatal("a description edit must change the hash")
	}
}

// materializeBaseline runs a plan-phase completion through the sweep so the
// ActPlan marker carries a ctx baseline.
func materializeBaseline(t *testing.T, srv *Server, id, runID string) {
	t.Helper()
	fakeRunReq(t, srv, runID, "draft [todo:: "+id+"] [phase:: plan] [persona:: plan]",
		"# Plan\n\n1. Pull the overlay.\n2. Write the memo.")
	sweep(srv)
	if rec := srv.readPlanRecord(id); !strings.Contains(rec.Plan, "Pull the overlay") {
		t.Fatalf("baseline plan must materialize: %+v", rec)
	}
}

func drainSpool(t *testing.T, srv *Server) {
	t.Helper()
	hermes := srv.eachHarness()[1].Spirits
	for _, f := range mustGlob(t, filepath.Join(hermes.Root(), "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}
}

func TestReplanOnContextChange(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	materializeBaseline(t, srv, id, "r20")
	drainSpool(t, srv)
	hermes := srv.eachHarness()[1].Spirits

	// unchanged world → no replan
	sweep(srv)
	if q := hermes.Queued(); len(q) != 0 {
		t.Fatalf("unchanged context must not spool: %+v", q)
	}

	// the todo's TEXT changes under the plan
	if err := os.WriteFile(srv.todosStore.Path(),
		[]byte("# To Do\n\n## Inbox\n- [ ] research zoning AND parking minimums [todo:: inbox/research-zoning] [owner:: agent:hermes] [added:: 2026-08-14]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweep(srv)
	q := hermes.Queued()
	if len(q) != 1 {
		t.Fatalf("changed context must spool exactly one replan: %+v", q)
	}
	req := q[0].Request
	for _, want := range []string{"REPLAN — the task context changed", "[phase:: plan]", "[persona:: plan]", "Pull the overlay"} {
		if !strings.Contains(req, want) {
			t.Fatalf("replan order missing %q:\n%s", want, req)
		}
	}

	// same change, sweep again: the ctx throttle holds (and the queued spool
	// parks the state anyway)
	sweep(srv)
	if q := hermes.Queued(); len(q) != 1 {
		t.Fatalf("replan must be one attempt per distinct change: %+v", q)
	}
	drainSpool(t, srv)
	sweep(srv)
	if q := hermes.Queued(); len(q) != 0 {
		t.Fatalf("ctx throttle must hold after the spool drains: %+v", q)
	}

	// the replan run completes with a new brief → plan REPLACED + trace comment
	fakeRunReqAt(t, srv, "r21", "draft [todo:: "+id+"] [phase:: plan] [persona:: plan]",
		"# Plan v2\n\n1. Pull the overlay AND parking code.\n2. Write the memo.", "2026-08-15T06:00:00Z")
	sweep(srv)
	rec := srv.readPlanRecord(id)
	if !strings.Contains(rec.Plan, "parking code") {
		t.Fatalf("replan brief must replace the plan: %+v", rec)
	}
	found := false
	for _, c := range srv.listThread(id) {
		if strings.Contains(c.Text, "plan replaced — the task context changed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing the replacement trace comment: %+v", srv.listThread(id))
	}
	// second ingestion no-ops
	before := len(srv.listThread(id))
	sweep(srv)
	if after := len(srv.listThread(id)); after != before {
		t.Fatalf("re-ingestion duplicated: %d → %d", before, after)
	}
}

func TestReplanNegativeGates(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits

	// no baseline ctx (plan written by hand, no ActPlan marker) → no replan
	if _, ok := srv.pinTodoID(id); !ok {
		t.Fatal("pin")
	}
	if err := srv.setPlanAssignee(id, "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	if err := srv.writePlanSection("todo-plans", id, "plan", "1. hand-written plan"); err != nil {
		t.Fatal(err)
	}
	sweep(srv)
	if q := hermes.Queued(); len(q) != 0 {
		t.Fatalf("no baseline → no replan: %+v", q)
	}

	// with a baseline but a fresh owner comment → hold off
	materializeBaseline(t, srv, id, "r30")
	drainSpool(t, srv)
	if err := srv.writePlanSection("todo-plans", id, "description", "deadline moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActComment, "hold on, rethinking this", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	sweep(srv)
	// the owner comment ALSO triggers the relay lane (assigned todo), so
	// filter for the replan shape specifically
	for _, q := range hermes.Queued() {
		if strings.Contains(q.Request, "REPLAN —") {
			t.Fatalf("fresh owner comment must suppress the replan: %+v", q)
		}
	}

	// go-queued parks replans: drain, then leave a go spool in the queue
	drainSpool(t, srv)
	if err := srv.spoolTodoWorkOrder(srv.findHarness("hermes"), id, "go", "the plan", ""); err != nil {
		t.Fatal(err)
	}
	sweep(srv)
	for _, q := range hermes.Queued() {
		if strings.Contains(q.Request, "REPLAN —") {
			t.Fatalf("go-queued must suppress the replan: %+v", q)
		}
	}
}
