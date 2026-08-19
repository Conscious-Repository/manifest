package realestate

import (
	"strings"
	"testing"
)

// A contract record round-trips: NewContractRecord → ParseContract preserves
// every field, including the multi-property allocation split.
func TestContractRoundTrip(t *testing.T) {
	c := Contract{
		Slug: "twisted-brick-masonry", Name: "Twisted Brick — masonry, 751 + 753 Bayard",
		Contractor: "twisted-brick", Status: "accepted", Total: 25500,
		Date: "2026-08-16", Expires: "2026-11-14", Doc: "sha256:ab3f00",
		Allocations: []ContractAllocation{
			{Property: "751-bayard-ave", NodeID: "exterior-structural/masonry", Amount: 12750},
			{Property: "753-bayard-ave", NodeID: "exterior-structural/masonry", Amount: 12750},
		},
		Terms:      []string{"50% deposit, 25% at halfway, balance at completion"},
		Exclusions: []string{"exact brick/mortar match not guaranteed"},
		RiskItems:  []string{"$10/lf decking", "$100/sheet plywood"},
	}
	raw := NewContractRecord(c)
	got := ParseContract("system/realestate/contracts/twisted-brick-masonry.md", c.Slug, raw)
	if got.Contractor != "twisted-brick" || got.Status != "accepted" || got.Total != 25500 {
		t.Fatalf("head fields: %+v", got)
	}
	if got.Date != c.Date || got.Expires != c.Expires || got.Doc != c.Doc || got.Name != c.Name {
		t.Fatalf("meta fields: %+v", got)
	}
	if len(got.Allocations) != 2 || got.Allocations[0] != c.Allocations[0] || got.Allocations[1] != c.Allocations[1] {
		t.Fatalf("allocations: %+v", got.Allocations)
	}
	if got.AllocTotal() != got.Total {
		t.Fatalf("Σ allocations %v != total %v", got.AllocTotal(), got.Total)
	}
	if len(got.Terms) != 1 || len(got.Exclusions) != 1 || len(got.RiskItems) != 2 {
		t.Fatalf("prose sections: %+v", got)
	}
	// a hand-edit through the same parse → emit converges
	again := ParseContract(got.Path, got.Slug, NewContractRecord(got))
	if !strings.Contains(NewContractRecord(again), "751-bayard-ave | exterior-structural/masonry | 12750") {
		t.Fatalf("re-emit lost the allocation:\n%s", NewContractRecord(again))
	}
}

// AllocationsFor projects only ACCEPTED contracts' slices for one property.
func TestAllocationsFor(t *testing.T) {
	cs := []Contract{
		{Slug: "tb", Contractor: "twisted-brick", Status: "accepted", Doc: "sha256:aa", Allocations: []ContractAllocation{
			{Property: "751-bayard-ave", NodeID: "exterior-structural/masonry", Amount: 12750},
			{Property: "753-bayard-ave", NodeID: "exterior-structural/masonry", Amount: 12750},
		}},
		{Slug: "pending", Contractor: "x", Status: "proposed", Allocations: []ContractAllocation{
			{Property: "751-bayard-ave", NodeID: "demo", Amount: 999},
		}},
	}
	got := AllocationsFor(cs, "751-bayard-ave")
	if len(got) != 1 || got[0].Contract != "tb" || got[0].Amount != 12750 || got[0].Doc != "sha256:aa" {
		t.Fatalf("allocations for 751: %+v", got)
	}
}

