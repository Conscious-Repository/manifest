package aion

import (
	"strings"

	"manifest/decisions"
)

// THE DECISION LEDGER ADAPTER (manifest P3 Phase 1). The dedicated
// `decisions` entity displaces the backlog's KindDecision line as the place a
// decision LIVES; the backlog line is not migrated (that is a later pass) —
// it COEXISTS, projected read-only into the same shape so one list can show
// both and the graph/ledger can point at either by ref (`decision:aion-bl/…`
// is a backlog decision; `decision:<slug>` is a ledger note). The backlog
// parse is untouched: this file only reads the item.
//
// Field for field, the aion contract the ledger entity reuses: Owner,
// Captured, NeededBy, Decided, Outcome (what was decided), Sources
// (provenance wikilinks). Status maps onto the ledger's lifecycle:
// open → open, in_progress → deliberating, decided → decided.

// DecisionSource is the Source a projected backlog decision carries.
const DecisionSource = "aion"

// AsDecision projects one backlog decision item; ok=false for a task.
func (it *BacklogItem) AsDecision() (decisions.Decision, bool) {
	if it == nil || it.Kind != KindDecision {
		return decisions.Decision{}, false
	}
	status := decisions.StatusOpen
	switch strings.ToLower(strings.TrimSpace(it.Status)) {
	case StatusDecided:
		status = decisions.StatusDecided
	case StatusInProgress:
		status = decisions.StatusDeliberating
	}
	if it.Decided != "" || it.Outcome != "" {
		status = decisions.StatusDecided
	}
	d := decisions.Decision{
		ID: it.ID, Title: it.Text, Owner: it.Owner, Status: status, Outcome: it.Outcome,
		Captured: it.Captured, NeededBy: it.NeededBy, Decided: it.Decided, Source: DecisionSource,
		Evidence: []decisions.Link{}, Alternatives: []decisions.Alternative{}, Downstream: []decisions.Link{},
		Sources: append([]string{}, it.Sources...),
	}
	for _, f := range it.Unknown {
		d.Unknown = append(d.Unknown, f)
	}
	return d, true
}

// Decisions projects every top-level backlog decision, in backlog order.
func (d *BacklogDoc) Decisions() []decisions.Decision {
	out := []decisions.Decision{}
	for _, it := range d.Items() {
		if dec, ok := it.AsDecision(); ok {
			out = append(out, dec)
		}
	}
	return out
}
