package server

import (
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The Codex "Update available" chooser is answered by the runtime (skip until
// next version) and the message goes through once the prompt is back; any
// other dialog still refuses the send.
func TestSendTextSkipsCodexUpdatePrompt(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var prompts, skips atomic.Int32
	screen := "❯"
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			herdrFixtureReply(c, map[string]any{})
		case "pane.send_keys":
			skips.Add(1)
			screen = "› Ask Codex to do anything" // the chooser is gone
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": screen}})
		case "agent.prompt":
			prompts.Add(1)
			herdrFixtureReply(c, map[string]any{})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	se := createCodingDraft(t, s, "codex")
	if w := receiptInput(s, se.ID, `{"text":"start here","requestId":"update-input-001"}`); w.Code != 200 {
		t.Fatal("start", w.Code, w.Body.String())
	}
	screen = "  ✨ Update available! 0.154.0 -> 0.155.0\n\n› 1. Update now\n  2. Skip\n  3. Skip until next version\n\n  Press enter to continue"
	if w := receiptInput(s, se.ID, `{"text":"carry on","requestId":"update-input-002"}`); w.Code != 200 {
		t.Fatal("send through the update prompt", w.Code, w.Body.String())
	}
	if skips.Load() != 1 || prompts.Load() != 2 {
		t.Fatalf("skip=%d prompts=%d", skips.Load(), prompts.Load())
	}
	// a real question still holds the message
	screen = "Do you trust the files in this folder?\n  Yes, I trust this folder"
	if w := receiptInput(s, se.ID, `{"text":"held","requestId":"update-input-003"}`); w.Code == 200 || !strings.Contains(w.Body.String(), "nothing was sent") {
		t.Fatal("trust dialog answered for the owner", w.Code, w.Body.String())
	}
	if prompts.Load() != 2 {
		t.Fatal("held message crossed the boundary")
	}
}
