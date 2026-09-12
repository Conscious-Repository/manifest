package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"manifest/reintake"
)

func TestCanaryCommandNoProductionInputs(t *testing.T) {
	// Config, paths, prompt and execution switches cannot enter the checker.
	for _, args := range [][]string{{"--config", "/production/config"}, {"--document", "/production/vault"}, {"--execute"}, {"--evidence-dir", "/production"}} {
		var out bytes.Buffer
		if run(args, &out) != 2 || out.Len() != 0 {
			t.Fatal("accepted production input")
		}
	}
	for _, args := range [][]string{nil, {"--test-fixture"}} {
		var out bytes.Buffer
		if run(args, &out) != 1 {
			t.Fatal("readiness did not refuse")
		}
		var r reintake.ClaudeCanaryReport
		if json.Unmarshal(out.Bytes(), &r) != nil || r.Status != "unsupported/unverified" || r.LiveUsageVerified || r.ProductionRouted || !r.PageOwnerRequired || r.FixtureValidated != (len(args) == 1) {
			t.Fatal(out.String())
		}
	}
}
