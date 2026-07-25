package realestate

import "strings"

// Rollup is the derived money picture for a property (or, summed, a deal). Every
// field is computed from budget + ledger at read time — nothing here is stored.
type Rollup struct {
	Budget       float64          `json:"budget"`
	Paid         float64          `json:"paid"`         // Σ expense rows
	Committed    float64          `json:"committed"`    // paid + Σ accepted bids
	PaidPct      float64          `json:"paidPct"`      // paid / budget (0 when no budget)
	CommittedPct float64          `json:"committedPct"` // committed / budget
	Categories   []CategoryRollup `json:"categories"`
	OverBudget   bool             `json:"overBudget"` // any category paid > its budget
}

// CategoryRollup is the same pair per budget category, so an over-budget category
// is visible before the total hides it.
type CategoryRollup struct {
	Category     string  `json:"category"`
	Budget       float64 `json:"budget"`
	Paid         float64 `json:"paid"`
	Committed    float64 `json:"committed"`
	PaidPct      float64 `json:"paidPct"`
	CommittedPct float64 `json:"committedPct"`
	Over         bool    `json:"over"` // paid > budget
}

// computeRollup derives the paid/committed/%out picture. Spend maps to a budget
// category case-insensitively; spend in an unbudgeted category still counts
// toward the totals (and surfaces as its own row with a zero budget).
func computeRollup(budget []BudgetLine, ledger []LedgerRow) Rollup {
	type acc struct {
		budget, paid, committed float64
	}
	cats := map[string]*acc{}
	var order []string
	touch := func(name string) *acc {
		key := strings.ToLower(strings.TrimSpace(name))
		if a, ok := cats[key]; ok {
			return a
		}
		a := &acc{}
		cats[key] = a
		order = append(order, key)
		return a
	}

	display := map[string]string{}
	for _, b := range budget {
		a := touch(b.Category)
		a.budget += b.Amount
		display[strings.ToLower(strings.TrimSpace(b.Category))] = b.Category
	}
	for _, r := range ledger {
		a := touch(r.Category)
		key := strings.ToLower(strings.TrimSpace(r.Category))
		if _, ok := display[key]; !ok {
			display[key] = r.Category
		}
		switch strings.ToLower(strings.TrimSpace(r.Type)) {
		case "expense":
			a.paid += r.Amount
			a.committed += r.Amount
		case "bid":
			if strings.EqualFold(strings.TrimSpace(r.Status), "accepted") {
				a.committed += r.Amount
			}
		}
	}

	var out Rollup
	for _, key := range order {
		a := cats[key]
		cr := CategoryRollup{
			Category: display[key], Budget: a.budget, Paid: a.paid, Committed: a.committed,
			PaidPct: pct(a.paid, a.budget), CommittedPct: pct(a.committed, a.budget),
			Over: a.paid > a.budget && a.budget > 0,
		}
		out.Categories = append(out.Categories, cr)
		out.Budget += a.budget
		out.Paid += a.paid
		out.Committed += a.committed
		if cr.Over {
			out.OverBudget = true
		}
	}
	out.PaidPct = pct(out.Paid, out.Budget)
	out.CommittedPct = pct(out.Committed, out.Budget)
	return out
}

// pct returns value/base as a fraction (0 when base is 0, never NaN/Inf).
func pct(value, base float64) float64 {
	if base <= 0 {
		return 0
	}
	return value / base
}
