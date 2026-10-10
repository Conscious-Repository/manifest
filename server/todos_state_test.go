package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"manifest/tasks"
	"manifest/teamportal"
)

// A rename keeps the task's identity: the panel that holds the id must still
// find the row (unpinned, the text-derived id moved and the panel lost it).
func TestTaskRenameKeepsID(t *testing.T) {
	srv, _ := unifiedHarness(t)
	rec := httptest.NewRecorder()
	srv.handleTaskUpdate(rec, httptest.NewRequest("POST", "/api/tasks/update",
		strings.NewReader(`{"id":"inbox/loose-personal-thing","text":"renamed thing"}`)))
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var v struct {
		Rows []struct{ ID, Text string } `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, r := range v.Rows {
		if r.ID == "inbox/loose-personal-thing" {
			if r.Text != "renamed thing" {
				t.Fatalf("renamed row text = %q", r.Text)
			}
			return
		}
	}
	t.Fatalf("renamed row lost its id: %+v", v.Rows)
}

// An owner edit from TASKS on an aion row clears a member's override, as the
// cockpit's own edit does — otherwise the AION board kept showing the
// member's value and the edit looked reverted there.
func TestTasksAionUpdateClearsTeamOverride(t *testing.T) {
	srv, live, team, baseID := liveFixture(t)
	st := tasks.NewStore(t.TempDir(), "to do.md", testWriteAbs)
	if err := os.WriteFile(st.Path(), []byte("# To Do\n\n## Inbox\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.tasksStore = st
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}
	if _, err := team.Patch(member, baseID, map[string]string{"owner": "MM"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.handleTaskUpdate(rec, httptest.NewRequest("POST", "/api/tasks/update",
		strings.NewReader(`{"id":"aion:`+baseID+`","owner":"RT"}`)))
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	for _, it := range live.EffectiveItems() {
		if it.ID == baseID {
			if it.Owner == nil || *it.Owner != "RT" {
				t.Fatalf("effective owner after TASKS edit = %v, want RT", it.Owner)
			}
			return
		}
	}
	t.Fatal("base item missing from the live projection")
}
