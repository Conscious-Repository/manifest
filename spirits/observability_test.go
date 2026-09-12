package spirits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func observationFile(t *testing.T, root, rel, data string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestRitualMissedChicagoAndAutoClear(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	now := time.Date(2026, 9, 12, 12, 16, 0, 0, time.UTC) // 07:16 Chicago
	observationFile(t, root, "vessel/state/ritual-status.json", `{"rituals":[{"spirit":"test","ritual":"daily","cadence":"0 7 * * *","valid":true,"last_error":"unpriced model"},{"spirit":"test","ritual":"manual","valid":true},{"spirit":"test","ritual":"paused","valid":true,"paused":true,"cadence":"0 7 * * *"}]}`)
	observationFile(t, root, "vessel/state/engine.heartbeat", "x")
	if err := os.Chtimes(filepath.Join(root, "vessel/state/engine.heartbeat"), now, now); err != nil {
		t.Fatal(err)
	}
	p := s.RitualObservations(now)
	r := p.For("test", "daily")
	if r.Health != "late" || !strings.Contains(r.Why, "engine alive") || !strings.Contains(r.Why, "unpriced model") {
		t.Fatalf("missed: %+v", r)
	}
	if p.For("test", "manual").Health != "on-demand" || p.For("test", "paused").Health != "paused" {
		t.Fatal("unscheduled duties must stay quiet")
	}
	observationFile(t, root, "artifacts/runs/2026-09-12-test-wrong.md", "---\nspirit: test\nritual: different\nstarted: 2026-09-12T12:01:00Z\noutcome: completed\n---\n")
	if s.RitualObservations(now).For("test", "daily").Health != "late" {
		t.Fatal("wrong ritual cleared signal")
	}
	observationFile(t, root, "artifacts/runs/2026-09-12-test-right.md", "---\nspirit: test\nritual: daily\nstarted: 2026-09-12T12:16:00Z\noutcome: completed\n---\n")
	if got := s.RitualObservations(now).For("test", "daily").Health; got != "ok" {
		t.Fatalf("artifact did not clear: %s", got)
	}
}
func TestRitualMissedGraceAndDST(t *testing.T) {
	for _, tc := range []struct{ now, want string }{
		{"2026-09-12T12:14:59Z", "2026-09-11T12:00:00Z"},
		{"2026-09-12T12:15:00Z", "2026-09-12T12:00:00Z"},
		{"2026-11-01T13:16:00Z", "2026-11-01T13:00:00Z"},
		{"2026-03-08T12:16:00Z", "2026-03-08T12:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		want, _ := time.Parse(time.RFC3339, tc.want)
		got, err := ritualDue("0 7 * * *", now)
		if err != nil || !got.Equal(want) {
			t.Fatalf("%s: %v %v", tc.now, got, err)
		}
	}
}
func TestRitualPlaneUnconfiguredVersusUnknown(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if s.RitualObservations(time.Now()).Health != "unconfigured" {
		t.Fatal("missing registry")
	}
	observationFile(t, root, "vessel/state/ritual-status.json", `{}`)
	if s.RitualObservations(time.Now()).Health != "unknown" {
		t.Fatal("malformed registry hidden")
	}
}
