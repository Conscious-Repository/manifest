package manifestmcp

import (
	"manifest/approvals"
	"manifest/graph"
	"manifest/recruiting"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryHumanFamilyFilesExactProposal(t *testing.T) {
	a, run, root := fixture(t)
	a.Approvals = approvals.NewStore(filepath.Join(root, "artifacts"))
	person := Ref{"graph", "manifest", "person", "ada"}
	lab := Ref{"graph", "manifest", "org", "lab"}
	inputs := []struct {
		args    any
		prepare func() (Object, error)
	}{
		{DraftInput{RunID: run.ID, DraftID: "d1"}, func() (Object, error) { return a.draftPrepare(DraftInput{RunID: run.ID, DraftID: "d1"}, true) }},
		{DraftInput{RunID: run.ID, DraftID: "d1"}, func() (Object, error) { return a.draftPrepare(DraftInput{RunID: run.ID, DraftID: "d1"}, false) }},
		{PersonInput{Ref: person, Person: recruiting.NetworkPerson{Source: "test"}}, func() (Object, error) {
			return a.personPrepare(PersonInput{Ref: person, Person: recruiting.NetworkPerson{Source: "test"}})
		}},
		{EdgeInput{From: person, To: lab, Edge: graph.Edge{Kind: "member_of", Basis: "lab page", Source: "test"}}, func() (Object, error) {
			return a.edgePrepare(EdgeInput{From: person, To: lab, Edge: graph.Edge{Kind: "member_of", Basis: "lab page", Source: "test"}})
		}},
	}
	for _, item := range inputs {
		out, err := item.prepare()
		id := savePrepared(t, a, out, err, item.args)
		p, err := a.Approvals.LoadPending(ProposalID(id))
		if err != nil {
			t.Fatal(err)
		}
		if p.Type != approvals.TypeManifestOperation || p.ApplyPath != id || !strings.Contains(p.Body, "human_approval") {
			t.Fatalf("bad projection: %+v", p)
		}
		o, err := a.loadOperation(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(o.BeforeFiles) != len(o.Files) {
			t.Fatal("exact before/after diff missing")
		}
		for path := range o.Files {
			if !strings.Contains(p.Body, path) {
				t.Fatalf("missing effect %s", path)
			}
		}
		// A producer-only store cannot accidentally confirm via generic raw writes.
		if err := a.Approvals.Confirm(p.ID); err == nil {
			t.Fatal("unwired store bypassed operation executor")
		}
	}
	if len(a.Approvals.List("pending")) != 4 {
		t.Fatal("missing human family")
	}
}
