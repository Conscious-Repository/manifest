package server

import (
	"os/exec"
	"testing"
)

// TestChatWorkbenchQuirksUI pins the finish-pass UI audit fixes in headless
// Chromium (testdata/chat-workbench-quirks.cjs). Runs only where Playwright
// resolves.
func TestChatWorkbenchQuirksUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-workbench-quirks.cjs").CombinedOutput(); err != nil {
		t.Fatalf("workbench quirks (browser): %v\n%s", err, out)
	}
}

// TestChatDeepLinkBrowserUI: the real front end over the stub API — a cold
// #/chat/<id> for an Alfred thread follows its owner, an id no store holds
// says so truthfully, and a failed load closes the composer until Retry
// (2026-09-26).
func TestChatDeepLinkBrowserUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-deep-link-browser.cjs").CombinedOutput(); err != nil {
		t.Fatalf("cold deep link (browser): %v\n%s", err, out)
	}
}

// TestChatWorkspaceLayoutsBrowserUI: Focus / Split / Workbench / Four panes
// over a populated thread keep panes apart, the draft, the tabs and the
// reader's turn through layout switches and resizes (1440→1024→861→1440);
// scroll regions do not fight and Tab does not trap (2026-09-26).
func TestChatWorkspaceLayoutsBrowserUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-workspace-layouts-browser.cjs").CombinedOutput(); err != nil {
		t.Fatalf("workspace layouts (browser): %v\n%s", err, out)
	}
}
