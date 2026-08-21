package realestate

import "testing"

func TestComputeProjectBudget(t *testing.T) {
	src := SourceMoney{PurchasePrice: 55000, ClosingCosts: 2000, ContingencyPct: 0.1, CarryCost: 13700}
	work := []WorkStage{{EstTotal: 100000}, {EstTotal: 83500}}
	ledger := []LedgerRow{
		{Type: "bid", Status: "accepted", Amount: 9000, WorkID: "demo/gut"},
		{Type: "expense", Amount: 3000, WorkID: "demo/gut"},  // draw — moves paid, not committed
		{Type: "expense", Amount: 1200, Cat: "soft"},         // soft actual (interest etc.)
		{Type: "expense", Amount: 900, Cat: "carry"},         // legacy carry token → soft
		{Type: "expense", Amount: 60000, Cat: "acquisition"}, // closing statement — over the 57k plan
		// operating-lane rows: MUST move no project figure (the safety pin)
		{Type: "expense", Amount: 800, Cat: "operating", Category: "internet"},
		{Type: "income", Amount: 1500, Category: "rent"},
	}
	pb := ComputeProjectBudget(src, work, ledger, AcqNone, nil)

	if len(pb.Categories) != 4 {
		t.Fatalf("categories = %d, want 4", len(pb.Categories))
	}
	byKey := map[string]BudgetCatRow{}
	for _, c := range pb.Categories {
		byKey[c.Key] = c
	}
	if h := byKey[CatHard]; h.Budget != 183500 || h.Committed != 9000 || h.Paid != 3000 || h.Over {
		t.Fatalf("hard = %+v", h)
	}
	if s := byKey[CatSoft]; s.Budget != 13700 || s.Paid != 2100 {
		t.Fatalf("soft (incl legacy carry row) = %+v", s)
	}
	if a := byKey[CatAcquisition]; a.Budget != 57000 || a.Paid != 60000 || !a.Over {
		t.Fatalf("acquisition = %+v", a)
	}
	if c := byKey[CatContingency]; c.Budget != 18350 || c.Committed != 0 || c.Paid != 0 {
		t.Fatalf("contingency = %+v", c)
	}
	wantPlan := 57000.0 + 183500 + 13700 + 18350
	if pb.PlanTotal != wantPlan {
		t.Fatalf("plan total = %v, want %v", pb.PlanTotal, wantPlan)
	}
	if pb.Paid != 3000+2100+60000 {
		t.Fatalf("paid = %v", pb.Paid)
	}
	if !pb.Over { // acquisition category is over → property flags over
		t.Fatalf("over should propagate from the acquisition category")
	}
}

