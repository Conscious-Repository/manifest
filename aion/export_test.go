package aion

import (
	"encoding/json"
	"strings"
	"testing"
)

// exportFixture builds a fully-populated ExportInput with LEAK CANARIES
// planted in every private surface: the finances body, a proposal-style
// quote, evidence-style lines. None of these strings may appear in any
// rendered contract byte.
const (
	canaryFinances = "CANARY-private-finances-body"
	canaryQuote    = "CANARY-transcript-quote"
)

func exportFixture() ExportInput {
	serves := "aion/human-prototype-mri"
	owner := "RT"
	return ExportInput{
		People: ParsePeople(SeedFiles["people.md"]),
		VTO: ParseVTO(`## 01 core values
- Morale is the most valuable resource we have

## 02 core focus
- [purpose:: control biology with fields]
- [niche:: field-based longevity]

## 03 10-year target
- A medbed in every home

## 04 marketing strategy
- [target:: longevity clinics]
- unique one
- unique two

## 05 3-year picture
- [date:: 2029-08-01]
- 100 installed units

## 06 1-year plan
- [date:: 2027-08-01] [goal:: aion/human-prototype-mri]
- first human image

## 07 quarter
- [start:: 2026-07-01] [end:: 2026-09-30]

## 08 issues
issues live in the backlog
`),
		Backlog: ParseBacklog(`## Tasks
- [ ] Secure the venue [kind:: task] [rock:: aion/human-prototype-mri-rock] [status:: open] [owner:: JR] [source:: [[2026-07-31 jack ruhl sync]]] [captured:: 2026-07-31]
- [x] Hire Morgan [kind:: task] [status:: done] [done_on:: 2026-07-06] [owner:: BA/MM] [source:: [[2026-07-06 aion team sync]]] [captured:: 2026-07-06]

## Decisions
- Outsource pig work [kind:: decision] [status:: decided] [decided:: 2026-07-27] [outcome:: use a CRO] [owner:: BA/HZ] [source:: [[2026-07-27 derya ii]]] [captured:: 2026-07-27]
`),
		Heuristics: ParseHeuristics(`- Take the longer path [first:: 2025-11-19]
    - [[aion biosciences]] [date:: 2026-07-02]
    - [[2026-07-27 derya ii]] [date:: 2026-07-27]

## retired
- A pruned idea [first:: 2025-01-01]
    - [[old note]] [date:: 2025-01-01]
`),
		Finances: ParseFinances(`---
capital: 1500000
monthly_burn: 95000
as_of: 2026-08-01
currency: USD
source: manual
note: seed round
---

` + canaryFinances + `
`),
		HiringMD:     []byte("# AION — hiring\n- [role:: lab engineer] [stage:: sourcing]\n"),
		ReferencesMD: []byte("# AION — references\n- primer [url:: https://example.com] [source:: arXiv] [date:: 2026-05-01]\n"),
		Goals: []ExportGoal{
			{ID: "aion/human-prototype-mri", Title: "Human prototype MRI", Horizon: "1yr",
				Status: "open", Children: []string{"aion/human-prototype-mri-rock"}},
			{ID: "aion/human-prototype-mri-rock", Title: "Human-scale spec + team hired", Horizon: "rock",
				Status: "open", Serves: &serves, Owner: &owner, Quarter: "2026-Q3",
				Children: []string{"aion/human-prototype-mri-rock/spec"}},
			{ID: "aion/human-prototype-mri-rock/spec", Title: "Prototype spec", Horizon: "30",
				Status: "open", Children: []string{}},
		},
		PublishedAt: "2026-08-07T00:00:00Z",
	}
}

func TestRenderContractShapes(t *testing.T) {
	out, err := RenderContract(exportFixture())
	if err != nil {
		t.Fatal(err)
	}
	// exactly the nine contract paths, nothing else
	if len(out) != len(ContractPaths()) {
		t.Fatalf("rendered %d files, want %d", len(out), len(ContractPaths()))
	}
	for _, p := range ContractPaths() {
		if _, ok := out[p]; !ok {
			t.Fatalf("missing contract file %s", p)
		}
	}
	// every json file is valid json ending in one newline
	for p, b := range out {
		if !strings.HasSuffix(p, ".json") {
			continue
		}
		if !json.Valid(b) {
			t.Fatalf("%s: invalid json", p)
		}
		if !strings.HasSuffix(string(b), "\n") || strings.HasSuffix(string(b), "\n\n") {
			t.Fatalf("%s: must end with exactly one newline", p)
		}
	}
	// determinism: byte-identical on re-render (the dirty-dot foundation)
	out2, _ := RenderContract(exportFixture())
	for p := range out {
		if string(out[p]) != string(out2[p]) {
			t.Fatalf("%s: non-deterministic render", p)
		}
	}

	// finances: runway materialized (§3 export exception), 1 decimal
	var fin struct {
		Capital      *float64 `json:"capital"`
		MonthlyBurn  *float64 `json:"monthly_burn"`
		RunwayMonths *float64 `json:"runway_months"`
		Source       string   `json:"source"`
	}
	if err := json.Unmarshal(out["public/portal/data/finances.json"], &fin); err != nil {
		t.Fatal(err)
	}
	if fin.RunwayMonths == nil || *fin.RunwayMonths != 15.8 {
		t.Fatalf("runway_months: %v", fin.RunwayMonths)
	}
	if fin.Source != "manual" {
		t.Fatalf("source: %q", fin.Source)
	}

	// vto: shapes per the aionbio contract
	var vto struct {
		CoreValues    []string          `json:"core_values"`
		CoreFocus     map[string]string `json:"core_focus"`
		TenYearTarget string            `json:"ten_year_target"`
		OneYearPlan   struct {
			Goals []string `json:"goals"`
		} `json:"one_year_plan"`
		Quarter map[string]string `json:"quarter"`
	}
	if err := json.Unmarshal(out["public/portal/data/vto.json"], &vto); err != nil {
		t.Fatal(err)
	}
	if len(vto.CoreValues) != 1 || vto.TenYearTarget != "A medbed in every home" ||
		vto.CoreFocus["purpose"] != "control biology with fields" ||
		len(vto.OneYearPlan.Goals) != 1 || vto.OneYearPlan.Goals[0] != "aion/human-prototype-mri" ||
		vto.Quarter["start"] != "2026-07-01" {
		t.Fatalf("vto: %+v", vto)
	}

	// backlog: ids prefixed, nulls for empty
	blob := string(out["public/portal/data/backlog.json"])
	if !strings.Contains(blob, `"id": "aion-bl/`) || !strings.Contains(blob, `"rock": null`) ||
		!strings.Contains(blob, `"done_on": "2026-07-06"`) ||
		!strings.Contains(blob, `"outcome": "use a CRO"`) {
		t.Fatalf("backlog.json:\n%s", blob)
	}

	// heuristics: retired excluded, order preserved, reinforcements present
	hblob := string(out["public/portal/data/heuristics.json"])
	if strings.Contains(hblob, "pruned idea") {
		t.Fatal("retired heuristic exported")
	}
	if !strings.Contains(hblob, `"id": "aion-h/`) || strings.Count(hblob, `"source"`) != 2 {
		t.Fatalf("heuristics.json:\n%s", hblob)
	}

	// hiring/references verbatim
	if string(out["public/portal/content/hiring.md"]) != "# AION — hiring\n- [role:: lab engineer] [stage:: sourcing]\n" {
		t.Fatal("hiring.md not verbatim")
	}

	// meta: sections + timestamp + source
	mblob := string(out["public/portal/data/meta.json"])
	if !strings.Contains(mblob, `"published_at": "2026-08-07T00:00:00Z"`) ||
		!strings.Contains(mblob, `"source": "manifest"`) {
		t.Fatalf("meta.json:\n%s", mblob)
	}
}

