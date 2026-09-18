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

// rebaseFixture builds an original held job on disk plus the live vault and
// ownership handoff it was read from.
func rebaseFixture(t *testing.T) (*Service, Job) {
	t.Helper()
	data, vault := t.TempDir(), t.TempDir()
	harness := prepareHandoff(t, data, "aion", 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // no real provider call
	s := New(ctx, data, vault, harness, Config{Aion: true}, hermes.NewRunner(hermes.Config{Enabled: true}), approvals.NewStore(t.TempDir()))
	i := inputFixture()
	writeInput(t, vault, i)
	j := Job{Version: 1, OwnershipRevision: 1, ID: i.ID(), Input: i, State: "uncertain", Reason: contextRebaseHold, Started: time.Now().UTC(), Finished: time.Now().UTC()}
	if err := s.save(j); err != nil {
		t.Fatal(err)
	}
	if err := s.report(j); err != nil {
		t.Fatal(err)
	}
	return s, j
}

func rebaseSourceHash(j Job) string { return approvals.EvidenceHash(j.Input.Documents[0].Text) }

// Drift the live context the way the owner's vault legitimately drifts.
func driftContext(t *testing.T, s *Service) {
	t.Helper()
	p := filepath.Join(s.vault, "system/aion/backlog.md")
	if err := os.WriteFile(p, []byte("- [x] old task [status:: done] [done:: 2026-09-18]\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// A rebased attempt must be refused while the context is unchanged: the drift
// gate is what makes a rebase different from a replay.
func TestRebaseRequiresRealContextDrift(t *testing.T) {
	s, j := rebaseFixture(t)
	before, _ := os.ReadFile(filepath.Join(s.dir, j.ID+".json"))

	_, err := s.RetryRebased(j.ID, j.Input.Documents[0].Name, rebaseSourceHash(j), "owner fixture authorization", []string{j.ID})
	if err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("expected unchanged-context refusal, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(s.dir, "rebase-reconciliations")); !os.IsNotExist(statErr) {
		t.Fatal("a refused rebase must not leave a receipt")
	}
	after, _ := os.ReadFile(filepath.Join(s.dir, j.ID+".json"))
	if string(before) != string(after) {
		t.Fatal("original job must never be modified")
	}
}

// The runner is stopped before any provider call, so the attempt is created and
// then held as uncertain — and the original stays byte-identical.
func TestRebasePreservesHistoryAndBindsReceipt(t *testing.T) {
	s, j := rebaseFixture(t)
	path := filepath.Join(s.dir, j.ID+".json")
	before, _ := os.ReadFile(path)
	driftContext(t, s)

	next, err := s.RetryRebased(j.ID, j.Input.Documents[0].Name, rebaseSourceHash(j), "owner fixture authorization", []string{j.ID})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == j.ID || next.ParentID != j.ID || next.Replay {
		t.Fatalf("rebased attempt identity is wrong: %+v", next)
	}
	if next.Input.ID() != j.ID {
		t.Fatal("rebased attempt must keep the original document identity")
	}
	if !s.validIdentity(next) {
		t.Fatal("rebased attempt identity rejected")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("original changed")
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "rebase-reconciliations", j.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var r RebaseReconciliation
	if json.Unmarshal(raw, &r) != nil || r.Replay || r.Version != 1 {
		t.Fatal("invalid rebase receipt")
	}
	if r.SourceHash != rebaseSourceHash(j) || r.AttemptID() != next.ID {
		t.Fatal("receipt does not bind the attempt")
	}
	if r.PriorContextHash == r.NewContextHash {
		t.Fatal("receipt must record a real context change")
	}
	if r.PriorContextHash != contextHash(j.Input) || r.NewContextHash != contextHash(next.Input) {
		t.Fatal("receipt context hashes do not match the attempt")
	}
	if len(r.PriorAttempts) != 1 || r.PriorAttempts[0] != j.ID {
		t.Fatal("receipt must name the prior attempt")
	}
	// A second rebase at the same context must be refused; the receipt is the hold.
	if _, err = s.RetryRebased(j.ID, j.Input.Documents[0].Name, rebaseSourceHash(j), "again", []string{j.ID}); err == nil {
		t.Fatal("repeat rebase authorized")
	}
	if len(s.ap.List("pending")) != 0 {
		t.Fatal("unexpected publication")
	}
}

// A job carrying provider effects, or a prior attempt that is not an honest
// hold, must never be rebased.
func TestRebaseRefusesUnsafeJobs(t *testing.T) {
	cases := map[string]func(*Job){
		"running":    func(j *Job) { j.State = "running" },
		"replay":     func(j *Job) { j.Replay = true },
		"published":  func(j *Job) { j.Published = 1 },
		"model":      func(j *Job) { j.Model = "deepseek-v4.1-flash" },
		"execution":  func(j *Job) { j.Execution = &hermes.ExtractionExecution{Steps: 1} },
		"cost":       func(j *Job) { j.SpentUSD = 1 },
		"candidates": func(j *Job) { j.Candidates = []approvals.Proposal{{ID: "abc"}} },
		"reason":     func(j *Job) { j.Reason = "candidate publication incomplete" },
		"source":     func(j *Job) { j.Input.Documents[0].Text += "changed" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, j := rebaseFixture(t)
			driftContext(t, s)
			mutate(&j)
			if err := s.save(j); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RetryRebased(j.ID, j.Input.Documents[0].Name, rebaseSourceHash(j), "owner", []string{j.ID}); err == nil {
				t.Fatal("accepted unsafe job")
			}
			files, _ := filepath.Glob(filepath.Join(s.dir, "rebase-reconciliations", "*.json"))
			if len(files) != 0 {
				t.Fatal("authorized unsafe rebase")
			}
		})
	}
	// A prior attempt that is not an eligible hold blocks the rebase.
	t.Run("ineligible prior", func(t *testing.T) {
		s, j := rebaseFixture(t)
		other := j
		other.ID = strings.Repeat("d", 64)
		other.Reason = "candidate publication incomplete"
		if err := s.save(other); err != nil {
			t.Fatal(err)
		}
		driftContext(t, s)
		if _, err := s.RetryRebased(j.ID, j.Input.Documents[0].Name, rebaseSourceHash(j), "owner", []string{other.ID}); err == nil {
			t.Fatal("accepted an ineligible prior attempt")
		}
	})
}
