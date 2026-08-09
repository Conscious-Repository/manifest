package aion

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"manifest/record"
)

// The export contract (aion-domain spec §6 / aionbio portal spec §2): the
// exact files manifest materializes into the aionbio checkout. This is the
// ONE deliberate place derived values may be stored (ARCHITECTURE §3
// export-contract exception — runway_months). Byte determinism is load-
// bearing: publish dirtiness is computed by byte-comparing these renders
// against the checkout, so structs/slices only, MarshalIndent, trailing \n.

// ContractPaths are the nine checkout-relative paths the effector may
// write — and the ONLY paths it may ever touch (canary-tested).
func ContractPaths() []string {
	return []string{
		"public/portal/content/hiring.md",
		"public/portal/content/references.md",
		"public/portal/data/finances.json",
		"public/portal/data/vto.json",
		"public/portal/data/goals.json",
		"public/portal/data/backlog.json",
		"public/portal/data/heuristics.json",
		"public/portal/data/people.json",
		"public/portal/data/meta.json",
	}
}

// ExportGoal is the neutral ladder node the server derives from the goals
// package's `## Aion` area (this package never imports goals).
type ExportGoal struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Horizon string  `json:"horizon"` // 1yr | rock | 30
	Status  string  `json:"status"`  // open | in_progress | done
	Serves  *string `json:"serves"`  // primary 1-year link (back-compat; first of ServesAll)
	// ServesAll is the FULL 1:many chain — a rock may feed several 1-year
	// goals (Series A feeds all of them). Portal-side grouping should
	// prefer this and fall back to serves.
	ServesAll []string `json:"serves_all,omitempty"`
	// Closed is the archive date on a historic (status:done) rock — the
	// portal renders these in the past cone, not the active 90-day band.
	Closed string `json:"closed,omitempty"`
	// Aliases are portal-matcher vocabulary that resolves to this goal
	// (aion.bio: rock → id → slug → alias). Absent when the goal has none.
	Aliases  []string `json:"aliases,omitempty"`
	Owner    *string  `json:"owner"`
	Quarter  string   `json:"quarter,omitempty"`
	Children []string `json:"children"`
}

// ExportInput is everything RenderContract needs, already loaded.
type ExportInput struct {
	People       *PeopleDoc
	VTO          *VTODoc
	Backlog      *BacklogDoc
	Heuristics   *HeuristicsDoc
	Finances     *FinancesDoc
	HiringMD     []byte // verbatim
	ReferencesMD []byte // verbatim
	Goals        []ExportGoal
	PublishedAt  string // RFC3339
}

// RenderContract materializes the nine contract files, keyed by
// checkout-relative path. It never renders transcript quotes, evidence
// lines, or the finances body (leak-canary tested).
func RenderContract(in ExportInput) (map[string][]byte, error) {
	out := map[string][]byte{}
	out["public/portal/content/hiring.md"] = append([]byte(nil), in.HiringMD...)
	out["public/portal/content/references.md"] = append([]byte(nil), in.ReferencesMD...)

	files := []struct {
		path string
		v    any
	}{
		{"public/portal/data/finances.json", exportFinances(in.Finances)},
		{"public/portal/data/vto.json", exportVTO(in.VTO)},
		{"public/portal/data/goals.json", map[string]any{"goals": in.Goals}},
		{"public/portal/data/backlog.json", exportBacklog(in.Backlog)},
		{"public/portal/data/heuristics.json", exportHeuristics(in.Heuristics)},
		{"public/portal/data/people.json", exportPeople(in.People)},
		{"public/portal/data/meta.json", exportMeta(in)},
	}
	for _, f := range files {
		b, err := json.MarshalIndent(f.v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", f.path, err)
		}
		out[f.path] = append(b, '\n')
	}
	if err := verifyChains(in); err != nil {
		return nil, err
	}
	return out, nil
}

