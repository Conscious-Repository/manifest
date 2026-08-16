package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// personaFixture: loopFixture + the three seed personas materialized under
// <vault>/system/agents/personas and wired via UsePersonas.
func personaFixture(t *testing.T) *Server {
	t.Helper()
	srv := loopFixture(t)
	return seedPersonasInto(t, srv)
}

func seedPersonasInto(t *testing.T, srv *Server) *Server {
	t.Helper()
	dir := t.TempDir()
	for intent, body := range SeedPersonas {
		if err := os.WriteFile(filepath.Join(dir, intent+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv.UsePersonas(dir, "system/agents/personas")
	return srv
}

func TestPersonasParseAndLint(t *testing.T) {
	srv := personaFixture(t)
	all := srv.personas()
	for _, intent := range []string{"brief", "info", "plan"} {
		p, ok := all[intent]
		if !ok || !p.Enabled || p.Prompt == "" || p.Rel != "system/agents/personas/"+intent+".md" {
			t.Fatalf("%s: %+v", intent, p)
		}
		if p.Model != "" { // the Phase 3 slot parses empty from the seeds
			t.Fatalf("%s model should be empty: %q", intent, p.Model)
		}
	}
	// intent/stem mismatch → skipped
	dir := srv.personasCfg.absDir
	if err := os.WriteFile(filepath.Join(dir, "wrong.md"), []byte("---\nintent: other\nenabled: true\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// disabled → parsed but not resolvable
	if err := os.WriteFile(filepath.Join(dir, "quiet.md"), []byte("---\nintent: quiet\nenabled: false\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	all = srv.personas()
	if _, ok := all["other"]; ok {
		t.Fatal("stem-mismatched persona must be skipped")
	}
	if _, ok := all["wrong"]; ok {
		t.Fatal("stem-mismatched persona must be skipped under its stem too")
	}
	if p, ok := all["quiet"]; !ok || p.Enabled {
		t.Fatalf("disabled persona should list as disabled: %+v", p)
	}
	if _, ok := srv.persona("quiet"); ok {
		t.Fatal("persona() must not resolve a disabled intent")
	}
	// a persona with a model value round-trips and stays inert (Phase 3 pin)
	if err := os.WriteFile(filepath.Join(dir, "fancy.md"), []byte("---\nintent: fancy\nmodel: deepseek-r2\nenabled: true\n---\nreply fancily\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, ok := srv.persona("fancy"); !ok || p.Model != "deepseek-r2" {
		t.Fatalf("model slot: %+v", p)
	}
}

func TestSplitAgentToken(t *testing.T) {
	cases := []struct{ in, base, intent string }{
		{"agent:hermes", "agent:hermes", ""},
		{"agent:hermes::brief", "agent:hermes", "brief"},
		{"agent:hermes::", "agent:hermes", ""},
		{"benjamin@aion.bio", "benjamin@aion.bio", ""},
		{"BA", "BA", ""},
	}
	for _, c := range cases {
		b, i := splitAgentToken(c.in)
		if b != c.base || i != c.intent {
			t.Fatalf("%q → (%q,%q), want (%q,%q)", c.in, b, i, c.base, c.intent)
		}
	}
}

func TestSpoolPersonaRequestShape(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits
	h := srv.findHarness("hermes")

	// brief intent: persona preamble + reply-shape protocol + tokens
	if err := srv.spoolTaskWorkOrder(h, id, personaPhase("brief"), "how deep is the setback?", "brief"); err != nil {
		t.Fatal(err)
	}
	q := hermes.Queued()
	if len(q) != 1 {
		t.Fatalf("queued: %+v", q)
	}
	req := q[0].Request
	for _, want := range []string{"PERSONA (how to respond", "[persona:: brief]", "[phase:: comment]",
		"reply in ONE library brief that IS your answer"} {
		if !strings.Contains(req, want) {
			t.Fatalf("missing %q in:\n%s", want, req)
		}
	}
	if strings.Contains(req, "QUESTIONS") || strings.Contains(req, "numbered plan") {
		t.Fatalf("brief intent must not carry the plan/questions protocol:\n%s", req)
	}
	for _, f := range mustGlob(t, filepath.Join(hermes.Root(), "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}

	// empty intent: today's request exactly — no persona traces
	if err := srv.spoolTaskWorkOrder(h, id, "comment", "plain relay", ""); err != nil {
		t.Fatal(err)
	}
	q = hermes.Queued()
	req = q[0].Request
	if strings.Contains(req, "PERSONA") || strings.Contains(req, "[persona::") {
		t.Fatalf("empty intent must not mention personas:\n%s", req)
	}
	if !strings.Contains(req, "PROTOCOL: your library brief must be exactly ONE of") {
		t.Fatalf("empty intent keeps the classic protocol:\n%s", req)
	}
}

// fakeRunReq drops a completed run report with a fully custom request line.
func fakeRunReq(t *testing.T, srv *Server, runID, request, briefBody string) {
	t.Helper()
	fakeRunReqAt(t, srv, runID, request, briefBody, "2026-08-15T05:00:00Z")
}

// fakeRunReqAt is fakeRunReq with an explicit start time — the delegation
// index prefers the NEWEST run per todo, so successive fixture runs need
// distinct starts.
func fakeRunReqAt(t *testing.T, srv *Server, runID, request, briefBody, started string) {
	t.Helper()
	root := srv.eachHarness()[1].Spirits.Root()
	for _, d := range []string{"runs", "library"} {
		if err := os.MkdirAll(filepath.Join(root, "artifacts", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	report := fmt.Sprintf("---\nrun: %s\nspirit: hermes\nritual: delegate\nrequest: %q\nstarted: %s\nfinished: %s\noutcome: completed\n---\nran\n", runID, request, started, started)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "runs", "2026-08-15-hermes-"+runID+".md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	brief := fmt.Sprintf("---\ntitle: brief %s\nrun: %s\ndate: 2026-08-15T05:01:00Z\n---\n%s\n", runID, runID, briefBody)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "library", runID+"-brief.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBriefPersonaReplyIngestion(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	fakeRunReq(t, srv, "r10", "ask [todo:: "+id+"] [phase:: comment] [persona:: brief]",
		"The setback is 25 feet on Bayard — from the 2019 overlay.")
	sweep(srv)
	th := srv.listThread(id)
	if len(th) != 1 || th[0].Author != "agent:hermes" || !strings.Contains(th[0].Text, "25 feet") {
		t.Fatalf("brief reply must land as ONE agent comment: %+v", th)
	}
	if p, _ := th[0].Meta["persona"].(string); p != "brief" {
		t.Fatalf("comment meta must carry the persona: %+v", th[0].Meta)
	}
	if rec := srv.readPlanRecord(id); strings.TrimSpace(rec.Plan) != "" {
		t.Fatalf("brief reply must NEVER touch the plan: %+v", rec)
	}
	sweep(srv) // ActReply marker makes it idempotent
	if th2 := srv.listThread(id); len(th2) != 1 {
		t.Fatalf("re-sweep duplicated the reply: %+v", th2)
	}
}

func TestPlanPersonaStillMaterializes(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	fakeRunReq(t, srv, "r11", "draft [todo:: "+id+"] [phase:: plan] [persona:: plan]",
		"# Plan\n\n1. Pull the overlay.\n2. Write the memo.")
	sweep(srv)
	rec := srv.readPlanRecord(id)
	if !strings.Contains(rec.Plan, "Pull the overlay") {
		t.Fatalf("plan persona must materialize: %+v", rec)
	}
	th := srv.listThread(id)
	if len(th) != 1 || !strings.Contains(th[0].Text, "plan attached") {
		t.Fatalf("plan-attached comment: %+v", th)
	}
	if p, _ := th[0].Meta["persona"].(string); p != "plan" {
		t.Fatalf("plan comment meta persona: %+v", th[0].Meta)
	}
}

func TestPersonaTokenRecoveryFromBrief(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	// the report's request is TRUNCATED past both trailing tokens — recovery
	// must ride the brief (which carries them near the top, per the ritual)
	fakeRunReq(t, srv, "r12", "ask about setbacks [todo:: "+id+"]",
		"[phase:: comment] [persona:: brief]\n\nThe setback is 25 feet.")
	d := srv.delegationIndex()[id]
	if d.Persona != "brief" || d.Phase != "comment" {
		t.Fatalf("token recovery from brief: %+v", d)
	}
}

func TestAssignRejectsIntentToken(t *testing.T) {
	srv := personaFixture(t)
	if _, ok := srv.pinTaskID("inbox/research-zoning"); !ok {
		t.Fatal("pin")
	}
	// the handler path guards it; the model-level guard is agentHarness
	if h := srv.agentHarness("agent:hermes::brief"); h != "" {
		t.Fatalf("agentHarness must reject suffixed tokens: %q", h)
	}
}

func TestMentionIntentRidesRelay(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits
	// intent-tagged mention on an unassigned todo: auto-assign to the BASE
	// token, spool carries the persona
	srv.threadDialogHook(id, []string{"agent:hermes::brief"}, "what's the setback?")
	if got := srv.readPlanRecord(id).Assignee; got != "agent:hermes" {
		t.Fatalf("auto-assign must use the bare token: %q", got)
	}
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[persona:: brief]") ||
		!strings.Contains(q[0].Request, "[phase:: comment]") {
		t.Fatalf("intent must ride the spool: %+v", q)
	}
	raw, _ := os.ReadFile(srv.tasksStore.Path())
	if strings.Contains(string(raw), "::brief") {
		t.Fatalf("[owner::] must never carry an intent:\n%s", raw)
	}
}
