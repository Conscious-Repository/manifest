package hermes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func subscriptionAuthority() DutyAuthority {
	budget := 1.0
	return DutyAuthority{Provider: "claude-sub", Model: "claude-sonnet-5", Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 30, MaxSteps: 1, CeilingUSD: &budget}
}
func subscriptionEvidence() string {
	return `{"type":"system","subtype":"init","model":"claude-sonnet-5","tools":[],"mcp_servers":[]}
{"type":"assistant","message":{"model":"claude-sonnet-5","content":[{"type":"text"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"{\"candidates\":[]}","total_cost_usd":0.01,"num_turns":1,"usage":{"input_tokens":10,"output_tokens":5},"modelUsage":{"claude-sonnet-5":{}}}
`
}
func TestSubscriptionEvidenceRefusesDrift(t *testing.T) {
	a := subscriptionAuthority()
	res, e := parseSubscriptionResult([]byte(subscriptionEvidence()), a)
	if e != nil || !res.DutyVerified() {
		t.Fatal(res, e)
	}
	for _, raw := range []string{
		strings.ReplaceAll(subscriptionEvidence(), "claude-sonnet-5", "other"),
		strings.Replace(subscriptionEvidence(), `"tools":[]`, `"tools":["Bash"]`, 1),
		strings.Replace(subscriptionEvidence(), `"mcp_servers":[]`, `"mcp_servers":[{}]`, 1),
		strings.Replace(subscriptionEvidence(), `"type":"text"`, `"type":"tool_use"`, 1),
		strings.Replace(subscriptionEvidence(), `"total_cost_usd":0.01`, `"total_cost_usd":3`, 1),
		strings.Replace(subscriptionEvidence(), `"num_turns":1`, `"num_turns":2`, 1),
		strings.SplitN(subscriptionEvidence(), "\n", 2)[1],
	} {
		res, e := parseSubscriptionResult([]byte(raw), a)
		if e == nil || res.Reply != "" || res.DutyVerified() {
			t.Fatal("accepted invalid evidence", e)
		}
	}
	if claudeDutyAllowed("extractor/re-intake", a) {
		t.Fatal("silently changed re-intake authority")
	}
}
func TestSubscriptionOSBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".claude"), 0700)
	os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte("{}"), 0600)
	outside := filepath.Join(home, "protected")
	os.WriteFile(outside, []byte("preserve"), 0600)
	bin := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	script := fmt.Sprintf(`#!/usr/bin/python3
import os, sys
try:
 open(%q).read()
 sys.exit(10)
except PermissionError:
 pass
try:
 open(%q,'w').write('changed')
 sys.exit(11)
except PermissionError:
 pass
assert 'ANTHROPIC_API_KEY' not in os.environ
assert '--safe-mode' in sys.argv and '--strict-mcp-config' in sys.argv
assert sys.argv[sys.argv.index('--tools')+1] == ''
sys.stdout.write(%q)
`, outside, outside, subscriptionEvidence())
	if e := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("ANTHROPIC_API_KEY", "must-not-pass")
	a := subscriptionAuthority()
	runner := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"extractor/aion": a}})
	res, e := runner.Run(context.Background(), Request{MigratedDuty: "extractor/aion", Prompt: "fixture"})
	if e != nil || !res.DutyVerified() {
		t.Fatalf("real OS boundary: %v", e)
	}
	b, _ := os.ReadFile(outside)
	if string(b) != "preserve" {
		t.Fatal("outside write")
	}
}
