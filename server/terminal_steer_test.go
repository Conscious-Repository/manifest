package server

import (
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A text send into a working agent is refused with nothing sent so the client
// holds it as a pending message; the same content with steer:true (and the
// same request ID) is delivered. Keys still pass while the agent works.
func TestTerminalInputHeldWhileAgentWorking(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var prompts atomic.Int32
	var state atomic.Value
	state.Store("idle")
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, state.Load().(string), 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input", "pane.send_keys":
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		case "agent.prompt":
			prompts.Add(1)
			herdrFixtureReply(c, map[string]any{})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	se := createCodingDraft(t, s, "codex")
	if w := receiptInput(s, se.ID, `{"text":"start here","requestId":"steer-input-001"}`); w.Code != 200 {
		t.Fatal("start", w.Code, w.Body.String())
	}
	state.Store("working")
	w := receiptInput(s, se.ID, `{"text":"follow-up","requestId":"steer-input-002"}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "agent is working; nothing sent") {
		t.Fatal("working agent accepted input", w.Code, w.Body.String())
	}
	if prompts.Load() != 1 {
		t.Fatal("held message crossed the boundary", prompts.Load())
	}
	if _, err := s.terminal.readInputReceipt(se.ID, "steer-input-002"); err == nil {
		t.Fatal("held message left a receipt")
	}
	if w := receiptInput(s, se.ID, `{"key":"n","requestId":"steer-key-001"}`); w.Code != 200 {
		t.Fatal("key refused while working", w.Code, w.Body.String())
	}
	if w := receiptInput(s, se.ID, `{"text":"follow-up","requestId":"steer-input-002","steer":true}`); w.Code != 200 {
		t.Fatal("steer refused", w.Code, w.Body.String())
	}
	if prompts.Load() != 2 {
		t.Fatal("steer not delivered", prompts.Load())
	}
	// the steered receipt answers a replay of the same request without steer
	if w := receiptInput(s, se.ID, `{"text":"follow-up","requestId":"steer-input-002"}`); w.Code != 200 || prompts.Load() != 2 {
		t.Fatal("replay", w.Code, w.Body.String(), prompts.Load())
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/?request=steer-input-002", nil)
	req.SetPathValue("id", se.ID)
	s.handleTermDelivery(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"sent"`) {
		t.Fatal("delivery", rec.Code, rec.Body.String())
	}
}
