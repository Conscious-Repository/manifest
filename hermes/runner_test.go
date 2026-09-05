package hermes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildArgs(t *testing.T) {
	r := NewRunner(Config{Enabled: true, Model: "cfg-model", Toolsets: "cfg-tools"})
	// per-request overrides win; usage file appended; no session/skill flags
	// ever (hermes -z ignores --resume and --skills — see the package comment).
	got := r.buildArgs(Request{Prompt: "do the thing", Model: "req-model"}, "/tmp/u.json")
	want := []string{"-z", "do the thing", "-m", "req-model", "-t", "cfg-tools", "--usage-file", "/tmp/u.json"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("buildArgs =\n  %v\nwant\n  %v", got, want)
	}
	// no usage file, no overrides → config defaults only.
	got2 := r.buildArgs(Request{Prompt: "hi"}, "")
	want2 := []string{"-z", "hi", "-m", "cfg-model", "-t", "cfg-tools"}
	if strings.Join(got2, "\x00") != strings.Join(want2, "\x00") {
		t.Errorf("buildArgs(default) =\n  %v\nwant\n  %v", got2, want2)
	}
}

// TestBuildArgsNeverResumes pins the continuity fix: nothing on Request can
// make the runner emit --resume or --skills, both silently dropped by -z.
func TestBuildArgsNeverResumes(t *testing.T) {
	r := NewRunner(Config{Enabled: true})
	got := r.buildArgs(Request{Prompt: "p", Skills: "scout,bio", Profile: "x"}, "/tmp/u.json")
	for _, dead := range []string{"--resume", "--skills", "-r", "-s"} {
		for _, a := range got {
			if a == dead {
				t.Errorf("argv carries %s, which hermes -z ignores: %v", dead, got)
			}
		}
	}
}

// TestComposePromptSkills: Skills become a one-line preamble naming the
// skills to load (Hermes loads them on demand); no Skills → prompt untouched.
func TestComposePromptSkills(t *testing.T) {
	if got := composePrompt(Request{Prompt: "dig it"}); got != "dig it" {
		t.Errorf("no skills must leave the prompt untouched, got %q", got)
	}
	got := composePrompt(Request{Prompt: "dig it", Skills: " aion-domain-scout, aion-biosciences ,,"})
	if !strings.HasPrefix(got, "Before anything else, load and follow these skills") {
		t.Errorf("preamble missing: %q", got)
	}
	if !strings.Contains(got, "`aion-domain-scout`, `aion-biosciences`") {
		t.Errorf("skills not named/normalized: %q", got)
	}
	if !strings.HasSuffix(got, "\n\ndig it") {
		t.Errorf("caller's prompt must follow the preamble verbatim: %q", got)
	}
	one := composePrompt(Request{Prompt: "x", Skills: "solo"})
	if !strings.Contains(one, "the skill `solo`") {
		t.Errorf("single skill wording: %q", one)
	}
	// the -z value is the composed prompt
	r := NewRunner(Config{Enabled: true})
	args := r.buildArgs(Request{Prompt: "x", Skills: "solo"}, "")
	if args[0] != "-z" || args[1] != one {
		t.Errorf("buildArgs must ship the composed prompt: %v", args)
	}
}

// TestBuildArgsProfile: a Profile prepends `-p <name>` before -z (the CLI
// selects the state root first); the no-profile argv is byte-identical to
// the pre-profiles shape.
func TestBuildArgsProfile(t *testing.T) {
	r := NewRunner(Config{Enabled: true})
	got := r.buildArgs(Request{Prompt: "ping", Profile: "scratch"}, "/tmp/u.json")
	want := []string{"-p", "scratch", "-z", "ping", "--usage-file", "/tmp/u.json"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("buildArgs(profile) =\n  %v\nwant\n  %v", got, want)
	}
	if got[0] != "-p" || got[1] != "scratch" {
		t.Errorf("-p must lead the argv, got %v", got)
	}
	// whitespace-only profile → no targeting, default path untouched
	got2 := r.buildArgs(Request{Prompt: "ping", Profile: "  "}, "")
	want2 := []string{"-z", "ping"}
	if strings.Join(got2, "\x00") != strings.Join(want2, "\x00") {
		t.Errorf("buildArgs(blank profile) = %v, want %v", got2, want2)
	}
}

