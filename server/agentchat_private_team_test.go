package server

import (
	"testing"
	"time"
)

func TestPrivateTeamProfilesDoNotCreateSharedThreads(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	shared, _ := chatFixture(t)
	s.chat = shared.chat
	s.agentChat.profiles = []hermesProfile{{Name: "kairos-private"}, {Name: "zeck-private"}}
	s.agentChat.profAt = time.Now()
	before := len(s.chat.Threads())
	for _, agent := range []string{"kairos-private", "zeck-private"} {
		code, out := agentChatJSON(t, s, "POST", "/api/agents/chat/"+agent+"/sessions", map[string]any{"title": "Private idea", "requestId": "private-create-" + agent})
		if code != 200 {
			t.Fatal(code, out)
		}
		session, _, _, ok := st.Get(agent, out["id"].(string))
		if !ok || session.Profile != agent || sessionConversation(session).Scope != "private" {
			t.Fatal("not a private profile", session)
		}
	}
	if len(s.chat.Threads()) != before {
		t.Fatal("private creation wrote shared history")
	}
	code, out := agentChatJSON(t, s, "POST", "/api/agents/chat/kairos/sessions", map[string]any{"text": "do not publish this"})
	if code != 400 {
		t.Fatal(code, out)
	}
	if len(s.chat.Threads()) != before {
		t.Fatal("implicit shared creation succeeded")
	}
}
