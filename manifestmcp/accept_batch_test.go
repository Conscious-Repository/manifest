package manifestmcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/recruiting"
)

func batchFixture(t *testing.T) (*Adapter, recruiting.Run) {
	t.Helper()
	a, run, _ := fixture(t)
	second := run.Drafts[0]
	second.ID = "d2"
	second.Draft.Name = "Grace Example"
	second.Draft.ExternalID = "grace-example"
	run.Drafts = append(run.Drafts, second)
	run.Counts.Fetched++
	run.Counts.New++
	for name, value := range map[string]any{"run.json": run.RunState, "drafts.json": run.Drafts} {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a.Runs.Root(), run.ID, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return a, run
}

func TestBatchAcceptSequentialDrafts(t *testing.T) {
	a, run := batchFixture(t)
	before := revision(snapshot(t, a.Vault))
	q := BatchAcceptInput{RunID: run.ID, DraftIDs: []string{"d1", "d2"}, Conversation: "batch-chat"}
	out, err := a.batchAcceptPrepare(q)
	id := savePrepared(t, a, out, err, q)
	if revision(snapshot(t, a.Vault)) != before {
		t.Fatal("preview wrote vault")
	}
	if _, err := a.Execute(t.Context(), id); err == nil {
		t.Fatal("bypassed approval")
	}
	approve(t, a, id)
	o := execute(t, a, id, "succeeded")
	if len(o.Result["accepted"].([]Object)) != 2 || o.ApprovalActor != "owner:local" || o.Conversation != "batch-chat" {
		t.Fatalf("bad receipt: %+v", o)
	}
	got, err := a.Runs.Get(run.ID)
	if err != nil || got.Counts.Accepted != 2 || got.Drafts[0].Status != recruiting.DraftAccepted || got.Drafts[1].Status != recruiting.DraftAccepted {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	for rel, want := range o.Files {
		b, err := os.ReadFile(filepath.Join(a.Vault, rel))
		if err != nil || string(b) != want {
			t.Fatalf("approved bytes differ: %s: %v", rel, err)
		}
	}
	for rel, want := range o.CacheFiles {
		b, err := os.ReadFile(filepath.Join(a.Data, rel))
		if err != nil || string(b) != want {
			t.Fatalf("queue bytes differ: %s: %v", rel, err)
		}
	}
	before = revision(snapshot(t, a.Vault))
	execute(t, a, id, "succeeded")
	if revision(snapshot(t, a.Vault)) != before {
		t.Fatal("replayed batch")
	}
	if _, err := a.batchAcceptPrepare(q); err == nil {
		t.Fatal("accepted again")
	}
}

func TestBatchAcceptStale(t *testing.T) {
	for _, change := range []string{"removed draft", "changed draft", "moved record", "accepted draft"} {
		t.Run(change, func(t *testing.T) {
			a, run := batchFixture(t)
			q := BatchAcceptInput{RunID: run.ID, DraftIDs: []string{"d1", "d2"}}
			out, err := a.batchAcceptPrepare(q)
			id := savePrepared(t, a, out, err, q)
			approve(t, a, id)
			switch change {
			case "removed draft", "changed draft":
				if change == "removed draft" {
					run.Drafts = run.Drafts[:1]
				} else {
					run.Drafts[1].Draft.Name = "Changed"
				}
				b, _ := json.Marshal(run.Drafts)
				if err := os.WriteFile(filepath.Join(a.Runs.Root(), run.ID, "drafts.json"), b, 0600); err != nil {
					t.Fatal(err)
				}
			case "moved record":
				src := filepath.Join(a.Vault, a.Graph.Root(), "entities.md")
				if err := os.Rename(src, src+".moved"); err != nil {
					t.Fatal(err)
				}
			case "accepted draft":
				single := DraftInput{RunID: run.ID, DraftID: "d1"}
				out, err := a.draftPrepare(single, true)
				singleID := savePrepared(t, a, out, err, single)
				approve(t, a, singleID)
				execute(t, a, singleID, "succeeded")
			}
			before := revision(snapshot(t, a.Vault))
			execute(t, a, id, "stale")
			if revision(snapshot(t, a.Vault)) != before {
				t.Fatal("stale batch wrote")
			}
		})
	}
}

func TestBatchAcceptSelectionValidation(t *testing.T) {
	a, run := batchFixture(t)
	for _, ids := range [][]string{nil, {"d1", "d1"}, {"d1", "missing"}, make([]string, recruiting.MaxRunMax+1)} {
		if _, err := a.batchAcceptPrepare(BatchAcceptInput{RunID: run.ID, DraftIDs: ids}); err == nil {
			t.Fatalf("accepted invalid selection %v", ids)
		}
	}
}

func TestBatchAcceptPartialDoesNotReplay(t *testing.T) {
	a, run := batchFixture(t)
	q := BatchAcceptInput{RunID: run.ID, DraftIDs: []string{"d1", "d2"}}
	out, err := a.batchAcceptPrepare(q)
	id := savePrepared(t, a, out, err, q)
	approve(t, a, id)
	real := a.approvedWriter()
	a.writeApproved = func(rel string, b []byte) error {
		if strings.Contains(rel, "grace-example.md") {
			return errors.New("injected disk failure")
		}
		return real(rel, b)
	}
	o := execute(t, a, id, "partial")
	if len(o.Result["confirmedDrafts"].([]string)) != 1 {
		t.Fatalf("missing partial queue recovery: %+v", o.Result)
	}
	got, err := a.Runs.Get(run.ID)
	if err != nil || got.Drafts[0].Status != recruiting.DraftAccepted || got.Drafts[1].Status != recruiting.DraftNew {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	a.writeApproved = real
	before := revision(snapshot(t, a.Vault))
	execute(t, a, id, "partial")
	if revision(snapshot(t, a.Vault)) != before {
		t.Fatal("partial batch replayed")
	}
}

// Fresh single prepares still work sequentially; callers preparing several at
// once use the batch tool so shared file changes are approved up front.
func TestFreshSequentialAccepts(t *testing.T) {
	a, run := batchFixture(t)
	for _, draftID := range []string{"d1", "d2"} {
		q := DraftInput{RunID: run.ID, DraftID: draftID}
		out, err := a.draftPrepare(q, true)
		id := savePrepared(t, a, out, err, q)
		approve(t, a, id)
		execute(t, a, id, "succeeded")
	}
}
