package realestate

import "testing"

// The operating lane: income + [cat:: operating] expenses, bucketed monthly.
func TestComputeOperating(t *testing.T) {
	ledger := []LedgerRow{
		{Type: "income", Date: "2026-07-04", Amount: 1750, Category: "rent"},
		{Type: "income", Date: "2026-08-04", Amount: 1750, Category: "rent"},
		{Type: "expense", Date: "2026-08-10", Amount: 83.94, Cat: "operating", Category: "electric"},
		{Type: "expense", Date: "2026-08-17", Amount: 60.96, Cat: "operating", Category: "internet"},
		{Type: "expense", Date: "2026-08-20", Amount: 40, Cat: "operating"}, // no display cat → "other"
		// project money never joins the operating lane
		{Type: "expense", Date: "2026-08-02", Amount: 3000, WorkID: "demo/gut"},
		{Type: "expense", Date: "2026-08-02", Amount: 900, Cat: "soft"},
		{Type: "bid", Status: "accepted", Date: "2026-08-02", Amount: 500, Cat: "operating"}, // a bid is not cash
		// undated operating row → the "" bucket
		{Type: "expense", Amount: 12, Cat: "operating", Category: "trash"},
	}
	v := ComputeOperating(ledger)
	if v == nil {
		t.Fatal("nil view for a ledger with operating rows")
	}
	if v.Income != 3500 || v.Expenses != 83.94+60.96+40+12 {
		t.Fatalf("totals: income=%v expenses=%v", v.Income, v.Expenses)
	}
	if v.Net != v.Income-v.Expenses {
		t.Fatalf("net = %v", v.Net)
	}
	if len(v.Months) != 3 { // "", 2026-07, 2026-08 — ascending, "" first
		t.Fatalf("months = %+v", v.Months)
	}
	if v.Months[0].Month != "" || v.Months[0].Expenses != 12 {
		t.Fatalf("undated bucket: %+v", v.Months[0])
	}
	if v.Months[1].Month != "2026-07" || v.Months[1].Income != 1750 || v.Months[1].Expenses != 0 {
		t.Fatalf("july: %+v", v.Months[1])
	}
	aug := v.Months[2]
	if aug.Month != "2026-08" || aug.Income != 1750 || aug.Expenses != 83.94+60.96+40 {
		t.Fatalf("august: %+v", aug)
	}
	if aug.ByCategory["electric"] != 83.94 || aug.ByCategory["internet"] != 60.96 || aug.ByCategory["other"] != 40 {
		t.Fatalf("august byCategory: %+v", aug.ByCategory)
	}
}

// A project-only ledger (today's status quo) produces NO operating view —
// the backward-compat lock keeping every existing payload byte-identical.
func TestComputeOperatingNilForProjectLedgers(t *testing.T) {
	ledger := []LedgerRow{
		{Type: "expense", Date: "2026-07-18", Amount: 3000, WorkID: "demo/gut"},
		{Type: "expense", Date: "2026-07-19", Amount: 1200, Cat: "soft"},
		{Type: "bid", Status: "accepted", Date: "2026-07-20", Amount: 9000, WorkID: "demo/gut"},
	}
	if v := ComputeOperating(ledger); v != nil {
		t.Fatalf("want nil, got %+v", v)
	}
	if v := ComputeOperating(nil); v != nil {
		t.Fatalf("empty ledger: want nil, got %+v", v)
	}
}

// The chart of accounts: parse/emit round-trip + tolerant defaults.
func TestMoneyCategoriesParseEmit(t *testing.T) {
	raw := "---\ncategories: [money-categories]\nitems: [\"internet | expense | operating\", \"rent | income | operating\", \"windows | expense | project\", \"bare\", \"junk | nope | huh\"]\n---\n"
	cats := ParseMoneyCategories(raw)
	if len(cats) != 5 {
		t.Fatalf("cats = %+v", cats)
	}
	byName := map[string]MoneyCategory{}
	for _, c := range cats {
		byName[c.Name] = c
	}
	if c := byName["internet"]; c.Kind != "expense" || c.Class != "operating" {
		t.Fatalf("internet: %+v", c)
	}
	if c := byName["rent"]; c.Kind != "income" || c.Class != "operating" {
		t.Fatalf("rent: %+v", c)
	}
	// missing/garbled kind+class default expense/project — the conservative
	// bucket that never hides money from the rehab budget
	if c := byName["bare"]; c.Kind != "expense" || c.Class != "project" {
		t.Fatalf("bare: %+v", c)
	}
	if c := byName["junk"]; c.Kind != "expense" || c.Class != "project" {
		t.Fatalf("junk: %+v", c)
	}
	again := ParseMoneyCategories("---\nitems: " + EmitMoneyCategoryItems(cats) + "\n---\n")
	if len(again) != len(cats) {
		t.Fatalf("round trip lost items: %d vs %d", len(again), len(cats))
	}
}
