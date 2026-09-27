package server

import (
	"strings"
	"testing"

	"manifest/agentchat"
)

// A standalone start is a first-class conversation: the default endpoint runs
// with no -p (no persona is forced), the session carries no task link, and a
// retried create is the same one conversation with one invocation.
func TestStandaloneStartForcesNoTaskOrProfile(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	body := map[string]any{"text": "standalone question", "requestId": "standalone-start-one"}
	code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", body)
	if code != 200 {
		t.Fatal(code, r)
	}
	id := r["id"].(string)
	if code, again := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", body); code != 200 || again["id"] != id {
		t.Fatal("retried create is not the same conversation", code, again)
	}
	sess := waitIdle(t, st, "alfred", id)
	if sess.Task != "" || sess.Profile != "" {
		t.Fatalf("standalone start forced a task or profile: task=%q profile=%q", sess.Task, sess.Profile)
	}
	if sess.Turns != 2 || len(sess.Deliveries) != 1 {
		t.Fatalf("retry duplicated the run: turns=%d deliveries=%d", sess.Turns, len(sess.Deliveries))
	}
	_, transcript, _, _ := st.Get("alfred", id)
	turns := agentchat.ParseTurns(transcript)
	if len(turns) != 2 || !strings.Contains(turns[1].Text, "profile=(default)") {
		t.Fatalf("default endpoint ran with a forced profile: %+v", turns)
	}
	if list := st.List("alfred"); len(list) != 1 {
		t.Fatalf("one standalone start produced %d conversations", len(list))
	}
}
