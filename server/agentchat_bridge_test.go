package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/chatthreads"
	"manifest/spirits"
	"manifest/threads"
)

// Agent-chat plan Phase 4 — the chat ↔ task bridges (§3.4f): "→ task" from
// an agent turn and "open in chat" from a task thread.

func TestPromoteChatToTask(t *testing.T) {
	srv := seedPersonasInto(t, loopFixture(t))
	st := agentchat.New(filepath.Join(t.TempDir(), "chats"))
	srv.UseAgentChat(st)
	hermes := srv.eachHarness()[1].Spirits

	id, err := st.Create("alfred", "", "gutter contractors", "")
	if err != nil {
		t.Fatal(err)
	}
	mustTurn := func(who, text string) {
		t.Helper()
		if _, err := st.AppendTurn("alfred", id, who, text, 0); err != nil {
			t.Fatal(err)
		}
	}
	mustTurn("user", "find me 10 gutter contractor options")
	mustTurn("alfred", "### Step 1 — say\n\nHere are three to start: A, B, C.")
	mustTurn("user", "later — not part of the task")

	// → task on turn 2: the todo is created through the capture path (Inbox,
	// title as the line), pinned, the window UP TO turn 2 copied, the agent
	// assigned — and NO turn spent
	req := httptest.NewRequest("POST", "/api/agents/chat/alfred/sessions/"+id+"/promote", strings.NewReader(`{"turn":2}`))
	req.SetPathValue("agent", "alfred")
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	srv.handleAgentChatPromote(w, req)
	if w.Code != 200 {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	taskID, _ := res["created"].(string)
	if taskID != "inbox/gutter-contractors" || res["assigned"] != true || res["copied"] != float64(2) || res["name"] != "Alfred" {
		t.Fatalf("promote response: %+v", res)
	}
	raw, _ := os.ReadFile(srv.tasksStore.Path())
	if !strings.Contains(string(raw), "[owner:: agent:alfred]") || !strings.Contains(string(raw), "[todo:: "+taskID+"]") {
		t.Fatalf("promote must assign + pin:\n%s", raw)
	}
	if n := len(hermes.Queued()); n != 0 {
		t.Fatalf("promote is record-only — must not spool, got %d", n)
	}
	if a := srv.readPlanRecord(taskID).Assignee; a != "agent:alfred" {
		t.Fatalf("plan record assignee = %q", a)
	}

	th := srv.listThread(taskID)
	if len(th) != 3 {
		t.Fatalf("thread: want 2 copied entries + assign, got %d: %+v", len(th), th)
	}
	first, second, third := th[0], th[1], th[2]
	if first.Author == "agent:alfred" || first.Text != "find me 10 gutter contractor options" || first.Meta["from"] != "chat" {
		t.Fatalf("first entry must be the owner's turn: %+v", first)
	}
	link, _ := first.Meta["chat"].(map[string]any)
	if link == nil || link["agent"] != "alfred" || link["id"] != id || link["title"] != "gutter contractors" {
		t.Fatalf("first entry must carry the chat link: %+v", first.Meta)
	}
	// the agent's turn posts under the id its LIVE replies use (agent:hermes
	// is Alfred's canonical thread id, agentTokenIdentity), so one thread never
	// shows the same agent under two ids
	if second.Author != "agent:hermes" || second.AuthorName != "Alfred" || second.Text != "Here are three to start: A, B, C." {
		t.Fatalf("second entry must be the agent's say body, as the agent: %+v", second)
	}
	if third.Action != threads.ActAssign || third.Meta["assignee"] != "agent:alfred" {
		t.Fatalf("third entry must be the assignment: %+v", third)
	}
	for _, c := range th {
		if strings.Contains(c.Text, "later — not part of the task") {
			t.Fatalf("turns after the pressed one must not be copied: %+v", c)
		}
	}

	// the session remembers its task
	sess, _, _, _ := st.Get("alfred", id)
	if sess.Task != taskID {
		t.Fatalf("session.Task = %q, want %q", sess.Task, taskID)
	}
	if b, _ := os.ReadFile(filepath.Join(st.Root(), "alfred", id+".md")); !strings.Contains(string(b), "task: "+taskID) {
		t.Fatalf("task must be in the session frontmatter:\n%s", b)
	}

	// open in chat: the panel names the conversation
	pw := httptest.NewRecorder()
	srv.handleTaskPanel(pw, httptest.NewRequest("GET", "/api/tasks/panel?id="+taskID, nil))
	var panel map[string]any
	if err := json.Unmarshal(pw.Body.Bytes(), &panel); err != nil {
		t.Fatal(err)
	}
	chat, _ := panel["chat"].(map[string]any)
	if chat == nil || chat["agent"] != "alfred" || chat["id"] != id || chat["label"] != "Alfred" || chat["title"] != "gutter contractors" {
		t.Fatalf("panel chat link: %+v", panel["chat"])
	}

	// the reverse list: the agent's rail section sees the task, with its chat
	tw := httptest.NewRecorder()
	treq := httptest.NewRequest("GET", "/api/agents/chat/alfred/tasks", nil)
	treq.SetPathValue("agent", "alfred")
	srv.handleAgentChatTasks(tw, treq)
	var tl struct {
		Tasks []agentChatTask `json:"tasks"`
	}
	if err := json.Unmarshal(tw.Body.Bytes(), &tl); err != nil {
		t.Fatal(err)
	}
	if len(tl.Tasks) != 1 || tl.Tasks[0].ID != taskID || tl.Tasks[0].ChatID != id || tl.Tasks[0].Container != "Inbox" {
		t.Fatalf("agent tasks: %+v", tl.Tasks)
	}

	// an explicit line replaces the title; an unknown session is a 404
	req = httptest.NewRequest("POST", "/api/agents/chat/alfred/sessions/"+id+"/promote", strings.NewReader(`{"text":"call the top three gutter contractors"}`))
	req.SetPathValue("agent", "alfred")
	req.SetPathValue("id", id)
	w = httptest.NewRecorder()
	srv.handleAgentChatPromote(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res["created"] != "inbox/call-the-top-three-gutter-contractors" {
		t.Fatalf("promote with text: %d %+v", w.Code, res)
	}
	req = httptest.NewRequest("POST", "/api/agents/chat/alfred/sessions/20260101-000000-zz/promote", strings.NewReader(`{}`))
	req.SetPathValue("agent", "alfred")
	req.SetPathValue("id", "20260101-000000-zz")
	w = httptest.NewRecorder()
	srv.handleAgentChatPromote(w, req)
	if w.Code != 404 {
		t.Fatalf("unknown session: %d", w.Code)
	}
}

// The portal backend promotes through the same handler: the thread is read
// through the shared transcript grammar, the agent speaks as itself in the
// todo thread, and — with no field on a portal thread — the link lives on
// the task side only.
func TestPromotePortalChatToTask(t *testing.T) {
	srv := loopFixture(t)
	srv.UseHarnesses([]Harness{{Name: "excalibur"}, {Name: "hermes", Spirits: spirits.NewStore(t.TempDir())},
		{Name: "kairos", Spirits: spirits.NewStore(t.TempDir()), Surface: "team"}})
	store, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseChatThreads(store)
	now := time.Now()
	who := chatthreads.Identity{ID: "benjamin@aion.bio", Name: "Benjamin"}
	if _, err := store.CreateThread("t1", "pig site setbacks", "", who, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(chatthreads.Message{ID: "m1", Thread: "t1", Kind: "ask", Author: who.ID, AuthName: who.Name,
		Text: "what's the setback on the pig site?", At: now}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(chatthreads.Message{ID: "m2", Thread: "t1", Kind: "kairos", Author: "agent:kairos", AuthName: "Kairos",
		Text: "Fifty feet from the lot line per the zoning memo.", Ritual: "ask", Outcome: "completed", Elapsed: "41s", At: now.Add(time.Minute)}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/agents/chat/kairos/sessions/t1/promote", strings.NewReader(`{"text":"confirm the pig site setback with the county"}`))
	req.SetPathValue("agent", "kairos")
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	srv.handleAgentChatPromote(w, req)
	if w.Code != 200 {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	taskID, _ := res["created"].(string)
	if taskID != "inbox/confirm-the-pig-site-setback-with-the-county" || res["assigned"] != true || res["name"] != "Kairos" || res["copied"] != float64(2) {
		t.Fatalf("promote response: %+v", res)
	}
	th := srv.listThread(taskID)
	if len(th) != 3 || th[1].Author != "agent:kairos" || th[1].AuthorName != "Kairos" ||
		th[1].Text != "Fifty feet from the lot line per the zoning memo." {
		t.Fatalf("thread: %+v", th)
	}
	link, _ := th[0].Meta["chat"].(map[string]any)
	if link == nil || link["agent"] != "kairos" || link["id"] != "t1" {
		t.Fatalf("chat link: %+v", th[0].Meta)
	}
	if n := len(srv.findHarness("kairos").Spirits.Queued()); n != 0 {
		t.Fatalf("promote must not spool an order, got %d", n)
	}
	if a := srv.readPlanRecord(taskID).Assignee; a != "agent:kairos" {
		t.Fatalf("assignee = %q", a)
	}
}

func TestTaskChatLinkFallsBackToTheSection(t *testing.T) {
	srv := loopFixture(t)
	// an agent-held task with no promoted entries points at the agent's
	// section (alfred/hermes are one section); a person-held task has none
	if l := srv.taskChatLink("x", nil, "agent:hermes"); l == nil || l.Agent != "alfred" || l.ID != "" || l.Label != "Alfred" {
		t.Fatalf("hermes-held: %+v", l)
	}
	if l := srv.taskChatLink("x", nil, "agent:alfred"); l == nil || l.Agent != "alfred" {
		t.Fatalf("alfred-held: %+v", l)
	}
	if l := srv.taskChatLink("x", nil, "BA"); l != nil {
		t.Fatalf("person-held must have no chat: %+v", l)
	}
	if l := srv.taskChatLink("x", nil, "agent:nobody"); l != nil {
		t.Fatalf("unknown agent must have no chat: %+v", l)
	}
	for tok, want := range map[string]string{"agent:alfred": "alfred", "agent:hermes": "alfred", "agent:kairos": "kairos", "agent:scout::brief": "", "BA": "", "": ""} {
		if got := chatSlugFor(tok); got != want {
			t.Errorf("chatSlugFor(%q) = %q, want %q", tok, got, want)
		}
	}
}