// verifyChains is the contract's integrity gate: every task rock and goal
// serves must resolve inside the exported graph.
func verifyChains(in ExportInput) error {
	ids := map[string]bool{}
	oneYear := map[string]bool{}
	for _, g := range in.Goals {
		ids[g.ID] = true
		if g.Horizon == "1yr" {
			oneYear[g.ID] = true
		}
	}
	for _, g := range in.Goals {
		if g.Serves != nil && !oneYear[*g.Serves] {
			return fmt.Errorf("chain integrity: %s serves unknown 1-year goal %q", g.ID, *g.Serves)
		}
		for _, sv := range g.ServesAll {
			if !oneYear[sv] {
				return fmt.Errorf("chain integrity: %s serves unknown 1-year goal %q", g.ID, sv)
			}
		}
	}
	// task rocks are exported verbatim, NOT chain-checked: the owner's
	// corpus tags rocks with free-text slugs; the portal groups a task under
	// a rock only when the id resolves and files the rest under `unanchored`
	// (aionbio spec rule 3 — never invent links, never block on them).
	_ = ids
	return nil
}

type exportFinancesT struct {
	Capital      *float64 `json:"capital"`
	MonthlyBurn  *float64 `json:"monthly_burn"`
	RunwayMonths *float64 `json:"runway_months"`
	AsOf         string   `json:"as_of"`
	Currency     string   `json:"currency"`
	Source       string   `json:"source"`
	Note         string   `json:"note"`
}

// ParseMoney reads a human money value the way the owner writes it —
// "1.95M", "85k", "$2,480,000", "2480000" — into dollars. The vault keeps
// his notation verbatim (surface, don't rewrite); parsing is liberal at
// the point of use. nil when the value is empty or not money-shaped.
func ParseMoney(v string) *float64 {
	v = strings.TrimSpace(v)
	v = strings.NewReplacer("$", "", ",", "", " ", "", "_", "").Replace(v)
	if v == "" {
		return nil
	}
	mult := 1.0
	switch v[len(v)-1] {
	case 'k', 'K':
		mult, v = 1e3, v[:len(v)-1]
	case 'm', 'M':
		mult, v = 1e6, v[:len(v)-1]
	case 'b', 'B':
		mult, v = 1e9, v[:len(v)-1]
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	f *= mult
	return &f
}

func exportFinances(d *FinancesDoc) exportFinancesT {
	num := func(key string) *float64 { return ParseMoney(d.Get(key)) }
	out := exportFinancesT{
		Capital: num("capital"), MonthlyBurn: num("monthly_burn"),
		AsOf: d.Get("as_of"), Currency: d.Get("currency"),
		Source: d.Get("source"), Note: d.Get("note"),
	}
	// The §3 exception: runway is materialized INTO the contract because the
	// portal reads it; internally it stays computed.
	if out.Capital != nil && out.MonthlyBurn != nil && *out.MonthlyBurn > 0 {
		r := math.Round(*out.Capital / *out.MonthlyBurn * 10) / 10
		out.RunwayMonths = &r
	}
	return out
}

type exportVTOT struct {
	CoreValues        []string          `json:"core_values"`
	CoreFocus         map[string]string `json:"core_focus"`
	TenYearTarget     string            `json:"ten_year_target"`
	MarketingStrategy exportMarketingT  `json:"marketing_strategy"`
	ThreeYearPicture  exportPictureT    `json:"three_year_picture"`
	OneYearPlan       exportPlanT       `json:"one_year_plan"`
	Quarter           map[string]string `json:"quarter"`
}

type exportMarketingT struct {
	Target  string   `json:"target"`
	Uniques []string `json:"uniques"`
}

type exportPictureT struct {
	Date        string   `json:"date"`
	Measurables []string `json:"measurables"`
}

type exportPlanT struct {
	Date        string   `json:"date"`
	Goals       []string `json:"goals"`
	Measurables []string `json:"measurables"`
}

func exportVTO(d *VTODoc) exportVTOT {
	nonNil := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	s01, s02, s03 := d.Section("01"), d.Section("02"), d.Section("03")
	s04, s05, s06, s07 := d.Section("04"), d.Section("05"), d.Section("06"), d.Section("07")
	ten := ""
	if s03 != nil {
		if ts := s03.EntryTexts(); len(ts) > 0 {
			ten = ts[0]
		}
	}
	return exportVTOT{
		CoreValues: nonNil(s01.EntryTexts()),
		CoreFocus: map[string]string{
			"purpose": s02.FieldValue("purpose"),
			"niche":   s02.FieldValue("niche"),
		},
		TenYearTarget: ten,
		MarketingStrategy: exportMarketingT{
			Target:  s04.FieldValue("target"),
			Uniques: nonNil(s04.EntryTexts()),
		},
		ThreeYearPicture: exportPictureT{
			Date:        s05.FieldValue("date"),
			Measurables: nonNil(s05.EntryTexts()),
		},
		OneYearPlan: exportPlanT{
			Date:        s06.FieldValue("date"),
			Goals:       nonNil(s06.FieldValues("goal")),
			Measurables: nonNil(s06.EntryTexts()),
		},
		Quarter: map[string]string{
			"start": s07.FieldValue("start"),
			"end":   s07.FieldValue("end"),
		},
	}
}

type exportItemT struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	Owner    *string `json:"owner"`
	Source   string  `json:"source"`
	Captured string  `json:"captured"`
	Rock     *string `json:"rock"`
	Due      *string `json:"due"`
	Status   *string `json:"status"`
	DoneOn   *string `json:"done_on"`
	NeededBy *string `json:"needed_by"`
	Decided  *string `json:"decided"`
	Outcome  *string `json:"outcome"`
}

