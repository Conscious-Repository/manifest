package server

import (
	"os/exec"
	"testing"
)

// The Agents tab and Settings › Agents label the primary harness tree's engine
// as the legacy runtime (retiring; Alfred/Hermes is the successor) with the
// tree's real name in the tooltip, keep team trees and Alfred fires on their
// own chips, and split the RUNS week-spend line by runtime
// (testdata/agents-legacy-labels.cjs).
func TestAgentsLegacyEngineLabelsUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/agents-legacy-labels.cjs").CombinedOutput(); err != nil {
		t.Fatalf("legacy labels: %v\n%s", err, out)
	}
}
