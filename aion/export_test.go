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
	// The private recruiting domain (system/aion/recruiting/**) shares this
	// package's capability PATTERN but nothing else: `aion` takes no
	// recruiting input, and a candidate name reaching a contract file would be
	// public at portal.aion.bio before any sign-in gate. The guard lives in
	// both packages — server/aion_recruiting_leak_test.go asserts it from the
	// wiring side, this asserts it from the render side.
	canaryRecruiting = "CANARY-recruiting-candidate-name"
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
` + canaryRecruiting + `
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

// The portal export omits a backlog item sourced from a HELD transcript
// entirely (its title is the disclosure), counts the omission, and leaves
// every other item — open/internal/unmapped transcript, non-transcript
// path, no source — exactly as before. Tier resolution is the data file,
// never a heuristic; the vault-side doc is untouched.
func TestExportBacklogSuppressesHeldSourced(t *testing.T) {
	const (
		heldNote   = "2026-06-01 heye immigration sync"
		heldNote2  = "2026-05-05 artemy sync"
		heldTitle  = "Compile the B1/O1 visa evidence packet"
		heldTitle2 = "Share the password-protected investor portal asset"
		heldTitle3 = "Review the corrected recommendation letters"
	)
	tm := TierMap{
		heldNote + ".md":                {Tier: TierHeld, Reason: "immigration"},
		heldNote2 + ".md":               {Tier: TierHeld, Reason: "personal"},
		"2026-01-19 aion team sync.md":  {Tier: TierOpen, Reason: "team sync"},
		"2025-12-15 mechanisms sync.md": {Tier: TierInternal, Reason: "internal ops"},
	}
	backlog := `- [ ] ` + heldTitle + ` [kind:: task] [status:: open] [source:: [[log/` + heldNote + `]]] [captured:: 2026-06-01]
- [ ] ` + heldTitle2 + ` [kind:: task] [status:: open] [source:: [[log/` + heldNote2 + `.md]]] [captured:: 2026-05-05]
- [ ] ` + heldTitle3 + ` [kind:: task] [status:: open] [source:: [[log/2026-01-19 aion team sync]]] [source:: [[log/` + heldNote + `]]] [captured:: 2026-06-01]
- [ ] Open-sourced item stays [kind:: task] [status:: open] [source:: [[log/2026-01-19 aion team sync]]] [captured:: 2026-01-19]
- [ ] Internal-sourced item stays [kind:: task] [status:: open] [source:: [[log/2025-12-15 mechanisms sync]]] [captured:: 2025-12-15]
- [ ] Unmapped-transcript item stays [kind:: task] [status:: open] [source:: [[log/2026-09-20 rj sync]]] [captured:: 2026-09-20]
- [ ] Non-transcript item stays [kind:: task] [status:: open] [source:: [[updates for justin]]] [captured:: 2026-08-01]
- [ ] Intrinsic item stays [kind:: task] [status:: open] [source:: [[intrinsic/2026-08-01 lab notes]]] [captured:: 2026-08-01]
- [ ] Sourceless item stays [kind:: task] [status:: open] [captured:: 2026-08-01]
- Held decision goes too [kind:: decision] [status:: decided] [decided:: 2026-05-05] [outcome:: no] [source:: [[log/` + heldNote2 + `]]] [captured:: 2026-05-05]
`
	in := exportFixture()
	in.Backlog = ParseBacklog(backlog)
	in.Tiers = tm
	if got := len(in.Backlog.Items()); got != 10 {
		t.Fatalf("fixture parsed %d items, want 10", got)
	}
	out, err := RenderContract(in)
	if err != nil {
		t.Fatal(err)
	}
	b := out["server/web/portal/data/backlog.json"]
	for _, leak := range []string{heldNote, heldNote2, heldTitle, heldTitle2, heldTitle3, "visa", "investor portal"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("backlog.json leaked held-sourced %q:\n%s", leak, b)
		}
	}
	var doc struct {
		Items          []struct{ ID, Title, Source string }
		SuppressedHeld int `json:"suppressed_held"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SuppressedHeld != 4 {
		t.Fatalf("suppressed_held = %d, want 4", doc.SuppressedHeld)
	}
	want := []string{
		"Open-sourced item stays", "Internal-sourced item stays", "Unmapped-transcript item stays",
		"Non-transcript item stays", "Intrinsic item stays", "Sourceless item stays",
	}
	if len(doc.Items) != len(want) {
		t.Fatalf("exported %d items, want %d: %+v", len(doc.Items), len(want), doc.Items)
	}
	for i, w := range want {
		if doc.Items[i].Title != w {
			t.Fatalf("item %d = %q, want %q", i, doc.Items[i].Title, w)
		}
	}
	// the intrinsic/ path is NOT a transcript reference: it rides through verbatim
	if doc.Items[4].Source != "intrinsic/2026-08-01 lab notes" {
		t.Fatalf("intrinsic source rewritten: %q", doc.Items[4].Source)
	}
	// meta's backlog census is the EXPORTED count, not the vault's
	var meta struct {
		Sections []struct {
			Name  string
			Count int
		}
	}
	if err := json.Unmarshal(out["server/web/portal/data/meta.json"], &meta); err != nil {
		t.Fatal(err)
	}
	for _, sec := range meta.Sections {
		if sec.Name == "backlog" && sec.Count != len(want) {
			t.Fatalf("meta backlog count = %d, want %d", sec.Count, len(want))
		}
	}
	// the vault-side doc still holds all ten: read-side filter only
	if got := len(in.Backlog.Items()); got != 10 {
		t.Fatalf("filter mutated the backlog doc: %d items", got)
	}
	// determinism: byte-identical on re-render
	out2, _ := RenderContract(in)
	for p := range out {
		if string(out[p]) != string(out2[p]) {
			t.Fatalf("%s: non-deterministic", p)
		}
	}
	// nil Tiers → the embedded data file: the owner's real held note is gated
	in.Tiers = nil
	out3, err := RenderContract(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out3["server/web/portal/data/backlog.json"]), heldTitle) {
		t.Fatal("embedded tier map did not gate the held-sourced item")
	}
	// the contract's acceptance gate still reads the filtered document
	if errs, _ := AcceptContract(out, nil); len(errs) != 0 {
		t.Fatalf("acceptance errors: %v", errs)
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
	if err := json.Unmarshal(out["server/web/portal/data/finances.json"], &fin); err != nil {
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
	if err := json.Unmarshal(out["server/web/portal/data/vto.json"], &vto); err != nil {
		t.Fatal(err)
	}
	if len(vto.CoreValues) != 1 || vto.TenYearTarget != "A medbed in every home" ||
		vto.CoreFocus["purpose"] != "control biology with fields" ||
		len(vto.OneYearPlan.Goals) != 1 || vto.OneYearPlan.Goals[0] != "aion/human-prototype-mri" ||
		vto.Quarter["start"] != "2026-07-01" {
		t.Fatalf("vto: %+v", vto)
	}

	// backlog: ids prefixed, nulls for empty
	blob := string(out["server/web/portal/data/backlog.json"])
	if !strings.Contains(blob, `"id": "aion-bl/`) || !strings.Contains(blob, `"rock": null`) ||
		!strings.Contains(blob, `"done_on": "2026-07-06"`) ||
		!strings.Contains(blob, `"outcome": "use a CRO"`) {
		t.Fatalf("backlog.json:\n%s", blob)
	}

	// heuristics: retired excluded, order preserved, reinforcements present
	hblob := string(out["server/web/portal/data/heuristics.json"])
	if strings.Contains(hblob, "pruned idea") {
		t.Fatal("retired heuristic exported")
	}
	if !strings.Contains(hblob, `"id": "aion-h/`) || strings.Count(hblob, `"source"`) != 2 {
		t.Fatalf("heuristics.json:\n%s", hblob)
	}

	// hiring/references verbatim
	if string(out["server/web/portal/content/hiring.md"]) != "# AION — hiring\n- [role:: lab engineer] [stage:: sourcing]\n" {
		t.Fatal("hiring.md not verbatim")
	}

	// meta: sections + timestamp + source
	mblob := string(out["server/web/portal/data/meta.json"])
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
		for _, canary := range []string{canaryFinances, canaryQuote, canaryRecruiting} {
			if strings.Contains(string(b), canary) {
				t.Fatalf("%s leaked %q", p, canary)
			}
		}
		// and no contract file may carry the private recruiting vocabulary at
		// all — an id prefix or a record path here means a private surface
		// found its way into a published one.
		for _, token := range []string{"aion/recruiting/", "cand/", "aion-net/"} {
			if strings.Contains(string(b), token) {
				t.Fatalf("%s carries the private recruiting token %q", p, token)
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
	if !strings.Contains(string(out["server/web/portal/data/backlog.json"]), `"rock": "free-text-rock"`) {
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
		blob := string(out["server/web/portal/data/"+p+".json"])
		if strings.Contains(blob, "null,") || strings.HasPrefix(blob, "null") {
			t.Fatalf("%s.json has null collection:\n%s", p, blob)
		}
		if !strings.Contains(blob, "[]") {
			t.Fatalf("%s.json missing empty array:\n%s", p, blob)
		}
	}
}

func TestExportArchivedRockShape(t *testing.T) {
	// a historic (closed) rock enters goals.json as a status:done rock with a
	// closed date; serves resolves; live goals are unaffected.
	in := exportFixture()
	serves := "aion/human-prototype-mri"
	in.Goals = append(in.Goals, ExportGoal{
		ID: "aion/ultrasound-platform", Title: "Ultrasound platform", Horizon: "rock",
		Status: "done", Serves: &serves, ServesAll: []string{serves},
		Closed: "2026-06-30", Children: []string{},
	})
	out, err := RenderContract(in)
	if err != nil {
		t.Fatal(err)
	}
	blob := string(out["server/web/portal/data/goals.json"])
	if !strings.Contains(blob, `"id": "aion/ultrasound-platform"`) ||
		!strings.Contains(blob, `"status": "done"`) ||
		!strings.Contains(blob, `"closed": "2026-06-30"`) {
		t.Fatalf("archived rock shape:\n%s", blob)
	}
	// an unrelated live goal did NOT gain a closed field
	if strings.Contains(blob, `"closed": ""`) {
		t.Fatal("empty closed leaked (omitempty broken)")
	}
}

func TestExportGoalDatesAndContract(t *testing.T) {
	in := exportFixture()
	// give the rock explicit start/due; the annual stays dateless
	for i := range in.Goals {
		if in.Goals[i].Horizon == "rock" {
			in.Goals[i].Start = "2026-07-01"
			in.Goals[i].Due = "2026-09-30"
		}
	}
	out, err := RenderContract(in)
	if err != nil {
		t.Fatal(err)
	}
	goalsBlob := string(out["server/web/portal/data/goals.json"])
	if !strings.Contains(goalsBlob, `"start": "2026-07-01"`) || !strings.Contains(goalsBlob, `"due": "2026-09-30"`) {
		t.Fatalf("rock start/due not exported:\n%s", goalsBlob)
	}
	// omitempty: a dateless goal emits no start/due keys at all
	if strings.Contains(goalsBlob, `"start": ""`) || strings.Contains(goalsBlob, `"due": ""`) {
		t.Fatal("empty start/due leaked (omitempty broken)")
	}
	// contract stamp in meta.json
	if !strings.Contains(string(out["server/web/portal/data/meta.json"]), `"contract": "2"`) {
		t.Fatalf("meta.json missing contract stamp:\n%s", out["server/web/portal/data/meta.json"])
	}
}

// buildRendered marshals just the two files AcceptContract reads.
func buildRendered(t *testing.T, goalsV, backlogV any) map[string][]byte {
	t.Helper()
	gb, _ := json.MarshalIndent(goalsV, "", "  ")
	bb, _ := json.MarshalIndent(backlogV, "", "  ")
	return map[string][]byte{
		"server/web/portal/data/goals.json":   gb,
		"server/web/portal/data/backlog.json": bb,
	}
}

func TestAcceptContract(t *testing.T) {
	goalsV := map[string]any{"goals": []map[string]any{
		{"id": "aion/mri", "horizon": "1yr"},
		{"id": "aion/rock-a", "horizon": "rock", "status": "open", "quarter": "2026-Q3",
			"start": "2026-07-01", "due": "2026-09-30", "aliases": []string{"fundraising"}},
		{"id": "aion/rock-b", "horizon": "rock", "status": "open", "quarter": "2026-Q3"},                    // no dates → warn
		{"id": "aion/bad-date", "horizon": "rock", "status": "open", "quarter": "2026-Q4", "start": "July"}, // non-ISO → err
	}}
	backlogV := map[string]any{"items": []map[string]any{
		{"id": "aion-bl/ok", "kind": "task", "rock": "aion/rock-a"},                              // resolves via id
		{"id": "aion-bl/alias", "kind": "task", "rock": "Fundraising"},                           // resolves via alias/slug
		{"id": "aion-bl/bad", "kind": "task", "rock": "nonexistent-thing"},                       // unresolvable → err
		{"id": "aion-bl/empty", "kind": "task", "rock": ""},                                      // empty string → err
		{"id": "aion-bl/dec1", "kind": "decision", "status": "decided", "decided": "not-a-date"}, // non-ISO → err
		{"id": "aion-bl/dec2", "kind": "decision", "status": "open"},                             // no needed_by → warn
		{"id": "aion-bl/dec3", "kind": "decision", "status": "decided"},                          // no decided date → warn
	}}
	errs, warns := AcceptContract(buildRendered(t, goalsV, backlogV), map[string]bool{"2026-Q3": true, "2026-Q4": true})

	joinErr := strings.Join(errs, " | ")
	for _, want := range []string{"aion/bad-date", "nonexistent-thing", "empty string", "not-a-date"} {
		if !strings.Contains(joinErr, want) {
			t.Errorf("expected error mentioning %q; got: %s", want, joinErr)
		}
	}
	// the empty-string rock must NOT also count as unresolvable noise beyond its own error;
	// resolvable rocks (id + alias) produce no error
	for _, bad := range []string{"aion/rock-a", "aion/rock-b", "Fundraising"} {
		if strings.Contains(joinErr, bad+" resolves to no goal") {
			t.Errorf("resolvable rock wrongly flagged: %s", bad)
		}
	}
	joinWarn := strings.Join(warns, " | ")
	for _, want := range []string{"aion/rock-b", "open decision has no needed_by", "no decided date"} {
		if !strings.Contains(joinWarn, want) {
			t.Errorf("expected warning mentioning %q; got: %s", want, joinWarn)
		}
	}
	// rock-a has dates → no coverage warning about it
	if strings.Contains(joinWarn, "aion/rock-a") {
		t.Errorf("dated rock wrongly warned: %s", joinWarn)
	}
}