func TestRecognizedSpend(t *testing.T) {
	// three tasks: done+firm+no cash (recognized, ⚑), done+firm+partial draw
	// (recognized at firm, ⚑ gap), open+cash (cash only, no flag)
	stages := ParseWork([]string{
		"- [ ] Demo [work:: demo]",
		"    - [x] a [work:: demo/a]",
		"    - [x] b [work:: demo/b]",
		"    - [ ] c [work:: demo/c]",
	})
	ledger := []LedgerRow{
		{Type: "bid", Status: "accepted", Amount: 5000, WorkID: "demo/a"},
		{Type: "bid", Status: "accepted", Amount: 8000, WorkID: "demo/b"},
		{Type: "expense", Amount: 3000, WorkID: "demo/b"},
		{Type: "expense", Amount: 1000, WorkID: "demo/c"},
		// TETHERED operating row — the nasty case: if it joined node Paid while
		// staying out of paid[hard], the hard accrual (rec + paid[hard] -
		// tethCash) would understate. Both guards must stay symmetric; every
		// assertion below holding is the pin.
		{Type: "expense", Amount: 250, WorkID: "demo/c", Cat: "operating", Category: "electric"},
	}
	JoinWorkLedger(stages, ledger, nil)
	td := stages[0].Tasks
	if td[0].Recognized != 5000 || td[0].Unreconciled != 5000 {
		t.Fatalf("done+firm no cash: %+v", td[0])
	}
	if td[1].Recognized != 8000 || td[1].Unreconciled != 5000 {
		t.Fatalf("done+firm partial draw: %+v", td[1])
	}
	if td[2].Recognized != 1000 || td[2].Unreconciled != 0 {
		t.Fatalf("open+cash: %+v", td[2])
	}
	if stages[0].Recognized != 14000 || stages[0].Unreconciled != 10000 {
		t.Fatalf("stage rollup: rec %v unrec %v", stages[0].Recognized, stages[0].Unreconciled)
	}

	// receipt on the accepted bid = evidence without cash → flag clears (bank OR receipt)
	stages2 := ParseWork([]string{
		"- [ ] Demo [work:: demo]",
		"    - [x] a [work:: demo/a]",
	})
	JoinWorkLedger(stages2, []LedgerRow{
		{Type: "bid", Status: "accepted", Amount: 5000, WorkID: "demo/a", Doc: "receipt.pdf"},
	}, nil)
	if td2 := stages2[0].Tasks[0]; td2.Recognized != 5000 || td2.Unreconciled != 0 || !td2.Receipted {
		t.Fatalf("receipted: %+v", td2)
	}

	// property level: hard RECOGNIZED = accrual + untethered cash; ⚑ total rides up
	pb := ComputeProjectBudget(SourceMoney{}, stages, append(ledger,
		LedgerRow{Type: "expense", Amount: 700}), AcqNone, nil) // untethered hard cash
	byKey := map[string]BudgetCatRow{}
	for _, c := range pb.Categories {
		byKey[c.Key] = c
	}
	if h := byKey[CatHard]; h.Recognized != 14700 {
		t.Fatalf("hard recognized = %v, want 14700 (14000 accrued + 700 untethered cash)", h.Recognized)
	}
	// and PAID is cash only: the 9,000 of done-at-firm-price work with no
	// expense row behind it is recognized but has not left any bank account
	if h := byKey[CatHard]; h.Paid != 4700 {
		t.Fatalf("hard paid = %v, want 4700 (expense rows only)", h.Paid)
	}
	if pb.Unreconciled != 10000 {
		t.Fatalf("unreconciled = %v, want 10000", pb.Unreconciled)
	}
}

// The acquisition plan becomes SPEND at closing, not at signing. Under
// contract it is committed — money we are obligated to bring to closing — and
// nothing more. Reading `control: owned` as "the purchase happened" put
// $558,000 of unclosed purchase prices into the portfolio's paid figure.
func TestAcquisitionSpendFollowsClosing(t *testing.T) {
	src := SourceMoney{PurchasePrice: 55000, ClosingCosts: 2000}
	cats := func(pb *ProjectBudget) map[string]BudgetCatRow {
		byKey := map[string]BudgetCatRow{}
		for _, c := range pb.Categories {
			byKey[c.Key] = c
		}
		return byKey
	}

	// closed, no ledger rows yet: the acquisition plan IS the spend
	pb := ComputeProjectBudget(src, nil, nil, AcqClosed, nil)
	if a := cats(pb)[CatAcquisition]; a.Paid != 57000 || a.Committed != 57000 || a.Over {
		t.Fatalf("closed acquisition = %+v, want paid/committed 57000, not over", a)
	}
	if pb.Paid != 57000 {
		t.Fatalf("paid = %v, want 57000", pb.Paid)
	}

	// UNDER CONTRACT: committed to close, but nothing has left the bank
	pb = ComputeProjectBudget(src, nil, nil, AcqUnderContract, nil)
	if a := cats(pb)[CatAcquisition]; a.Committed != 57000 || a.Paid != 0 || a.Recognized != 0 {
		t.Fatalf("under-contract acquisition = %+v, want committed 57000 and paid/recognized 0", a)
	}
	if pb.Paid != 0 || pb.Committed != 57000 {
		t.Fatalf("under contract: paid = %v (want 0), committed = %v (want 57000)", pb.Paid, pb.Committed)
	}

	// closed + closing statement already in the ledger: max, not double count
	pb = ComputeProjectBudget(src, nil, []LedgerRow{
		{Type: "expense", Amount: 60000, Cat: "acquisition"},
	}, AcqClosed, nil)
	if a := cats(pb)[CatAcquisition]; a.Paid != 60000 || !a.Over {
		t.Fatalf("closed acquisition with ledger row = %+v, want paid 60000 (no double count), over", a)
	}

	// an earnest-money deposit under contract is real cash and still counts
	pb = ComputeProjectBudget(src, nil, []LedgerRow{
		{Type: "expense", Amount: 2500, Cat: "acquisition"},
	}, AcqUnderContract, nil)
	if pb.Paid != 2500 {
		t.Fatalf("under-contract deposit: paid = %v, want 2500", pb.Paid)
	}

	// not ours: plan alone is neither spend nor commitment
	pb = ComputeProjectBudget(src, nil, nil, AcqNone, nil)
	if pb.Paid != 0 || pb.Committed != 0 {
		t.Fatalf("tracked property = paid %v / committed %v, want 0/0", pb.Paid, pb.Committed)
	}
}

