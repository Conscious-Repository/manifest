package server

import (
	"os/exec"
	"testing"
)

// TestChatPhoneResponsiveUI drives the real front end in headless Chromium
// (testdata/chat-phone-responsive.cjs): viewport grid, Back/Escape with the
// phone Chats list open, streaming reading position. Browser viewports, not
// physical-device acceptance. Runs only where Playwright resolves.
func TestChatPhoneResponsiveUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-phone-responsive.cjs").CombinedOutput(); err != nil {
		t.Fatalf("phone responsive (browser): %v\n%s", err, out)
	}
}
