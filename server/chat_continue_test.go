package server

import (
	"errors"
	"manifest/agentchat"
	"strings"
	"testing"
)

func TestContinueHerePinsEachQueuedRecipientAndKeepsAuthors(t *testing.T) {
	script := `#!/bin/sh
if [ "$1" = "profile" ]; then
 printf 'Profile   Model   Gateway   Alias   Distribution\n'
 printf '◆ default   claude-x   —   —   —\n'
 printf 'scout   gpt-5   —   scout   —\n'
 exit 0
fi
` + strings.TrimPrefix(echoStub, "#!/bin/sh\n")
	s, st, _ := agentChatFixture(t, script)
	id, err := st.Create("alfred", "", "Stable conversation", "")
	if err != nil {
		t.Fatal(err)
	}
	st.AppendTurn("alfred", id, "alfred", strings.Repeat("old context ", 4000), 0)
	_, before, _, _ := st.Get("alfred", id)
	scout := &agentchat.Recipient{Agent: "scout"}
	if _, err = s.agentChatSendTo("alfred", id, "continue-scout-001", "first recipient", nil, "", nil, scout); err != nil {
		t.Fatal(err)
	}
	if _, err = s.agentChatSendTo("alfred", id, "continue-owner-002", "second recipient", nil, "", nil, &agentchat.Recipient{Agent: "alfred"}); err != nil {
		t.Fatal(err)
	}
	scout.Agent = "alfred" // Caller mutation must not redirect the accepted instruction.
	waitIdle(t, st, "alfred", id)
	sess, body, _, _ := st.Get("alfred", id)
	turns := agentchat.ParseTurns(body)
	if !strings.HasPrefix(body, before) || sess.Agent != "alfred" || len(turns) != 5 {
		t.Fatal("source identity/history changed", len(turns))
	}
	if turns[2].Who != "scout" || !strings.Contains(turns[2].Text, "profile=scout") || turns[4].Who != "alfred" {
		t.Fatal("incorrect reply authors")
	}
	first, _ := st.Receipt("alfred", id, "continue-scout-001")
	second, _ := st.Receipt("alfred", id, "continue-owner-002")
	if first.Context.Recipient.Profile != "scout" || first.Context.Recipient.Model != "gpt-5" || second.Context.Recipient.Model != "claude-x" || first.HistoryOmitted != 1 {
		t.Fatal(first, second)
	}
	// Same request recovers its accepted routing even after live profile settings change.
	s.agentChat.pmu.Lock()
	s.agentChat.profiles = nil
	s.agentChat.pmu.Unlock()
	if _, err = s.agentChatSendTo("alfred", id, "continue-scout-001", "first recipient", nil, "", nil, &agentchat.Recipient{Agent: "scout"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.agentChatSendTo("alfred", id, "continue-scout-001", "first recipient", nil, "", nil, &agentchat.Recipient{Agent: "alfred"}); !errors.Is(err, agentchat.ErrRequestConflict) {
		t.Fatal("changed retry recipient accepted", err)
	}
}
