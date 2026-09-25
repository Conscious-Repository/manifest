package server

import (
	"manifest/agentchat"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentChatPreflightFailureFinishesReceipt(t *testing.T) {
	for _, kind := range []string{"runtime-absent", "runtime-missing", "runtime-replaced", "runtime-remote", "runner-unavailable"} {
		t.Run(kind, func(t *testing.T) {
			// Any unexpected invocation produces a distinctive assistant response.
			s, st, _ := agentChatFixture(t, echoStub)
			origin := agentchat.Origin{Backend: "terminal", Mode: "continue", Agent: "codex", ID: "aabbccddeeff0011"}
			id, err := st.CreateRelatedOnce("alfred", "", "Continuation", "", "preflight-create", origin)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "runtime-absent" {
				s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
			}
			switch kind {
			case "runtime-replaced":
				s.terminal.upsert(termSession{ID: origin.ID, Kind: "claude"})
			case "runtime-remote":
				s.terminal.upsert(termSession{ID: origin.ID, Kind: "codex", Device: "remote"})
			case "runner-unavailable":
				s.hermes = nil
			}
			for _, request := range []string{"preflight-first", "preflight-second"} {
				if _, err := st.Accept("alfred", id, request, "Continue the reviewed work"); err != nil {
					t.Fatal(err)
				}
			}
			s.startAgentChatDelivery("alfred", id)
			session := waitIdle(t, st, "alfred", id)
			if len(session.Deliveries) != 2 || session.Turns != 4 {
				t.Fatal(session)
			}
			for _, d := range session.Deliveries {
				if d.State != agentchat.DeliveryFailed || d.ToolScope != nil || d.ReplyTurn == 0 || d.Error == "" {
					t.Fatal(d)
				}
			}
			_, body, _, _ := st.Get("alfred", id)
			if strings.Count(body, "No agent invocation started.") != 2 {
				t.Fatal(body)
			}
			fresh := agentchat.New(st.Root())
			fresh.Recover()
			got, _ := fresh.Receipt("alfred", id, "preflight-first")
			if got.State != agentchat.DeliveryFailed {
				t.Fatal(got)
			}
			retry, err := fresh.Accept("alfred", id, "preflight-first", "Continue the reviewed work")
			if err != nil || retry.New {
				t.Fatal(retry, err)
			}
			after, _, _, _ := fresh.Get("alfred", id)
			if after.Turns != 4 {
				t.Fatal("retry appended turns", after.Turns)
			}
		})
	}
}
