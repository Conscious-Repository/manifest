package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/threads"
)

func TestCodingConversationResolvesOnlyVerifiedWorkOrder(t *testing.T) {
	s := codingFixture(t)
	h := s.findHarness("codex")
	run := boardRunID()
	se := termSession{ID: "session", Kind: "codex", BoardBrief: filepath.Join(h.Spirits.Root(), "work", run, "brief.md")}
	base := terminalConversation(se)
	check := func(wantTask string, warn bool) {
		t.Helper()
		d := s.terminalConversation(se)
		if d.Key != base.Key || d.Route != base.Route || d.Scope != "private" {
			t.Fatal("link changed source identity", d)
		}
		got := ""
		for _, link := range d.Links {
			if link.Kind == "task" {
				got = link.ID
			}
		}
		if got != wantTask || (len(d.Warnings) > 0) != warn {
			t.Fatalf("task=%q warnings=%v", got, d.Warnings)
		}
	}
	check("", true)
	write := func(task string) {
		t.Helper()
		if err := boardReport(h, run, task, "go", "", "running", "", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	write("inbox/wire-the-fence")
	check("inbox/wire-the-fence", false)
	se.BoardBrief = filepath.Join(t.TempDir(), run, "brief.md")
	check("", true)
	se.BoardBrief = filepath.Join(h.Spirits.Root(), "work", run, "brief.md")
	write("inbox/one] [todo:: inbox/two")
	check("", true)
	se.BoardBrief = ""
	check("", false)
}

func TestConversationIdentityUsesNativeSource(t *testing.T) {
	a := agentConversation("hermes", "alfred", "same-id", "private", "")
	b := agentConversation("portal", "alfred", "same-id", "team:aion", "")
	c := agentConversation("hermes", "other", "same-id", "private", "")
	if a.Key == b.Key || a.Key == c.Key {
		t.Fatal("source identity collision")
	}
	d := agentConversation("hermes", "alfred", "same-id", "private", "new-task")
	if a.Key != d.Key || len(d.Links) != 1 {
		t.Fatal("assigning a task changed conversation identity")
	}
	portal := agentConversation("portal", "kairos", "th/a b", "team:aion", "")
	if portal.Route != "#/chat/a/kairos/th%2Fa%20b" {
		t.Fatal(portal.Route)
	}
	x := terminalConversation(termSession{ID: "row", Kind: "codex", ResumeID: "first"})
	y := terminalConversation(termSession{ID: "row", Kind: "codex", ResumeID: "second"})
	if x.Key != y.Key || x.Links[1].ID == y.Links[1].ID {
		t.Fatal("runtime resume must update link, not identity")
	}
	remote := terminalConversation(termSession{ID: "remote", Kind: "codex", Device: "lab"})
	if remote.Route != "#/terminal/remote" {
		t.Fatal("remote session routed to unsupported local transcript", remote.Route)
	}
}

func TestTaskConversationReportsConflictingOrigins(t *testing.T) {
	s := &Server{}
	entry := func(id string) threads.Comment {
		return threads.Comment{Meta: map[string]any{"chat": map[string]any{"agent": "alfred", "id": id}}}
	}
	d := s.taskConversation("inbox/example", []threads.Comment{entry("one"), entry("one"), entry("two")})
	if len(d.Links) != 3 || len(d.Warnings) != 1 {
		t.Fatalf("lost conflicting origins: %+v", d)
	}
	if d.Route != "#/chat/task/inbox%2Fexample" {
		t.Fatal(d.Route)
	}
	if d.Links[1].ID == d.Links[2].ID {
		t.Fatal("distinct origins merged")
	}
	if link := s.taskChatLink("inbox/example", []threads.Comment{entry("one"), entry("two")}, "agent:alfred"); link != nil {
		t.Fatalf("ambiguous conversation chosen: %+v", link)
	}
	if link := s.taskChatLink("inbox/example", []threads.Comment{entry("one"), entry("one")}, "agent:alfred"); link == nil || link.ID != "one" {
		t.Fatalf("repeated evidence should retain one link: %+v", link)
	}
}

func TestAgentSessionExposesStableConversationDescriptor(t *testing.T) {
	s := loopFixture(t)
	st := agentchat.New(filepath.Join(t.TempDir(), "chats"))
	s.UseAgentChat(st)
	id, err := st.Create("alfred", "", "first title", "first model")
	if err != nil {
		t.Fatal(err)
	}
	read := func() conversationDescriptor {
		r := httptest.NewRequest("GET", "/api/agents/chat/alfred/sessions/"+id, nil)
		r.SetPathValue("agent", "alfred")
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleAgentChatSession(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var out struct {
			Conversation conversationDescriptor `json:"conversation"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Conversation
	}
	before := read()
	if err := st.Rename("alfred", id, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTask("alfred", id, "inbox/task"); err != nil {
		t.Fatal(err)
	}
	after := read()
	if before.Key == "" || before.Key != after.Key || after.Source.ID != id || after.Scope != "private" || len(after.Links) != 1 {
		t.Fatalf("descriptor: %+v -> %+v", before, after)
	}
}
