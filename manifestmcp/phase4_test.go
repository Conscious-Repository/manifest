package manifestmcp

import (
	"context"
	"errors"
	"manifest/recruiting/sources"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/graph"
	"manifest/recruiting"
)

func checkEffects(t *testing.T, a *Adapter, o *OperationRecord) {
	t.Helper()
	for rel, want := range o.Files {
		b, err := os.ReadFile(filepath.Join(a.Vault, rel))
		if err != nil || string(b) != want {
			t.Fatalf("vault effect %s differs: %v", rel, err)
		}
	}
	for rel, want := range o.CacheFiles {
		b, err := os.ReadFile(filepath.Join(a.Data, rel))
		if err != nil || string(b) != want {
			t.Fatalf("cache effect %s differs: %v", rel, err)
		}
	}
}
func TestPhase4Verbs(t *testing.T) {
	for _, verb := range []string{"unreject", "pin", "lookup", "entity", "seed"} {
		t.Run(verb, func(t *testing.T) {
			a, run, _ := fixture(t)
			// No external adapters in the lookup test: shared lookup returns empty evidence honestly.
			if verb == "lookup" {
				a.Runs, _ = recruiting.NewRunStore(a.Runs.Root(), a.Records)
			}
			var out Object
			var err error
			var args any
			switch verb {
			case "unreject":
				// Fixture records carry an unrestricted test writer; the MCP path below must use approved provenance.
				writes := func(p string, b []byte) error { return os.WriteFile(p, b, 0600) }
				r := recruiting.NewStore(a.Vault, a.Root, writes)
				runs, _ := recruiting.NewRunStore(a.Runs.Root(), r)
				if _, err = runs.Reject(run.ID, "d1", "pass", time.Now()); err != nil {
					t.Fatal(err)
				}
				q := DraftInput{RunID: run.ID, DraftID: "d1", IdempotencyKey: "unreject-1"}
				args = q
				out, err = a.unrejectPrepare(q)
			case "pin":
				q := PinInput{RunID: run.ID, Pinned: true, IdempotencyKey: "pin-1"}
				args = q
				out, err = a.pinPrepare(q)
			case "lookup":
				q := DraftInput{RunID: run.ID, DraftID: "d1", IdempotencyKey: "lookup-1"}
				args = q
				out, err = a.lookupPrepare(q)
			case "entity":
				q := GraphEntityInput{Entity: graph.Entity{Kind: "org", ID: "new-lab", Title: "New Lab", Source: "test"}, IdempotencyKey: "entity-1"}
				args = q
				out, err = a.graphEntityPrepare(q)
			case "seed":
				q := SeedInput{Seed: recruiting.Seed{Class: "lab", Name: "New Lab", URL: "https://example.org"}, IdempotencyKey: "seed-1"}
				args = q
				out, err = a.seedPrepare(q)
			}
			id := savePrepared(t, a, out, err, args)
			standing := verb == "pin" || verb == "lookup"
			o, _ := a.loadOperation(id)
			if standing {
				if o.Policy != "standing_authorization" {
					t.Fatal(o.Policy)
				}
			} else {
				if o.Policy != "human_approval" {
					t.Fatal(o.Policy)
				}
				if _, err := a.Execute(context.Background(), id); err == nil {
					t.Fatal("approval bypass")
				}
				approve(t, a, id)
			}
			o = execute(t, a, id, "succeeded")
			checkEffects(t, a, o)
			if o.Agent != "agent:alfred" {
				t.Fatal(o.Agent)
			}
			if !standing {
				audit, err := os.ReadFile(filepath.Join(a.Data, "write-audit.log"))
				if err != nil || !strings.Contains(string(audit), "approved-proposal") {
					t.Fatalf("audit: %s %v", audit, err)
				}
			}
			before := revision(snapshot(t, a.Vault))
			cacheBefore := revision(snapshot(t, a.Runs.Root()))
			execute(t, a, id, "succeeded")
			if before != revision(snapshot(t, a.Vault)) || cacheBefore != revision(snapshot(t, a.Runs.Root())) {
				t.Fatal("repeated effect")
			}
			old, err := a.previousRequest(o.Tool, args)
			if err != nil || old.ID != id {
				t.Fatalf("retry key: %v", err)
			}
			switch verb {
			case "unreject":
				out, err = a.unrejectPrepare(args.(DraftInput))
			case "pin":
				out, err = a.pinPrepare(args.(PinInput))
			case "entity":
				out, err = a.graphEntityPrepare(args.(GraphEntityInput))
			default:
				return
			}
			if err != nil || out["policy"] != "no_change" {
				t.Fatalf("no-op: %v %v", out, err)
			}
		})
	}
}
func TestPhase4StaleAndInterruptedPin(t *testing.T) {
	a, run, _ := fixture(t)
	q := PinInput{RunID: run.ID, Pinned: true}
	out, err := a.pinPrepare(q)
	id := savePrepared(t, a, out, err, q)
	if _, err = a.Runs.Pin(run.ID, true); err != nil {
		t.Fatal(err)
	}
	execute(t, a, id, "stale")
	// Simulate a process dying after the effect but before its final receipt.
	o, _ := a.loadOperation(id)
	o.Status = "executing"
	o.Result = Object{}
	if err = a.saveOperation(o); err != nil {
		t.Fatal(err)
	}
	o = execute(t, a, id, "partial")
	if len(o.Result["confirmedCacheFiles"].([]string)) == 0 {
		t.Fatal("lost cache effect")
	}
	execute(t, a, id, "partial")
}
func TestPhase4ResolveAndEdgeVocabulary(t *testing.T) {
	a, _, _ := fixture(t)
	for _, query := range []string{"Ada Example", "ada-example", "ada", "Example"} {
		matches := a.resolve(ResolveInput{Query: query, Domain: "graph"})
		if len(matches) == 0 || matches[0].Match == "" {
			t.Fatalf("missing provenance %s", query)
		}
		if query == "Example" && len(matches) != 2 {
			t.Fatal("partial ambiguity hidden")
		}
	}
	if _, err := a.neighbors(NeighborsInput{Ref: Ref{"graph", "wrong", "person", "ada"}}); err == nil {
		t.Fatal("namespace guessed")
	}
	from, to := Ref{"graph", "manifest", "person", "ada"}, Ref{"graph", "manifest", "org", "lab"}
	for _, kind := range a.Graph.Vocabulary().EdgeKinds {
		q := EdgeInput{From: from, To: to, Edge: graph.Edge{Kind: kind, Basis: "test evidence", Source: "test"}}
		out, err := a.edgePrepare(q)
		if err != nil {
			t.Fatalf("blocked kind %s: %v", kind, err)
		}
		id := savePrepared(t, a, out, err, q)
		approve(t, a, id)
		o := execute(t, a, id, "succeeded")
		checkEffects(t, a, o)
		out, err = a.edgePrepare(q)
		if err != nil || out["policy"] != "no_change" {
			t.Fatalf("duplicate %s", kind)
		}
		execute(t, a, id, "succeeded")
	}
}

