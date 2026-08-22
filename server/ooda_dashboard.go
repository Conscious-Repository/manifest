package server

import (
	"sort"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/teamportal"
)

// The OODA dashboard + work projections (ooda-portal plan, Stage B §6).
// Pure functions over a composed snapshot — no I/O, table-driven, so the KPI
// math is testable without a vault and provably agrees with the cockpit.
//
// The phase map and the three attention rules are ported VERBATIM from the
// cockpit's portfolio list (81-properties-board.js:19-60) so the two surfaces
// never disagree about which properties need attention.

var oodaPhase = map[string]string{
	"construction":    "construction",
	"pre_development": "pre-dev",
	"under_contract":  "pipeline",
	"negotiating":     "pipeline",
	"completed":       "stabilized",
	"leased":          "stabilized",
	"listed":          "stabilized",
	"sold":            "closed",
}

func oodaSettled(phase string) bool { return phase == "stabilized" || phase == "closed" }

// oodaFacts is one property's derived picture — every figure computed, none
// stored (RE spec §2).
type oodaFacts struct {
	Slug       string  `json:"slug"`
	Address    string  `json:"address"`
	Short      string  `json:"short"`
	Entity     string  `json:"entity"`
	Status     string  `json:"status"`
	Phase      string  `json:"phase"`
	Deal       string  `json:"deal,omitempty"`
	Rock       string  `json:"rock,omitempty"`
	DoneBy     string  `json:"doneBy,omitempty"`
	Open       int     `json:"open"`
	PctDone    float64 `json:"pctDone"`
	HasStages  bool    `json:"hasStages"`
	Plan       float64 `json:"plan"`
	Paid       float64 `json:"paid"`                 // cash out the door
	Recognized float64 `json:"recognized,omitempty"` // accrual: cash + done-at-firm-price work
	Committed  float64 `json:"committed"`            // signed contracts (+ purchase price once committed)
	ToClose    float64 `json:"toClose,omitempty"`    // purchase price + closing costs still to fund
	ToGo       float64 `json:"toGo"`
	// Acq is the honest ownership word for this row: owned | under-contract |
	// pipeline. Partners read this column, so it must never say "owned" about
	// a deal that has not closed.
	Acq     string `json:"acq"`
	Over    bool   `json:"over"`
	Late    bool   `json:"late"`
	Stalled bool   `json:"stalled"`
}

// Attention is the three-rule outlier test, cockpit-identical.
func (f oodaFacts) Attention() bool { return f.Over || f.Late || f.Stalled }

func oodaPropertyFacts(p realestate.Property, today string) oodaFacts {
	f := oodaFacts{
		Slug: p.Slug, Address: p.Address, Short: p.Short, Entity: p.Entity,
		Status: p.Status, Deal: p.Deal,
		Phase: oodaPhase[p.Status],
	}
	if f.Phase == "" {
		f.Phase = "pipeline"
	}
	if f.Short == "" {
		f.Short = p.Address
	}
	done := 0
	for i := range p.Work {
		if p.Work[i].Checked {
			done++
			continue
		}
		if f.Rock == "" {
			f.Rock, f.DoneBy = p.Work[i].Text, p.Work[i].DoneBy
		}
	}
	f.HasStages = len(p.Work) > 0
	if f.HasStages {
		f.PctDone = float64(done) / float64(len(p.Work)) * 100
	}
	realestate.WalkNodes(p.Work, func(_ *realestate.WorkStage, n *realestate.WorkNode) {
		if n.Task != nil && !n.Task.Checked && !n.Decision {
			f.Open++
		}
	})
	acq := realestate.AcqStateOf(p.Control, p.Status)
	f.Acq = acq.String()
	if p.Project != nil {
		f.Plan, f.Paid, f.Committed = p.Project.PlanTotal, p.Project.Paid, p.Project.Committed
		f.Recognized = p.Project.Recognized
		f.ToGo = f.Plan - f.Paid
		if acq == realestate.AcqUnderContract {
			f.ToClose = p.Project.CategoryBudget(realestate.CatAcquisition)
		}
	}
	f.Over = f.Plan > 0 && (f.Recognized > f.Plan || f.Committed > f.Plan)
	f.Late = f.DoneBy != "" && f.DoneBy < today
	// stalled needs a WORK PLAN to be stalled against — without the hasStages
	// arm every under-contract parcel with no rocks lit the filter, which is
	// exactly the noise it exists to cut.
	f.Stalled = f.HasStages && f.Open == 0 && f.PctDone < 100 &&
		p.Status != "negotiating" && !oodaSettled(f.Phase)
	return f
}

