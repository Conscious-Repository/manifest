package domainextract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/hermes"
)

type legacyFixture struct{ calls int }

func (l *legacyFixture) SpoolRunNow(string, string, string, string) error { l.calls++; return nil }
func (l *legacyFixture) EngineAlive() (bool, time.Time)                   { return false, time.Time{} }
func TestRoutingNeverFallsBack(t *testing.T) {
	legacy := &legacyFixture{}
	r := &Router{Config: Config{Aion: true}, Vault: t.TempDir(), Legacy: legacy}
	if e := r.SpoolRunNow("extractor", "aion", "- log/missing.md", ""); e == nil || legacy.calls != 0 {
		t.Fatal("enabled route fell back")
	}
	if e := r.SpoolRunNow("extractor", "real-estate", "fixture", ""); e == nil || legacy.calls != 0 {
		t.Fatal("retired legacy route dispatched")
	}
	if alive, _ := r.For("aion").EngineAlive(); alive {
		t.Fatal("unattached successor ready")
	}
	if alive, _ := r.For("real-estate").EngineAlive(); alive {
		t.Fatal("legacy health ignored")
	}
}

// Watcher batching/restarts must not change durable per-note identities.
func TestSuccessorSinkWithoutLegacyEngine(t *testing.T) {
	data, vault := t.TempDir(), t.TempDir()
	legacy := &legacyFixture{}
	r := &Router{Config: Config{Aion: true}, Vault: vault, Legacy: legacy}
	sink := aion.NewExtractSink(aion.ExtractorDomain, vault, "system", "extrinsic", data, r.For("aion"))
	input := inputFixture()
	input.Documents[0].Text = "---\ncategories: [sync, aion]\n---\nJane: I will review the draft."
	writeInput(t, vault, input)
	path := input.Documents[0].Name
	sink.Notify([]string{path}) // unattached successor retains retry
	if sink.QueuedCount() != 1 {
		t.Fatal("lost unattached retry")
	}
	harness := prepareHandoff(t, data, "aion", 1)
	svc := New(context.Background(), data, vault, harness, r.Config, hermes.NewRunner(hermes.Config{Enabled: true}), approvals.NewStore(t.TempDir()))
	r.Attach(svc) // no worker started: inspect durable acceptance only
	sink.Notify([]string{path})
	if sink.QueuedCount() != 0 || legacy.calls != 0 {
		t.Fatal("successor failed or fell back")
	}
	// Repeated notification and direct resubmission preserve the same job.
	sink.Notify([]string{path})
	if err := r.For("aion").SubmitNote(path); err != nil {
		t.Fatal(err)
	}
	jobs, _ := filepath.Glob(filepath.Join(svc.dir, "*.json"))
	if len(jobs) != 1 {
		t.Fatalf("duplicate jobs: %d", len(jobs))
	}
	var job Job
	b, _ := os.ReadFile(jobs[0])
	json.Unmarshal(b, &job)
	if job.State != "queued" || job.OwnershipRevision != 1 || len(job.Input.Documents) != 1 {
		t.Fatal("invalid durable job")
	}
	if err := r.For("real-estate").SubmitNote(path); err == nil || legacy.calls != 0 {
		t.Fatal("disabled domain dispatched")
	}
}
