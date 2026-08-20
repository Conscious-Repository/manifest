package server

import (
	"sort"
	"strings"
	"time"

	"manifest/realestate"
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
	Slug      string  `json:"slug"`
	Address   string  `json:"address"`
	Short     string  `json:"short"`
	Entity    string  `json:"entity"`
	Status    string  `json:"status"`
	Phase     string  `json:"phase"`
	Deal      string  `json:"deal,omitempty"`
	Rock      string  `json:"rock,omitempty"`
	DoneBy    string  `json:"doneBy,omitempty"`
	Open      int     `json:"open"`
	PctDone   float64 `json:"pctDone"`
	HasStages bool    `json:"hasStages"`
	Plan      float64 `json:"plan"`
	Paid      float64 `json:"paid"`
	Committed float64 `json:"committed"`
	ToGo      float64 `json:"toGo"`
	Over      bool    `json:"over"`
	Late      bool    `json:"late"`
	Stalled   bool    `json:"stalled"`
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
	if p.Project != nil {
		f.Plan, f.Paid, f.Committed = p.Project.PlanTotal, p.Project.Paid, p.Project.Committed
		f.ToGo = f.Plan - f.Paid
	}
	f.Over = f.Plan > 0 && f.Paid > f.Plan
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

type oodaEntityRow struct {
	Entity    string  `json:"entity"`
	Owned     int     `json:"owned"`
	Acquiring int     `json:"acquiring"`
	Committed float64 `json:"committed"`
	Paid      float64 `json:"paid"`
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
	KPIs struct {
		Committed     float64 `json:"committed"`
		Paid          float64 `json:"paid"`
		PlanToGo      float64 `json:"planToGo"`
		OverPlan      int     `json:"overPlan"`
		LiveProjects  int     `json:"liveProjects"`
		Entities      int     `json:"entities"`
		AsOf          string  `json:"asOf,omitempty"`
		PaidTotal     float64 `json:"paidTotal"` // alias the cockpit cross-check reads
		PortfolioSize int     `json:"portfolioSize"`
	} `json:"kpis"`
	Entities  []oodaEntityRow `json:"entities"`
	Attention []oodaFacts     `json:"attention"`
	Owners    []oodaOwnerRow  `json:"owners"`
	Deals     []oodaDealRow   `json:"deals"`
}

// buildOodaDashboard computes every tile. Money sums the SAME Project rollup
// the cockpit's SPENT figure reads, so /api/ooda/dashboard.kpis.paidTotal and
// the cockpit's Σ rollup.paid must be identical — the correctness test.
func buildOodaDashboard(snap *oodaSnapshot, today string) oodaDashboard {
	var d oodaDashboard
	props := oodaVisibleProps(snap)
	byEntity := map[string]*oodaEntityRow{}
	lastLedger := ""

	for _, p := range props {
		f := oodaPropertyFacts(p, today)
		d.KPIs.Committed += f.Committed
		d.KPIs.Paid += f.Paid
		if f.ToGo > 0 {
			d.KPIs.PlanToGo += f.ToGo
		}
		if f.Over {
			d.KPIs.OverPlan++
		}
		if !oodaSettled(f.Phase) {
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
		if p.From != "" {
			row.Acquiring++
		} else {
			row.Owned++
		}
		row.Committed += f.Committed
		row.Paid += f.Paid
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
	work := buildOodaWork(snap, today)
	names := map[string]string{}
	for _, p := range snap.People {
		if p != nil {
			names[strings.ToUpper(p.Initials)] = p.Name
		}
	}
	for _, g := range work {
		d.Owners = append(d.Owners, oodaOwnerRow{
			Owner: g.Owner, Name: names[strings.ToUpper(g.Owner)],
			Open:      len(g.Open) + len(g.Overdue) + len(g.DueThisWeek),
			Decisions: len(g.Decisions), Overdue: len(g.Overdue),
		})
	}
	return d
}

// oodaToday is the local calendar date the late/overdue rules compare against.
func oodaToday() string { return time.Now().Format("2006-01-02") }
