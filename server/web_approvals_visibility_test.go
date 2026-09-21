package server

import (
	"os/exec"
	"testing"
)

// The approvals card proposes a visibility tier for a synced transcript in
// the people/category chip vocabulary: pre-selected, overridable by one tap,
// shown only while the note carries `aion`, sent on Confirm only when it was
// offered (testdata/approvals-visibility.cjs).
func TestApprovalsVisibilitySuggestionUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/approvals-visibility.cjs").CombinedOutput(); err != nil {
		t.Fatalf("approvals visibility: %v\n%s", err, out)
	}
}
