package reintake

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
	"manifest/hermes"
)

func authority() hermes.DutyAuthority {
	ceiling := 0.0
	return hermes.DutyAuthority{CostPolicy: hermes.LocalCostPolicy, Endpoint: hermes.LocalEndpoint, ProviderBinding: hermes.LocalProviderBinding, Provider: "deepseek-local", Model: Model, Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 120, MaxSteps: 1, CeilingUSD: &ceiling}
}
func duties() map[string]hermes.DutyAuthority {
	return map[string]hermes.DutyAuthority{Duty: authority()}
}
func load(t *testing.T, id string) fixture {
	t.Helper()
	b, err := fixtures.ReadFile("fixtures/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err = decodeStrict(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func encode(t *testing.T, f fixture) []byte {
	t.Helper()
	b, e := json.Marshal(f)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestReIntakeDefaultOff(t *testing.T) {
	dir := t.TempDir()
	r, e := Replay(dir, Config{}, duties(), "single")
	if e == nil || r.StructuredParity {
		t.Fatal("default enabled")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("disabled replay wrote state")
	}
}
func TestReIntakeStructuredParityAndIsolation(t *testing.T) {
	for _, id := range []string{"single", "split"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			for _, p := range []string{"artifacts/approvals/pending/sentinel", "vault/sentinel", "vessel/spool/sentinel", "portal/sentinel"} {
				path := filepath.Join(dir, p)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r, e := Replay(dir, Config{ShadowEnabled: true}, duties(), id)
			if e != nil || !r.StructuredParity || r.LiveUsageVerified || r.ProductionRouted || r.PortalVisible {
				t.Fatalf("invalid shadow report: %v", e)
			}
			count := 0
			err := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(dir, p)
				if strings.HasPrefix(rel, ShadowPath+"/") {
					count++
					return nil
				}
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				if string(b) != "untouched" {
					t.Fatal("production sentinel changed")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if count != 2 {
				t.Fatalf("expected shadow receipt and private ledger, got %d", count)
			}
			t.Log("structured source/actor/target/apply-path/type/payload parity; prose semantic-review-only; production writers absent")
		})
	}
}
func TestReIntakeAuthorityRefusal(t *testing.T) {
	for _, kind := range []string{"missing", "model", "provider", "tools", "default-tools", "mcp", "bounds", "ceiling"} {
		t.Run(kind, func(t *testing.T) {
			a := authority()
			ds := duties()
			switch kind {
			case "missing":
				ds = nil
			case "model":
				a.Model = "drift"
			case "provider":
				a.Provider = "auto"
			case "tools":
				a.Tools = nil
			case "default-tools":
				a.Tools = []string{"web"}
			case "mcp":
				a.MCP = ""
			case "bounds":
				a.MaxSteps = 0
			case "ceiling":
				a.CeilingUSD = nil
			}
			if ds != nil {
				ds[Duty] = a
			}
			_, e := Replay(t.TempDir(), Config{ShadowEnabled: true}, ds, "single")
			if e == nil || !strings.Contains(e.Error(), StopAndPage) {
				t.Fatal("authority accepted")
			}
		})
	}
}
func TestReIntakeUsageRefusal(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"model":"drift","provider":"deepseek-local","completed":true,"cost_usd":0,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"drift","completed":true,"cost_usd":0,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":false,"cost_usd":0,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_usd":3,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_usd":null,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_usd":0,"steps":16}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_usd":0,"cost_usd":1,"steps":1}`,
	} {
		f := load(t, "single")
		f.Usage = json.RawMessage(raw)
		if compare(authority(), f) == nil {
			t.Fatal("invalid usage accepted")
		}
	}
}
func TestReIntakeScopeAndPayloadRefusal(t *testing.T) {
	for _, kind := range []string{"owner", "documents", "portal", "proposal-count", "action", "backlog", "generic", "actor", "ritual", "path", "proposed", "source", "total", "kind", "allocation", "unknown", "duplicate", "target-parity", "payload-parity"} {
		t.Run(kind, func(t *testing.T) {
			f := load(t, "single")
			p := &f.Proposals[0]
			switch kind {
			case "owner":
				f.Request.OwnerTriggered = false
			case "documents":
				f.Request.Documents = append(f.Request.Documents, "another-document")
			case "portal":
				f.Request.PortalVisible = true
			case "proposal-count":
				f.Proposals = append(f.Proposals, *p)
			case "action":
				p.Action = ""
			case "backlog":
				p.Type = approvals.TypeReBacklog
				p.ApplyPath = approvals.ReBacklogPath
			case "generic":
				p.Type = "approval"
			case "actor":
				p.Agent = "hermes"
			case "ritual":
				p.Ritual = "real-estate"
			case "path":
				p.ApplyPath = "system/realestate/contracts/../../escape.md"
			case "proposed":
				p.Proposed = "unauthorized write"
			case "source":
				f.Request.Documents[0] = "different"
			case "total":
				p.Body = strings.Replace(p.Body, `"total": 100`, `"total": 101`, 1)
			case "kind":
				p.Body = strings.Replace(p.Body, `"kind": "bid"`, `"kind": "heuristic"`, 1)
			case "allocation":
				p.Body = strings.Replace(p.Body, `"amount": 100`, `"amount": -1`, 1)
			case "unknown":
				p.Body = strings.Replace(p.Body, `"total": 100`, `"unknown": true, "total": 100`, 1)
			case "duplicate":
				p.Body = strings.Replace(p.Body, `"total": 100`, `"total": 1, "total": 100`, 1)
			case "target-parity":
				f.Expected.Target = "different"
			case "payload-parity":
				f.Expected.Payload.Date = "2001-01-01"
			}
			if compare(authority(), f) == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}
func TestReIntakeProseSemanticOnly(t *testing.T) {
	f := load(t, "single")
	p, ok := approvals.ParseReContractPayload(f.Proposals[0].Body)
	if !ok {
		t.Fatal("payload")
	}
	p.Name = "different synthetic prose"
	p.Terms = []string{"different synthetic terms"}
	// Keep cardinalities; original terms can have multiple entries.
	p.Terms = make([]string, len(f.Expected.Payload.Terms))
	for i := range p.Terms {
		p.Terms[i] = "different synthetic terms"
	}
	b, _ := json.Marshal(p)
	f.Proposals[0].Body = "New synthetic summary\n````re-contract\n" + string(b) + "\n````"
	if e := compare(authority(), f); e != nil {
		t.Fatal(e)
	}
}
func TestReIntakeStopAndPageIsLatched(t *testing.T) {
	dir := t.TempDir()
	f := load(t, "single")
	f.Request.PortalVisible = true
	r, e := replay(dir, duties(), encode(t, f))
	if e == nil || r.Entry.Kind != "run.refused" || r.Entry.Text != StopAndPage {
		t.Fatal("no explicit stop-and-page")
	}
	if _, e = Replay(dir, Config{ShadowEnabled: true}, duties(), "single"); e == nil {
		t.Fatal("automatic recovery accepted")
	}
	b, e := os.ReadFile(filepath.Join(dir, ShadowPath, "STOP.json"))
	if e != nil || !strings.Contains(string(b), "pageOwnerRequired") {
		t.Fatal("missing local page evidence")
	}
}
func TestReIntakePathsRefuseSymlinksAndOverwrite(t *testing.T) {
	for _, rel := range []string{"excalibur-retirement", "excalibur-retirement/shadow", ShadowPath} {
		t.Run(rel, func(t *testing.T) {
			dir, out := t.TempDir(), t.TempDir()
			dest := filepath.Join(dir, rel)
			if e := os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(out, dest); e != nil {
				t.Fatal(e)
			}
			if _, e := Replay(dir, Config{ShadowEnabled: true}, duties(), "single"); e == nil {
				t.Fatal("symlink accepted")
			}
			entries, _ := os.ReadDir(out)
			if len(entries) != 0 {
				t.Fatal("escaped shadow")
			}
		})
	}
	dir := t.TempDir()
	if _, e := Replay(dir, Config{ShadowEnabled: true}, duties(), "single"); e != nil {
		t.Fatal(e)
	}
	if _, e := Replay(dir, Config{ShadowEnabled: true}, duties(), "single"); e == nil {
		t.Fatal("replay overwrote evidence")
	}
	if _, e := Replay("relative", Config{ShadowEnabled: true}, duties(), "single"); e == nil {
		t.Fatal("relative dataDir accepted")
	}
	if _, e := Replay(t.TempDir(), Config{ShadowEnabled: true}, duties(), "../single"); e == nil {
		t.Fatal("arbitrary fixture path accepted")
	}
}
func TestReIntakeCutoverRequiresRecordedPause(t *testing.T) {
	e := CutoverEvidence{ManifestFlagOff: true, OldEnginePaused: true, PauseRecord: "owner-reviewed-record", SnapshotSHA256: strings.Repeat("a", 64), OwnershipRecord: "owner-reviewed-transfer", NoQueuedOrRunning: true, OwnerComparisonApproved: true}
	if err := e.Check(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"paused", "record", "off", "snapshot", "ownership", "pending", "comparison"} {
		t.Run(kind, func(t *testing.T) {
			bad := e
			switch kind {
			case "paused":
				bad.OldEnginePaused = false
			case "record":
				bad.PauseRecord = ""
			case "off":
				bad.ManifestFlagOff = false
			case "snapshot":
				bad.SnapshotSHA256 = ""
			case "ownership":
				bad.OwnershipRecord = ""
			case "pending":
				bad.NoQueuedOrRunning = false
			case "comparison":
				bad.OwnerComparisonApproved = false
			}
			if err := bad.Check(); err == nil || !strings.Contains(err.Error(), StopAndPage) {
				t.Fatal("incomplete cutover accepted")
			}
		})
	}
}
func TestReIntakeRealRunnerStillRefusesClaudeBeforeLaunch(t *testing.T) {
	// /does-not-exist can never run. A specific authority refusal, not an exec
	// error, proves neither the legacy CLI nor successor process was reached.
	r := hermes.NewRunner(hermes.Config{Enabled: true, Bin: "/does-not-exist", Duties: map[string]hermes.DutyAuthority{Duty: canaryAuthority()}})
	res, err := r.Run(context.Background(), hermes.Request{MigratedDuty: Duty, Prompt: "synthetic fixture"})
	if err == nil || !strings.Contains(err.Error(), "unsupported zero-cost successor identity") || res.DutyVerified() || res.Reply != "" {
		t.Fatal("Claude execution gate weakened")
	}
}

func TestReIntakeEvidenceFailureStopsWithoutEscape(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	root, e := shadowRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	root.Close()
	if e = os.Symlink(filepath.Join(out, "ledger"), filepath.Join(dir, ShadowPath, "2000-01-01.jsonl")); e != nil {
		t.Fatal(e)
	}
	_, e = Replay(dir, Config{ShadowEnabled: true}, duties(), "single")
	if e == nil || !strings.Contains(e.Error(), StopAndPage) {
		t.Fatal("ledger failure accepted")
	}
	files, _ := os.ReadDir(out)
	if len(files) != 0 {
		t.Fatal("ledger escaped")
	}
	if _, e = os.Stat(filepath.Join(dir, ShadowPath, "STOP.json")); e != nil {
		t.Fatal("missing failure latch")
	}
}

func TestReIntakeUsageCannotClaimTools(t *testing.T) {
	f := load(t, "single")
	f.Usage = json.RawMessage(`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_usd":0,"steps":1,"tool_calls":["shell"]}`)
	if compare(authority(), f) == nil {
		t.Fatal("unexpected usage authority accepted")
	}
}

func TestReIntakePrimaryExactContract(t *testing.T) {
	a := authority()
	if err := validateAuthority(a); err != nil {
		t.Fatal(err)
	}
	if a.Provider != "deepseek-local" || a.Model != "deepseek-v4.1-flash" || a.MaxSteps != 1 || a.TimeoutSeconds != 120 || *a.CeilingUSD != 0 {
		t.Fatal("primary declaration drift")
	}
	for _, kind := range []string{"cost", "steps", "timeout", "claude", "codex"} {
		b := authority()
		switch kind {
		case "cost":
			*b.CeilingUSD = 0.01
		case "steps":
			b.MaxSteps = 2
		case "timeout":
			b.TimeoutSeconds = 121
		case "claude":
			b = canaryAuthority()
		case "codex":
			b.Provider = "codex-sub"
			b.Model = "gpt-6-astra"
		}
		if validateAuthority(b) == nil {
			t.Fatalf("accepted %s", kind)
		}
	}
}

func TestReIntakePrimaryUncertaintyFreezesWithoutFallback(t *testing.T) {
	for _, claim := range []string{`"completed":false`, `"cost_usd":0.01`, `"model":"drift"`, `"provider":"drift"`, `"tool_calls":["shell"]`, `"mcp":"portal"`, `"fallback":true`, `"outcome":"uncertain"`, `"error":"timeout"`, `"error":"crash-after-effect"`} {
		t.Run(claim, func(t *testing.T) {
			f := load(t, "single")
			var usage map[string]json.RawMessage
			if err := json.Unmarshal(f.Usage, &usage); err != nil {
				t.Fatal(err)
			}
			var mutation map[string]json.RawMessage
			if err := json.Unmarshal([]byte("{"+claim+"}"), &mutation); err != nil {
				t.Fatal(err)
			}
			for k, v := range mutation {
				usage[k] = v
			}
			f.Usage, _ = json.Marshal(usage)
			dir := t.TempDir()
			r, err := replay(dir, duties(), encode(t, f))
			if err == nil || r.StructuredParity || r.LiveUsageVerified || r.ProductionRouted || r.Entry.Meta["fallback"] != false {
				t.Fatal("uncertainty accepted")
			}
			if _, err = Replay(dir, Config{ShadowEnabled: true}, duties(), "single"); err == nil {
				t.Fatal("lane not frozen")
			}
		})
	}
}
