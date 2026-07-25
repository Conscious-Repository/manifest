package realestate

import "testing"

// The rollup is the load-bearing derived math: expenses move paid AND committed;
// an accepted bid moves ONLY committed; other bid statuses move neither; an
// over-budget category is flagged. This pins that contract.
func TestComputeRollup(t *testing.T) {
	budget := []BudgetLine{{"exterior", 40000}, {"interiors", 30000}}
	ledger := []LedgerRow{
		{Type: "expense", Category: "exterior", Amount: 28150, Status: "paid"},
		{Type: "bid", Category: "interiors", Amount: 18000, Status: "accepted"},
		{Type: "bid", Category: "interiors", Amount: 9999, Status: "requested"}, // pending — counts nowhere
	}
	r := computeRollup(budget, ledger)

	if r.Budget != 70000 || r.Paid != 28150 || r.Committed != 28150+18000 {
		t.Fatalf("totals = budget %.0f paid %.0f committed %.0f", r.Budget, r.Paid, r.Committed)
	}
	// paid% = 28150/70000 ≈ 0.402; committed% = 46150/70000 ≈ 0.659
	if got := round3(r.PaidPct); got != 0.402 {
		t.Fatalf("paidPct = %v, want 0.402", got)
	}
	if got := round3(r.CommittedPct); got != 0.659 {
		t.Fatalf("committedPct = %v, want 0.659", got)
	}
	// exterior: paid 28150 of 40000 (not over); a requested bid never counts.
	ext := catByName(r, "exterior")
	if ext.Paid != 28150 || ext.Committed != 28150 || ext.Over {
		t.Fatalf("exterior rollup wrong: %+v", ext)
	}
}

func TestComputeRollupOverBudget(t *testing.T) {
	r := computeRollup(
		[]BudgetLine{{"roof", 10000}},
		[]LedgerRow{{Type: "expense", Category: "roof", Amount: 12500, Status: "paid"}},
	)
	if !r.OverBudget || !catByName(r, "roof").Over {
		t.Fatalf("roof spend 12500 > 10000 budget must flag over: %+v", r)
	}
	if got := round3(r.PaidPct); got != 1.25 {
		t.Fatalf("paidPct = %v, want 1.25", got)
	}
}

func TestParsing(t *testing.T) {
	body := "# 4848 Page\nfree prose\n\n## budget\n| category | budget |\n| exterior | 42000 |\n| interiors | 30,000 |\n\n## log\n- 2026-07-20 framing passed\n- older note\n"
	secs := parseSections(body)
	budget := parseBudget(secs["budget"])
	if len(budget) != 2 || budget[0] != (BudgetLine{"exterior", 42000}) || budget[1].Amount != 30000 {
		t.Fatalf("budget parse (header skipped, comma money) wrong: %+v", budget)
	}
	log := parseLog(secs["log"])
	if len(log) != 2 || log[0] != "2026-07-20 framing passed" {
		t.Fatalf("log parse wrong: %+v", log)
	}

	csv := "date,type,category,vendor,contractor,amount,status,note,doc\n2026-07-18,expense,exterior,Home Depot,,1240.55,paid,fascia,\n"
	rows := parseLedger([]byte(csv))
	if len(rows) != 1 || rows[0].Vendor != "Home Depot" || rows[0].Amount != 1240.55 {
		t.Fatalf("ledger parse wrong: %+v", rows)
	}
}

func round3(f float64) float64 { return float64(int(f*1000+0.5)) / 1000 }

func catByName(r Rollup, name string) CategoryRollup {
	for _, c := range r.Categories {
		if c.Category == name {
			return c
		}
	}
	return CategoryRollup{}
}
