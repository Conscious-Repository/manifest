package server

import (
	"os/exec"
	"testing"
)

// Moving between chat threads turns the stage over synchronously: the previous
// transcript never waits on the network, a thread seen earlier repaints from
// the stage cache, the thread fetch starts alongside the list refresh, and an
// unchanged payload does not repaint (testdata/chat-stage-switch.cjs).
func TestChatStageSwitchUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-stage-switch.cjs").CombinedOutput(); err != nil {
		t.Fatalf("stage switch: %v\n%s", err, out)
	}
}

// The same turnover in the real front end: index.html and every script over a
// stub API, driven through A → B → A (testdata/chat-stage-switch-browser.cjs).
// Browser-driven, so it runs only where Playwright resolves (NODE_PATH).
func TestChatStageSwitchBrowserUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-stage-switch-browser.cjs").CombinedOutput(); err != nil {
		t.Fatalf("stage switch (browser): %v\n%s", err, out)
	}
}
