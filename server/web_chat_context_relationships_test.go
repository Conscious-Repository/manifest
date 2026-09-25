package server

import (
	"os/exec"
	"testing"
)

// The relationships row in the real front end over a stub API: the `[[`
// record typeahead (exact IDs, unavailable kinds named, review before
// selection, nothing sent or retained by choosing), explicit retention, the
// Context skill inventory, the shared-conversation gate and a 390px fit
// (testdata/chat-context-relationships.cjs). Browser-driven, so it runs only
// where Playwright resolves (NODE_PATH); it skips elsewhere.
func TestChatContextRelationshipsBrowserUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/chat-context-relationships.cjs").CombinedOutput(); err != nil {
		t.Fatalf("context relationships (browser): %v\n%s", err, out)
	}
}
