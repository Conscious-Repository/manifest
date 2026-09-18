package hermes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractionCLI(t *testing.T) {
	old := extractionCommand
	t.Cleanup(func() { extractionCommand = old })
	prompt := "fixture $(touch /tmp/not-executed); `literal`"
	extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		if len(args) != 0 {
			t.Fatalf("prompt must not travel in process argv: %q", args)
		}
		// A fixture process inspects the actual environment and private config.
		return exec.CommandContext(ctx, "/usr/bin/python3", "-c", `import os,json,sys
assert sys.stdin.read()=="fixture $(touch /tmp/not-executed); `+"`literal`"+`"
assert os.getcwd()==os.environ['HERMES_HOME']==os.environ['HOME']
c=json.load(open(os.path.join(os.environ['HERMES_HOME'],'config.yaml')))
assert c['model']['provider']=='lab-sparks' and c['model']['default']=='sparks'
assert c['model']['aliases']['sparks']=='lab-sparks/deepseek-v4.1-flash'
assert c['custom_providers'][0]['name']=='lab-sparks'
assert c['custom_providers'][0]['extra_body']=={'model':'deepseek-v4.1-flash','tool_choice':'none'}
assert c['fallback_providers']==[] and c['fallback_model'] is None and c['mcp_servers']=={}
assert 'HERMES_IGNORE_USER_CONFIG' not in os.environ
assert 'HERMES_IGNORE_RULES' not in os.environ
assert os.environ['HERMES_SAFE_MODE']=='1'
assert os.stat(os.getcwd()).st_mode & 0o777 == 0o700
assert os.stat('config.yaml').st_mode & 0o777 == 0o600
assert c['plugins']=={'enabled':[]}
assert c['memory']=={'memory_enabled':False,'user_profile_enabled':False,'provider':''}
assert 'ANTHROPIC_API_KEY' not in os.environ
assert 'HERMES_KANBAN_TASK' not in os.environ
json.dump({'provider':'lab-sparks','model':'deepseek-v4.1-flash','responseModel':'deepseek-v4.1-flash','steps':1,'completed':True,'status':200},open('execution.json','w'))
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
	if extractionBinary != "/home/benjamin/.hermes/hermes-agent/venv/bin/python" {
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

// Exercise the production OS boundary and full prompt transport with an inert
// Hermes module. No real runtime imports or model calls occur in this test.
func TestExtractionContainedLargePrompt(t *testing.T) {
	old := extractionCommand
	t.Cleanup(func() { extractionCommand = old })
	runtime := t.TempDir()
	outside := t.TempDir()
	for _, name := range []string{"vault", "approvals", "secret", "plugins", "mcp"} {
		if err := os.WriteFile(filepath.Join(outside, name), []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"agent", "hermes_cli", "tools", "providers", "gateway", "cron", "assets", "locales", "hermes_agent.egg-info", "venv/lib"} {
		if err := os.MkdirAll(filepath.Join(runtime, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	code := `import os, sys, json, subprocess

def main():
    args = sys.argv[1:]
    assert args[:3] == ['chat', '-Q', '-q']
    assert args[4:] == ['-m', 'sparks', '--provider', 'lab-sparks', '-t', 'none', '--max-turns', '1', '--source', 'tool', '--cli']
    assert args[3] == 'x' * 524288
    assert os.getcwd() == os.environ['HOME'] == os.environ['HERMES_HOME']
    assert 'ANTHROPIC_API_KEY' not in os.environ
    c = json.load(open('config.yaml'))
    assert c['model']['context_length'] == 1048576
    assert c['custom_providers'][0]['models']['sparks']['context_length'] == 1048576
    assert c['compression']['enabled'] is False
    assert c['fallback_providers'] == [] and c['mcp_servers'] == {}
    outside = OUTSIDE
    for name in ('vault', 'approvals', 'secret', 'plugins', 'mcp'):
        path = os.path.join(outside, name)
        for mode in ('r', 'w'):
            try:
                open(path, mode)
            except PermissionError:
                pass
            else:
                raise AssertionError('uncontained filesystem')
        try:
            os.chmod(path, 0o777)
        except PermissionError:
            pass
        else:
            raise AssertionError('metadata escape')
    try:
        subprocess.run(['/bin/sh', '-c', 'touch ' + outside + '/shell'], check=True)
    except PermissionError:
        pass
    else:
        raise AssertionError('shell execution')
    import httpx
    response = httpx.Client().send(httpx.Request())
    assert response.status_code == 200
    try:
        httpx.Client().send(httpx.Request())
    except RuntimeError:
        pass
    else:
        raise AssertionError('second provider call permitted')
    open('scratch-test', 'w').write('allowed')
    print('{"candidates":[]}')
`
	code = strings.ReplaceAll(code, "OUTSIDE", "'"+outside+"'")
	if err := os.WriteFile(filepath.Join(runtime, "hermes_cli", "main.py"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime, "httpx.py"), []byte(`class Request:
    url = 'http://192.168.87.11:8000/v1/chat/completions'
    content = b'{"model":"deepseek-v4.1-flash"}'
class Response:
    status_code = 200
    def read(self): return b'{"model":"deepseek-v4.1-flash"}'
class Client:
    def send(self, request, *args, **kwargs): return Response()
`), 0600); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(extractionScript, "/home/benjamin/.hermes/hermes-agent", runtime)
	calls := 0
	extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", script)
	}
	t.Setenv("ANTHROPIC_API_KEY", "must-not-pass")
	r := NewRunner(Config{Enabled: true})
	res, err := r.Run(context.Background(), Request{MigratedDuty: "extractor/aion", Prompt: strings.Repeat("x", extractionPromptLimit)})
	if err != nil || !res.DutyVerified() {
		t.Fatal(res, err)
	}
	for _, prompt := range []string{strings.Repeat("x", extractionPromptLimit+1), "bad\x00prompt", "bad\xffprompt"} {
		if _, err := r.Run(context.Background(), Request{MigratedDuty: "extractor/aion", Prompt: prompt}); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	if calls != 1 {
		t.Fatal("overflow launched process", calls)
	}
	for _, name := range []string{"vault", "approvals", "secret", "plugins", "mcp"} {
		b, err := os.ReadFile(filepath.Join(outside, name))
		if err != nil || string(b) != "private" {
			t.Fatal("side effect", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "shell")); !os.IsNotExist(err) {
		t.Fatal("shell side effect", err)
	}
	// Missing isolation support must refuse before importing even the fake runtime.
	script = strings.ReplaceAll(script, "if libc.syscall(444, 0, 0, 1) < 3", "if True")
	if res, err := r.Run(context.Background(), Request{MigratedDuty: "extractor/aion", Prompt: "fixture"}); err == nil || res.DutyVerified() {
		t.Fatal("isolation failure accepted", res, err)
	}
}

func TestExtractionAuthorityProjection(t *testing.T) {
	r := NewRunner(Config{Enabled: true})
	duties := r.DutyAuthorities()
	if len(duties) != 3 {
		t.Fatal(duties)
	}
	for duty, a := range duties {
		if !extractionDutyAllowed(duty, a) {
			t.Fatal(duty, a)
		}
		a.Tools[0] = "shell"
		*a.CeilingUSD = 100
	}
	if err := r.ValidateExtractionDuty("aion"); err != nil {
		t.Fatal("projection mutated authority", err)
	}
}

func TestExtractionRequiresObservedProviderReceipt(t *testing.T) {
	old := extractionCommand
	t.Cleanup(func() { extractionCommand = old })
	for _, mode := range []string{"absent", "wrong-model", "wrong-provider", "two-steps", "incomplete", "http-error", "initialization"} {
		t.Run(mode, func(t *testing.T) {
			extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, "/usr/bin/python3", "-c", `import json,sys
mode=sys.argv[1]
r={'provider':'lab-sparks','model':'deepseek-v4.1-flash','responseModel':'deepseek-v4.1-flash','steps':1,'completed':True,'status':200}
if mode=='wrong-model': r['responseModel']='other'
if mode=='wrong-provider': r['provider']='other'
if mode=='two-steps': r['steps']=2
if mode=='incomplete': r['completed']=False
if mode=='http-error': r['status']=500
if mode not in ('absent','initialization'): json.dump(r,open('execution.json','w'))
if mode=='initialization':
 print('Failed to initialize agent: Permission denied: redacted')
 sys.exit(1)
print('{"candidates":[]}')`, mode)
			}
			res, err := NewRunner(Config{Enabled: true}).Run(context.Background(), Request{MigratedDuty: "extractor/aion", Prompt: "fixture"})
			if err == nil || res.DutyVerified() || res.Reply != "" {
				t.Fatal("unverified response accepted", res, err)
			}
			if mode == "initialization" && !strings.Contains(err.Error(), "initialization denied by filesystem boundary") {
				t.Fatal(err)
			}
			if mode != "absent" && mode != "initialization" && res.Extraction == nil {
				t.Fatal("lost observed failure evidence")
			}
		})
	}
}
