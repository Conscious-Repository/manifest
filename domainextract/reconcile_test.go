package domainextract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/hermes"
)

func reconcileFixture(t *testing.T) (*Service, Job) {
	t.Helper()
	data, vault := t.TempDir(), t.TempDir()
	harness := prepareHandoff(t, data, "aion", 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // no real provider call
	s := New(ctx, data, vault, harness, Config{Aion: true}, hermes.NewRunner(hermes.Config{Enabled: true}), approvals.NewStore(t.TempDir()))
	i := inputFixture()
	writeInput(t, vault, i)
	j := Job{Version: 1, OwnershipRevision: 1, ID: i.ID(), Input: i, State: "uncertain", Reason: scratchHold, Started: time.Now().UTC(), Finished: time.Now().UTC()}
	if err := s.save(j); err != nil {
		t.Fatal(err)
	}
	if err := s.report(j); err != nil {
		t.Fatal(err)
	}
	return s, j
}
func TestPreProviderReconciliationPreservesHistory(t *testing.T) {
	s, j := reconcileFixture(t)
	path := filepath.Join(s.dir, j.ID+".json")
	before, _ := os.ReadFile(path)
	next, err := s.RetryPreProvider(j.ID, j.Input.Documents[0].Name, "owner fixture authorization")
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == j.ID || next.ParentID != j.ID || next.Input.ID() != j.ID || next.State != "uncertain" || next.Replay {
		t.Fatal(next.State)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("original changed")
	}
	if !s.validIdentity(next) {
		t.Fatal("attempt identity rejected")
	}
	raw, _ := os.ReadFile(filepath.Join(s.dir, "reconciliations", j.ID+".json"))
	var r Reconciliation
	if json.Unmarshal(raw, &r) != nil || r.Replay || r.SourceHash != approvals.EvidenceHash(j.Input.Documents[0].Text) {
		t.Fatal("invalid reconciliation")
	}
	if _, err = s.RetryPreProvider(j.ID, j.Input.Documents[0].Name, "again"); err == nil {
		t.Fatal("repeat authorized")
	}
	next.ParentID = ""
	if s.validIdentity(next) {
		t.Fatal("unbound attempt accepted")
	}
	if len(s.ap.List("pending")) != 0 {
		t.Fatal("unexpected publication")
	}
}
func TestPreProviderReconciliationRefusesUnsafeJobs(t *testing.T) {
	cases := map[string]func(*Job){
		"running":    func(j *Job) { j.State = "running" },
		"replay":     func(j *Job) { j.Replay = true },
		"published":  func(j *Job) { j.Published = 1 },
		"model":      func(j *Job) { j.Model = "deepseek-v4.1-flash" },
		"execution":  func(j *Job) { j.Execution = &hermes.ExtractionExecution{Steps: 1} },
		"cost":       func(j *Job) { j.SpentUSD = 1 },
		"candidates": func(j *Job) { j.Candidates = []approvals.Proposal{{ID: "abc"}} },
		"timeout": func(j *Job) {
			j.Reason = "bounded execution not verified; owner review required: bounded Hermes timeout or cancellation"
		},
		"reason suffix": func(j *Job) { j.Reason += " after provider" },
		"fence":         func(j *Job) { j.OwnershipRevision++ },
		"version":       func(j *Job) { j.Version++ },
		"source hash":   func(j *Job) { j.Input.Documents[0].Text += "changed" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, j := reconcileFixture(t)
			mutate(&j)
			s.save(j)
			if _, err := s.RetryPreProvider(j.ID, j.Input.Documents[0].Name, "owner"); err == nil {
				t.Fatal("accepted unsafe job")
			}
			files, _ := filepath.Glob(filepath.Join(s.dir, "reconciliations", "*.json"))
			if len(files) != 0 {
				t.Fatal("authorized unsafe retry")
			}
		})
	}
	for _, mode := range []string{"wrong path", "changed source", "changed context", "proposal", "missing authorization", "receipt without attempt", "output report"} {
		t.Run(mode, func(t *testing.T) {
			s, j := reconcileFixture(t)
			source, auth := j.Input.Documents[0].Name, "owner"
			switch mode {
			case "wrong path":
				source = "log/another.md"
			case "changed source":
				os.WriteFile(filepath.Join(s.vault, source), []byte("changed"), 0600)
			case "changed context":
				os.WriteFile(filepath.Join(s.vault, "system/aion/backlog.md"), []byte("changed"), 0600)
			case "proposal":
				ps, err := ValidateReply(j.Input, replyFixture)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.ap.ProposeOnce(ps[0]); err != nil {
					t.Fatal(err)
				}
			case "missing authorization":
				auth = ""
			case "output report":
				j.Published = 1
				if err := s.report(j); err != nil {
					t.Fatal(err)
				}
			case "receipt without attempt":
				atomic(filepath.Join(s.dir, "reconciliations", j.ID+".json"), []byte("{}"))
			}
			if _, err := s.RetryPreProvider(j.ID, source, auth); err == nil {
				t.Fatal("accepted unsafe reconciliation")
			}
		})
	}
}
