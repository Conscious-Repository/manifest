package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
	"manifest/threads"
)

// Agent-chat plan Phase 3 — the task board "do this" entry points: composer
// modes (Comment / Ask / Do), typed @mentions, the capture-bar address, the
// description section, and the panel's presence + proposals payload.

func drainHermesSpool(t *testing.T, srv *Server) {
	t.Helper()
	root := srv.eachHarness()[1].Spirits.Root()
	for _, f := range mustGlob(t, filepath.Join(root, "vessel", "spool", "*.json")) {
		_ = os.Remove(f)
	}
}

func TestComposerModes(t *testing.T) {
	srv := seedPersonasInto(t, loopFixture(t))
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits

	// Comment on an UNASSIGNED todo: record only, no assignment, no turn
	if _, err := srv.postAndDispatch(id, "comment", "", nil, nil, "just a note to self"); err != nil {
		t.Fatal(err)
	}
	if n := len(hermes.Queued()); n != 0 {
		t.Fatalf("comment must not spool, got %d", n)
	}
	if a := srv.readPlanRecord(id).Assignee; a != "" {
		t.Fatalf("comment must not assign: %q", a)
	}

	// Ask with no agent → auto-assigns Alfred and spends ONE info turn in the
	// thread (comment phase); the ask is the record with meta.mode
	if _, err := srv.postAndDispatch(id, "ask", "", nil, nil, "what zoning applies here?"); err != nil {
		t.Fatal(err)
	}
	if a := srv.readPlanRecord(id).Assignee; a != "agent:alfred" {
		t.Fatalf("ask must auto-assign the default agent: %q", a)
	}
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: comment]") ||
		!strings.Contains(q[0].Request, "[persona:: info]") ||
		!strings.Contains(q[0].Request, "what zoning applies here?") {
		t.Fatalf("ask spool: %+v", q)
	}
	th := srv.listThread(id)
	last := th[len(th)-1]
	if last.Action != threads.ActComment || last.Meta["mode"] != "ask" {
		t.Fatalf("ask must be recorded as a comment with meta.mode: %+v", last)
	}
	drainHermesSpool(t, srv)

	// a plain comment on the now-assigned todo stays silent (reply guard)
	if _, err := srv.postAndDispatch(id, "comment", "", nil, nil, "thanks"); err != nil {
		t.Fatal(err)
	}
	if n := len(hermes.Queued()); n != 0 {
		t.Fatalf("reply guard: plain comment must not spool, got %d", n)
	}

	// Do → plan-phase order carrying the text as the OWNER'S ASK (no empty
	// plan order first); fire stays explicit
	if _, err := srv.postAndDispatch(id, "do", "", nil, nil, "find me 10 gutter contractor options"); err != nil {
		t.Fatal(err)
	}
	q = hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: plan]") ||
		!strings.Contains(q[0].Request, "[persona:: plan]") ||
		!strings.Contains(q[0].Request, "OWNER'S ASK:\nfind me 10 gutter contractor options") {
		t.Fatalf("do spool: %+v", q)
	}
	if !srv.threads.private.HasAction(id, threads.ActFire, "") {
		// nothing fired — the go phase is the owner's explicit act
	} else {
		t.Fatal("do must never fire")
	}
	drainHermesSpool(t, srv)

	// unknown agent on Ask → error, nothing recorded
	before := len(srv.listThread(id))
	if _, err := srv.postAndDispatch(id, "ask", "agent:zeus", nil, nil, "hi"); err == nil {
		t.Fatal("unknown agent must be refused")
	}
	if len(srv.listThread(id)) != before {
		t.Fatal("refused ask must not land in the thread")
	}
}

func TestTypedMentionsAndFailClosed(t *testing.T) {
	srv := seedPersonasInto(t, loopFixture(t))
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits

	got := srv.textMentions("ping @alfred and @nobody, also @Alfred::plan and mail@example.com")
	want := []string{"agent:alfred", "agent:alfred::plan"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("textMentions = %v, want %v", got, want)
	}

	// `@alfred::plan` in a PLAIN comment opens the Do lifecycle
	if _, err := srv.postAndDispatch(id, "comment", "", nil, nil, "@alfred::plan draft the lender outreach"); err != nil {
		t.Fatal(err)
	}
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: plan]") {
		t.Fatalf("::plan mention must open Do: %+v", q)
	}
	if a := srv.readPlanRecord(id).Assignee; a != "agent:alfred" {
		t.Fatalf("::plan mention must assign: %q", a)
	}
	// the recorded comment carries the merged structural mention
	th := srv.listThread(id)
	last := th[len(th)-1]
	if len(last.Mentions) != 1 || last.Mentions[0] != "agent:alfred::plan" {
		t.Fatalf("typed mention must be recorded structurally: %+v", last.Mentions)
	}
	drainHermesSpool(t, srv)

	// an unknown name is prose: no turn, no assignment change
	if _, err := srv.postAndDispatch(id, "comment", "", nil, nil, "@zeus is not real"); err != nil {
		t.Fatal(err)
	}
	if n := len(hermes.Queued()); n != 0 {
		t.Fatalf("unknown @name must stay prose, got %d spools", n)
	}
}

