package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/spirits"
	"manifest/tasks"
)

func testWriteAbs(abs string, data []byte) error { return os.WriteFile(abs, data, 0o644) }

// assignFixture: panelFixture + a personal todos store + a hermes harness.
func assignFixture(t *testing.T) (*Server, string) {
	t.Helper()
	srv, vault := panelFixture(t)
	dir := t.TempDir()
	st := tasks.NewStore(dir, "to do.md", testWriteAbs)
	if err := os.WriteFile(st.Path(), []byte("# To Do\n\n## Inbox\n- [ ] wire the fence [added:: 2026-08-14]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.tasksStore = st
	// hermes needs a real spirits store — assignment spools the plan-phase
	// work order into it (Phase 4)
	srv.UseHarnesses([]Harness{{Name: "excalibur"}, {Name: "hermes", Spirits: spirits.NewStore(t.TempDir())}})
	return srv, vault
}

func TestAgentsRoster(t *testing.T) {
	srv, _ := assignFixture(t)
	// the do-bot is addressed as Alfred (agent-chat plan Q2); agent:hermes
	// stays a resolving alias for existing [owner::] data
	roster := srv.agentRoster()
	if len(roster) != 1 || roster[0]["id"] != "agent:alfred" || roster[0]["name"] != "Alfred" || roster[0]["harness"] != "hermes" {
		t.Fatalf("roster: %+v", roster)
	}
	if srv.agentHarness("agent:hermes") != "hermes" || srv.agentHarness("agent:alfred") != "hermes" {
		t.Fatal("agent:hermes and agent:alfred must both resolve to the hermes harness")
	}
	if srv.defaultAgentToken() != "agent:alfred" {
		t.Fatalf("default agent: %q", srv.defaultAgentToken())
	}
	if srv.agentHarness("agent:zeus") != "" {
		t.Fatal("unknown agent must not resolve")
	}
	// agent-assigned todos stay mine (rows keep their place)
	if !srv.isMine("agent:hermes") {
		t.Fatal("agent-owned rows must stay mine")
	}
}

// A teammate whose initials spell the "me" sentinel (ME = Matthias Estermann)
// owes his own tasks — they belong under Outstanding, not on my list.
func TestIsMineInitialsCollision(t *testing.T) {
	srv, _ := assignFixture(t)
	srv.UseOwner("BA")
	for _, owner := range []string{"ME", "HZ", "HG/ME"} {
		if srv.isMine(owner) {
			t.Errorf("isMine(%q) = true, want false", owner)
		}
	}
	for _, owner := range []string{"", "me", "BA", "BA/RT"} {
		if !srv.isMine(owner) {
			t.Errorf("isMine(%q) = false, want true", owner)
		}
	}
}

func TestAssignFlow(t *testing.T) {
	srv, _ := assignFixture(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/tasks/assign", strings.NewReader(body))
		w := httptest.NewRecorder()
		srv.handleTaskAssign(w, req)
		return w
	}
	// unknown agent → 400, nothing written
	if w := post(`{"id":"inbox/wire-the-fence","owner":"agent:zeus"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown agent: %d %s", w.Code, w.Body.String())
	}
	// agent assign: pins id, owner token verbatim in the file, record + thread
	if w := post(`{"id":"inbox/wire-the-fence","owner":"agent:hermes"}`); w.Code != 200 {
		t.Fatalf("assign: %d %s", w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile(srv.tasksStore.Path())
	if !strings.Contains(string(raw), "[todo:: inbox/wire-the-fence]") {
		t.Fatalf("assign must pin identity:\n%s", raw)
	}
	if !strings.Contains(string(raw), "[owner:: agent:hermes]") {
		t.Fatalf("agent token must land verbatim:\n%s", raw)
	}
	rec := srv.readPlanRecord("inbox/wire-the-fence")
	if !rec.Exists || rec.Assignee != "agent:hermes" {
		t.Fatalf("record assignee: %+v", rec)
	}
	th := srv.listThread("inbox/wire-the-fence")
	if len(th) != 1 || th[0].Action != "assign" || th[0].Meta["assignee"] != "agent:hermes" {
		t.Fatalf("assign thread entry: %+v", th)
	}
	// person assign on the same todo: token verbatim, record follows
	if w := post(`{"id":"inbox/wire-the-fence","owner":"RT"}`); w.Code != 200 {
		t.Fatalf("person assign: %d %s", w.Code, w.Body.String())
	}
	raw, _ = os.ReadFile(srv.tasksStore.Path())
	if !strings.Contains(string(raw), "[owner:: RT]") {
		t.Fatalf("person owner must land:\n%s", raw)
	}
	// the record file survives both writes with sections intact
	if _, err := os.Stat(filepath.Join(srv.tasksStore.Path())); err != nil {
		t.Fatal(err)
	}
}
