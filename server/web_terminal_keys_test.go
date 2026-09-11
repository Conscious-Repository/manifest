package server

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"sync/atomic"
	"testing"
)

// Exercise real Chat button handlers, then their emitted bytes against a Unix
// socket daemon fixture. Never send keys to a real terminal or conversation.
func TestChatQuickKeysMatchHerdrAdapter(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	out, err := exec.Command(node, "testdata/chat-quick-keys.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var keys []string
	if err := json.Unmarshal(out, &keys); err != nil {
		t.Fatal(err, string(out))
	}
	names := map[string]string{"\r": "enter", "\x1b": "esc", "\x1b[A": "up", "\x1b[B": "down", "\x1b[D": "left", "\x1b[C": "right", "\x03": "ctrl+c"}
	var sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		if r.Method == "pane.send_keys" {
			got := r.Params["keys"].([]any)
			i := int(sends.Add(1)) - 1
			if len(got) != 1 || got[0] != names[keys[i]] || r.Params["pane_id"] != "p_1" {
				t.Errorf("wrong exact key target: %+v", r)
			}
			herdrFixtureReply(c, map[string]any{})
			return
		}
		herdrFixtureSnapshot(c, "idle", 1)
	})
	for _, key := range keys {
		if err := h.SendKey(context.Background(), herdrFixtureID(t, h), key); err != nil {
			t.Fatal(err)
		}
	}
	if int(sends.Load()) != len(keys) {
		t.Fatal("missing key delivery")
	}
	for _, key := range []string{"\t", "\x1b[Z"} {
		if err := h.SendKey(context.Background(), herdrFixtureID(t, h), key); err == nil {
			t.Fatal("unsupported quick key contract changed")
		}
	}
	if int(sends.Load()) != len(keys) {
		t.Fatal("unsupported keys reached daemon")
	}
}
