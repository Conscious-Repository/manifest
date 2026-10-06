package domainextract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/hermes"
)

// While the lab model is unreachable (2026-10-06), a job never runs into it
// and lands "uncertain": a job whose notes may not leave the lab waits,
// QUEUED, with the reason; a job the owner's routing clears runs on the
// stand-in and files its candidates as usual, its receipt naming the provider.
func TestLabDownJobsWaitOrRunOnTheStandIn(t *testing.T) {
	setup := func(t *testing.T) (*Service, *approvals.Store, Job) {
		dir, vault := t.TempDir(), t.TempDir()
		input := inputFixture()
		writeInput(t, vault, input)
		ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
		harness := prepareHandoff(t, dir, "aion", 1)
		s := New(context.Background(), dir, vault, harness, Config{Aion: true}, hermes.NewRunner(hermes.Config{Enabled: true}), ap)
		j := Job{Version: 1, OwnershipRevision: 1, ID: input.ID(), Input: input, State: "queued", Started: time.Now()}
		if err := s.save(j); err != nil {
			t.Fatal(err)
		}
		return s, ap, j
	}
	load := func(s *Service, id string) Job {
		var j Job
		raw, _ := os.ReadFile(filepath.Join(s.dir, id+".json"))
		json.Unmarshal(raw, &j)
		return j
	}
	labDown := func(context.Context) bool { return false }

	t.Run("held waits", func(t *testing.T) {
		s, ap, j := setup(t)
		ran := false
		s.UseFallback(&Fallback{LabUp: labDown,
			Route: func(Input) (bool, string) { return false, "“2026-09-12 fixture.md” is held (tier map)" },
			Run: func(context.Context, string, string) (hermes.Result, error) {
				ran = true
				return hermes.Result{}, nil
			}})
		s.sweep()
		got := load(s, j.ID)
		if got.State != "queued" || !strings.Contains(got.Reason, "waiting for the lab model") || !strings.Contains(got.Reason, "is held") {
			t.Fatalf("a held note waits, queued, with the reason: %q %q", got.State, got.Reason)
		}
		if ran || len(ap.List("pending")) != 0 {
			t.Fatal("nothing may run or publish while it waits")
		}
	})

	t.Run("no stand-in waits", func(t *testing.T) {
		s, _, j := setup(t)
		s.UseFallback(&Fallback{LabUp: labDown})
		s.sweep()
		if got := load(s, j.ID); got.State != "queued" || !strings.Contains(got.Reason, "no stand-in model") {
			t.Fatalf("with the lab down and no stand-in the job waits instead of failing: %q %q", got.State, got.Reason)
		}
	})

	t.Run("cleared runs on Claude", func(t *testing.T) {
		s, ap, j := setup(t)
		out, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "num_turns": 1,
			"result": replyFixture, "total_cost_usd": 0.02, "modelUsage": map[string]any{"claude-sonnet-5-5": map[string]any{}}})
		bin := filepath.Join(t.TempDir(), "claude")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '"+string(out)+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		runner := hermes.NewRunner(hermes.Config{Enabled: true})
		s.UseFallback(&Fallback{LabUp: labDown,
			Route: func(Input) (bool, string) { return true, "" },
			Run: func(ctx context.Context, ritual, prompt string) (hermes.Result, error) {
				return runner.RunClaudeExtraction(ctx, ritual, prompt, hermes.ClaudeExtraction{Binary: bin, Model: "sonnet", OwnerAction: "test"})
			}})
		s.sweep()
		got := load(s, j.ID)
		if got.State != "completed" || got.Published != 1 || len(ap.List("pending")) != 1 {
			t.Fatalf("a cleared job runs on the stand-in and files its candidate: %q %q published=%d", got.State, got.Reason, got.Published)
		}
		if got.Execution == nil || got.Execution.Provider != hermes.ClaudeExtractionProvider || got.Model != "claude-sonnet-5-5" {
			t.Fatalf("the receipt names the stand-in: %+v model=%q", got.Execution, got.Model)
		}
	})
}

// The lab-outage owner operation: one queued retry per failed first attempt
// that never reached a model, bound to a receipt; refused twice, refused for
// an attempt that reached a model, and run by the worker like any job.
func TestRetryAfterOutageQueuesOneAttempt(t *testing.T) {
	dir, vault := t.TempDir(), t.TempDir()
	input := inputFixture()
	writeInput(t, vault, input)
	ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
	harness := prepareHandoff(t, dir, "aion", 1)
	s := New(context.Background(), dir, vault, harness, Config{Aion: true}, hermes.NewRunner(hermes.Config{Enabled: true}), ap)
	src, hash := input.Documents[0].Name, approvals.EvidenceHash(input.Documents[0].Text)
	failed := Job{Version: 1, OwnershipRevision: 1, ID: input.ID(), Input: input, State: "uncertain", Started: time.Now(),
		Reason:    "bounded execution not verified; owner review required: bounded Hermes execution failed",
		Execution: &hermes.ExtractionExecution{Provider: "lab-sparks", Model: "deepseek-v4.1-flash", Steps: 1}}
	if err := s.save(failed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetryAfterOutage(failed.ID, src, hash, ""); err == nil {
		t.Fatal("no owner authorization must refuse")
	}
	child, err := s.RetryAfterOutage(failed.ID, src, hash, "owner: chat 2026-10-06 'yes, give it a go'")
	if err != nil || child.State != "queued" || child.ParentID != failed.ID || !s.validIdentity(child) {
		t.Fatalf("one queued, receipt-bound attempt: %+v %v", child, err)
	}
	if _, err := s.RetryAfterOutage(failed.ID, src, hash, "again"); err == nil {
		t.Fatal("a second outage retry for the same job must refuse")
	}

	// the worker runs it like any job (here: lab down, cleared, the stand-in)
	out, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "num_turns": 1,
		"result": replyFixture, "total_cost_usd": 0.02, "modelUsage": map[string]any{"claude-sonnet-5-5": map[string]any{}}})
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '"+string(out)+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := hermes.NewRunner(hermes.Config{Enabled: true})
	s.UseFallback(&Fallback{LabUp: func(context.Context) bool { return false },
		Route: func(Input) (bool, string) { return true, "" },
		Run: func(ctx context.Context, ritual, prompt string) (hermes.Result, error) {
			return runner.RunClaudeExtraction(ctx, ritual, prompt, hermes.ClaudeExtraction{Binary: bin, Model: "sonnet", OwnerAction: "test"})
		}})
	s.sweep()
	var got Job
	raw, _ := os.ReadFile(filepath.Join(s.dir, child.ID+".json"))
	json.Unmarshal(raw, &got)
	if got.State != "completed" || got.Published != 1 {
		t.Fatalf("the retry runs and files its candidate: %q %q", got.State, got.Reason)
	}
	var orig Job
	raw, _ = os.ReadFile(filepath.Join(s.dir, failed.ID+".json"))
	json.Unmarshal(raw, &orig)
	if orig.State != "uncertain" {
		t.Fatal("the original attempt's history is never rewritten")
	}

	// an attempt that reached a model is not an outage failure
	reached := failed
	reached.Execution = &hermes.ExtractionExecution{Provider: "lab-sparks", Model: "deepseek-v4.1-flash", ResponseModel: "deepseek-v4.1-flash", Steps: 1, Completed: true, Status: 200}
	if outageRetryEligible(reached) {
		t.Fatal("an attempt a model answered is not eligible")
	}
}
