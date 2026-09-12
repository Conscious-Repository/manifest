package server

import (
	"os/exec"
	"testing"
)

// A terminal send's pending echo never duplicates a user turn the transcript
// tail already landed while the delivery waited for the prompt, and the tail
// reconciles pending rows against every turn since the send began
// (testdata/chat-term-echo.cjs).
func TestChatTermEchoUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-term-echo.cjs").CombinedOutput(); err != nil {
		t.Fatalf("terminal echo: %v\n%s", err, out)
	}
}
