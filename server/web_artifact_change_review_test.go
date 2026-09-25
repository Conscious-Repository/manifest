package server

import (
	"os/exec"
	"testing"
)

// The change-review row in the real artifact workspace over a stub API: the
// working-file state (a saved version is not a tree write), folded version
// comparison, an exact newer-line change request carrying its range
// fingerprint, a refused stale range that records nothing, and a decision
// shown as version-bound once a newer head exists, at 320/390/1440px in both
// themes (testdata/artifact-change-review.cjs). Browser-driven, so it runs
// only where Playwright resolves (NODE_PATH); it skips elsewhere.
func TestArtifactChangeReviewBrowserUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	if out, err := exec.Command(node, "testdata/artifact-change-review.cjs").CombinedOutput(); err != nil {
		t.Fatalf("artifact change review (browser): %v\n%s", err, out)
	}
}
