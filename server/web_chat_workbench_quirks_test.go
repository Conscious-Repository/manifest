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
