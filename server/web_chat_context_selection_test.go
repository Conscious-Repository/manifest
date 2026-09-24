package server

import (
	"os/exec"
	"testing"
)

func TestChatContextSelectionUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-context-selection.cjs").CombinedOutput(); err != nil {
		t.Fatalf("context selections: %v\n%s", err, out)
	}
}
