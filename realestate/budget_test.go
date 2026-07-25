package realestate

import "testing"

func TestBudgetLockRoundTrip(t *testing.T) {
	line := FormatBaselineLock("2026-07-25", map[string]float64{
		CatAcquisition: 57000, CatHard: 183500, CatSoft: 8000, CatContingency: 18350,
	})
	locks := ParseBudgetLocks([]string{"| acquisition | 30000 |", line})
	if len(locks) != 1 {
		t.Fatalf("locks = %d, want 1 (legacy table row ignored)", len(locks))
	}
	l := locks[0]
	if l.Date != "2026-07-25" || l.Total != 266850 || l.Amounts[CatHard] != 183500 {
		t.Fatalf("round trip mismatch: %+v", l)
	}
	// old 5-category lock lines (with carry) still parse — Total is what matters
	old := ParseBudgetLocks([]string{"- [locked:: 2026-07-01] acquisition 57000 · hard 183500 · soft 5975 · carry 8000 · contingency 18350 · total 272825"})
	if len(old) != 1 || old[0].Total != 272825 {
		t.Fatalf("legacy 5-cat lock parse: %+v", old)
	}
}

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
	pb := ComputeProjectBudget(src, work, ledger, nil)

	if len(pb.Categories) != 4 {
		t.Fatalf("categories = %d, want 4", len(pb.Categories))
	}
	byKey := map[string]BudgetCatRow{}
	for _, c := range pb.Categories {
		byKey[c.Key] = c
	}
	if h := byKey[CatHard]; h.Budget != 183500 || h.Committed != 9000 || h.Paid != 3000 || h.Live != 183500 {
		t.Fatalf("hard = %+v", h)
	}
	if s := byKey[CatSoft]; s.Budget != 13700 || s.Paid != 2100 {
		t.Fatalf("soft (incl legacy carry row) = %+v", s)
	}
	if a := byKey[CatAcquisition]; a.Budget != 57000 || a.Live != 60000 || !a.Over {
		t.Fatalf("acquisition = %+v", a)
	}
	if c := byKey[CatContingency]; c.Budget != 18350 || c.Live != 0 {
		t.Fatalf("contingency = %+v", c)
	}
	wantPlan := 57000.0 + 183500 + 13700 + 18350
	if pb.PlanTotal != wantPlan {
		t.Fatalf("plan total = %v, want %v", pb.PlanTotal, wantPlan)
	}
	wantLive := 60000.0 + 183500 + 13700
	if pb.LiveTotal != wantLive {
		t.Fatalf("live total = %v, want %v", pb.LiveTotal, wantLive)
	}
	// unlocked → variance vs plan (live under plan because contingency is headroom)
	if pb.VariancePct >= 0 {
		t.Fatalf("variance = %v, want negative (under plan)", pb.VariancePct)
	}

	// locked baseline lower than live → positive variance + drift detection
	pb2 := ComputeProjectBudget(src, work, ledger, []BaselineLock{{Date: "2026-01-01", Total: 250000}})
	if pb2.VariancePct <= 0 || !pb2.Drift {
		t.Fatalf("locked variance/drift = %v %v", pb2.VariancePct, pb2.Drift)
	}

	// same-day re-lock: the LATER line in the file must win the tie
	pb3 := ComputeProjectBudget(src, work, ledger, []BaselineLock{
		{Date: "2026-07-25", Total: 100500}, {Date: "2026-07-25", Total: 133500},
	})
	if pb3.Baseline.Total != 133500 || len(pb3.History) != 1 || pb3.History[0].Total != 100500 {
		t.Fatalf("same-day re-lock: baseline %v history %+v", pb3.Baseline.Total, pb3.History)
	}
}

func TestSourceMoneyFallbackHardCosts(t *testing.T) {
	pb := ComputeProjectBudget(SourceMoney{HardCosts: 275000, ContingencyPct: 0.1}, nil, nil, nil)
	for _, c := range pb.Categories {
		if c.Key == CatHard && c.Budget != 275000 {
			t.Fatalf("hard fallback = %v, want 275000", c.Budget)
		}
		if c.Key == CatContingency && c.Budget != 27500 {
			t.Fatalf("contingency on fallback = %v, want 27500", c.Budget)
		}
	}
}