func exportBacklog(d *BacklogDoc) map[string]any {
	items := []exportItemT{}
	for _, it := range d.Items() {
		src := ""
		if len(it.Sources) > 0 {
			src = it.Sources[0]
		}
		items = append(items, exportItemT{
			// title-slug ids (the shape already consumed portal-side)
			ID: "aion-bl/" + record.Slug(it.Text, 60), Kind: it.Kind, Title: it.Text,
			Owner: nullable(it.Owner), Source: src, Captured: it.Captured,
			Rock: nullable(it.Rock), Due: nullable(it.Due),
			Status: nullable(it.Status), DoneOn: nullable(it.DoneOn),
			NeededBy: nullable(it.NeededBy), Decided: nullable(it.Decided),
			Outcome: nullable(it.Outcome),
		})
	}
	return map[string]any{"items": items}
}

type exportHeuristicT struct {
	ID             string             `json:"id"`
	Statement      string             `json:"statement"`
	First          string             `json:"first"`
	Reinforcements []exportReinforceT `json:"reinforcements"`
}

type exportReinforceT struct {
	Source string  `json:"source"`
	Date   *string `json:"date"` // null when the reinforcement is undated
}

// exportHeuristics preserves owner ordering and excludes retired entries.
func exportHeuristics(d *HeuristicsDoc) map[string]any {
	hs := []exportHeuristicT{}
	for _, h := range d.LiveEntries() {
		re := []exportReinforceT{}
		for _, r := range h.Sources {
			re = append(re, exportReinforceT{Source: r.Note, Date: nullable(r.Date)})
		}
		hs = append(hs, exportHeuristicT{
			ID: "aion-h/" + record.Slug(h.Text, 60), Statement: h.Text, First: h.First, Reinforcements: re,
		})
	}
	return map[string]any{"heuristics": hs}
}

type exportPersonT struct {
	Initials string `json:"initials"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

func exportPeople(d *PeopleDoc) map[string]any {
	ps := []exportPersonT{}
	for _, p := range d.People() {
		ps = append(ps, exportPersonT{Initials: p.Initials, Name: p.Name, Role: p.Role})
	}
	return map[string]any{"people": ps}
}

type exportSectionT struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Count int    `json:"count"`
}

func exportMeta(in ExportInput) map[string]any {
	sections := []exportSectionT{
		{"finances", "data/finances.json", 1},
		{"vto", "data/vto.json", 1},
		{"goals", "data/goals.json", len(in.Goals)},
		{"backlog", "data/backlog.json", len(in.Backlog.Items())},
		{"heuristics", "data/heuristics.json", len(in.Heuristics.LiveEntries())},
		{"people", "data/people.json", len(in.People.People())},
		{"hiring", "content/hiring.md", 1},
		{"references", "content/references.md", 1},
	}
	return map[string]any{
		"sections":     sections,
		"published_at": in.PublishedAt,
		"source":       "manifest",
	}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
