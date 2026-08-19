package realestate

import (
	"sort"
	"strings"
)

// The operating lane (money-workbench v2): a stabilized property's real
// in-and-out — rent collected, utilities, maintenance — derived from the
// ledger, never stored. Selection: income rows (any category) + expense rows
// carrying [cat:: operating] (the chart-of-accounts class writes the token
// at apply). Project/rehab math never sees these rows (the skip gates in
// budget.go / rollup.go / work.go), and this view never sees project spend.

// OperatingMonth is one month bucket of the operating lane.
type OperatingMonth struct {
	Month    string  `json:"month"` // "2026-08"; "" bucket for undated rows
	Income   float64 `json:"income"`
	Expenses float64 `json:"expenses"`
	Net      float64 `json:"net"`
	// ByCategory breaks the EXPENSES down by the display category column
	// (math-inert everywhere else — put to work here).
	ByCategory map[string]float64 `json:"byCategory,omitempty"`
}

// OperatingView is the property's whole operating lane. Nil when the ledger
// holds no operating money — existing payloads stay byte-identical.
type OperatingView struct {
	Months   []OperatingMonth `json:"months"`
	Income   float64          `json:"income"`
	Expenses float64          `json:"expenses"`
	Net      float64          `json:"net"`
}

// ComputeOperating derives the view from a parsed ledger.
func ComputeOperating(ledger []LedgerRow) *OperatingView {
	months := map[string]*OperatingMonth{}
	touch := func(date string) *OperatingMonth {
		key := ""
		if len(date) >= 7 && date[4] == '-' {
			key = date[:7]
		}
		m, ok := months[key]
		if !ok {
			m = &OperatingMonth{Month: key, ByCategory: map[string]float64{}}
			months[key] = m
		}
		return m
	}
	view := &OperatingView{}
	for _, r := range ledger {
		switch {
		case strings.EqualFold(r.Type, "income"):
			touch(r.Date).Income += r.Amount
			view.Income += r.Amount
		case strings.EqualFold(r.Type, "expense") && normalizeCat(r.Cat) == CatOperating:
			m := touch(r.Date)
			m.Expenses += r.Amount
			cat := strings.ToLower(strings.TrimSpace(r.Category))
			if cat == "" {
				cat = "other"
			}
			m.ByCategory[cat] += r.Amount
			view.Expenses += r.Amount
		}
	}
	if len(months) == 0 {
		return nil // the backward-compat lock: no operating money → no payload
	}
	view.Net = view.Income - view.Expenses
	for _, m := range months {
		m.Net = m.Income - m.Expenses
		if len(m.ByCategory) == 0 {
			m.ByCategory = nil
		}
		view.Months = append(view.Months, *m)
	}
	sort.Slice(view.Months, func(i, j int) bool { return view.Months[i].Month < view.Months[j].Month })
	return view
}
