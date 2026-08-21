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
	// CatOperating marks post-stabilization money (rent-period utilities,
	// maintenance, management…) — the operating lane. NEVER a project
	// category: excluded from every rehab-budget figure and from BudgetCats.
	CatOperating = "operating"
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
	Key        string  `json:"key"`
	Budget     float64 `json:"budget"` // current plan (drifts with edits — it IS the budget)
	Committed  float64 `json:"committed"`
	Paid       float64 `json:"paid"`                 // cash out the door
	Recognized float64 `json:"recognized,omitempty"` // accrual (hard lane only; == Paid elsewhere)
	Over       bool    `json:"over"`                 // committed, recognized, or paid exceeds the plan
}

// ProjectBudget is the derived budget picture for one property: the current
// plan vs actual spend (plan-vs-spend only — no frozen baseline; the plan
// drifting with edits is the point).
//
// PAID is CASH: money we can verify left a bank account (expense rows, plus
// the acquisition plan once the deal actually closed). RECOGNIZED is the
// accrual read — cash plus work that is done at a firm price but has no
// expense row yet. They are separate fields because they answer different
// questions, and conflating them made "spent" unauditable.
type ProjectBudget struct {
	Categories   []BudgetCatRow `json:"categories"`
	PlanTotal    float64        `json:"planTotal"` // Σ category plans INCL contingency
	Committed    float64        `json:"committed"`
	Paid         float64        `json:"paid"`                   // cash out the door
	Recognized   float64        `json:"recognized,omitempty"`   // accrual: cash + done-at-firm-price work
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
// acq places the property relative to its own closing and decides how the
// acquisition plan lands: committed once under contract, spent once closed.
// Committed's hard lane comes from accepted-contract allocations (overhaul
// decision 4); legacy fallback: with none, accepted bid rows still commit.
func ComputeProjectBudget(src SourceMoney, work []WorkStage, ledger []LedgerRow, acq AcqState, allocs []NodeAllocation) *ProjectBudget {
	// actuals per category — the hard lane keeps the draw-aware max() semantics
	// (an expense against a committed node draws DOWN the contract, not up committed)
	type acc struct{ acceptedSum, expenseSum float64 }
	legacyBids := len(allocs) == 0
	paid := map[string]float64{}
	committed := map[string]float64{}
	perWork := map[string]map[string]*acc{} // cat → workID → sums
	touch := func(cat, workID string) *acc {
		pw := perWork[cat]
		if pw == nil {
			pw = map[string]*acc{}
			perWork[cat] = pw
		}
		a := pw[workID]
		if a == nil {
			a = &acc{}
			pw[workID] = a
		}
		return a
	}
	for _, a := range allocs {
		touch(CatHard, a.NodeID).acceptedSum += a.Amount // construction contracts are the hard lane
	}
	for _, row := range ledger {
		cat := normalizeCat(row.Cat)
		if cat == CatOperating {
			continue // operating lane — never project money
		}
		isExpense := strings.EqualFold(row.Type, "expense")
		accepted := legacyBids && strings.EqualFold(row.Type, "bid") && strings.EqualFold(row.Status, "accepted")
		if isExpense {
			paid[cat] += row.Amount
		}
		if row.WorkID != "" && (isExpense || accepted) {
			a := touch(cat, row.WorkID)
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
			row.Recognized = row.Paid
			if key == CatAcquisition && acq != AcqNone {
				// Under contract, the purchase price is money we are OBLIGATED
				// to bring to closing — committed, not spent. Only a closed
				// deal has actually moved it.
				//
				// max, not +: a closing-statement expense row already in the
				// ledger must not double count against the plan.
				if row.Budget > row.Committed {
					row.Committed = row.Budget
				}
				if acq == AcqClosed && row.Budget > row.Paid {
					row.Paid = row.Budget
					row.Recognized = row.Budget
				}
			}
			if key == CatHard {
				// The work list accrues: work done at a firm price is
				// recognized even before its expense row lands. That is a real
				// figure but it is not cash, so it rides in Recognized while
				// Paid stays strictly what the ledger can prove.
				var rec, tethCash float64
				for _, st := range work {
					rec += st.Recognized
					tethCash += st.Paid
					pb.Unreconciled += st.Unreconciled
				}
				row.Recognized = rec + (paid[CatHard] - tethCash)
			}
			row.Over = row.Budget > 0 &&
				(row.Committed > row.Budget || row.Recognized > row.Budget || row.Paid > row.Budget)
			pb.Committed += row.Committed
			pb.Paid += row.Paid
			pb.Recognized += row.Recognized
			if row.Over {
				pb.Over = true
			}
		}
		pb.PlanTotal += row.Budget
		pb.Categories = append(pb.Categories, row)
	}
	return pb
}

// CategoryBudget returns one category's plan (0 when absent) — the caller that
// needs "what does it cost to close" asks for CatAcquisition.
func (pb *ProjectBudget) CategoryBudget(key string) float64 {
	if pb == nil {
		return 0
	}
	for _, row := range pb.Categories {
		if row.Key == key {
			return row.Budget
		}
	}
	return 0
}

// normalizeCat maps a `[cat::]` token value to a category key (default hard).
func normalizeCat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case CatAcquisition:
		return CatAcquisition
	case CatSoft, "carry": // legacy carry token folds into soft
		return CatSoft
	case CatOperating: // the operating lane — must never default into hard
		return CatOperating
	default:
		return CatHard
	}
}
