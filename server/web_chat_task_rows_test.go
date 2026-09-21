package server

import (
	"os/exec"
	"testing"
)

// Task conversations are entries of the CHAT rail: keyed apart from
// sessions, titled by the task's words, held by their assignee, sorted by
// their newest comment, stated by their delegation and searchable
// (testdata/chat-task-rows.cjs).
func TestChatTaskRowsUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-task-rows.cjs").CombinedOutput(); err != nil {
		t.Fatalf("task rows: %v\n%s", err, out)
	}
}
