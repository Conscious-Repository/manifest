package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The rituals do different kinds of work: `ask` answers a question from the
// tree, `delegate` executes an approved plan. A flat ceiling SIGKILLed a real
// delegate at exactly 10m00s on 2026-08-20.
func TestTimeoutForIsPerRitual(t *testing.T) {
	r := &runner{timeout: 10 * time.Minute, byRitual: map[string]time.Duration{}}
	if got := r.timeoutFor("delegate"); got != 45*time.Minute {
		t.Errorf("delegate should get its own default, got %s", got)
	}
	if got := r.timeoutFor("ask"); got != 10*time.Minute {
		t.Errorf("ask should fall back to -timeout, got %s", got)
	}
	if got := r.timeoutFor(""); got != 10*time.Minute {
		t.Errorf("unknown ritual should fall back to -timeout, got %s", got)
	}
	// an explicit override beats the built-in default
	r.byRitual["delegate"] = 90 * time.Minute
	if got := r.timeoutFor("delegate"); got != 90*time.Minute {
		t.Errorf("override ignored, got %s", got)
	}
}

// A deadline kill must not reach the report as "signal: killed" — that reads
// identically to an OOM or a manual kill, and it is what sent a bare "failed"
// to the FEED with no WHY.
func TestInvokeNamesTheTimeout(t *testing.T) {
	r := &runner{
		root: t.TempDir(), bin: "/bin/sh", args: []string{"-c", "sleep 5"},
		timeout: 50 * time.Millisecond, byRitual: map[string]time.Duration{},
	}
	_, err := r.invoke("ask", "x")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if strings.Contains(err.Error(), "signal: killed") {
		t.Errorf("bare kill signal surfaced: %v", err)
	}
	for _, want := range []string{"timed out after", "ritual-timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// The report's "## Outcome" section IS manifest's OutcomeDetail — the runner
// never wrote one, so failures reached the FEED as a bare "failed".
func TestReportCarriesTheOutcomeDetail(t *testing.T) {
	dir := t.TempDir()
	r := &runner{root: dir, model: "m"}
	path := filepath.Join(dir, "run.md")
	o := spoolOrder{Spirit: "kairos", Ritual: "delegate", Request: "do the thing"}
	r.writeReport(path, o, "rid", time.Now(), time.Now(), "failed", "boom", "timed out after 45m0s")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, "\n## Outcome\n\nfailed — timed out after 45m0s\n") {
		t.Fatalf("outcome section missing or malformed:\n%s", body)
	}
	// exactly what manifest's parser lifts (spirits.outcomeDetail)
	_, after, found := strings.Cut(body, "\n## Outcome\n")
	if !found {
		t.Fatal("manifest's parser would find no section")
	}
	var first string
	for _, ln := range strings.Split(after, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			first = t
			break
		}
	}
	if first != "failed — timed out after 45m0s" {
		t.Fatalf("manifest would read %q", first)
	}
	// a clean run stays quiet — no empty section
	clean := filepath.Join(dir, "ok.md")
	r.writeReport(clean, o, "rid2", time.Now(), time.Now(), "completed", "the answer", "")
	cb, _ := os.ReadFile(clean)
	if strings.Contains(string(cb), "## Outcome") {
		t.Error("a completed run should not write an Outcome section")
	}
}
