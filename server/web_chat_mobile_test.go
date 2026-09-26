package server

import (
	"os/exec"
	"testing"
)

// The phone conversation chrome (95-mobile.css Rev 6 + Rev 7): a 50–58px
// composer row that keeps its height through focus and one-line messages and
// moves the field to its own row only once the text wraps, a quiet model label
// and neutral send with 44px targets, a "Back to chats" control leading the
// head, and the workspace opener behind ··· — with the desktop head and
// composer unchanged at 861/1000/1280. Browser-driven, so it runs only where
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

// TestChatRailMetaTokensUI: the rail's metadata line drops a token whole
// rather than cutting it mid-word ("Claude Coc", 2026-09-26), at desktop and
// phone widths.
func TestChatRailMetaTokensUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-rail-meta-truncation.cjs").CombinedOutput(); err != nil {
		t.Fatalf("rail meta tokens: %v\n%s", err, out)
	}
}

// TestChatRailDotAlignmentUI: a conversation's state dot follows its title at
// one gap instead of floating to the row's far end beside ⋯ (2026-09-26,
// 1440px), and stays centred on the title line at phone widths.
func TestChatRailDotAlignmentUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-rail-dot-alignment.cjs").CombinedOutput(); err != nil {
		t.Fatalf("rail dot alignment: %v\n%s", err, out)
	}
}
