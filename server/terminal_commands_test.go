package server

import (
	"strings"
	"testing"

	"manifest/agentchat"
)

func TestNativeCommandValidation(t *testing.T) {
	for _, s := range []string{"/goal reduce latency", "/model", "/custom:review arg", "/goal pause"} {
		if !validNativeCommand(s) {
			t.Errorf("rejected %q", s)
		}
	}
	for _, s := range []string{"hello", "/", "/goal\n/clear", "/goal\x1b", "/tmp/file", "/goal\x00", "/goal\tbad"} {
		if validNativeCommand(s) {
			t.Errorf("accepted %q", s)
		}
	}
}
func TestNativeCommandReceiptNoReplay(t *testing.T) {
	s, se, prompts := terminalReceiptFixture(t, false, func(text string) {
		if text != "/status" {
			t.Errorf("native command was wrapped: %q", text)
		}
	})
	se.Origin = &agentchat.Origin{Mode: "side", Backend: "terminal", Agent: "claude", ID: "parent", Context: "Parent context must not wrap a command"}
	s.terminal.save([]termSession{se})
	body := `{"text":"/status","command":true,"requestId":"receipt-input-001"}`
	for i := 0; i < 2; i++ {
		if w := receiptInput(s, se.ID, body); w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if prompts.Load() != 1 {
		t.Fatal("command replayed")
	}
	if w := receiptInput(s, se.ID, `{"text":"/status","command":true,"files":["x"],"requestId":"receipt-input-002"}`); w.Code != 400 {
		t.Fatalf("command context accepted: %d", w.Code)
	}
	if w := receiptInput(s, se.ID, `{"text":"/new","command":true,"requestId":"receipt-input-003"}`); w.Code != 400 {
		t.Fatalf("session switch accepted: %d", w.Code)
	}
}

func TestClaudeRunCompletionEvidence(t *testing.T) {
	final := `{"type":"assistant","timestamp":"2026-09-24T12:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Done"}],"stop_reason":"end_turn"}}` + "\n"
	tr := parseClaudeTranscript(strings.NewReader(final))
	if tr.Run == nil || tr.Run.State != "completed" || tr.Run.Evidence == "" {
		t.Fatalf("missing completed turn: %+v", tr.Run)
	}
	tr = parseClaudeTranscript(strings.NewReader(final + `{"type":"user","message":{"content":"Next request"}}` + "\n"))
	if tr.Run.State != "running" {
		t.Fatal("old completion survived new input")
	}
}
