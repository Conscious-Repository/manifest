package aion

import (
	"testing"

	"manifest/decisions"
)

func TestBacklogDecisionsProjectIntoTheLedgerShape(t *testing.T) {
	src := "## Tasks\n- [ ] Ship the thing [id:: aion-bl/ship] [owner:: BA] [kind:: task]\n" +
		"## Decisions\n- Pick the vendor [id:: aion-bl/pick-the-vendor] [kind:: decision] [owner:: HZ] [captured:: 2026-09-01] [needed_by:: 2026-09-20] [source:: [[vendor call]]] [mood:: tense]\n" +
		"- [x] Drop the old CRM [id:: aion-bl/drop-crm] [kind:: decision] [status:: decided] [decided:: 2026-08-01] [outcome:: moved to attio]\n" +
		"- Rename the team [id:: aion-bl/rename] [kind:: decision] [status:: in_progress]\n"
	doc := ParseBacklog(src)
	if SerializeBacklog(doc) != src {
		t.Fatal("the adapter must not touch the backlog fixpoint")
	}
	ds := doc.Decisions()
	if len(ds) != 3 {
		t.Fatalf("decisions: %+v", ds)
	}
	open := ds[0]
	if open.ID != "aion-bl/pick-the-vendor" || open.Title != "Pick the vendor" || open.Owner != "HZ" || open.Status != decisions.StatusOpen ||
		open.Captured != "2026-09-01" || open.NeededBy != "2026-09-20" || open.Source != DecisionSource ||
		len(open.Sources) != 1 || open.Sources[0] != "vendor call" || len(open.Unknown) != 1 || open.Unknown[0].Key != "mood" {
		t.Fatalf("open: %+v", open)
	}
	if open.Evidence == nil || open.Alternatives == nil || open.Downstream == nil || open.Ref() != "decision:aion-bl/pick-the-vendor" {
		t.Fatalf("empty lists must be lists, and the ref is the graph endpoint: %+v", open)
	}
	if d := ds[1]; d.Status != decisions.StatusDecided || d.Decided != "2026-08-01" || d.Outcome != "moved to attio" {
		t.Fatalf("decided: %+v", d)
	}
	if d := ds[2]; d.Status != decisions.StatusDeliberating {
		t.Fatalf("in progress → deliberating: %+v", d)
	}
	if _, ok := doc.Items()[0].AsDecision(); ok {
		t.Fatal("a task is not a decision")
	}
}
