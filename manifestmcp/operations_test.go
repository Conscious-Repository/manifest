package manifestmcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/graph"
	"manifest/recruiting"
)

func savePrepared(t *testing.T, a *Adapter, out Object, err error, args any) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.persist(out, args)
	if err != nil {
		t.Fatal(err)
	}
	return saved["operationId"].(string)
}
func approve(t *testing.T, a *Adapter, id string) {
	t.Helper()
	if _, err := a.Decide(id, "approved", "owner:local"); err != nil {
		t.Fatal(err)
	}
}
func execute(t *testing.T, a *Adapter, id, status string) *OperationRecord {
	t.Helper()
	out, err := a.Execute(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != status {
		t.Fatalf("want %s: %#v", status, out["record"])
	}
	return out["record"].(*OperationRecord)
}
func TestApprovedAcceptRealVaultWriter(t *testing.T) {
	a, run, _ := fixture(t)
	q := DraftInput{RunID: run.ID, DraftID: "d1", Conversation: "chat-1", Turn: "turn-2"}
	out, err := a.draftPrepare(q, true)
	id := savePrepared(t, a, out, err, q)
	if _, err = a.Execute(context.Background(), id); err == nil {
		t.Fatal("agent bypassed approval")
	}
	if _, err = a.Decide(id, "approved", "agent:alfred"); err == nil {
		t.Fatal("agent actor approved")
	}
	approve(t, a, id)
	o := execute(t, a, id, "succeeded")
	if o.Conversation != "chat-1" || o.Turn != "turn-2" || o.ApprovalActor != "owner:local" {
		t.Fatalf("missing provenance: %+v", o)
	}
	for rel, want := range o.Files {
		b, err := os.ReadFile(filepath.Join(a.Vault, rel))
		if err != nil || string(b) != want {
			t.Fatalf("effect differs from preview: %s: %v", rel, err)
		}
	}
	p := out["operation"].(Object)["preview"].(Object)
	for rel, want := range p["cacheFiles"].(map[string]string) {
		b, err := os.ReadFile(filepath.Join(a.Data, rel))
		if err != nil || string(b) != want {
			t.Fatalf("queue differs from preview: %s: %v", rel, err)
		}
	}
	audit, err := os.ReadFile(filepath.Join(a.Data, "write-audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), "approved-proposal") || !strings.Contains(string(audit), "aion-recruiting-approved") || !strings.Contains(string(audit), "graph-approved") || strings.Contains(string(audit), "user-action") {
		t.Fatalf("bad audit: %s", audit)
	}
	before := revision(snapshot(t, a.Vault))
	execute(t, a, id, "succeeded")
	if revision(snapshot(t, a.Vault)) != before {
		t.Fatal("double apply")
	}
	// Receipt remains available with the source cache removed, including after restart.
	if err = os.RemoveAll(a.Runs.Root()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(a.Vault, a.Data, a.System)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, restarted, id, "succeeded")
	if len(o.Result["objectRefs"].([]Ref)) < 2 {
		t.Fatal("missing candidate and graph refs")
	}
}
func TestStandingSourceAndIdempotency(t *testing.T) {
	a, _, _ := fixture(t)
	q := SourceInput{Request: recruiting.RunRequest{Source: "manual", Query: "Grace Example", Max: 1}}
	out, err := a.sourcePrepare(q)
	id := savePrepared(t, a, out, err, q)
	o := execute(t, a, id, "succeeded")
	if o.Policy != "standing_authorization" || o.ApprovalActor != "" {
		t.Fatal("wrong policy")
	}
	runID, ok := o.Result["runId"].(string)
	if !ok || runID == "" {
		t.Fatal("missing run ref")
	}
	if o.Result["queueCounts"].(map[string]int)["new"] != 1 {
		t.Fatal("missing queue")
	}
	again := execute(t, a, id, "succeeded")
	if again.Result["runId"] != runID {
		t.Fatal("duplicated run")
	}
}
func TestStaleAndDecisions(t *testing.T) {
	a, run, _ := fixture(t)
	q := DraftInput{RunID: run.ID, DraftID: "d1"}
	out, err := a.draftPrepare(q, true)
	id := savePrepared(t, a, out, err, q)
	approve(t, a, id)
	if err = os.WriteFile(filepath.Join(a.Vault, a.Root, "passed.md"), []byte("owner edit"), 0600); err != nil {
		t.Fatal(err)
	}
	execute(t, a, id, "stale")
	if len(a.Records.CandidateSlugs()) != 0 {
		t.Fatal("stale write")
	}
	out, err = a.draftPrepare(q, true)
	id = savePrepared(t, a, out, err, q)
	if _, err = a.Decide(id, "cancelled", "owner:local"); err != nil {
		t.Fatal(err)
	}
	execute(t, a, id, "cancelled")
}
func TestPartialAndInterruptedReconcile(t *testing.T) {
	a, run, _ := fixture(t)
	q := DraftInput{RunID: run.ID, DraftID: "d1"}
	out, err := a.draftPrepare(q, true)
	id := savePrepared(t, a, out, err, q)
	approve(t, a, id)
	real := a.approvedWriter()
	calls := 0
	a.writeApproved = func(rel string, b []byte) error {
		calls++
		if strings.HasPrefix(rel, a.Graph.Root()+"/") {
			return errors.New("injected graph failure")
		}
		return real(rel, b)
	}
	o := execute(t, a, id, "partial")
	if len(o.Applied) != 1 || len(a.Records.CandidateSlugs()) != 1 {
		t.Fatalf("candidate missing: %+v", o)
	}
	if len(o.Result["objectRefs"].([]Ref)) != 3 || !strings.Contains(o.Error, "graph failure") {
		t.Fatal("dishonest partial result")
	}
	for _, ref := range o.Result["objectRefs"].([]Ref) {
		if ref.Domain == "graph" {
			t.Fatal("unconfirmed graph ref reported as confirmed")
		}
	}
	prior := calls
	execute(t, a, id, "partial")
	if calls != prior {
		t.Fatal("blind retry")
	}
	// Model a crash after the candidate effect but before the final receipt.
	o.Status = "executing"
	o.Applied = nil
	if err = a.saveOperation(o); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(a.Vault, a.Data, a.System)
	if err != nil {
		t.Fatal(err)
	}
	recovered := execute(t, restarted, id, "partial")
	if len(recovered.Applied) != 1 {
		t.Fatal("failed reconciliation")
	}
	if len(a.Records.CandidateSlugs()) != 1 {
		t.Fatal("duplicate after restart")
	}
}
func TestOtherApprovedVerbs(t *testing.T) {
	for _, verb := range []string{"reject", "person", "edge"} {
		t.Run(verb, func(t *testing.T) {
			a, run, _ := fixture(t)
			var out Object
			var err error
			switch verb {
			case "reject":
				out, err = a.draftPrepare(DraftInput{RunID: run.ID, DraftID: "d1", Reason: "not now"}, false)
			case "person":
				out, err = a.personPrepare(PersonInput{Ref: Ref{"graph", "manifest", "person", "ada"}, Person: recruiting.NetworkPerson{Source: "fixture"}})
			case "edge":
				out, err = a.edgePrepare(EdgeInput{From: Ref{"graph", "manifest", "person", "ada"}, To: Ref{"graph", "manifest", "org", "lab"}, Edge: graph.Edge{Kind: "member_of", Basis: "lab page", Source: "fixture"}})
			}
			id := savePrepared(t, a, out, err, Object{})
			approve(t, a, id)
			o := execute(t, a, id, "succeeded")
			for rel, want := range o.Files {
				b, err := os.ReadFile(filepath.Join(a.Vault, rel))
				if err != nil || string(b) != want {
					t.Fatalf("preview mismatch: %s", rel)
				}
			}
			execute(t, a, id, "succeeded")
		})
	}
}
func TestMovedDraftStales(t *testing.T) {
	a, run, _ := fixture(t)
	q := DraftInput{RunID: run.ID, DraftID: "d1"}
	out, err := a.draftPrepare(q, true)
	id := savePrepared(t, a, out, err, q)
	approve(t, a, id)
	p := filepath.Join(a.Runs.Root(), run.ID, "drafts.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.ReplaceAll(string(b), "Ada Example", "Changed Example"))
	if err = os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	execute(t, a, id, "stale")
}
