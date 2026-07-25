package signals

import (
	"testing"
	"time"

	"manifest/realestate"
)

type fakeProps struct{ props []realestate.Property }

func (f fakeProps) Properties() ([]realestate.Property, error) { return f.props, nil }

func TestOverBudgetProperties(t *testing.T) {
	p := realestate.Property{Slug: "4848-page", Address: "4848 Page Blvd"}
	p.Rollup.Categories = []realestate.CategoryRollup{
		{Category: "exterior", Budget: 10000, Paid: 12500, Over: true},
		{Category: "interiors", Budget: 30000, Paid: 5000, Over: false},
	}
	sigs, err := OverBudgetProperties(fakeProps{[]realestate.Property{p}}).Emit(time.Now())
	if err != nil || len(sigs) != 1 {
		t.Fatalf("want exactly one over-budget signal, got %d (%v)", len(sigs), err)
	}
	s := sigs[0]
	if s.ID != "property-overbudget:4848-page:exterior" || s.ActHref != "#/properties/4848-page" {
		t.Fatalf("bad signal identity: %+v", s)
	}
	if s.Hash != "2500" { // overage — dismissal re-arms only on further slippage
		t.Fatalf("hash = %q, want the overage 2500", s.Hash)
	}
}

func TestStalledProperties(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	fresh := realestate.Property{Slug: "a", Status: "construction",
		Log: []string{"2026-07-20 framing passed"}} // 5d ago — not stalled
	stale := realestate.Property{Slug: "b", Address: "738 N Euclid", Status: "construction",
		Log:    []string{"2026-06-01 demo done"},
		Ledger: []realestate.LedgerRow{{Date: "2026-06-20", Type: "expense", Amount: 1}}} // 35d — stalled
	pipeline := realestate.Property{Slug: "c", Status: "negotiating"} // not active bucket

	sigs, err := StalledProperties(fakeProps{[]realestate.Property{fresh, stale, pipeline}}).Emit(now)
	if err != nil || len(sigs) != 1 {
		t.Fatalf("want exactly one stalled signal, got %d (%v)", len(sigs), err)
	}
	s := sigs[0]
	if s.ID != "property-stalled:b" || s.Hash != "2026-06-20" {
		t.Fatalf("stalled signal should key on the LATEST activity (ledger 6/20): %+v", s)
	}
	if s.Age != 35 {
		t.Fatalf("age = %d, want 35 days", s.Age)
	}
}
