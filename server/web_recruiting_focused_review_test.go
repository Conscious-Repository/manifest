package server

import (
	"os/exec"
	"testing"
)

func TestRecruitingFocusedReviewSelection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/recruiting-focused-review.cjs").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
