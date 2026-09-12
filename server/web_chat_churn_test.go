package server

import (
	"os/exec"
	"testing"
)

// The terminal runtime's ~2 s state tick must not rebuild the rail rows and
// the thread head when nothing rendered changed, and a rebuild that does
// happen must keep keyboard focus and an open row menu (QA 2026-09-12).
func TestChatLiveStateChurnUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-live-churn.cjs").CombinedOutput(); err != nil {
		t.Fatalf("live-state churn UI: %v\n%s", err, out)
	}
}
