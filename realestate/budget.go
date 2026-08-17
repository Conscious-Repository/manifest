package realestate

import (
	"encoding/json"
	"os"
	"strings"
)

// Project budget: the full-project-cost view (acquisition + hard + soft +
// contingency), PLAN vs SPEND only. The plan drifts as underwriting fields and
// stage ests are edited — that drifting number IS the budget (no frozen
// baseline; the owner manages forward). Nothing here is ever stored.

// Budget category keys (also the `[cat:: x]` ledger note-token values; rows
// without a cat token — including every work-tethered row — are hard costs).
// SOFT = construction interest · taxes · insurance · utilities (the pro forma's
// soft costs). Permits/design/arch are Pre-development task ests — hard.
// A legacy `carry` token normalizes to soft.
const (
	CatAcquisition = "acquisition"
	CatHard        = "hard"
	CatSoft        = "soft"
	CatContingency = "contingency"
)

// BudgetCats is the canonical category order.
var BudgetCats = []string{CatAcquisition, CatHard, CatSoft, CatContingency}

// SourceMoney is the underwriting money peek from the source.json sidecar
// (property-slice or single-parcel full-deal shape — properties[0]).
type SourceMoney struct {
	PurchasePrice  float64
	ClosingCosts   float64
	HardCosts      float64 // underwrite hard costs — fallback when the work list has no ests yet
	CarryCost      float64 // carry_cost field: the SOFT budget (interest/taxes/ins/util)
	ContingencyPct float64
}

// BudgetCatRow is one category line of the project budget.
type BudgetCatRow struct {
	Key       string  `json:"key"`
	Budget    float64 `json:"budget"` // current plan (drifts with edits — it IS the budget)
	Committed float64 `json:"committed"`
	Paid      float64 `json:"paid"`
	Over      bool    `json:"over"` // committed or paid exceeds the plan
}

// ProjectBudget is the derived budget picture for one property: the current
// plan vs actual spend (plan-vs-spend only — no frozen baseline; the plan
// drifting with edits is the point). SPENT is accrual for the work list
// (done + firm price = recognized) and cash for everything else.
type ProjectBudget struct {
	Categories   []BudgetCatRow `json:"categories"`
	PlanTotal    float64        `json:"planTotal"` // Σ category plans INCL contingency
	Committed    float64        `json:"committed"`
	Paid         float64        `json:"paid"`                   // recognized spend (hard accrual + cash rest)
	Unreconciled float64        `json:"unreconciled,omitempty"` // done-but-unlinked firm money (⚑)
	Over         bool           `json:"over"`                   // any category over its plan
}

// sourceMoney reads the underwriting money keys (tolerant, like sourceUnits).
func sourceMoney(path string) SourceMoney {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SourceMoney{}
	}
	type slice struct {
		PurchasePrice  float64 `json:"purchase_price"`
		ClosingCosts   float64 `json:"closing_costs"`
		HardCosts      float64 `json:"hard_costs"`
		CarryCost      float64 `json:"carry_cost"`
		ContingencyPct float64 `json:"contingency_pct"`
	}
	var v struct {
		slice
		Properties []slice `json:"properties"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return SourceMoney{}
	}
	pick := v.slice
	if pick.PurchasePrice == 0 && pick.HardCosts == 0 && len(v.Properties) > 0 {
		pick = v.Properties[0]
	}
	return SourceMoney{
		PurchasePrice: pick.PurchasePrice, ClosingCosts: pick.ClosingCosts,
		HardCosts: pick.HardCosts, CarryCost: pick.CarryCost, ContingencyPct: pick.ContingencyPct,
	}
}

// ComputeProjectBudget derives the category table + totals (plan vs spend).
// owned = the purchase already happened (control: owned), so the acquisition
// plan is recognized as spent even before closing rows land in the ledger.
func ComputeProjectBudget(src SourceMoney, work []WorkStage, ledger []LedgerRow, owned bool) *ProjectBudget {
	// actuals per category — the hard lane keeps the draw-aware max() semantics
	// (an expense against an accepted bid draws DOWN the contract, not up committed)
	type acc struct{ acceptedSum, expenseSum float64 }
	paid := map[string]float64{}
	committed := map[string]float64{}
	perWork := map[string]map[string]*acc{} // cat → workID → sums
	for _, row := range ledger {
		cat := normalizeCat(row.Cat)
		isExpense := strings.EqualFold(row.Type, "expense")
		accepted := strings.EqualFold(row.Type, "bid") && strings.EqualFold(row.Status, "accepted")
		if isExpense {
			paid[cat] += row.Amount
		}
		if row.WorkID != "" && (isExpense || accepted) {
			pw := perWork[cat]
			if pw == nil {
				pw = map[string]*acc{}
				perWork[cat] = pw
			}
			a := pw[row.WorkID]
			if a == nil {
				a = &acc{}
				pw[row.WorkID] = a
			}
			if isExpense {
				a.expenseSum += row.Amount
			} else {
				a.acceptedSum += row.Amount
			}
			continue
		}
		if isExpense || accepted {
			committed[cat] += row.Amount
		}
	}
	for cat, pw := range perWork {
		for _, a := range pw {
			c := a.expenseSum
			if a.acceptedSum > c {
				c = a.acceptedSum
			}
			committed[cat] += c
		}
	}

	// plans
	hardPlan := 0.0
	for _, st := range work {
		hardPlan += st.EstTotal
	}
	if hardPlan == 0 {
		hardPlan = src.HardCosts // work list not yet estimated → underwrite number
	}
	plan := map[string]float64{
		CatAcquisition: src.PurchasePrice + src.ClosingCosts,
		CatHard:        hardPlan,
		CatSoft:        src.CarryCost, // carry_cost field = interest/taxes/ins/util total (soft_cost_items is engine-only)
		CatContingency: src.ContingencyPct * hardPlan,
	}

	pb := &ProjectBudget{}
	for _, key := range BudgetCats {
		row := BudgetCatRow{Key: key, Budget: plan[key]}
		if key != CatContingency {
			row.Committed = committed[key]
			row.Paid = paid[key]
			if key == CatAcquisition && owned {
				// max, not +: a closing-statement expense row already in the
				// ledger must not double count against the plan
				if row.Budget > row.Paid {
					row.Paid = row.Budget
				}
				if row.Budget > row.Committed {
					row.Committed = row.Budget
				}
			}
			if key == CatHard {
				// accrual for the work list: recognized (done+firm counts) for
				// tethered money, cash for untethered/orphan-tethered rows
				var rec, tethCash float64
				for _, st := range work {
					rec += st.Recognized
					tethCash += st.Paid
					pb.Unreconciled += st.Unreconciled
				}
				row.Paid = rec + (paid[CatHard] - tethCash)
			}
			row.Over = row.Budget > 0 && (row.Committed > row.Budget || row.Paid > row.Budget)
			pb.Committed += row.Committed
			pb.Paid += row.Paid
			if row.Over {
				pb.Over = true
			}
		}
		pb.PlanTotal += row.Budget
		pb.Categories = append(pb.Categories, row)
	}
	return pb
}

// normalizeCat maps a `[cat::]` token value to a category key (default hard).
func normalizeCat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case CatAcquisition:
		return CatAcquisition
	case CatSoft, "carry": // legacy carry token folds into soft
		return CatSoft
	default:
		return CatHard
	}
}