func TestParseMoneyShorthand(t *testing.T) {
	cases := map[string]float64{
		"1.95M": 1950000, "85k": 85000, "$2,480,000": 2480000,
		"2480000": 2480000, "1.2B": 1.2e9, " 95 K ": 95000,
	}
	for in, want := range cases {
		got := ParseMoney(in)
		if got == nil || *got != want {
			t.Errorf("ParseMoney(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "a lot", "M", "August 7, 2026"} {
		if got := ParseMoney(bad); got != nil {
			t.Errorf("ParseMoney(%q) = %v, want nil", bad, *got)
		}
	}
	// the runway math through the shorthand path: 1.95M / 85k = 22.9
	fin := ParseFinances("---\ncapital: 1.95M\nmonthly_burn: 85k\n---\n")
	out := exportFinances(fin)
	if out.RunwayMonths == nil || *out.RunwayMonths != 22.9 {
		t.Fatalf("runway from shorthand: %v", out.RunwayMonths)
	}
}

func TestRenderContractLeakCanary(t *testing.T) {
	out, err := RenderContract(exportFixture())
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range out {
		for _, canary := range []string{canaryFinances, canaryQuote} {
			if strings.Contains(string(b), canary) {
				t.Fatalf("%s leaked %q", p, canary)
			}
		}
	}
}

func TestRenderContractChainIntegrity(t *testing.T) {
	// a task rock is exported VERBATIM even when unresolvable — the owner's
	// corpus tags free-text rocks; the portal groups only what resolves
	// (aionbio spec rule 3: never invent links, never block on them)
	in := exportFixture()
	in.Backlog = ParseBacklog("## Tasks\n- [ ] orphan [kind:: task] [rock:: free-text-rock] [captured:: 2026-08-07]\n")
	out, err := RenderContract(in)
	if err != nil {
		t.Fatalf("free-text rock blocked the render: %v", err)
	}
	if !strings.Contains(string(out["public/portal/data/backlog.json"]), `"rock": "free-text-rock"`) {
		t.Fatal("rock not exported verbatim")
	}
	// a rock serving an unknown 1yr goal refuses (goals-internal chain —
	// that graph is ours and must stay sound)
	in2 := exportFixture()
	bad := "aion/ghost"
	in2.Goals[1].Serves = &bad
	if _, err := RenderContract(in2); err == nil || !strings.Contains(err.Error(), "unknown 1-year goal") {
		t.Fatalf("chain integrity (serves): %v", err)
	}
}

func TestRenderContractEmptyVault(t *testing.T) {
	// absent corpora (empty strings) must render valid, empty-shaped files —
	// no 500s before the first seed
	in := ExportInput{
		People: ParsePeople(""), VTO: ParseVTO(""), Backlog: ParseBacklog(""),
		Heuristics: ParseHeuristics(""), Finances: ParseFinances(""),
		Goals: []ExportGoal{}, PublishedAt: "2026-08-07T00:00:00Z",
	}
	out, err := RenderContract(in)
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range out {
		if strings.HasSuffix(p, ".json") && !json.Valid(b) {
			t.Fatalf("%s invalid on empty vault", p)
		}
	}
	// empty collections are [] not null (the portal iterates them)
	for _, p := range []string{"backlog", "heuristics", "people", "goals"} {
		blob := string(out["public/portal/data/"+p+".json"])
		if strings.Contains(blob, "null,") || strings.HasPrefix(blob, "null") {
			t.Fatalf("%s.json has null collection:\n%s", p, blob)
		}
		if !strings.Contains(blob, "[]") {
			t.Fatalf("%s.json missing empty array:\n%s", p, blob)
		}
	}
}
