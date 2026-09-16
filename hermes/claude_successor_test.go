package hermes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExtractionCLI(t *testing.T) {
	old := extractionCommand
	t.Cleanup(func() { extractionCommand = old })
	prompt := "fixture $(touch /tmp/not-executed); `literal`"
	extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		want := []string{"chat", "-Q", "-q", prompt, "-m", "sparks", "--provider", "lab-sparks", "--safe-mode", "-t", "none", "--max-turns", "1", "--source", "tool", "--cli"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("argv = %q", args)
		}
		// A fixture process inspects the actual environment and private config.
		return exec.CommandContext(ctx, "/usr/bin/python3", "-c", `import os,json
assert os.getcwd()==os.environ['HERMES_HOME']==os.environ['HOME']
c=json.load(open(os.path.join(os.environ['HERMES_HOME'],'config.yaml')))
assert c['model']['aliases']['sparks']=='lab-sparks/deepseek-v4.1-flash'
assert c['custom_providers'][0]['name']=='lab-sparks'
assert c['custom_providers'][0]['extra_body']=={'model':'deepseek-v4.1-flash','tool_choice':'none'}
assert c['fallback_providers']==[] and c['fallback_model'] is None and c['mcp_servers']=={}
assert 'ANTHROPIC_API_KEY' not in os.environ
assert 'HERMES_KANBAN_TASK' not in os.environ
print('{"candidates":[]}')`)
	}
	t.Setenv("ANTHROPIC_API_KEY", "must-not-pass")
	t.Setenv("HERMES_KANBAN_TASK", "must-not-pass")
	r := NewRunner(Config{Enabled: true, Bin: "/must-not-use"})
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		if err := r.ValidateExtractionDuty(ritual); err != nil {
			t.Fatal(err)
		}
		res, err := r.Run(context.Background(), Request{MigratedDuty: "extractor/" + ritual, Prompt: prompt})
		if err != nil || !res.DutyVerified() || res.Model != "deepseek-v4.1-flash" || res.Reply != `{"candidates":[]}` || res.CostTelemetry != "unavailable" {
			t.Fatal(res, err)
		}
	}
	if r.ValidateExtractionDuty("re-intake") == nil {
		t.Fatal("widened duty scope")
	}
}

func TestExtractionFailuresAndNoFallback(t *testing.T) {
	old := extractionCommand
	t.Cleanup(func() { extractionCommand = old })
	for _, mode := range []string{"nonzero", "timeout", "cancel", "malformed", "missing"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
				calls++
				switch mode {
				case "nonzero":
					return exec.CommandContext(ctx, "/usr/bin/python3", "-c", `print('{"candidates":[]}');exit(1)`)
				case "timeout", "cancel":
					return exec.CommandContext(ctx, "/usr/bin/sleep", "10")
				case "missing":
					return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "absent"))
				default:
					return exec.CommandContext(ctx, "/usr/bin/printf", "not JSON")
				}
			}
			r := NewRunner(Config{Enabled: true})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			started := time.Now()
			res, err := r.Run(ctx, Request{MigratedDuty: "extractor/aion", Prompt: "fixture", TimeoutSeconds: 1})
			if err == nil || res.Reply != "" || res.DutyVerified() || calls != 1 {
				t.Fatal(res, err, calls)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("unbounded process")
			}
		})
	}
	calls := 0
	extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		return exec.CommandContext(ctx, "/usr/bin/true")
	}
	r := NewRunner(Config{Enabled: true})
	for _, req := range []Request{
		{MigratedDuty: "extractor/aion", Prompt: "fixture", Fallback: &FallbackChoice{}},
		{MigratedDuty: "extractor/aion", Prompt: "fixture", Model: "other"},
		{MigratedDuty: "extractor/aion", Prompt: "fixture", Toolsets: "file"},
		{MigratedDuty: "extractor/aion", Prompt: "fixture", Profile: "interactive"},
		{MigratedDuty: "extractor/aion", Prompt: "fixture", Skills: "write"},
	} {
		if _, err := r.Run(context.Background(), req); err == nil {
			t.Fatal("accepted override")
		}
	}
	if calls != 0 {
		t.Fatal("launched invalid authority")
	}
}

func TestExtractionResultParsing(t *testing.T) {
	a := defaultExtractionDuties()["extractor/aion"]
	for _, raw := range []string{``, `null`, `{}`, `{"candidates":null}`, `{"candidates":{}}`, `{"candidates":[],"candidates":[]}`, `{"candidates":[],"extra":true}`, "```json\n{\"candidates\":[]}\n```", strings.Repeat("x", 64001)} {
		if r, err := parseExtractionResult([]byte(raw), a); err == nil || r.Reply != "" || r.DutyVerified() {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{`{"candidates":[]}`, `{"summary":"Jane committed to review.","candidates":[{"type":"aion-backlog","applyPath":"system/aion/backlog.md","source":"log/fixture.md","payload":{"kind":"task","title":"Review draft","quote":"I will review the draft."}}]}`} {
		if _, err := parseExtractionResult([]byte(raw), a); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExtractionAuthorityBounds(t *testing.T) {
	for _, mutate := range []func(*DutyAuthority){
		func(a *DutyAuthority) { a.Provider = "claude-sub" }, func(a *DutyAuthority) { a.Model = "other" },
		func(a *DutyAuthority) { a.MaxSteps = 2 }, func(a *DutyAuthority) { a.TimeoutSeconds = 121 },
		func(a *DutyAuthority) { a.TimeoutSeconds = 0 }, func(a *DutyAuthority) { a.MCP = "manifest" },
		func(a *DutyAuthority) { a.Tools = []string{"file"} }, func(a *DutyAuthority) { b := 5.0; a.CeilingUSD = &b },
	} {
		a := defaultExtractionDuties()["extractor/aion"]
		mutate(&a)
		r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"extractor/aion": a}})
		if r.ValidateExtractionDuty("aion") == nil {
			t.Fatal("accepted invalid authority")
		}
	}
	if extractionBinary != "/home/benjamin/.local/bin/hermes" {
		t.Fatal(extractionBinary)
	}
	if _, err := os.Stat(extractionBinary); os.IsNotExist(err) {
		t.Log("runtime not installed; fixture tests only")
	}
}

func TestExtractionCLIStartupWarnings(t *testing.T) {
	raw := "Warning: Unknown toolsets: none\n  ⚠ tirith security scanner enabled but not available — command scanning will use pattern matching only\n" + `{"candidates":[],"summary":"No commitments."}`
	if _, err := parseExtractionResult([]byte(raw), defaultExtractionDuties()["extractor/aion"]); err != nil {
		t.Fatal(err)
	}
}

func TestExtractionCannotReachOtherExecutor(t *testing.T) {
	zero := 0.0
	a := DutyAuthority{Provider: "deepseek-local", Model: "deepseek-v4.1-flash", Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 120, MaxSteps: 1, CeilingUSD: &zero}
	for _, duty := range []string{"extractor/aion", "extractor/real-estate", "extractor/ooda-email"} {
		r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{duty: a}})
		if _, err := r.dutyAuthority(Request{MigratedDuty: duty}); err == nil {
			t.Fatal("accepted another executor", duty)
		}
	}
}