func TestEnabled(t *testing.T) {
	if NewRunner(Config{Enabled: false}).Enabled() {
		t.Error("disabled runner reports Enabled")
	}
	if !NewRunner(Config{Enabled: true, Bin: "hermes"}).Enabled() {
		t.Error("enabled runner with a bin reports not-enabled")
	}
	if _, err := NewRunner(Config{Enabled: false}).Run(context.Background(), Request{Prompt: "x"}); err != ErrNotEnabled {
		t.Errorf("disabled Run err = %v, want ErrNotEnabled", err)
	}
}

// TestParseUsage reads the v0.20.0 report shape (estimated cost, model,
// session_id); an exact cost_usd wins over the estimate; a missing or
// malformed file is the zero report.
func TestParseUsage(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "u.json")
	real := `{"estimated_cost_usd": 0.0, "cost_status": "unknown", "input_tokens": 19751,
	  "model": "deepseek-v4-flash-vision-exp", "provider": "custom",
	  "session_id": "20260904_135845_2214c8", "completed": true, "failed": false}`
	if err := os.WriteFile(f, []byte(real), 0o644); err != nil {
		t.Fatal(err)
	}
	u := parseUsage(f)
	if u.SessionID != "20260904_135845_2214c8" || u.Model != "deepseek-v4-flash-vision-exp" || u.usd() != 0 {
		t.Errorf("parseUsage(real) = %+v", u)
	}
	if err := os.WriteFile(f, []byte(`{"cost_usd": 0.5, "estimated_cost_usd": 0.1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := parseUsage(f).usd(); got != 0.5 {
		t.Errorf("exact cost must win, got %v", got)
	}
	if err := os.WriteFile(f, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if u := parseUsage(f); u != (usageReport{}) {
		t.Errorf("malformed → zero report, got %+v", u)
	}
	if u := parseUsage(filepath.Join(dir, "missing.json")); u != (usageReport{}) {
		t.Errorf("missing → zero report, got %+v", u)
	}
}

// TestTurnTimeout pins the per-turn budget selection (2026-09-05: the one
// runner cap killed a legitimate execution turn at 8m). A Request that sets
// TimeoutSeconds gets exactly that; unset falls back to the runner default —
// which is DefaultTimeout (8m) when the config is silent, or the configured
// Config.TimeoutSeconds when set. The regression half: the unset path is
// byte-identical to before the field existed.
func TestTurnTimeout(t *testing.T) {
	def := NewRunner(Config{Enabled: true})
	if DefaultTimeout != 8*time.Minute {
		t.Fatalf("DefaultTimeout = %s, the quick-ask default must stay 8m0s", DefaultTimeout)
	}
	if got := def.turnTimeout(Request{Prompt: "ask"}); got != DefaultTimeout {
		t.Errorf("unset request on default config → %s, want 8m0s", got)
	}
	if got := def.turnTimeout(Request{Prompt: "go", TimeoutSeconds: 30 * 60}); got != 30*time.Minute {
		t.Errorf("request budget 1800s → %s, want 30m0s", got)
	}
	if got := def.turnTimeout(Request{Prompt: "go", TimeoutSeconds: -5}); got != DefaultTimeout {
		t.Errorf("a non-positive request budget must fall back to the default, got %s", got)
	}
	cfg := NewRunner(Config{Enabled: true, TimeoutSeconds: 480})
	if got := cfg.turnTimeout(Request{Prompt: "ask"}); got != 8*time.Minute {
		t.Errorf("configured 480s, unset request → %s, want 8m0s", got)
	}
	cfg2 := NewRunner(Config{Enabled: true, TimeoutSeconds: 120})
	if got := cfg2.turnTimeout(Request{Prompt: "ask"}); got != 2*time.Minute {
		t.Errorf("configured 120s, unset request → %s, want 2m0s", got)
	}
	// the per-turn budget is the primary mechanism: it wins over the config
	// default in both directions, never widening the cap for budget-less turns
	if got := cfg2.turnTimeout(Request{Prompt: "go", TimeoutSeconds: 1500}); got != 25*time.Minute {
		t.Errorf("request 1500s over configured 120s → %s, want 25m0s", got)
	}
	if got := cfg2.turnTimeout(Request{Prompt: "quick", TimeoutSeconds: 30}); got != 30*time.Second {
		t.Errorf("request 30s under configured 120s → %s, want 30s", got)
	}
}

// TestRunTimeoutEnforced proves the Request budget is what the deadline runs
// on, not just what turnTimeout reports: a stub that sleeps past a 1s request
// budget is killed and the error names the EFFECTIVE budget, while the same
// stub inside a generous budget completes.
func TestRunTimeoutEnforced(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hermes-slow")
	// `exec` so the sleep IS the killed process: a forked child would inherit
	// the stdout pipe and hold Run's Wait open past the deadline (the same
	// holds for a real CLI that leaves tool subprocesses behind — the error is
	// still the timed-out one, it just surfaces once the pipe closes).
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec sleep 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{Enabled: true, Bin: stub})
	start := time.Now()
	_, err := r.Run(context.Background(), Request{Prompt: "slow", TimeoutSeconds: 1})
	if err == nil || !strings.Contains(err.Error(), "hermes turn timed out after 1s") {
		t.Fatalf("1s budget on a 3s turn: err = %v, want the timed-out error naming 1s", err)
	}
	if el := time.Since(start); el > 2500*time.Millisecond {
		t.Errorf("the 1s request budget was not the deadline (took %s)", el)
	}
	// the same wait inside a generous budget completes normally
	quick := filepath.Join(dir, "hermes-quick")
	if err := os.WriteFile(quick, []byte("#!/bin/sh\nsleep 1\nprintf done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := NewRunner(Config{Enabled: true, Bin: quick}).Run(context.Background(), Request{Prompt: "ok", TimeoutSeconds: 20})
	if err != nil || res.Reply != "done" {
		t.Fatalf("20s budget on a 1s turn: res=%q err=%v, want done/nil", res.Reply, err)
	}
}

// TestRunTimeoutSurvivesAnOrphanHoldingStdout pins the pipe grace: a stub
// that forks a background child (a tool subprocess the CLI leaves behind)
// and then outlives its 1s budget is killed at the deadline, and Run returns
// within the grace — not when the orphan lets go of stdout 30s later.
func TestRunTimeoutSurvivesAnOrphanHoldingStdout(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hermes-orphan")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nsleep 30 &\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	was := pipeGrace
	pipeGrace = 500 * time.Millisecond
	defer func() { pipeGrace = was }()
	r := NewRunner(Config{Enabled: true, Bin: stub})
	start := time.Now()
	_, err := r.Run(context.Background(), Request{Prompt: "orphan", TimeoutSeconds: 1})
	if err == nil || !strings.Contains(err.Error(), "hermes turn timed out after 1s") {
		t.Fatalf("err = %v, want the timed-out error", err)
	}
	if el := time.Since(start); el > 4*time.Second {
		t.Errorf("Run waited on the orphan's stdout (took %s) — the pipe grace did not bound Wait", el)
	}
}

// TestRunWithStub points the runner at a stub that mimics `hermes -z`: it prints
// a reply on stdout and writes a usage report to the --usage-file path — so we
// exercise exec, stdout capture, and usage parsing (cost, model, session id)
// without the real CLI.
func TestRunWithStub(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hermes-stub")
	script := `#!/bin/sh
usage=""
while [ $# -gt 0 ]; do
  case "$1" in
    --usage-file) usage="$2"; shift 2 ;;
    --resume|--skills) echo "dead flag $1" >&2; exit 3 ;;
    *) shift ;;
  esac
done
[ -n "$usage" ] && printf '{"estimated_cost_usd":0.0123,"model":"ds4","session_id":"20260904_140003_c40083"}' > "$usage"
printf 'PLAN\n1. do it'
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{Enabled: true, Bin: stub})
	res, err := r.Run(context.Background(), Request{Prompt: "make a plan", Skills: "scout"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reply != "PLAN\n1. do it" {
		t.Errorf("reply = %q", res.Reply)
	}
	if res.SpentUSD != 0.0123 || res.Model != "ds4" {
		t.Errorf("usage parse = %v / %q, want 0.0123 / ds4", res.SpentUSD, res.Model)
	}
	if res.SessionID != "20260904_140003_c40083" {
		t.Errorf("session id not captured: %q", res.SessionID)
	}
}