// oodaVisibleProps drops hidden records and the research tail (parcels the
// owner tracks but does not hold) — the portfolio the partners share.
func oodaVisibleProps(snap *oodaSnapshot) []realestate.Property {
	var out []realestate.Property
	for _, p := range snap.Properties {
		if p.Hidden || !(p.Control == "owned" || p.Entity != "") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// oodaEntityRow counts an entity's holdings in three separate buckets — they
// are NEVER summed into one "properties" number. "The Garden SPE owns 32" was
// exactly that error: 28 of those 32 are still under contract.
type oodaEntityRow struct {
	Entity    string  `json:"entity"`
	Owned     int     `json:"owned"`     // closed — on the books today
	Acquiring int     `json:"acquiring"` // under contract — obligated to close
	Pipeline  int     `json:"pipeline"`  // negotiating — no obligation yet
	Committed float64 `json:"committed"`
	Paid      float64 `json:"paid"`
	ToClose   float64 `json:"toClose"` // purchase money still to fund
	OpenWork  int     `json:"openWork"`
}

type oodaDealRow struct {
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Members   int     `json:"members"`
	Committed float64 `json:"committed"`
	Paid      float64 `json:"paid"`
}

type oodaOwnerRow struct {
	Owner     string `json:"owner"`
	Name      string `json:"name,omitempty"`
	Open      int    `json:"open"`
	Decisions int    `json:"decisions"`
	Overdue   int    `json:"overdue"`
}

// oodaDashboard is the whole main surface in one payload.
type oodaDashboard struct {
	// The three money figures are the owner's definitions, verbatim:
	//
	//   committed = acquired projects' TOTAL budgets + under-contract budgets to close
	//   paid      = money we can verify left a bank account (ledger expenses)
	//   planToGo  = owned remaining + under-contract total, less what is spent
	//
	// Contracted and Recognized ride alongside because they answer the two
	// questions "committed" used to blur: what have we actually signed, and
	// what work is done but not yet paid for.
	KPIs struct {
		Committed     float64 `json:"committed"`
		Contracted    float64 `json:"contracted"` // Σ signed contract allocations
		Paid          float64 `json:"paid"`
		Recognized    float64 `json:"recognized"`
		PlanToGo      float64 `json:"planToGo"`
		ToClose       float64 `json:"toClose"` // purchase money still to fund
		OverPlan      int     `json:"overPlan"`
		LiveProjects  int     `json:"liveProjects"`
		Entities      int     `json:"entities"`
		AsOf          string  `json:"asOf,omitempty"`
		PaidTotal     float64 `json:"paidTotal"` // alias the cockpit cross-check reads
		PortfolioSize int     `json:"portfolioSize"`
		Owned         int     `json:"owned"`
		UnderContract int     `json:"underContract"`
		Pipeline      int     `json:"pipeline"`
	} `json:"kpis"`
	Entities  []oodaEntityRow `json:"entities"`
	Attention []oodaFacts     `json:"attention"`
	Owners    []oodaOwnerRow  `json:"owners"`
	Deals     []oodaDealRow   `json:"deals"`
	// Week is what lands in the next seven days across the WHOLE portfolio,
	// overdue first. The WORK tab has always had this per person; nobody could
	// see the schedule across people, which is the question a construction
	// programme actually asks.
	Week []oodaWorkItem `json:"week"`
	// Waiting is every item held up on somebody, from `[waiting:: who]`. The
	// parser has populated it since the WORK tab shipped and no surface has
	// ever shown it — it is the clearest "who is blocking whom" signal we have.
	Waiting []oodaWorkItem `json:"waiting"`
}

// buildOodaDashboard computes every tile. Money sums the SAME Project rollup
// the cockpit's SPENT figure reads, so /api/ooda/dashboard.kpis.paidTotal and
// the cockpit's Σ rollup.paid must be identical — the correctness test.
// The overrides map keeps the per-person counts and week lanes on the same
// open/closed rule as the WORK tab (see buildOodaWork).
func buildOodaDashboard(snap *oodaSnapshot, today string, overrides map[string]teamportal.Override) oodaDashboard {
	var d oodaDashboard
	props := oodaVisibleProps(snap)
	byEntity := map[string]*oodaEntityRow{}
	lastLedger := ""

	for _, p := range props {
		f := oodaPropertyFacts(p, today)
		// committed, the owner's definition: a project we already own commits
		// us to its whole budget; one still under contract commits us only to
		// what it takes to close.
		switch f.Acq {
		case "owned":
			d.KPIs.Committed += f.Plan
			d.KPIs.Owned++
		case "under-contract":
			d.KPIs.Committed += f.ToClose
			d.KPIs.ToClose += f.ToClose
			d.KPIs.UnderContract++
		default:
			d.KPIs.Pipeline++
		}
		d.KPIs.Contracted += f.Committed
		d.KPIs.Paid += f.Paid
		d.KPIs.Recognized += f.Recognized
		// plan-to-go covers deals we are actually on the hook for. A parcel
		// still being negotiated has no budget to go — we have not agreed to
		// spend anything on it.
		if f.ToGo > 0 && f.Acq != "pipeline" {
			d.KPIs.PlanToGo += f.ToGo
		}
		if f.Over {
			d.KPIs.OverPlan++
		}
		// a "live project" is one we own and are still working — a deal under
		// contract has no project running on it yet, and counting 29 of those
		// as live made the number meaningless
		if f.Acq == "owned" && !oodaSettled(f.Phase) {
			d.KPIs.LiveProjects++
		}
		if f.Attention() {
			d.Attention = append(d.Attention, f)
		}
		// entity board — owned and acquiring are NEVER summed (entities.go:44)
		key := p.Entity
		if strings.TrimSpace(key) == "" {
			key = "" // the unassigned row, surfaced not hidden
		}
		row, ok := byEntity[key]
		if !ok {
			row = &oodaEntityRow{Entity: key}
			byEntity[key] = row
		}
		switch f.Acq {
		case "owned":
			row.Owned++
		case "under-contract":
			row.Acquiring++
		default:
			row.Pipeline++
		}
		row.Committed += f.Committed
		row.Paid += f.Paid
		row.ToClose += f.ToClose
		row.OpenWork += f.Open
		for _, lr := range p.Ledger {
			if lr.Date > lastLedger {
				lastLedger = lr.Date
			}
		}
	}
	d.KPIs.PaidTotal = d.KPIs.Paid
	d.KPIs.PortfolioSize = len(props)
	d.KPIs.AsOf = lastLedger

	for _, row := range byEntity {
		d.Entities = append(d.Entities, *row)
	}
	sort.Slice(d.Entities, func(i, j int) bool {
		if (d.Entities[i].Entity == "") != (d.Entities[j].Entity == "") {
			return d.Entities[j].Entity == "" // unassigned last
		}
		return d.Entities[i].Entity < d.Entities[j].Entity
	})
	d.KPIs.Entities = len(snap.Entities)

	sort.Slice(d.Attention, func(i, j int) bool { return d.Attention[i].Short < d.Attention[j].Short })

	// deals — member count + money, membership by the property's deal link
	for _, deal := range snap.Deals {
		row := oodaDealRow{Slug: deal.Slug, Name: deal.Name, Status: deal.Status}
		if row.Name == "" {
			row.Name = deal.Slug
		}
		for _, p := range props {
			if !strings.EqualFold(p.Deal, deal.Slug) && !strings.EqualFold(p.Deal, deal.Name) {
				continue
			}
			row.Members++
			if p.Project != nil {
				row.Committed += p.Project.Committed
				row.Paid += p.Project.Paid
			}
		}
		d.Deals = append(d.Deals, row)
	}
	sort.Slice(d.Deals, func(i, j int) bool { return d.Deals[i].Name < d.Deals[j].Name })

	// open work per person, from the same source the WORK tab groups
	// buildOodaWork already resolves each owner token to a person or contractor
	// record — reuse it rather than keeping a second, thinner name map here
	// (the first version looked at people.md only, so contractors rendered as
	// raw slugs: "M-W-SERVICES").
	work := buildOodaWork(snap, today, overrides)
	for _, g := range work {
		d.Owners = append(d.Owners, oodaOwnerRow{
			Owner: g.Owner, Name: g.Name,
			Open:      len(g.Open) + len(g.Overdue) + len(g.DueThisWeek),
			Decisions: len(g.Decisions), Overdue: len(g.Overdue),
		})
		// the week is overdue + due-this-week, flattened across people. Overdue
		// belongs in "this week" (AION's rule too): something that was due
		// Monday is this week's problem, not last week's.
		d.Week = append(d.Week, g.Overdue...)
		d.Week = append(d.Week, g.DueThisWeek...)
		d.Waiting = append(d.Waiting, g.Waiting...)
	}
	sortOodaWeek(d.Week)
	sort.SliceStable(d.Waiting, func(i, j int) bool { return d.Waiting[i].Title < d.Waiting[j].Title })
	// never nil: the client does .length on these
	if d.Week == nil {
		d.Week = []oodaWorkItem{}
	}
	if d.Waiting == nil {
		d.Waiting = []oodaWorkItem{}
	}
	return d
}

// sortOodaWeek puts the soonest first and everything UNDATED last. An item
// with no due date cannot be ranked against one that has one, and sorting ""
// naturally would float all of them to the top of the week — crowding out the
// dated work the section exists to show.
func sortOodaWeek(items []oodaWorkItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Due, items[j].Due
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
}

// oodaToday is the local calendar date the late/overdue rules compare against.
func oodaToday() string { return time.Now().Format("2006-01-02") }
