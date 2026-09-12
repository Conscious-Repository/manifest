package server

import (
	"encoding/json"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestQueuedFollowupDelivery(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	s.UseChatState(t.TempDir())
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
	if w := receiptInput(s, se.ID, `{"text":"start","requestId":"queue-start-001"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	key := s.terminalConversation(se).Key
	queue := func(id, text, at string) json.RawMessage {
		item := map[string]any{"stateKey": key, "scope": "codex/" + se.ID, "agent": "codex", "url": "/api/terminal/session/" + se.ID + "/input", "at": at, "staged": true, "draft": nil, "payload": map[string]any{"text": text, "requestId": id}}
		raw, _ := json.Marshal(item)
		snap, _ := s.chatState.Read(key, "deliveries")
		var value chatQueueValue
		json.Unmarshal(snap.Value, &value)
		if value.Items == nil {
			value.Items = map[string]json.RawMessage{}
		}
		value.Items[id] = raw
		bytes, _ := json.Marshal(value)
		if _, err := s.chatState.Write(key, "deliveries", snap.Revision, bytes); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	count := func() int {
		snap, _ := s.chatState.Read(key, "deliveries")
		var value chatQueueValue
		json.Unmarshal(snap.Value, &value)
		return len(value.Items)
	}
	first := queue("queue-follow-001", "first", "2026-09-12T10:00:00Z")
	queue("queue-follow-002", "second", "2026-09-12T10:01:00Z")
	for _, status := range []string{"working", "blocked", "unknown"} {
		state.Store(status)
		s.chatQueuedFollowupSweep()
		if prompts.Load() != 1 || count() != 2 {
			t.Fatalf("%s dispatched or lost queue", status)
		}
	}
	// A fresh Server consumes the persisted queue with no browser involved.
	restarted := &Server{terminal: s.terminal, chatState: s.chatState}
	h.server = restarted
	state.Store("idle")
	restarted.chatQueuedFollowupSweep()
	if prompts.Load() != 2 || count() != 1 {
		t.Fatal("first queued send", prompts.Load(), count())
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, "queue-follow-001")
	if err != nil || receipt.State != "sent" {
		t.Fatal("no durable receipt", receipt, err)
	}
	// The old editable snapshot cannot resurrect or steer a consumed message.
	if s.replaceQueuedFollowup(key, "queue-follow-001", first, first) {
		t.Fatal("stale edit accepted")
	}
	state.Store("working")
	restarted.chatQueuedFollowupSweep()
	if prompts.Load() != 2 {
		t.Fatal("second message steered the next run")
	}
	state.Store("idle")
	restarted.chatQueuedFollowupSweep()
	if prompts.Load() != 3 || count() != 0 {
		t.Fatal("second queued send", prompts.Load(), count())
	}
	// Cancellation and uncertain claims never send automatically.
	raw := queue("queue-cancel-001", "cancel me", "2026-09-12T10:02:00Z")
	if !s.replaceQueuedFollowup(key, "queue-cancel-001", raw, nil) {
		t.Fatal("cancel")
	}
	raw = queue("queue-uncertain-001", "uncertain", "2026-09-12T10:03:00Z")
	var item map[string]any
	json.Unmarshal(raw, &item)
	item["staged"] = false
	claimed, _ := json.Marshal(item)
	if !s.replaceQueuedFollowup(key, "queue-uncertain-001", raw, claimed) {
		t.Fatal("claim")
	}
	queue("queue-behind-001", "wait behind uncertainty", "2026-09-12T10:04:00Z")
	restarted.chatQueuedFollowupSweep()
	if prompts.Load() != 3 || count() != 2 {
		t.Fatal("uncertain delivery replayed or overtaken")
	}
	// The guarded send endpoint also rejects stale automatic dispatches.
	state.Store("working")
	if w := receiptInput(s, se.ID, `{"text":"stale","requestId":"queue-stale-001","afterRun":true}`); w.Code != 409 {
		t.Fatal("stale automatic send", w.Code, w.Body.String())
	}
	if prompts.Load() != 3 {
		t.Fatal("stale send crossed runtime boundary")
	}
}
