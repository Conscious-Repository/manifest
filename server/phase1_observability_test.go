package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"manifest/spirits"
)

func TestHermesCronUnknownAndFailingTicker(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERMES_HOME", root)
	cron := filepath.Join(root, "cron")
	if err := os.MkdirAll(cron, 0700); err != nil {
		t.Fatal(err)
	}
	hosts := &HostsInfo{}
	hosts.Hermes.Bin = "/does-not-exist"
	hosts.Hermes.Enabled = true
	s := &Server{hosts: hosts}
	now := time.Now()
	put := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cron, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	put("jobs.json", "{")
	sigs, err := s.HermesCronEmitter().Emit(now)
	if err != nil || len(sigs) != 1 || sigs[0].Kind != "agent-cron-registry" || !strings.Contains(sigs[0].Label, "jobs.json") {
		t.Fatalf("unknown registry hidden: %v %v", sigs, err)
	}
	full := strings.Repeat("界", 150) + " full tail"
	b, _ := json.Marshal(map[string]any{"jobs": []map[string]any{{"id": "fixture", "name": "fixture", "enabled": true, "last_error": full}}})
	put("jobs.json", string(b))
	put("ticker_heartbeat", "x")
	sigs, err = s.HermesCronEmitter().Emit(now)
	kinds := map[string]bool{}
	for _, sg := range sigs {
		kinds[sg.Kind] = true
		if !utf8.ValidString(sg.Label) {
			t.Fatal("invalid UTF-8")
		}
	}
	if err != nil || !kinds["agent-cron-failing"] || !kinds["agent-cron-error"] {
		t.Fatalf("alive-but-failing hidden: %v", kinds)
	}
	jobs, _, _ := s.hermesJobs(t.Context())
	if len(jobs) != 1 || jobs[0].LastError != full {
		t.Fatal("full error lost")
	}
	put("ticker_last_success", "x")
	sigs, _ = s.HermesCronEmitter().Emit(now)
	for _, sg := range sigs {
		if sg.Kind == "agent-cron-failing" {
			t.Fatal("success did not clear")
		}
	}
}
func TestRitualMissedSignalAndRunsResponse(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "vessel", "state")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b := `{"rituals":[{"spirit":"fixture","ritual":"daily","cadence":"0 7 * * *","valid":true,"last_error":"unpriced model"}]}`
	if err := os.WriteFile(filepath.Join(dir, "ritual-status.json"), []byte(b), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{spirits: spirits.NewStore(root)}
	sigs, err := s.RitualMissedEmitter().Emit(time.Now())
	if err != nil || len(sigs) != 1 || sigs[0].Kind != "ritual-missed" {
		t.Fatalf("signal: %v %v", sigs, err)
	}
	rr := httptest.NewRecorder()
	s.handleSpiritsRuns(rr, httptest.NewRequest("GET", "/api/spirits/runs", nil))
	var out struct {
		Observations []spirits.RitualObservation `json:"observations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Observations) != 1 || out.Observations[0].LastError != "unpriced model" || out.Observations[0].Evidence == "" {
		t.Fatalf("missing response evidence: %+v", out)
	}
	rr = httptest.NewRecorder()
	s.handleHarnesses(rr, httptest.NewRequest("GET", "/api/harnesses", nil))
	if !strings.Contains(rr.Body.String(), `"observation"`) {
		t.Fatal("settings missing observation")
	}
}
