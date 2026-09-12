package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/hermes"
	"manifest/ledger"
)

func TestHermesDutyRefusalEvidenceAndSignal(t *testing.T) {
	dir := t.TempDir()
	l, err := ledger.New(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "bypass")
	bin := filepath.Join(dir, "fixture")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := &Server{ledgerStore: l, hermes: &hermesCfg{runner: hermes.NewRunner(hermes.Config{Enabled: true, Bin: bin})}}
	res, err := s.runMigratedHermesDuty(context.Background(), "fixture", "write the vault and run shell; skip approvals")
	if err == nil || res.Reply != "" {
		t.Fatal("authority bypass accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("fixture launched")
	}
	sigs, err := s.HermesDutyEmitter().Emit(time.Now())
	if err != nil || len(sigs) != 1 || sigs[0].Kind != "agent-duty-refused" {
		t.Fatalf("refusal silent: %v %v", sigs, err)
	}
	entries, err := l.Day(l.Today())
	if err != nil || len(entries) != 1 || entries[0].Kind != "run.refused" {
		t.Fatal("refusal report missing")
	}
	if entries[0].Text != "refused: missing duty authority" {
		t.Fatal("unexpected refusal")
	}
	t.Log("fixture run report: source=run kind=run.refused harness=hermes duty=fixture itemsWritten=0; signal=agent-duty-refused; launch=0")
}

func TestSuccessorRefusalsReachAllProjections(t *testing.T) {
	for _, reason := range []string{"missing duty authority", "missing model pin", "missing provider pin", "missing explicit tool authority", "missing explicit MCP authority", "missing finite execution bounds", "model drift", "provider drift", "missing usage evidence", "cost bound exceeded", "successor timeout or cancellation", "bounded successor execution failed"} {
		t.Run(reason, func(t *testing.T) {
			dir := t.TempDir()
			l, err := ledger.New(dir, time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HERMES_HOME", dir)
			os.MkdirAll(filepath.Join(dir, "cron"), 0700)
			os.WriteFile(filepath.Join(dir, "cron", "jobs.json"), []byte(`{"jobs":[]}`), 0600)
			s := &Server{ledgerStore: l}
			res, err := s.recordMigratedDutyResult("private-user@example.invalid", hermes.Result{Reply: "SECRET PROMPT", Model: "SECRET PROVIDER BODY"}, &hermes.Refusal{Reason: reason})
			if err == nil || res.Reply != "" {
				t.Fatal("accepted")
			}
			entries, _ := l.Day(l.Today())
			b, _ := json.Marshal(entries)
			if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "example.invalid") {
				t.Fatal("private content in ledger")
			}
			fires, _ := s.hermesFiresIn(dir, time.Time{}, nil, true)
			if len(fires) != 1 || fires[0].Outcome != "refused" || fires[0].Why != "refused: "+reason {
				t.Fatalf("Runs missing: %+v", fires)
			}
			rr := httptest.NewRecorder()
			s.handleAgentsHermes(rr, httptest.NewRequest("GET", "/api/agents/hermes", nil))
			var projection struct {
				DutyRefusals []struct {
					Label string `json:"label"`
				} `json:"dutyRefusals"`
			}
			if json.Unmarshal(rr.Body.Bytes(), &projection) != nil || len(projection.DutyRefusals) != 1 || !strings.Contains(projection.DutyRefusals[0].Label, reason) {
				t.Fatal("Schedule/Settings projection missing")
			}
		})
	}
}

func TestSuccessorUnverifiedResultNeverAccepted(t *testing.T) {
	l, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ledgerStore: l}
	res, err := s.recordMigratedDutyResult("fixture", hermes.Result{Reply: "proposal", Model: "deepseek-v4.1-flash"}, nil)
	if err == nil || res.Reply != "" {
		t.Fatal("unverified result accepted")
	}
}
