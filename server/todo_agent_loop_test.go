package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/spirits"
	"manifest/tasks"
	"manifest/threads"
)

// loopFixture: fire fixture + helpers that fake completed hermes runs (trace
// files only — exactly what the engine leaves behind).
func loopFixture(t *testing.T) *Server {
	t.Helper()
	srv, _ := panelFixture(t)
	dir := t.TempDir()
	st := tasks.NewStore(dir, "to do.md", testWriteAbs)
	if err := os.WriteFile(st.Path(), []byte("# To Do\n\n## Inbox\n- [ ] research zoning [added:: 2026-08-14]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.tasksStore = st
	hermes := spirits.NewStore(t.TempDir())
	srv.UseHarnesses([]Harness{{Name: "excalibur"}, {Name: "hermes", Spirits: hermes}})
	return srv
}

// fakeRun drops a completed run report + library brief into the hermes tree.
func fakeRun(t *testing.T, srv *Server, runID, taskID, phase, briefBody string) {
	t.Helper()
	root := srv.eachHarness()[1].Spirits.Root()
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "library"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := fmt.Sprintf(`---
run: %s
spirit: hermes
ritual: delegate
request: "work [todo:: %s] [phase:: %s]"
started: 2026-08-15T05:00:00Z
finished: 2026-08-15T05:01:00Z
outcome: completed
---
ran
`, runID, taskID, phase)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "runs", "2026-08-15-hermes-"+runID+".md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	brief := fmt.Sprintf(`---
title: brief %s
run: %s
date: 2026-08-15T05:01:00Z
---
%s
`, runID, runID, briefBody)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "library", runID+"-brief.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sweep(srv *Server) { srv.agentLoopSweep(srv.delegationIndex()) }

func TestQuestionsBriefBecomesComment(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	fakeRun(t, srv, "r1", id, "comment", "# questions\n\n1. Which parcel?\n2. What deadline?")
	sweep(srv)
	th := srv.listThread(id)
	if len(th) != 1 || !strings.Contains(th[0].Text, "Which parcel?") || th[0].Author != "agent:hermes" {
		t.Fatalf("questions comment: %+v", th)
	}
	if rec := srv.readPlanRecord(id); rec.Exists && strings.TrimSpace(rec.Plan) != "" {
		t.Fatalf("questions must NOT materialize a plan: %+v", rec)
	}
	// idempotent on re-sweep
	sweep(srv)
	if th2 := srv.listThread(id); len(th2) != 1 {
		t.Fatalf("re-sweep duplicated: %+v", th2)
	}
	// the questions signal pages; after the owner answers it clears
	sigs, _ := planReadyEmitter{srv}.Emit(time.Now())
	found := false
	for _, sg := range sigs {
		if sg.Kind == "agent-questions" && sg.GoalID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("no agent-questions signal: %+v", sigs)
	}
	_, _ = srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActComment, "parcel 12, no deadline", nil, nil, nil)
	sigs, _ = planReadyEmitter{srv}.Emit(time.Now())
	for _, sg := range sigs {
		if sg.Kind == "agent-questions" && sg.GoalID == id {
			t.Fatalf("answered questions must clear the card: %+v", sg)
		}
	}
}

func TestCommentPhasePlanMaterializes(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	// the owner's exact bug: a PLAN arriving via a comment-phase run
	fakeRun(t, srv, "r2", id, "comment", "# Plan\n\n1. Pull the zoning map.\n2. Draft the memo.")
	sweep(srv)
	rec := srv.readPlanRecord(id)
	if !strings.Contains(rec.Plan, "Pull the zoning map") {
		t.Fatalf("comment-phase plan must materialize: %+v", rec)
	}
	th := srv.listThread(id)
	if len(th) != 1 || !strings.Contains(th[0].Text, "plan attached") {
		t.Fatalf("missing 'plan attached' comment: %+v", th)
	}
	if r, _ := th[0].Meta["artifactRef"].(string); r == "" {
		t.Fatalf("brief must be linked on the comment: %+v", th[0].Meta)
	}
	// plan-ready signal pages (state was done+comment, not plan-ready)
	sigs, _ := planReadyEmitter{srv}.Emit(time.Now())
	found := false
	for _, sg := range sigs {
		if sg.Kind == "plan-ready" && sg.GoalID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("no plan-ready signal: %+v", sigs)
	}
}

func TestEmbeddedQuestionsSplitOut(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	fakeRun(t, srv, "r3", id, "plan", "# Plan\n\n1. Do the thing.\n\n## Open questions\n\n- Which vendor?")
	sweep(srv)
	rec := srv.readPlanRecord(id)
	if !strings.Contains(rec.Plan, "Do the thing") {
		t.Fatalf("plan must materialize despite embedded questions: %+v", rec)
	}
	th := srv.listThread(id)
	if len(th) != 2 {
		t.Fatalf("want plan-attached + split-out questions comments: %+v", th)
	}
	if !strings.Contains(th[1].Text, "Which vendor?") {
		t.Fatalf("questions not split out: %+v", th[1])
	}
}

func TestRelayAlwaysAndAutoAssign(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits
	// mention on an UNASSIGNED todo → auto-assign + comment-phase spool
	srv.threadDialogHook(id, []string{"agent:hermes"}, "take a pass at a plan?")
	if got := srv.readPlanRecord(id).Assignee; got != "agent:hermes" {
		t.Fatalf("mention must auto-assign: %q", got)
	}
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: comment]") ||
		!strings.Contains(q[0].Request, "take a pass at a plan?") {
		t.Fatalf("auto-assign spool: %+v", q)
	}
	raw, _ := os.ReadFile(srv.tasksStore.Path())
	if !strings.Contains(string(raw), "[owner:: agent:hermes]") {
		t.Fatalf("owner token missing:\n%s", raw)
	}
	// drain the spool (simulate the engine consuming it). A PLAIN comment on
	// the assigned todo is the record only — the reply guard (agent-chat plan
	// Q6) never spends a turn on it
	for _, f := range mustGlob(t, filepath.Join(hermes.Root(), "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}
	srv.threadDialogHook(id, nil, "prefer the cheaper vendor")
	if q = hermes.Queued(); len(q) != 0 {
		t.Fatalf("plain comment must NOT relay (reply guard): %+v", q)
	}
	// a typed @mention in free text IS an ask — one comment-phase turn
	srv.threadDialogHook(id, nil, "@hermes prefer the cheaper vendor")
	q = hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "prefer the cheaper vendor") ||
		!strings.Contains(q[0].Request, "[phase:: comment]") {
		t.Fatalf("typed mention must relay as an ask: %+v", q)
	}
	// context rides along: protocol
	if !strings.Contains(q[0].Request, "PROTOCOL:") {
		t.Fatalf("relay missing protocol: %s", q[0].Request)
	}
}

func TestRelaySweepRetries(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	if err := srv.setPlanAssignee(id, "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	// a queued spool makes SpoolRunNow refuse → the owner's ASK relay drops
	// (parked as a relay-pending marker)
	if err := srv.spoolTaskWorkOrder(srv.findHarness("hermes"), id, "plan", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.postAndDispatch(id, "ask", "", nil, nil, "answer: use vendor B"); err != nil {
		t.Fatal(err)
	}
	if n := len(hermes.Queued()); n != 1 {
		t.Fatalf("refused relay should leave one spool, got %d", n)
	}
	if !srv.threads.private.HasAction(id, actRelayPending, "") {
		t.Fatal("refused ask must leave a relay-pending marker")
	}
	// while the spool is still queued the sweep waits (agent busy)
	sweep(srv)
	if n := len(hermes.Queued()); n != 1 {
		t.Fatalf("sweep must not double-spool while busy, got %d", n)
	}
	// engine consumes the old spool; the sweep retries the missed relay
	for _, f := range mustGlob(t, filepath.Join(hermes.Root(), "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}
	sweep(srv)
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: comment]") ||
		!strings.Contains(q[0].Request, "answer: use vendor B") {
		t.Fatalf("sweep must retry the relay: %+v", q)
	}
	// and only once: the closing relay marker supersedes the pending one
	for _, f := range mustGlob(t, filepath.Join(hermes.Root(), "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}
	sweep(srv)
	if n := len(hermes.Queued()); n != 0 {
		t.Fatalf("a retried relay must not repeat, got %d", n)
	}
}

func mustGlob(t *testing.T, pat string) []string {
	t.Helper()
	m, err := filepath.Glob(pat)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
