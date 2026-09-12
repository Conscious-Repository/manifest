package server

import (
	"os/exec"
	"testing"
)

// A user turn that carried attachments shows the message and a preview card
// per file; the model's context (tokens, the attachment block, the CLI's
// paste marker) never reaches the reader, and file metadata is fetched once
// per file however often the turn repaints (testdata/chat-attachment-turn.cjs).
func TestChatAttachmentTurnUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-attachment-turn.cjs").CombinedOutput(); err != nil {
		t.Fatalf("attachment turn: %v\n%s", err, out)
	}
}
