package server

import (
	"context"
	"manifest/agentchat"
	"manifest/ledger"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitAgentChatOutcome(t *testing.T, st *ledger.Store, request string) ledger.Entry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		entries, err := st.Day(st.Today())
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Meta["requestId"] == request && e.Kind != "chat.user" {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("missing outcome for", request)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentChatOutcomeUsesSavedInterruption(t *testing.T) {
	s, st, led := agentChatFixture(t, echoStub)
	id, _ := st.Create("alfred", "", "", "")
	st.Accept("alfred", id, "race-outcome", "first")
	st.Claim("alfred", id)
	st.RequestStop("alfred", id, "race-outcome")
	// The runner returned success after the owner requested interruption.
	if err := st.FinishWithResult("alfred", id, "race-outcome", "alfred", "returned text", agentchat.DeliveryCompleted, "", 0, agentchat.InvocationResult{SessionID: "native-session", ReportedModel: "reported"}); err != nil {
		t.Fatal(err)
	}
	s.recordAgentChatOutcome("alfred", id, "race-outcome", agentchat.Recipient{Agent: "zeck", Model: "selected"}, "returned text", 0)
	e := waitAgentChatOutcome(t, led, "race-outcome")
	if e.Kind != "run.interrupted" || e.Actor != "agent:zeck" || e.Session != id || e.Meta["deliveryState"] != agentchat.DeliveryInterrupted || e.Meta["model"] != "reported" || e.Meta["selectedModel"] != "selected" || e.Meta["sessionId"] != "native-session" {
		t.Fatal(e)
	}
	entries, _ := led.Day(led.Today())
	if len(entries) != 1 {
		t.Fatal(entries)
	}
}

func TestAgentChatOutcomeRefusesUnfinishedReceipt(t *testing.T) {
	for _, state := range []string{agentchat.DeliveryRunning, agentchat.DeliveryQueued, agentchat.DeliveryCancelled, ""} {
		if e, ok := agentChatOutcomeEntry("alfred", "session", agentchat.Delivery{ID: "unfinished", State: state}, agentchat.Recipient{Agent: "alfred"}, "reply", 0); ok {
			t.Fatal(e)
		}
	}
}

func TestAgentChatFailedSaveDoesNotPublishOutcome(t *testing.T) {
	root := t.TempDir()
	started, release := filepath.Join(root, "started"), filepath.Join(root, "release")
	script := "#!/bin/sh\ntouch '" + started + "'\nwhile [ ! -f '" + release + "' ]; do sleep 0.01; done\necho failed >&2\nexit 3\n"
	s, st, led := agentChatFixture(t, script)
	id, _ := st.Create("alfred", "", "", "")
	st.Accept("alfred", id, "failed-save-request", "first")
	st.Claim("alfred", id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.runAgentChatTurnContext(ctx, "alfred", id, "failed-save-request") }()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("runner did not start")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	file := filepath.Join(st.Root(), "alfred", id+".md")
	if err := os.Rename(file, file+".unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("expected receipt save failure")
	}
	entries, err := led.Day(led.Today())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("uncommitted outcome emitted: %+v", entries)
	}
}
