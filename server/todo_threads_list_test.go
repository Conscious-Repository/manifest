package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"manifest/threads"
)

// The CHAT rail's task rows: every task with a visible conversation, newest
// first, titled by the task's words (the id when the record is unknown),
// with the assignee, the newest comment and the comment count; a thread
// that holds only hidden markers is not a conversation (2026-09-21).
func TestTaskThreadsList(t *testing.T) {
	srv, _ := panelFixture(t)
	me := srv.ownerIdentity()
	base := time.Date(2026, 9, 21, 4, 0, 0, 0, time.UTC)
	if _, err := srv.threads.private.Add(me, "manifest/consider-opencode", threads.ActComment, "consider if opencode would be valuable?", nil, nil, nil, base); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.threads.private.Add(threads.Identity{ID: "agent:alfred", Name: "Alfred"}, "manifest/consider-opencode", threads.ActComment, "plan attached to this task", nil, nil, nil, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.threads.private.Add(me, "personal/older", threads.ActComment, "first", nil, nil, nil, base.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.threads.private.Add(me, "inbox/marker-only", "turn-open", "", nil, nil, map[string]any{"marker": true}, base); err != nil {
		t.Fatal(err)
	}
	if err := srv.ensurePlanRecord("manifest/consider-opencode", "agent:alfred"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.handleTaskThreads(w, httptest.NewRequest("GET", "/api/tasks/threads", nil))
	var out struct{ Threads []taskThreadRow }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Threads) != 2 {
		t.Fatalf("want 2 conversations (the marker-only thread is none), got %+v", out.Threads)
	}
	first := out.Threads[0]
	if first.ID != "manifest/consider-opencode" || first.Domain != "manifest" || first.Agent != "agent:alfred" || first.Comments != 2 || first.LastAuthor != "Alfred" || !first.Open {
		t.Fatalf("first = %+v", first)
	}
	if first.Title != "manifest/consider-opencode" {
		t.Fatalf("an unknown task keeps its id as the title: %q", first.Title)
	}
	if first.LastText != "plan attached to this task" {
		t.Fatalf("lastText = %q", first.LastText)
	}
	if out.Threads[1].ID != "personal/older" || out.Threads[1].Comments != 1 {
		t.Fatalf("second = %+v", out.Threads[1])
	}
	if taskDomain("aion:56b66ee0") != "aion" || taskDomain("re:aion-bl/x") != "re" || taskDomain("loose") != "" {
		t.Fatal("taskDomain")
	}
}
