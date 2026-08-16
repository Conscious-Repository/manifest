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
	}
	pb := ComputeProjectBudget(src, work, ledger, false)

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
	stages := []WorkStage{{ID: "demo", Text: "Demo", Tasks: []WorkTask{
		{ID: "demo/a", Text: "a", Checked: true},
		{ID: "demo/b", Text: "b", Checked: true},
		{ID: "demo/c", Text: "c"},
	}}}
	ledger := []LedgerRow{
		{Type: "bid", Status: "accepted", Amount: 5000, WorkID: "demo/a"},
		{Type: "bid", Status: "accepted", Amount: 8000, WorkID: "demo/b"},
		{Type: "expense", Amount: 3000, WorkID: "demo/b"},
		{Type: "expense", Amount: 1000, WorkID: "demo/c"},
	}
	JoinWorkLedger(stages, ledger)
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
	stages2 := []WorkStage{{ID: "demo", Text: "Demo", Tasks: []WorkTask{{ID: "demo/a", Text: "a", Checked: true}}}}
	JoinWorkLedger(stages2, []LedgerRow{
		{Type: "bid", Status: "accepted", Amount: 5000, WorkID: "demo/a", Doc: "receipt.pdf"},
	})
	if td2 := stages2[0].Tasks[0]; td2.Recognized != 5000 || td2.Unreconciled != 0 || !td2.Receipted {
		t.Fatalf("receipted: %+v", td2)
	}

	// property level: hard spent = recognized + untethered cash; ⚑ total rides up
	pb := ComputeProjectBudget(SourceMoney{}, stages, append(ledger,
		LedgerRow{Type: "expense", Amount: 700}), false) // untethered hard cash
	byKey := map[string]BudgetCatRow{}
	for _, c := range pb.Categories {
		byKey[c.Key] = c
	}
	if h := byKey[CatHard]; h.Paid != 14700 {
		t.Fatalf("hard spent = %v, want 14700 (14000 recognized + 700 untethered cash)", h.Paid)
	}
	if pb.Unreconciled != 10000 {
		t.Fatalf("unreconciled = %v, want 10000", pb.Unreconciled)
	}
}

func TestOwnedAcquisitionCountsAsSpent(t *testing.T) {
	src := SourceMoney{PurchasePrice: 55000, ClosingCosts: 2000}

	// owned, no ledger rows yet: the acquisition plan IS the spend
	pb := ComputeProjectBudget(src, nil, nil, true)
	byKey := map[string]BudgetCatRow{}
	for _, c := range pb.Categories {
		byKey[c.Key] = c
	}
	if a := byKey[CatAcquisition]; a.Paid != 57000 || a.Committed != 57000 || a.Over {
		t.Fatalf("owned acquisition = %+v, want paid/committed 57000, not over", a)
	}
	if pb.Paid != 57000 {
		t.Fatalf("paid = %v, want 57000", pb.Paid)
	}

	// owned + closing statement already in the ledger: max, not double count
	pb = ComputeProjectBudget(src, nil, []LedgerRow{
		{Type: "expense", Amount: 60000, Cat: "acquisition"},
	}, true)
	for _, c := range pb.Categories {
		if c.Key == CatAcquisition && (c.Paid != 60000 || !c.Over) {
			t.Fatalf("owned acquisition with ledger row = %+v, want paid 60000 (no double count), over", c)
		}
	}

	// not owned: plan alone is not spend
	pb = ComputeProjectBudget(src, nil, nil, false)
	if pb.Paid != 0 {
		t.Fatalf("tracked property paid = %v, want 0", pb.Paid)
	}
}

func TestSourceMoneyFallbackHardCosts(t *testing.T) {
	pb := ComputeProjectBudget(SourceMoney{HardCosts: 275000, ContingencyPct: 0.1}, nil, nil, false)
	for _, c := range pb.Categories {
		if c.Key == CatHard && c.Budget != 275000 {
			t.Fatalf("hard fallback = %v, want 275000", c.Budget)
		}
		if c.Key == CatContingency && c.Budget != 27500 {
			t.Fatalf("contingency on fallback = %v, want 27500", c.Budget)
		}
	}
}
