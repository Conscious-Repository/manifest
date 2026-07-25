package realestate

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Pass-6 project budget: the full-project-cost view (acquisition + hard + soft +
// carry + contingency) with a frozen baseline. The PLAN drifts as underwriting
// fields and stage ests are edited; LOCKING snapshots it into the `## budget`
// section (one `- [locked:: date] …` line per lock, latest = baseline). LIVE is
// the forecast at completion; variance = live vs locked baseline. Nothing here
// is stored except the lock lines.

// Budget category keys (also the `[cat:: x]` ledger note-token values; rows
// without a cat token — including every work-tethered row — are hard costs).
const (
	CatAcquisition = "acquisition"
	CatHard        = "hard"
	CatSoft        = "soft"
	CatCarry       = "carry"
	CatContingency = "contingency"
)

// BudgetCats is the canonical category order.
var BudgetCats = []string{CatAcquisition, CatHard, CatSoft, CatCarry, CatContingency}

// SourceMoney is the underwriting money peek from the source.json sidecar
// (property-slice or single-parcel full-deal shape — properties[0]).
type SourceMoney struct {
	PurchasePrice  float64
	ClosingCosts   float64
	HardCosts      float64 // underwrite hard costs — fallback when the work list has no ests yet
	CarryCost      float64
	ContingencyPct float64
	SoftTotal      float64 // Σ soft_cost_items values
}

// BudgetCatRow is one category line of the project budget.
type BudgetCatRow struct {
	Key       string  `json:"key"`
	Budget    float64 `json:"budget"` // current plan (drifts with edits)
	Live      float64 `json:"live"`   // forecast at completion — max(budget, committed, paid)
	Committed float64 `json:"committed"`
	Paid      float64 `json:"paid"`
	Over      bool    `json:"over"` // live exceeds plan
}

// BaselineLock is one `- [locked:: date] …` line of the `## budget` section.
type BaselineLock struct {
	Date    string             `json:"date"`
	Amounts map[string]float64 `json:"amounts"`
	Total   float64            `json:"total"`
}

// ProjectBudget is the derived budget picture for one property.
type ProjectBudget struct {
	Categories  []BudgetCatRow `json:"categories"`
	PlanTotal   float64        `json:"planTotal"` // Σ category plans INCL contingency
	LiveTotal   float64        `json:"liveTotal"` // Σ category forecasts (contingency excluded — it's headroom)
	Committed   float64        `json:"committed"`
	Paid        float64        `json:"paid"`
	Baseline    *BaselineLock  `json:"baseline,omitempty"` // latest lock
	History     []BaselineLock `json:"history,omitempty"`  // earlier locks, newest first
	VariancePct float64        `json:"variancePct"` // (live − base)/base; base = baseline total when locked, else plan total
	Drift       bool           `json:"drift,omitempty"` // locked and the plan has moved since
}

// sourceMoney reads the underwriting money keys (tolerant, like sourceUnits).
func sourceMoney(path string) SourceMoney {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SourceMoney{}
	}
	type slice struct {
		PurchasePrice  float64            `json:"purchase_price"`
		ClosingCosts   float64            `json:"closing_costs"`
		HardCosts      float64            `json:"hard_costs"`
		CarryCost      float64            `json:"carry_cost"`
		ContingencyPct float64            `json:"contingency_pct"`
		SoftCostItems  map[string]float64 `json:"soft_cost_items"`
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
	m := SourceMoney{
		PurchasePrice: pick.PurchasePrice, ClosingCosts: pick.ClosingCosts,
		HardCosts: pick.HardCosts, CarryCost: pick.CarryCost, ContingencyPct: pick.ContingencyPct,
	}
	for _, amt := range pick.SoftCostItems {
		m.SoftTotal += amt
	}
	return m
}

// ComputeProjectBudget derives the category table + totals + variance.
func ComputeProjectBudget(src SourceMoney, work []WorkStage, ledger []LedgerRow, locks []BaselineLock) *ProjectBudget {
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
		CatSoft:        src.SoftTotal,
		CatCarry:       src.CarryCost,
		CatContingency: src.ContingencyPct * hardPlan,
	}

	pb := &ProjectBudget{}
	for _, key := range BudgetCats {
		row := BudgetCatRow{Key: key, Budget: plan[key]}
		if key != CatContingency {
			row.Committed = committed[key]
			row.Paid = paid[key]
			row.Live = row.Budget
			if row.Committed > row.Live {
				row.Live = row.Committed
			}
			if row.Paid > row.Live {
				row.Live = row.Paid
			}
			row.Over = row.Live > row.Budget
			pb.LiveTotal += row.Live
			pb.Committed += row.Committed
			pb.Paid += row.Paid
		}
		pb.PlanTotal += row.Budget
		pb.Categories = append(pb.Categories, row)
	}

	if len(locks) > 0 {
		// newest wins; locks arrive in file order, so reverse BEFORE the stable
		// sort — a same-day re-lock (later in the file) must beat the earlier one
		ordered := make([]BaselineLock, len(locks))
		for i, l := range locks {
			ordered[len(locks)-1-i] = l
		}
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Date > ordered[j].Date })
		pb.Baseline = &ordered[0]
		if len(ordered) > 1 {
			pb.History = ordered[1:]
		}
		pb.Drift = money(pb.PlanTotal) != money(pb.Baseline.Total)
	}
	base := pb.PlanTotal
	if pb.Baseline != nil {
		base = pb.Baseline.Total
	}
	if base > 0 {
		pb.VariancePct = (pb.LiveTotal - base) / base
	}
	return pb
}

// normalizeCat maps a `[cat::]` token value to a category key (default hard).
func normalizeCat(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case CatAcquisition:
		return CatAcquisition
	case CatSoft:
		return CatSoft
	case CatCarry:
		return CatCarry
	default:
		return CatHard
	}
}

// ParseBudgetLocks reads `- [locked:: date] acquisition N · hard N · …` lines
// from the `## budget` section (legacy table rows and anything else are ignored).
func ParseBudgetLocks(sectionLines []string) []BaselineLock {
	var out []BaselineLock
	for _, ln := range sectionLines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		lock := BaselineLock{Amounts: map[string]float64{}}
		rest := workFieldRe.ReplaceAllStringFunc(t[2:], func(m string) string {
			g := workFieldRe.FindStringSubmatch(m)
			if strings.EqualFold(g[1], "locked") {
				lock.Date = strings.TrimSpace(g[2])
			}
			return ""
		})
		if lock.Date == "" {
			continue
		}
		for _, part := range strings.Split(rest, "·") {
			fields := strings.Fields(part)
			if len(fields) != 2 {
				continue
			}
			amt, err := strconv.ParseFloat(strings.ReplaceAll(fields[1], ",", ""), 64)
			if err != nil {
				continue
			}
			if strings.EqualFold(fields[0], "total") {
				lock.Total = amt
			} else {
				lock.Amounts[strings.ToLower(fields[0])] = amt
			}
		}
		out = append(out, lock)
	}
	return out
}

// FormatBaselineLock renders one lock line (the write side of ParseBudgetLocks).
func FormatBaselineLock(date string, amounts map[string]float64) string {
	parts := make([]string, 0, len(BudgetCats)+1)
	total := 0.0
	for _, key := range BudgetCats {
		parts = append(parts, fmt.Sprintf("%s %s", key, money(amounts[key])))
		total += amounts[key]
	}
	parts = append(parts, "total "+money(total))
	return "- [locked:: " + date + "] " + strings.Join(parts, " · ")
}

// money renders an amount without exponent noise (whole dollars, cents kept when present).
func money(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
