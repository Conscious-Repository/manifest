package server

import (
	"os/exec"
	"testing"
)

func TestChatDeliveryRecoveryUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-delivery.cjs").CombinedOutput(); err != nil {
		t.Fatalf("delivery UI: %v\n%s", err, out)
	}
}
