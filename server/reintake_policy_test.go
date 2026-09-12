package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"manifest/hermes"
)

func TestReIntakePolicyProjection(t *testing.T) {
	dir := t.TempDir()
	s := &Server{}
	p := s.reIntakePrimaryProjection(dir)
	if p["lastAttempt"] != "unknown" || p["lastError"] != "unknown" {
		t.Fatal(p)
	}
	zero := 0.0
	s.hosts = &HostsInfo{}
	s.hosts.Hermes.Duties = map[string]hermes.DutyAuthority{"extractor/re-intake": {Provider: "deepseek-local", Model: "deepseek-v4.1-flash", Endpoint: hermes.LocalEndpoint, CostPolicy: hermes.LocalCostPolicy, ProviderBinding: hermes.LocalProviderBinding, Tools: []string{"none"}, MCP: "no_mcp", MaxSteps: 1, TimeoutSeconds: 120, CeilingUSD: &zero}}
	old := "35-deepseek-primary-canary.jsonl"
	if err := os.WriteFile(filepath.Join(dir, old), []byte("{}\n{\"status\":\"refused\",\"reason\":\"missing usage evidence\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p = s.reIntakePrimaryProjection(dir)
	if p["lastAttempt"] != old || p["lastError"] != "missing usage evidence" || p["evidenceUpdatedAt"] == nil {
		t.Fatal(p)
	}
	for _, record := range []string{
		`{"status":"canary passed","dutyVerified":true}`,
		`{"status":"refused","reason":"private sentinel"}`,
		`{"status":"refused","reason":"outcome uncertain"}`,
		`not-json`,
	} {
		if err := os.WriteFile(filepath.Join(dir, "36-deepseek-primary-canary.jsonl"), []byte("{}\n"+record+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		p = s.reIntakePrimaryProjection(dir)
		if record == `{"status":"canary passed","dutyVerified":true}` && p["canaryStatus"] != "passed (synthetic only)" {
			t.Fatal(p)
		}
		b, _ := json.Marshal(p)
		if strings.Contains(string(b), "sentinel") || p["primary"] != "local DeepSeek" || p["status"] != "shadow" || p["productionRouted"] != false || p["productionOwner"] != "Excalibur" || p["productionRoute"] != "disabled; source-ingest integration unavailable" || p["cost_policy"] != "local-zero-marginal" || p["cost_telemetry"] != "unavailable" || p["configuredAuthority"] != "valid declaration; not routed" || !strings.Contains(p["fallback"].(string), "unsupported/unverified; never automatic") {
			t.Fatal(string(b))
		}
	}
}

func TestReIntakePolicyBrowserProjection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node required for changed UI fixture")
	}
	// Evaluate the actual shared renderer in an isolated JS context; no API calls.
	script := `const fs=require('fs'), vm=require('vm'), assert=require('assert');
 const source=fs.readFileSync('web/js/40-agents.js','utf8');
 const fn=source.slice(source.indexOf('function reIntakePrimarySummary('));
 const ctx={}; vm.createContext(ctx); vm.runInContext(fn,ctx);
 const result=ctx.reIntakePrimarySummary({canaryStatus:'passed (synthetic only)',primary:'local DeepSeek',model:'deepseek-v4.1-flash',cost_policy:'local-zero-marginal',cost_telemetry:'unavailable',provider_binding:'fixed-local-endpoint',configuredAuthority:'missing or invalid',fallback:'owner-invoked Claude Code/Codex only; unsupported/unverified; never automatic',lastAttempt:'35-deepseek-primary-canary.jsonl',lastError:'missing usage evidence'});
 for (const text of ['production route disabled','owner: Excalibur','canary: passed (synthetic only)','shadow / not routed','local-zero-marginal policy; provider cost telemetry unavailable','owner-invoked','unsupported/unverified','last attempt receipt: 35-','last error: missing usage evidence']) assert(result.includes(text),text);
 for (const f of ['41-agents-schedule.js','42-agents-runs.js','60-settings.js']) assert(fs.readFileSync('web/js/'+f,'utf8').includes('reIntakePrimarySummary('),f);
 assert(ctx.reIntakePrimarySummary(null).includes('unavailable'));`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