func TestCaptureDispatch(t *testing.T) {
	srv := seedPersonasInto(t, loopFixture(t))
	hermes := srv.eachHarness()[1].Spirits

	cases := []struct{ in, text, agent, mode string }{
		{"find me 10 gutter contractor options @alfred", "find me 10 gutter contractor options", "agent:alfred", "ask"},
		{"find lenders for the 4848 and 4852 deals @alfred::plan", "find lenders for the 4848 and 4852 deals", "agent:alfred", "do"},
		{"shortlist movers !do", "shortlist movers", "agent:alfred", "do"},
		{"email @bob about the fence", "email @bob about the fence", "", ""},
		{"plain capture", "plain capture", "", ""},
	}
	for _, c := range cases {
		text, agent, mode := srv.captureDispatch(c.in)
		if text != c.text || agent != c.agent || mode != c.mode {
			t.Errorf("captureDispatch(%q) = (%q, %q, %q), want (%q, %q, %q)", c.in, text, agent, mode, c.text, c.agent, c.mode)
		}
	}

	// end to end: the capture creates the todo (address stripped), pins it,
	// posts the opening ask and spools the turn; the response names the id
	req := httptest.NewRequest("POST", "/api/tasks/item", strings.NewReader(`{"text":"find me 10 gutter contractor options @alfred"}`))
	w := httptest.NewRecorder()
	srv.handleTaskAdd(w, req)
	if w.Code != 200 {
		t.Fatalf("add: %d %s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	id, _ := res["created"].(string)
	if id != "inbox/find-me-10-gutter-contractor-options" {
		t.Fatalf("created id: %q (%v)", id, res["dispatchError"])
	}
	raw, _ := os.ReadFile(srv.tasksStore.Path())
	if strings.Contains(string(raw), "@alfred") {
		t.Fatalf("the address must not land in the todo text:\n%s", raw)
	}
	if !strings.Contains(string(raw), "[owner:: agent:alfred]") || !strings.Contains(string(raw), "[todo:: "+id+"]") {
		t.Fatalf("capture ask must assign + pin:\n%s", raw)
	}
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "[phase:: comment]") ||
		!strings.Contains(q[0].Request, "TASK (from your todo board): find me 10 gutter contractor options") {
		t.Fatalf("capture ask spool: %+v", q)
	}
	th := srv.listThread(id)
	if len(th) == 0 || th[len(th)-1].Text != "find me 10 gutter contractor options" || th[len(th)-1].Meta["mode"] != "ask" {
		t.Fatalf("opening ask must be the thread record: %+v", th)
	}
}

func TestDescriptionRidesOrdersAndHash(t *testing.T) {
	srv := seedPersonasInto(t, loopFixture(t))
	id := "inbox/research-zoning"
	hermes := srv.eachHarness()[1].Spirits

	h0 := srv.planCtxHash(id)
	req := httptest.NewRequest("POST", "/api/tasks/description", strings.NewReader(`{"id":"`+id+`","text":"Parcel 12; the buyer wants mixed-use."}`))
	w := httptest.NewRecorder()
	srv.handleTaskDescription(w, req)
	if w.Code != 200 {
		t.Fatalf("description: %d %s", w.Code, w.Body.String())
	}
	rec := srv.readPlanRecord(id)
	if rec.Description != "Parcel 12; the buyer wants mixed-use." || rec.Plan != "" {
		t.Fatalf("record: %+v", rec)
	}
	if srv.planCtxHash(id) == h0 {
		t.Fatal("a changed description must change the plan-context hash")
	}
	// the record keeps both sections; a plan write leaves the description intact
	if err := srv.writePlanSection("todo-plans", id, "plan", "1. call the county"); err != nil {
		t.Fatal(err)
	}
	rec = srv.readPlanRecord(id)
	if rec.Description == "" || rec.Plan != "1. call the county" {
		t.Fatalf("sections must not collide: %+v", rec)
	}
	// every work order carries it
	if _, err := srv.postAndDispatch(id, "do", "", nil, nil, "plan it"); err != nil {
		t.Fatal(err)
	}
	q := hermes.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "DESCRIPTION (the owner's context for this task):\nParcel 12; the buyer wants mixed-use.") {
		t.Fatalf("description must ride the order: %+v", q)
	}
}

func TestPanelPresenceAndProposals(t *testing.T) {
	srv := seedPersonasInto(t, loopFixture(t))
	id := "inbox/research-zoning"
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	srv.UseApprovals(store)

	panel := func() map[string]any {
		req := httptest.NewRequest("GET", "/api/tasks/panel?id="+id, nil)
		w := httptest.NewRecorder()
		srv.handleTaskPanel(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if p := panel(); p["inflight"] != nil || len(p["proposals"].([]any)) != 0 {
		t.Fatalf("quiet panel: %+v", p)
	}
	// a queued plan turn → presence, derived from the spool
	if _, err := srv.postAndDispatch(id, "do", "", nil, nil, "plan it"); err != nil {
		t.Fatal(err)
	}
	p := panel()
	inflight, _ := p["inflight"].(map[string]any)
	if inflight == nil || inflight["name"] != "Alfred" || inflight["phase"] != "plan" {
		t.Fatalf("presence: %+v", p["inflight"])
	}
	// a pending proposal carrying the todo token → the review link's payload
	if _, err := store.Propose(approvals.Proposal{Agent: "hermes", Ritual: "delegate", Action: "create vault note Vendor Shortlist.md [todo:: " + id + "]", Body: "- acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Propose(approvals.Proposal{Agent: "hermes", Ritual: "delegate", Action: "create vault note Other.md [todo:: inbox/other]", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	props := panel()["proposals"].([]any)
	if len(props) != 1 || props[0].(map[string]any)["action"] != "create vault note Vendor Shortlist.md" {
		t.Fatalf("proposals: %+v", props)
	}
}
