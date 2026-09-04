package hermes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
