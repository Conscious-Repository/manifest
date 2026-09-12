package server

import (
	"os/exec"
	"testing"
)

// The phone conversation chrome (95-mobile.css Rev 6): a 56–64px composer
// pill that expands only when focused or holding text, a "Back to chats"
// control leading the head, and the workspace opener behind ··· — with the
// desktop head and composer unchanged. Browser-driven, so it runs only where
// Playwright resolves (NODE_PATH); elsewhere it skips like the other fixtures
// that need a browser.
func TestChatMobileChromeUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-mobile-chrome.cjs").CombinedOutput(); err != nil {
		t.Fatalf("mobile chat chrome UI: %v\n%s", err, out)
	}
}