// AcqStateOf reads STATUS, not control — `control: owned` is set the day a
// deal is signed, which is why it can never mean "the purchase happened".
func TestAcqStateReadsStatusNotControl(t *testing.T) {
	for _, tc := range []struct {
		control, status string
		want            AcqState
	}{
		{"owned", "under_contract", AcqUnderContract}, // the 28 Garden SPE parcels
		{"owned", "negotiating", AcqNone},
		{"owned", "pre_development", AcqClosed},
		{"owned", "construction", AcqClosed},
		{"owned", "leased", AcqClosed},
		{"owned", "sold", AcqClosed},
		{"owned", "", AcqNone},          // unknown status never assumes spend
		{"owned", "squatting", AcqNone}, // ditto for a word we have not met
		{"tracked", "construction", AcqNone},
		{"tracked", "negotiating", AcqNone},
		{"", "pre_development", AcqNone},
	} {
		if got := AcqStateOf(tc.control, tc.status); got != tc.want {
			t.Errorf("AcqStateOf(%q, %q) = %v, want %v", tc.control, tc.status, got, tc.want)
		}
	}
}

// Holdings splits an entity's records three ways and never sums them.
func TestHoldingsSplitsOwnedFromUnderContract(t *testing.T) {
	props := []Property{
		{Entity: "The Garden SPE LLC", Control: "owned", Status: "under_contract"},
		{Entity: "The Garden SPE LLC", Control: "owned", Status: "under_contract"},
		{Entity: "The Garden SPE LLC", Control: "owned", Status: "construction"},
		{Entity: "The Garden SPE LLC", Control: "owned", Status: "negotiating"},
		{Entity: "The Garden SPE LLC", Control: "owned", Status: "leased", Hidden: true},
		{Control: "tracked", Status: "negotiating"}, // no entity — counts nowhere
	}
	h := Holdings(props)["The Garden SPE LLC"]
	if h.Owned != 1 || h.Acquiring != 2 || h.Pipeline != 1 {
		t.Fatalf("holdings = %+v, want owned 1 / acquiring 2 / pipeline 1", h)
	}
}

func TestSourceMoneyFallbackHardCosts(t *testing.T) {
	pb := ComputeProjectBudget(SourceMoney{HardCosts: 275000, ContingencyPct: 0.1}, nil, nil, AcqNone, nil)
	for _, c := range pb.Categories {
		if c.Key == CatHard && c.Budget != 275000 {
			t.Fatalf("hard fallback = %v, want 275000", c.Budget)
		}
		if c.Key == CatContingency && c.Budget != 27500 {
			t.Fatalf("contingency on fallback = %v, want 27500", c.Budget)
		}
	}
}
