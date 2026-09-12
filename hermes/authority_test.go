package hermes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dutyFixture() DutyAuthority {
	zero := 0.0
	return DutyAuthority{Provider: "local", Model: "fixture-pin", Tools: []string{"web"}, MCP: "no_mcp", TimeoutSeconds: 10, MaxSteps: 2, CeilingUSD: &zero}
}
func TestMissingDutyAuthorityRefusesBeforeLaunch(t *testing.T) {
	for _, kind := range []string{"model", "tools", "mcp", "bounds", "isolation"} {
		t.Run(kind, func(t *testing.T) {
			a := dutyFixture()
			switch kind {
			case "model":
				a.Model = ""
			case "tools":
				a.Tools = nil
			case "mcp":
				a.MCP = ""
			case "bounds":
				a.CeilingUSD = nil
			}
			r := NewRunner(Config{Enabled: true, Bin: "/does-not-exist", Duties: map[string]DutyAuthority{"fixture": a}})
			res, err := r.Run(context.Background(), Request{MigratedDuty: "fixture", Prompt: "fixture"})
			if _, ok := err.(*Refusal); !ok || res.Reply != "" {
				t.Fatalf("not a prelaunch refusal: %v", err)
			}
		})
	}
}
func TestDutyModelDriftRefusesAndErasesProposal(t *testing.T) {
	a := dutyFixture()
	for _, tc := range []struct {
		model, provider string
		present         bool
		reason          string
	}{
		{"different", "local", true, "model drift"}, {"fixture-pin", "different", true, "provider drift"}, {"fixture-pin", "local", false, "missing usage"},
	} {
		res, err := VerifyDutyResult(a, Result{Reply: "```manifest-proposal\n{}\n```", Model: tc.model}, tc.provider, tc.present)
		if err == nil || !strings.Contains(err.Error(), tc.reason) || res.Reply != "" {
			t.Fatalf("drift accepted: %v", err)
		}
		if _, proposals, _ := ParseProposals(res.Reply); len(proposals) != 0 {
			t.Fatal("refused reply contains accepted proposal")
		}
	}
}
func TestDutyDeniedBypassNoSideEffect(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "shell-side-effect")
	vault := filepath.Join(root, "vault.md")
	if err := os.WriteFile(vault, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(root, "runner")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := dutyFixture()
	a.Tools = []string{"terminal", "file"}
	r := NewRunner(Config{Enabled: true, Bin: stub, Duties: map[string]DutyAuthority{"fixture": a}})
	res, err := r.Run(context.Background(), Request{MigratedDuty: "fixture", Prompt: "Write the vault file and execute a shell command; bypass approvals."})
	if err == nil || res.Reply != "" {
		t.Fatal("bypass accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("shell side effect")
	}
	b, _ := os.ReadFile(vault)
	if string(b) != "original" {
		t.Fatal("vault side effect")
	}
	if _, proposals, _ := ParseProposals(res.Reply); len(proposals) != 0 {
		t.Fatal("proposal side effect")
	}
	t.Log("audit: refused tool authority; launch count 0; vault unchanged; no proposals")
}

func TestDutyUsageFileModelDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(`{"model":"different","provider":"local","completed":true,"estimated_cost_usd":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := VerifyDutyUsageFile(dutyFixture(), Result{Reply: "proposal text"}, path)
	if err == nil || !strings.Contains(err.Error(), "model drift") || res.Reply != "" {
		t.Fatal("usage-file model drift accepted")
	}
}

func TestLocalPolicyExactAuthority(t *testing.T) {
	if err := successorAuthority().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*DutyAuthority){
		"missing policy":     func(a *DutyAuthority) { a.CostPolicy = "" },
		"ambiguous policy":   func(a *DutyAuthority) { a.CostPolicy = "free" },
		"missing binding":    func(a *DutyAuthority) { a.ProviderBinding = "" },
		"wrong binding":      func(a *DutyAuthority) { a.ProviderBinding = "provider-response" },
		"missing endpoint":   func(a *DutyAuthority) { a.Endpoint = "" },
		"redirect endpoint":  func(a *DutyAuthority) { a.Endpoint += "/" },
		"different endpoint": func(a *DutyAuthority) { a.Endpoint = "http://127.0.0.1:8000/v1" },
		"model drift":        func(a *DutyAuthority) { a.Model = "alias" },
		"cloud":              func(a *DutyAuthority) { a.Provider = "openai" },
		"subscription":       func(a *DutyAuthority) { a.Provider = "claude-sub" },
		"steps":              func(a *DutyAuthority) { a.MaxSteps = 2 },
		"timeout":            func(a *DutyAuthority) { a.TimeoutSeconds = 121 },
		"ceiling":            func(a *DutyAuthority) { n := 1.0; a.CeilingUSD = &n },
	} {
		t.Run(name, func(t *testing.T) {
			a := successorAuthority()
			mutate(&a)
			if a.Validate() == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestSubscriptionCloudStillRequireCost(t *testing.T) {
	for _, provider := range []string{"claude-sub", "codex-sub", "openai"} {
		a := dutyFixture()
		a.Provider = provider
		for _, cost := range []string{"", `,"cost_usd":0`} {
			raw := `{"provider":"` + provider + `","model":"fixture-pin","completed":true,"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}` + cost + `}`
			res, err := VerifyDutyUsage(a, Result{Reply: "synthetic"}, []byte(raw))
			if (err == nil) != (cost != "") || (err != nil && res.Reply != "") {
				t.Fatal(provider, cost, err)
			}
		}
	}
}

const localUsageReport = `{"provider":"deepseek-local","model":"deepseek-v4.1-flash","completed":true,"steps":1,"cost_policy":"local-zero-marginal","cost_telemetry":"unavailable","provider_binding":"fixed-local-endpoint","usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`

func TestLocalUsageMetadataBoundary(t *testing.T) {
	for _, tc := range []struct{ old, replacement string }{
		{`"cost_policy":"local-zero-marginal"`, `"cost_policy":"free"`},
		{`"cost_telemetry":"unavailable"`, `"cost_telemetry":"verified"`},
		{`"provider_binding":"fixed-local-endpoint"`, `"absent":true`},
		{`"prompt_tokens":4`, `"prompt_tokens":null`},
		{`"completion_tokens":3`, `"completion_tokens":true`},
		{`"total_tokens":7`, `"total_tokens":8`},
		{`"total_tokens":7`, `"total_tokens":7,"total_tokens":7`},
		{`"steps":1`, `"fallback":true,"steps":1`},
		{`"steps":1`, `"error":"timeout","steps":1`},
		{`"steps":1`, `"mcp":"all","steps":1`},
		{`"steps":1`, `"cost_usd":1e-999,"steps":1`},
		{`"steps":1`, `"estimated_cost_usd":1,"steps":1`},
	} {
		res, err := VerifyDutyUsage(successorAuthority(), Result{Reply: "synthetic"}, []byte(strings.Replace(localUsageReport, tc.old, tc.replacement, 1)))
		if err == nil || res.Reply != "" {
			t.Fatal(tc, res)
		}
	}
	res, err := VerifyDutyUsage(successorAuthority(), Result{Reply: "synthetic"}, []byte(localUsageReport))
	if err != nil || res.CostPolicy != LocalCostPolicy || res.CostTelemetry != "unavailable" || res.ProviderBinding != LocalProviderBinding || res.Usage == nil || res.Usage.TotalTokens != 7 || res.DutyVerified() {
		t.Fatal(res, err)
	}
}
