package hermes

import "context"

// PrimaryCanaryReport contains only fixed authority and verified metadata. Null
// usage means unknown, never an inferred zero-cost/completed outcome.
type PrimaryCanaryReport struct {
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	CostUSD      *float64 `json:"cost_usd"`
	Completed    *bool    `json:"completed"`
	Steps        *int     `json:"steps"`
	Tools        []string `json:"tools"`
	MCP          string   `json:"mcp"`
	Fallback     bool     `json:"fallback"`
	DutyVerified bool     `json:"dutyVerified"`
}

func PrimaryCanaryRefusal(reason string) PrimaryCanaryReport {
	// Never reflect arbitrary error strings or provider data.
	switch reason {
	case "confirmation required", "invalid arguments", "receipt unavailable", "prior or uncertain invocation", "outcome persistence failed", "outcome uncertain", "reply mismatch", "successor OS isolation unavailable", "cost bound exceeded", "model drift", "provider drift", "incomplete successor completion", "missing usage evidence", "bounded successor execution failed", "successor timeout or cancellation", "invalid successor step evidence", "usage evidence unavailable":
	default:
		reason = "outcome uncertain"
	}
	return PrimaryCanaryReport{Status: "refused", Reason: reason, Provider: "deepseek-local", Model: "deepseek-v4.1-flash", Tools: []string{"none"}, MCP: "no_mcp"}
}

// RunPrimaryCanary is owner-command-only. No document, config, prompt, provider,
// fallback or production handle can be supplied. The command durably reserves
// its one-time receipt before calling this function.
func RunPrimaryCanary(ctx context.Context) PrimaryCanaryReport {
	zero := 0.0
	a := DutyAuthority{Provider: "deepseek-local", Model: "deepseek-v4.1-flash", Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 120, MaxSteps: 1, CeilingUSD: &zero}
	runner := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"extractor/re-intake": a}})
	res, err := runner.Run(ctx, Request{MigratedDuty: "extractor/re-intake", Prompt: "Return exactly the word CANARY-OK and no tool calls.", TimeoutSeconds: 120})
	if err != nil {
		if refusal, ok := err.(*Refusal); ok {
			return PrimaryCanaryRefusal(refusal.Reason)
		}
		return PrimaryCanaryRefusal("outcome uncertain")
	}
	if !res.DutyVerified() || res.Model != a.Model || res.SpentUSD != 0 {
		return PrimaryCanaryRefusal("outcome uncertain")
	}
	if res.Reply != "CANARY-OK" {
		return PrimaryCanaryRefusal("reply mismatch")
	}
	report := PrimaryCanaryRefusal("")
	report.Status, report.Reason = "canary passed", ""
	completed, steps := true, 1
	report.CostUSD, report.Completed, report.Steps, report.DutyVerified = &zero, &completed, &steps, true
	return report
}
