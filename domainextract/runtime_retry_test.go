package domainextract

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/approvals"
	"manifest/hermes"
)

func runtimeFixture(t *testing.T) (*Service, Job, Job) {
	t.Helper()
	s, j := reconcileFixture(t)
	p, err := s.RetryPreProvider(j.ID, j.Input.Documents[0].Name, "first authorization")
	if err != nil {
		t.Fatal(err)
	}
	p.Reason = "bounded execution not verified; owner review required: Hermes agent initialization denied by filesystem boundary"
	if err = s.save(p); err != nil {
		t.Fatal(err)
	}
	return s, j, p
}

func TestRuntimeRetryPreservesHistoryAndSingleChild(t *testing.T) {
	s, j, p := runtimeFixture(t)
	paths := []string{filepath.Join(s.dir, j.ID+".json"), filepath.Join(s.dir, p.ID+".json"), filepath.Join(s.dir, "reconciliations", j.ID+".json"), filepath.Join(s.vault, j.Input.Documents[0].Name)}
	before := map[string]string{}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(b)
	}
	hash := approvals.EvidenceHash(j.Input.Documents[0].Text)
	child, err := s.RetryPostRuntimeFix(j.ID, j.Input.Documents[0].Name, hash, p.ID, "owner brief", "runtime:28404650")
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID != p.ID || child.Replay || child.State != "uncertain" || !s.validIdentity(child) {
		t.Fatal("invalid child", child.State)
	}
	for path, want := range before {
		b, _ := os.ReadFile(path)
		if string(b) != want {
			t.Fatal("history changed", path)
		}
	}
	for _, runtime := range []string{"runtime:28404650", "different-runtime"} {
		if _, err = s.RetryPostRuntimeFix(j.ID, j.Input.Documents[0].Name, hash, p.ID, "owner brief", runtime); err == nil {
			t.Fatal("second child allowed")
		}
	}
	p.Reason = "changed"
	s.save(p)
	if s.validIdentity(child) {
		t.Fatal("parent drift accepted")
	}
	if len(s.ap.List("pending")) != 0 {
		t.Fatal("unexpected publication")
	}
}

func TestRuntimeRetryRejectsUnsafeParent(t *testing.T) {
	cases := map[string]func(*Job){
		"state":         func(j *Job) { j.State = "completed" },
		"replay":        func(j *Job) { j.Replay = true },
		"published":     func(j *Job) { j.Published = 1 },
		"model":         func(j *Job) { j.Model = "model" },
		"http or usage": func(j *Job) { j.Execution = &hermes.ExtractionExecution{} },
		"cost":          func(j *Job) { j.SpentUSD = 0.01 },
		"candidates":    func(j *Job) { j.Candidates = []approvals.Proposal{{ID: "candidate"}} },
		"reason":        func(j *Job) { j.Reason = "interrupted execution; owner review required" },
		"fence":         func(j *Job) { j.OwnershipRevision++ },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, j, p := runtimeFixture(t)
			mutate(&p)
			s.save(p)
			if _, err := s.RetryPostRuntimeFix(j.ID, j.Input.Documents[0].Name, approvals.EvidenceHash(j.Input.Documents[0].Text), p.ID, "owner", "runtime"); err == nil {
				t.Fatal("unsafe parent accepted")
			}
		})
	}
	for _, mode := range []string{"hash", "source", "parent", "authorization", "runtime", "source drift", "context drift", "receipt only"} {
		t.Run(mode, func(t *testing.T) {
			s, j, p := runtimeFixture(t)
			source := j.Input.Documents[0].Name
			hash := approvals.EvidenceHash(j.Input.Documents[0].Text)
			parent, auth, runtime := p.ID, "owner", "runtime"
			switch mode {
			case "hash":
				hash = approvals.EvidenceHash("wrong")
			case "source":
				source = "log/wrong.md"
			case "parent":
				parent = j.ID
			case "authorization":
				auth = " "
			case "runtime":
				runtime = " "
			case "source drift":
				os.WriteFile(filepath.Join(s.vault, source), []byte("changed"), 0600)
			case "context drift":
				os.WriteFile(filepath.Join(s.vault, "system/aion/backlog.md"), []byte("changed"), 0600)
			case "receipt only":
				atomic(filepath.Join(s.dir, "runtime-reconciliations", j.ID+".json"), []byte("{}"))
			}
			if _, err := s.RetryPostRuntimeFix(j.ID, source, hash, parent, auth, runtime); err == nil {
				t.Fatal("unsafe request accepted")
			}
		})
	}
}
