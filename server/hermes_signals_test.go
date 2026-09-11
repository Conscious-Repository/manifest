package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ⚠ THE AGENT RUNTIME'S FAILURES MUST ANNOUNCE THEMSELVES (plan §4b H1).
// A dead ticker, a missed fire and an errored fire each page the FEED; a
// dead ticker suppresses the per-job missed chips it already explains; and
// a healthy plane emits nothing at all.
func TestHermesCronEmitterPagesTheThreeFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	cron := filepath.Join(home, "cron")
	if err := os.MkdirAll(cron, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	writeJobs := func(jobs []map[string]any) {
		b, _ := json.Marshal(map[string]any{"jobs": jobs})
		if err := os.WriteFile(filepath.Join(cron, "jobs.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	beat := func(at time.Time) {
		p := filepath.Join(cron, "ticker_heartbeat")
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{}
	emit := func() []string {
		sigs, err := s.HermesCronEmitter().Emit(now)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(sigs))
		for _, sg := range sigs {
			out = append(out, sg.Kind+" "+sg.Label)
		}
		return out
	}

	// healthy: fresh heartbeat, a scheduled job with a future fire → silence
	writeJobs([]map[string]any{{
		"id": "j1", "name": "waiting-on", "enabled": true, "state": "scheduled",
		"model": "deepseek-v4-flash-vision-exp",
		"next_run_at": now.Add(time.Hour).Format(time.RFC3339),
		"last_run_at": now.Add(-time.Hour).Format(time.RFC3339),
	}})
	beat(now.Add(-30 * time.Second))
	if got := emit(); len(got) != 0 {
		t.Fatalf("a healthy plane paged: %v", got)
	}

	// an errored last fire pages with the error VERBATIM
	writeJobs([]map[string]any{{
		"id": "j1", "name": "aion-domain-scout-daily", "enabled": true, "state": "scheduled",
		"next_run_at": now.Add(time.Hour).Format(time.RFC3339),
		"last_run_at": now.Add(-5 * time.Hour).Format(time.RFC3339),
		"last_status": "error", "last_error": "RuntimeError: Connection error.",
	}})
	got := emit()
	if len(got) != 1 || !strings.Contains(got[0], "agent-cron-error") ||
		!strings.Contains(got[0], "RuntimeError: Connection error.") {
		t.Fatalf("the 09-11 morning failure would still be silent: %v", got)
	}

	// a missed fire pages once the grace window passes
	writeJobs([]map[string]any{{
		"id": "j1", "name": "waiting-on", "enabled": true, "state": "scheduled",
		"next_run_at": now.Add(-time.Hour).Format(time.RFC3339),
		"last_run_at": now.Add(-3 * time.Hour).Format(time.RFC3339),
	}})
	got = emit()
	if len(got) != 1 || !strings.Contains(got[0], "agent-cron-missed") {
		t.Fatalf("missed fire: %v", got)
	}

	// a dead ticker is the HEADLINE: it pages, and the per-job missed chips
	// it explains are suppressed
	beat(now.Add(-20 * time.Minute))
	got = emit()
	if len(got) != 1 || !strings.Contains(got[0], "agent-cron-dead") {
		t.Fatalf("dead ticker: %v", got)
	}

	// paused jobs alarm nobody, and with nothing enabled the ticker's
	// silence is a setup state, not an emergency
	writeJobs([]map[string]any{{
		"id": "j1", "name": "waiting-on", "enabled": true, "state": "paused",
		"next_run_at": now.Add(-time.Hour).Format(time.RFC3339),
	}})
	if got := emit(); len(got) != 0 {
		t.Fatalf("a paused plane paged: %v", got)
	}

	// no cron tree at all → nothing (a box without Hermes is not on fire)
	t.Setenv("HERMES_HOME", filepath.Join(home, "nope"))
	if got := emit(); len(got) != 0 {
		t.Fatalf("a hermes-less box paged: %v", got)
	}
}
