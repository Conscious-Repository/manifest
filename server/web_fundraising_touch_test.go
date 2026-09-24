package server

import (
	"os/exec"
	"testing"
)

// The tracker's touch lines: one quiet kind · date · person line under each
// touch, none when there is no source, Sheet-typed names shown as pending,
// and the inspector keeping a losing hand-typed date visible
// (testdata/fundraising-touch-rows.cjs).
func TestFundraisingTouchRowsUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/fundraising-touch-rows.cjs").CombinedOutput(); err != nil {
		t.Fatalf("touch rows: %v\n%s", err, out)
	}
}
