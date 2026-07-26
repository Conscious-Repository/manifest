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
	pb := ComputeProjectBudget(src, work, ledger)

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

func TestSourceMoneyFallbackHardCosts(t *testing.T) {
	pb := ComputeProjectBudget(SourceMoney{HardCosts: 275000, ContingencyPct: 0.1}, nil, nil)
	for _, c := range pb.Categories {
		if c.Key == CatHard && c.Budget != 275000 {
			t.Fatalf("hard fallback = %v, want 275000", c.Budget)
		}
		if c.Key == CatContingency && c.Budget != 27500 {
			t.Fatalf("contingency on fallback = %v, want 27500", c.Budget)
		}
	}
}
