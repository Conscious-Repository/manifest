package server

import (
	"os/exec"
	"testing"
)

// The re-intake lane is a one-line status row on the SCHEDULE / RUNS boards
// (state chip · bits · `details →` to Settings › Agents), never the full
// policy sentence as a bare paragraph above the schedule; the legacy Excalibur
// card on Settings › Agents is compact by default with its ritual inventory,
// evidence path, agents and conduits behind a native details fold that keeps
// every evidence string (testdata/agents-ia-compact.cjs).
func TestAgentsInformationArchitectureUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/agents-ia-compact.cjs").CombinedOutput(); err != nil {
		t.Fatalf("agents IA: %v\n%s", err, out)
	}
}
