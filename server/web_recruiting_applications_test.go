package server

import (
	"os/exec"
	"testing"
)

func TestRecruitingApplicationUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/recruiting-applications.cjs").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func TestRecruitingStageControls(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/recruiting-stage-controls.cjs").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

// Recruiting edits stay saved on screen across save repaints, racing live
// reads and refusals (recruiting-edit-state-browser.cjs).
func TestFixtureRecruitingEditState(t *testing.T) {
	runFixture(t, "recruiting-edit-state-browser.cjs", true, false)
}
