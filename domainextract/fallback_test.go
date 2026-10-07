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

// A queued job whose CONTEXT moved before anything ran (an approval landed
// in the backlog) takes the current context and carries on instead of being
// dropped;
// a job whose SOURCE changed is still refused, with no silent retry.
func TestQueuedJobOutlivesABacklogChange(t *testing.T) {
	for _, sourceChanged := range []bool{false, true} {
		dir, vault := t.TempDir(), t.TempDir()
		input := inputFixture()
		writeInput(t, vault, input)
		in, err := ReadInput(vault, "aion", []Document{{Name: input.Documents[0].Name}})
		if err != nil {
			t.Fatal(err)
		}
		ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
		harness := prepareHandoff(t, dir, "aion", 1)
		s := New(context.Background(), dir, vault, harness, Config{Aion: true}, hermes.NewRunner(hermes.Config{Enabled: true}), ap)
		j := Job{Version: 1, OwnershipRevision: 1, ID: in.ID(), Input: in, State: "queued", Started: time.Now()}
		if err := s.save(j); err != nil {
			t.Fatal(err)
		}
		// an approval lands in the backlog while the job waits
		if err := os.WriteFile(filepath.Join(vault, "system/aion/backlog.md"), []byte("# aion backlog\n\n- [ ] Approved meanwhile [id:: aion-bl/x] [kind:: task]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if sourceChanged {
			if err := os.WriteFile(filepath.Join(vault, filepath.FromSlash(in.Documents[0].Name)), []byte("Jane: something else entirely."), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// the lab is "down" and the job may not leave: it must not run, only be judged fresh or not
		s.UseFallback(&Fallback{LabUp: func(context.Context) bool { return false }})
		s.run(filepath.Join(s.dir, j.ID+".json"))
		var got Job
		raw, _ := os.ReadFile(filepath.Join(s.dir, j.ID+".json"))
		json.Unmarshal(raw, &got)
		files, _ := filepath.Glob(filepath.Join(s.dir, "*.json"))
		if sourceChanged {
			if got.State != "refused" || len(files) != 1 {
				t.Fatalf("a changed source is refused without a retry: %q %q (%d jobs)", got.State, got.Reason, len(files))
			}
			continue
		}
		if got.State != "queued" || !strings.Contains(got.Reason, "waiting for the lab model") || len(files) != 1 {
			t.Fatalf("a queued job outlives a context change and carries on (here: waits for the lab): %q %q (%d jobs)", got.State, got.Reason, len(files))
		}
		if !strings.Contains(got.Input.Context["system/aion/backlog.md"], "Approved meanwhile") || !s.validIdentity(got) || got.ID != j.ID {
			t.Fatal("the job keeps its identity and takes the current backlog")
		}
	}
}

// Submitting a note again re-queues a job that was refused before any model
// ran; a refused job that did run stays held for reconciliation.
func TestResubmitRequeuesOnlyAnUnexecutedRefusal(t *testing.T) {
	for _, ran := range []bool{false, true} {
		dir, vault := t.TempDir(), t.TempDir()
		input := inputFixture()
		writeInput(t, vault, input)
		in, err := ReadInput(vault, "aion", []Document{{Name: input.Documents[0].Name}})
		if err != nil {
			t.Fatal(err)
		}
		ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
		harness := prepareHandoff(t, dir, "aion", 1)
		s := New(context.Background(), dir, vault, harness, Config{Aion: true}, hermes.NewRunner(hermes.Config{Enabled: true}), ap)
		old := Job{Version: 1, OwnershipRevision: 1, ID: in.ID(), Input: in, State: "refused", Reason: "source or domain context changed before publication", Started: time.Now()}
		if ran {
			old.Execution = &hermes.ExtractionExecution{Provider: "lab-sparks", Model: "deepseek-v4.1-flash", ResponseModel: "deepseek-v4.1-flash", Steps: 1, Completed: true, Status: 200}
			old.Model = "deepseek-v4.1-flash"
		}
		if err := s.save(old); err != nil {
			t.Fatal(err)
		}
		id, err := s.Submit(in)
		var got Job
		raw, _ := os.ReadFile(filepath.Join(s.dir, old.ID+".json"))
		json.Unmarshal(raw, &got)
		if ran {
			if err == nil || got.State != "refused" {
				t.Fatalf("a refusal after a model ran stays held: %v %q", err, got.State)
			}
			continue
		}
		if err != nil || id != old.ID || got.State != "queued" || !strings.Contains(got.Reason, "earlier refused before any model ran") {
			t.Fatalf("an unexecuted refusal is queued again: %v %q %q", err, got.State, got.Reason)
		}
	}
}