// Embed the interface to ensure Lookup only calls the explicitly supplied ID/Search.
type lookupFailure struct {
	sources.Adapter
	calls int
}

func (f *lookupFailure) ID() string { return "openalex" }
func (f *lookupFailure) Search(context.Context, sources.Scope) ([]sources.CandidateDraft, error) {
	f.calls++
	return nil, errors.New("injected source outage")
}
func TestLookupPartialRecovery(t *testing.T) {
	a, run, _ := fixture(t)
	a.Runs, _ = recruiting.NewRunStore(a.Runs.Root(), a.Records)
	f := &lookupFailure{}
	a.Runs.Register(f)
	q := DraftInput{RunID: run.ID, DraftID: "d1"}
	out, err := a.lookupPrepare(q)
	id := savePrepared(t, a, out, err, q)
	o := execute(t, a, id, "partial")
	if f.calls != 1 || !strings.Contains(o.Error, "openalex") || len(o.Result["confirmedCacheFiles"].([]string)) == 0 {
		t.Fatal("missing partial evidence", o)
	}
	execute(t, a, id, "partial")
	if f.calls != 1 {
		t.Fatal("repeated external lookup")
	}
	o.Status = "executing"
	if err = a.saveOperation(o); err != nil {
		t.Fatal(err)
	}
	execute(t, a, id, "partial")
	if f.calls != 1 {
		t.Fatal("recovery repeated lookup")
	}
}
func TestSourceIntentIdentityRecovery(t *testing.T) {
	a, _, _ := fixture(t)
	q := SourceInput{Request: recruiting.RunRequest{Source: "manual", Query: "Recovery Person"}}
	out, err := a.sourcePrepare(q)
	id := savePrepared(t, a, out, err, q)
	o, _ := a.loadOperation(id)
	o.Status = "executing"
	o.Result = Object{}
	run, err := a.Runs.ExecuteTracked(context.Background(), q.Request, o.CreatedAt, func(runID string) error { o.Result["intendedRunId"] = runID; return a.saveOperation(o) })
	if err != nil {
		t.Fatal(err)
	}
	o = execute(t, a, id, "partial")
	if o.Result["runId"] != run.ID {
		t.Fatal("lost intended run")
	}
	execute(t, a, id, "partial")
}
func TestNewPrepareStale(t *testing.T) {
	for _, verb := range []string{"lookup", "seed", "entity", "unreject"} {
		t.Run(verb, func(t *testing.T) {
			a, run, _ := fixture(t)
			var out Object
			var err error
			var args any
			switch verb {
			case "lookup":
				q := DraftInput{RunID: run.ID, DraftID: "d1"}
				args = q
				out, err = a.lookupPrepare(q)
			case "seed":
				q := SeedInput{Seed: recruiting.Seed{Class: "lab", Name: "Stale Lab"}}
				args = q
				out, err = a.seedPrepare(q)
			case "entity":
				q := GraphEntityInput{Entity: graph.Entity{Kind: "org", ID: "stale", Title: "Stale Lab", Source: "test"}}
				args = q
				out, err = a.graphEntityPrepare(q)
			case "unreject":
				r := recruiting.NewStore(a.Vault, a.Root, func(p string, b []byte) error { return os.WriteFile(p, b, 0600) })
				runs, _ := recruiting.NewRunStore(a.Runs.Root(), r)
				if _, err = runs.Reject(run.ID, "d1", "pass", time.Now()); err != nil {
					t.Fatal(err)
				}
				q := DraftInput{RunID: run.ID, DraftID: "d1"}
				args = q
				out, err = a.unrejectPrepare(q)
			}
			id := savePrepared(t, a, out, err, args)
			o, _ := a.loadOperation(id)
			if o.Policy == "human_approval" {
				approve(t, a, id)
			}
			if err = os.WriteFile(filepath.Join(a.Vault, a.Root, "passed.md"), []byte("owner takeover"), 0600); err != nil {
				t.Fatal(err)
			}
			execute(t, a, id, "stale")
		})
	}
}
