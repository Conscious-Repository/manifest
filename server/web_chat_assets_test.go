package server

import (
	"bytes"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The two portals are served from disjoint go:embed subtrees (portal.go does
// fs.Sub over web/portal | web/ooda), so a shared file has to be two physical
// copies. This is the guard that keeps them one file in practice.
func TestChatActionsCopiesIdentical(t *testing.T) {
	a, err := fs.ReadFile(webFiles, "web/portal/src/chat-actions.js")
	if err != nil {
		t.Fatalf("aion copy: %v", err)
	}
	b, err := fs.ReadFile(webFiles, "web/ooda/src/chat-actions.js")
	if err != nil {
		t.Fatalf("ooda copy: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("chat-actions.js copies have drifted (%d vs %d bytes) — "+
			"edit one and copy it over the other:\n"+
			"  cp server/web/portal/src/chat-actions.js server/web/ooda/src/chat-actions.js", len(a), len(b))
	}
	if !bytes.Contains(a, []byte("ritualOf")) {
		t.Fatal("chat-actions.js lost ritualOf — the UI-key → wire-ritual mapping")
	}
}

// The composer labels by OUTPUT and maps to the wire in exactly one place.
// These assertions stop a future edit from quietly restoring the mode toggle.
func TestChatComposersLabelByOutcome(t *testing.T) {
	for _, c := range []struct{ name, path, banned string }{
		{"aion", "web/portal/src/chat.jsx", "['ask', 'delegate'].map"},
		{"ooda", "web/ooda/src/view-chat.jsx", `<option value="delegate"`},
	} {
		b, err := fs.ReadFile(webFiles, c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		src := string(b)
		if !strings.Contains(src, "CHAT_ACTIONS.ritualOf(") {
			t.Errorf("%s composer does not route through CHAT_ACTIONS.ritualOf", c.name)
		}
		if strings.Contains(src, c.banned) {
			t.Errorf("%s composer restored the ritual mode control (%q)", c.name, c.banned)
		}
		// both send paths must be reachable, or one outcome is unreachable
		for _, want := range []string{`send('ask')`, `send('delegate')`, `send("ask")`, `send("delegate")`} {
			if strings.Contains(src, want) {
				goto found
			}
		}
		t.Errorf("%s composer has no explicit send(action) call", c.name)
	found:
	}
}

// A half-landed version bump serves the new JSX against a cached old module,
// which is exactly the blank-page failure mode Babel gives no error for.
func TestChatActionsScriptWiredAtOneVersion(t *testing.T) {
	re := regexp.MustCompile(`src="src/[a-zA-Z-]+\.(?:js|jsx)\?v=(\d+)"`)
	for _, page := range []string{"web/portal/index.html", "web/ooda/index.html"} {
		b, err := fs.ReadFile(webFiles, page)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		src := string(b)
		if !strings.Contains(src, "src/chat-actions.js?v=") {
			t.Fatalf("%s does not load chat-actions.js — CHAT_ACTIONS would be undefined", page)
		}
		ms := re.FindAllStringSubmatch(src, -1)
		if len(ms) < 2 {
			t.Fatalf("%s: found %d versioned scripts", page, len(ms))
		}
		for _, m := range ms[1:] {
			if m[1] != ms[0][1] {
				t.Fatalf("%s: mixed cache-buster versions (%s vs %s) — bump them all", page, ms[0][1], m[1])
			}
		}
		// the plain module must be parsed BEFORE the JSX that reads it
		if strings.Index(src, "src/chat-actions.js") > strings.Index(src, `type="text/babel"`) {
			t.Fatalf("%s loads chat-actions.js after the JSX block", page)
		}
	}
}
