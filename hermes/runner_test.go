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
	// per-request overrides win; session + usage file appended.
	got := r.buildArgs(Request{Prompt: "do the thing", Session: "todo-42", Model: "req-model", Skills: "scout,bio"}, "/tmp/u.json")
	want := []string{"-z", "do the thing", "-m", "req-model", "-t", "cfg-tools", "--resume", "todo-42", "--skills", "scout,bio", "--usage-file", "/tmp/u.json"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("buildArgs =\n  %v\nwant\n  %v", got, want)
	}
	// no session, no usage file, no overrides → config defaults only.
	got2 := r.buildArgs(Request{Prompt: "hi"}, "")
	want2 := []string{"-z", "hi", "-m", "cfg-model", "-t", "cfg-tools"}
	if strings.Join(got2, "\x00") != strings.Join(want2, "\x00") {
		t.Errorf("buildArgs(default) =\n  %v\nwant\n  %v", got2, want2)
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

// TestRunWithStub points the runner at a stub that mimics `hermes -z`: it prints
// a reply on stdout and writes a usage report to the --usage-file path — so we
// exercise exec, stdout capture, and usage parsing without the real CLI.
func TestRunWithStub(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "hermes-stub")
	script := `#!/bin/sh
usage=""
while [ $# -gt 0 ]; do
  case "$1" in
    --usage-file) usage="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$usage" ] && printf '{"estimated_cost_usd":0.0123,"model":"ds4"}' > "$usage"
printf 'PLAN\n1. do it'
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{Enabled: true, Bin: stub})
	res, err := r.Run(context.Background(), Request{Prompt: "make a plan", Session: "todo-1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reply != "PLAN\n1. do it" {
		t.Errorf("reply = %q", res.Reply)
	}
	if res.SpentUSD != 0.0123 || res.Model != "ds4" {
		t.Errorf("usage parse = %v / %q, want 0.0123 / ds4", res.SpentUSD, res.Model)
	}
}
