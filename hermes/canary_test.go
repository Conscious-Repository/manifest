package hermes

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPrimaryCanaryTransport(t *testing.T) {
	valid := strings.Replace(matchingResponse, "verified proposal text", "CANARY-OK", 1)
	cases := map[string]string{
		"success":            valid,
		"standard":           strings.Replace(strings.Replace(valid, `"provider":"deepseek-local",`, "", 1), `,"cost_usd":0`, "", 1),
		"reply":              strings.Replace(valid, "CANARY-OK", "private response sentinel", 1),
		"whitespace":         strings.Replace(valid, "CANARY-OK", "CANARY-OK ", 1),
		"model":              strings.Replace(valid, "deepseek-v4.1-flash", "drift", 1),
		"provider":           strings.Replace(valid, "deepseek-local", "drift", 1),
		"cost":               strings.Replace(valid, `"cost_usd":0`, `"cost_usd":1`, 1),
		"missing usage":      strings.Replace(valid, `"usage"`, `"absent"`, 1),
		"null":               strings.Replace(valid, `"cost_usd":0`, `"cost_usd":null`, 1),
		"underflow":          strings.Replace(valid, `"cost_usd":0`, `"cost_usd":1e-999`, 1),
		"duplicate":          strings.Replace(valid, `"cost_usd":0`, `"cost_usd":1,"cost_usd":0`, 1),
		"incomplete":         strings.Replace(valid, `"stop"`, `"length"`, 1),
		"tools":              strings.Replace(valid, `"content":`, `"tool_calls":[{"name":"forbidden"}],"content":`, 1),
		"function":           strings.Replace(valid, `"content":`, `"function_call":{"name":"forbidden"},"content":`, 1),
		"fallback":           strings.Replace(valid, `"usage":`, `"fallback":true,"usage":`, 1),
		"uncertain fallback": strings.Replace(valid, `"usage":`, `"fallback":null,"usage":`, 1),
		"claimed tools":      strings.Replace(valid, `"usage":`, `"tools":["web"],"usage":`, 1),
		"claimed MCP":        strings.Replace(valid, `"usage":`, `"mcp":"all","usage":`, 1),
		"multiple choices":   strings.Replace(valid, `"choices":[`, `"choices":[{"finish_reason":"stop","message":{"content":"CANARY-OK"}},`, 1),
		"claimed tool calls": strings.Replace(valid, `"usage":`, `"tool_calls":[{}],"usage":`, 1),
		"malformed":          `b'not-json'`,
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			calls := stubSuccessor(t, response, `original_loads = json.loads
def checked_loads(raw, **kwargs):
    data = original_loads(raw, **kwargs)
    if 'authority' in data:
        assert data == {'authority': {'costPolicy': 'local-zero-marginal', 'endpoint': 'http://192.168.87.11:8000/v1', 'providerBinding': 'fixed-local-endpoint', 'provider': PROVIDER, 'model': MODEL, 'tools': ['none'], 'mcp': 'no_mcp', 'timeoutSeconds': 120, 'maxSteps': 1, 'ceilingUsd': 0}, 'prompt': 'Return exactly the word CANARY-OK and no tool calls.'}
    return data
json.loads = checked_loads`)
			report := RunPrimaryCanary(t.Context())
			if *calls != 1 || (report.Status == "canary passed") != (name == "success" || name == "standard") {
				t.Fatalf("calls=%d report=%+v", *calls, report)
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), "sentinel") || strings.Contains(string(raw), "CANARY-OK") {
				t.Fatal("response leaked")
			}
			if name == "success" || name == "standard" {
				if report.CostPolicy != LocalCostPolicy || report.ProviderBinding != LocalProviderBinding || report.CostTelemetry != "unavailable" || report.Usage == nil || report.Usage.TotalTokens != 7 {
					t.Fatal(report)
				}
				if !report.DutyVerified || report.CostUSD == nil || *report.CostUSD != 0 || report.Completed == nil || !*report.Completed || report.Steps == nil || *report.Steps != 1 {
					t.Fatal(report)
				}
			} else if report.DutyVerified || report.CostUSD != nil || report.Completed != nil || report.Steps != nil {
				t.Fatal("unknown usage asserted")
			}
		})
	}
}

func TestPrimaryCanaryRefusedLauncher(t *testing.T) {
	for _, diagnostic := range []string{"isolation", "cost", "model", "provider_drift", "completion", "usage", "provider", "private diagnostic sentinel"} {
		t.Run(diagnostic, func(t *testing.T) {
			old := successorCommand
			t.Cleanup(func() { successorCommand = old })
			calls := 0
			successorCommand = func(ctx context.Context, path string) *exec.Cmd {
				calls++
				return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", "import sys;sys.stderr.write(sys.argv[1]);sys.exit(1)", diagnostic)
			}
			r := RunPrimaryCanary(t.Context())
			b, _ := json.Marshal(r)
			if calls != 1 || r.Status != "refused" || strings.Contains(string(b), "sentinel") {
				t.Fatal(string(b))
			}
		})
	}
}

func TestPrimaryCanaryTimeout(t *testing.T) {
	calls := stubSuccessor(t, matchingResponse, "import time\ntime.sleep(20)")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := RunPrimaryCanary(ctx)
	if r.Reason != "successor timeout or cancellation" || *calls != 1 || time.Since(start) > 3*time.Second {
		t.Fatal(r)
	}
}

func TestPrimaryCanaryUncertainSteps(t *testing.T) {
	for _, steps := range []string{"", `,"steps":0`, `,"steps":2`, `,"steps":null`} {
		t.Run(steps, func(t *testing.T) {
			old := successorCommand
			t.Cleanup(func() { successorCommand = old })
			calls := 0
			successorCommand = func(ctx context.Context, path string) *exec.Cmd {
				calls++
				return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", `import sys;open(sys.argv[1],'w').write(sys.argv[2]);sys.stdout.write('CANARY-OK')`, path, `{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_policy":"local-zero-marginal","cost_telemetry":"unavailable","provider_binding":"fixed-local-endpoint","usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7},"cost_usd":0`+steps+`}`)
			}
			r := RunPrimaryCanary(t.Context())
			if r.Status != "refused" || r.Reason != "invalid successor step evidence" || calls != 1 {
				t.Fatal(r)
			}
		})
	}
}