// Contract mode: allocations are the committed source; bids stop counting;
// expenses draw the contract down; recognition/receipt ride the allocation.
func TestJoinWorkLedgerContractMode(t *testing.T) {
	stages := ParseWork([]string{
		"- [ ] Exterior [work:: exterior]",
		"    - [x] masonry [work:: exterior/masonry]",
	})
	ledger := []LedgerRow{
		// converted legacy bid row — must NOT count once contracts exist
		{Type: "bid", Status: "accepted", Amount: 12750, WorkID: "exterior/masonry"},
		{Type: "expense", Status: "paid", Amount: 5000, WorkID: "exterior/masonry", Contract: "tb"},
		// operating lane — invisible to project rollups (assertions are the pin)
		{Type: "expense", Status: "paid", Amount: 61, Cat: "operating", Category: "internet"},
		{Type: "income", Status: "received", Amount: 1750, Category: "rent"},
	}
	allocs := []NodeAllocation{{Contract: "tb", Contractor: "twisted-brick", NodeID: "exterior/masonry", Amount: 12750}}
	JoinWorkLedger(stages, ledger, allocs)
	n := stages[0].Tasks[0]
	if n.Committed != 12750 || n.Paid != 5000 {
		t.Fatalf("draw-aware: committed=%v paid=%v (want 12750/5000, no bid double count)", n.Committed, n.Paid)
	}
	if len(n.Contracts) != 1 || n.Contracts[0].Slug != "tb" {
		t.Fatalf("contract chip: %+v", n.Contracts)
	}
	if len(n.Bids) != 0 {
		t.Fatalf("legacy bid chips must not render in contract mode: %+v", n.Bids)
	}
	// done + firm > paid, no doc on the contract → ⚑ unreconciled gap
	if n.Recognized != 12750 || n.Unreconciled != 7750 {
		t.Fatalf("recognition: rec=%v unrec=%v", n.Recognized, n.Unreconciled)
	}
	// same shape with the contract's doc attached → receipted, no flag
	stages2 := ParseWork([]string{
		"- [ ] Exterior [work:: exterior]",
		"    - [x] masonry [work:: exterior/masonry]",
	})
	allocs2 := []NodeAllocation{{Contract: "tb", NodeID: "exterior/masonry", Amount: 12750, Doc: "sha256:ab"}}
	JoinWorkLedger(stages2, []LedgerRow{}, allocs2)
	n2 := stages2[0].Tasks[0]
	if !n2.Receipted || n2.Unreconciled != 0 || n2.Recognized != 12750 {
		t.Fatalf("receipted via contract doc: %+v", n2)
	}

	// rollup + project budget agree on the committed source
	r := computeMoneyRollup(stages, ledger, allocs)
	if r.Committed != 12750 || r.Paid != 5000 {
		t.Fatalf("rollup: %+v", r)
	}
	pb := ComputeProjectBudget(SourceMoney{}, stages, ledger, false, allocs)
	for _, c := range pb.Categories {
		if c.Key == CatHard && c.Committed != 12750 {
			t.Fatalf("hard committed = %v, want 12750", c.Committed)
		}
	}
}

// A BID is an option, not money: ProposedFor projects it so it can be seen and
// accepted at the work node, while AllocationsFor (the budget's input) still
// sees only accepted records.
func TestProposedForAndBidJoin(t *testing.T) {
	cs := []Contract{
		{Slug: "wm-electric", Contractor: "wm-electric", Status: "proposed", Date: "2026-07-27",
			Allocations: []ContractAllocation{{Property: "4852-fountain-ave", NodeID: "rough-in/electrical", Amount: 42150}}},
		{Slug: "sparks-co", Contractor: "sparks-co", Status: "proposed", Date: "2026-08-02",
			Allocations: []ContractAllocation{{Property: "4852-fountain-ave", NodeID: "rough-in/electrical", Amount: 39800}}},
		{Slug: "twisted-brick", Contractor: "twisted-brick", Status: "accepted",
			Allocations: []ContractAllocation{{Property: "4852-fountain-ave", NodeID: "exterior-structural/masonry", Amount: 4500}}},
		{Slug: "old-quote", Contractor: "someone", Status: "declined",
			Allocations: []ContractAllocation{{Property: "4852-fountain-ave", NodeID: "rough-in/electrical", Amount: 51000}}},
	}
	bids := ProposedFor(cs, "4852-fountain-ave")
	if len(bids) != 2 {
		t.Fatalf("expected the two proposed bids, got %d", len(bids))
	}
	if bids[0].Date != "2026-07-27" {
		t.Fatalf("a bid carries its quote date for comparison, got %q", bids[0].Date)
	}
	// the accepted record must not leak into the bid list, and a bid must not
	// leak into the allocation list the budget sums
	for _, b := range bids {
		if b.Contract == "twisted-brick" || b.Contract == "old-quote" {
			t.Fatalf("only proposed records are bids, got %s", b.Contract)
		}
	}
	for _, a := range AllocationsFor(cs, "4852-fountain-ave") {
		if a.Contract != "twisted-brick" {
			t.Fatalf("only accepted records are allocations, got %s", a.Contract)
		}
	}

	stages := ParseWork([]string{
		"- [ ] Rough-in",
		"    - [ ] electrical [milestone::]",
	})
	JoinWorkBids(stages, bids)
	n := stages[0].Tasks[0]
	if len(n.OpenBids) != 2 {
		t.Fatalf("both bids attach to the node they quote, got %d", len(n.OpenBids))
	}
	if n.Committed != 0 {
		t.Fatalf("a bid must carry no committed weight, got %v", n.Committed)
	}
}
