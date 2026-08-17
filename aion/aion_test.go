package aion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// canonical fixtures — one per corpus, exercising the full grammar
// (frontmatter, unknown fields, verbatim interleave, nesting).
var fixtures = map[string]string{
	"people.md": `# AION — people

- [initials:: BA] [name:: Benjamin Anderson] [role:: CEO]
- [initials:: HZ] [name:: Hannah Zmuda] [role:: Founding Scientist] [phone:: x]
a stray comment line
- [initials:: YS] [name:: Yashiro]
`,
	"vto.md": `## 01 core values
- Morale is the most valuable resource we have
- Take the longer path that gets you there faster

## 02 core focus
- [purpose:: control biology with fields]
- [niche:: field-based longevity]
free text line survives

## 07 quarter
- [start:: 2026-07-01] [end:: 2026-09-30]
`,
	"backlog.md": `# AION — backlog

## Tasks
- [ ] Secure the Deep Tech Week venue [kind:: task] [rock:: aion/deep-tech] [due:: 2026-08-20] [status:: open] [owner:: JR] [source:: [[2026-07-31 jack ruhl sync]]] [captured:: 2026-07-31]
- [x] Hire Morgan for engineering [kind:: task] [status:: done] [done_on:: 2026-07-06] [owner:: BA/MM] [source:: [[2026-07-06 aion team sync]]] [captured:: 2026-07-06] [extra:: kept]
    - [ ] a nested child task [kind:: task]
    stray indented note kept verbatim

## Decisions
- Outsource pig work rather than vertically integrate [kind:: decision] [status:: decided] [decided:: 2026-07-27] [outcome:: use a CRO for pig studies] [owner:: BA/HZ] [source:: [[2026-07-27 derya ii]]] [captured:: 2026-07-27]
- Choose the final regulatory path [kind:: decision] [status:: open] [needed_by:: before pig studies] [owner:: BA/RT] [source:: [[regulatory strategy]]] [source:: [[updates for justin]]] [captured:: 2026-07-02]
`,
	"heuristics.md": `# AION — heuristics

- Take the longer path that gets you there faster [first:: 2025-11-19]
    - [[aion biosciences]] [date:: 2026-07-02]
    - [[2026-07-27 derya ii]] [date:: 2026-07-27]
- Morale is the most valuable resource we have [first:: 2026-07-02] [weight:: high]
    - [[aion biosciences]] [date:: 2026-07-02]

## retired
- An old idea that got pruned [first:: 2025-01-01]
    - [[some note]] [date:: 2025-01-01]
`,
	"hiring.md": `- [role:: MRI systems architect] [candidate:: Yashiro] [stage:: hired] [priority:: 1]
- [role:: lab engineer] [stage:: sourcing] [next:: post the role] [priority:: 2] [via:: referral]
`,
	"references.md": `- Magnetoacoustics primer [url:: https://example.com/ma] [source:: arXiv] [date:: 2026-05-01]
- [url:: https://example.com/bare]
not a reference line
`,
	"finances.md": `---
capital: 1500000
monthly_burn: 95000
as_of: 2026-08-01
currency: USD
source: manual
note: seed round
---

private notes stay here
- [ ] even checkbox-looking lines are untouched
`,
}

func TestFixpointAllCorpora(t *testing.T) {
	for name, roundtrip := range Corpora {
		raw, ok := fixtures[name]
		if !ok {
			t.Fatalf("no fixture for %s", name)
		}
		if got := roundtrip(raw); got != raw {
			t.Errorf("%s round-trip diverged:\n--- got ---\n%s\n--- want ---\n%s", name, got, raw)
		}
		// double round-trip: emitted form is itself a fixpoint
		if once := roundtrip(raw); roundtrip(once) != once {
			t.Errorf("%s emitted form is not a fixpoint", name)
		}
	}
}

func TestSeedsAreFixpoints(t *testing.T) {
	for _, name := range Files {
		seed, ok := SeedFiles[name]
		if !ok {
			t.Fatalf("no seed for %s", name)
		}
		if got := Corpora[name](seed); got != seed {
			t.Errorf("seed %s is not a fixpoint:\n--- got ---\n%s\n--- want ---\n%s", name, got, seed)
		}
	}
}

func TestUnknownPreservation(t *testing.T) {
	// unknown field on a task survives a semantic edit
	doc := ParseBacklog(fixtures["backlog.md"])
	it := doc.Find(ItemID(KindTask, "Hire Morgan for engineering"))
	if it == nil {
		t.Fatal("item not found")
	}
	if len(it.Unknown) != 1 || it.Unknown[0].Key != "extra" {
		t.Fatalf("unknown fields: %+v", it.Unknown)
	}
	it.Owner = "BA"
	out := SerializeBacklog(doc)
	if !strings.Contains(out, "[extra:: kept]") {
		t.Fatal("unknown field dropped on edit")
	}
	if !strings.Contains(out, "stray indented note kept verbatim") {
		t.Fatal("raw child line dropped")
	}
	// unknown field on a heuristic survives
	h := ParseHeuristics(fixtures["heuristics.md"])
	morale := h.FindLive(HeuristicID("Morale is the most valuable resource we have"))
	if morale == nil || len(morale.Unknown) != 1 {
		t.Fatalf("heuristic unknown fields: %+v", morale)
	}
}

func TestHandEditReadback(t *testing.T) {
	// mutate → emit → re-parse → emit: still a fixpoint (demokind pattern)
	doc := ParseBacklog(fixtures["backlog.md"])
	it := doc.Find(ItemID(KindTask, "Secure the Deep Tech Week venue"))
	it.Status = StatusInProgress
	edited := SerializeBacklog(doc)
	if SerializeBacklog(ParseBacklog(edited)) != edited {
		t.Fatal("edited backlog is not a fixpoint")
	}
	hd := ParseHeuristics(fixtures["heuristics.md"])
	hd.LiveEntries()[0].Sources = append(hd.LiveEntries()[0].Sources,
		Reinforcement{Note: "2026-08-03 aion sync", Date: "2026-08-03"})
	he := SerializeHeuristics(hd)
	if SerializeHeuristics(ParseHeuristics(he)) != he {
		t.Fatal("edited heuristics is not a fixpoint")
	}
}

func TestPeopleEmailField(t *testing.T) {
	// email is first-class: parsed, round-tripped byte-stable, exported
	src := "- [initials:: BA] [name:: Benjamin Anderson] [role:: CEO] [email:: ben@aion.bio]\n" +
		"- [initials:: NM] [name:: Nirosha Murugan] [role:: External]\n"
	doc := ParsePeople(src)
	ppl := doc.People()
	if len(ppl) != 2 || ppl[0].Email != "ben@aion.bio" {
		t.Fatalf("email not parsed: %+v", ppl)
	}
	if ppl[1].Email != "" {
		t.Fatalf("emailless person must stay empty: %q", ppl[1].Email)
	}
	if out := SerializePeople(doc); out != src {
		t.Fatalf("people.md not a fixpoint with email:\n--- got ---\n%s\n--- want ---\n%s", out, src)
	}
	ex := exportPeople(doc)["people"].([]exportPersonT)
	if ex[0].Email != "ben@aion.bio" || ex[1].Email != "" {
		t.Fatalf("export email: %+v", ex)
	}
}

func TestBacklogParseShapes(t *testing.T) {
	doc := ParseBacklog(fixtures["backlog.md"])
	items := doc.Items()
	if len(items) != 4 {
		t.Fatalf("items: %d", len(items))
	}
	venue := items[0]
	if venue.Kind != KindTask || venue.Owner != "JR" || venue.Rock != "aion/deep-tech" ||
		len(venue.Sources) != 1 || venue.Sources[0] != "2026-07-31 jack ruhl sync" {
		t.Fatalf("venue: %+v", venue)
	}
	pig := items[2]
	if pig.Kind != KindDecision || pig.Status != StatusDecided || pig.Outcome != "use a CRO for pig studies" {
		t.Fatalf("pig: %+v", pig)
	}
	reg := items[3]
	if len(reg.Sources) != 2 || reg.NeededBy != "before pig studies" {
		t.Fatalf("reg: %+v", reg)
	}
	if len(items[1].Children) != 1 {
		t.Fatalf("nested child: %+v", items[1].Children)
	}
}

func TestDecideAppendOnly(t *testing.T) {
	doc := ParseBacklog(fixtures["backlog.md"])
	before := strings.Count(SerializeBacklog(doc), "\n")
	it := doc.Find(ItemID(KindDecision, "Choose the final regulatory path"))
	it.Status = StatusDecided
	it.Decided = "2026-08-07"
	it.Outcome = "Class III PMA"
	out := SerializeBacklog(doc)
	if strings.Count(out, "\n") < before {
		t.Fatal("decide reduced line count")
	}
	if !strings.Contains(out, "[decided:: 2026-08-07]") || !strings.Contains(out, "[outcome:: Class III PMA]") {
		t.Fatalf("decide fields missing:\n%s", out)
	}
	// the decided decision from the fixture is still present untouched
	if !strings.Contains(out, "Outsource pig work") {
		t.Fatal("permanent log line lost")
	}
}

func TestHeuristicsMergeUnionsSources(t *testing.T) {
	d := ParseHeuristics(fixtures["heuristics.md"])
	a := d.LiveEntries()[0] // 2 sources
	b := d.LiveEntries()[1] // 1 source (aion biosciences 2026-07-02 — duplicate of a's)
	total := map[string]bool{}
	for _, r := range append(append([]Reinforcement{}, a.Sources...), b.Sources...) {
		total[r.Note+"|"+r.Date] = true
	}
	if !d.Merge(a.ID, b.ID) {
		t.Fatal("merge refused")
	}
	if len(d.LiveEntries()) != 1 {
		t.Fatalf("live entries after merge: %d", len(d.LiveEntries()))
	}
	if len(a.Sources) != len(total) {
		t.Fatalf("merged sources %d, want union %d", len(a.Sources), len(total))
	}
	// earlier first wins
	if a.First != "2025-11-19" {
		t.Fatalf("first: %s", a.First)
	}
	// retired zone untouched
	if len(d.Retired) == 0 {
		t.Fatal("retired lost")
	}
	out := SerializeHeuristics(d)
	if SerializeHeuristics(ParseHeuristics(out)) != out {
		t.Fatal("post-merge not a fixpoint")
	}
}

func TestRetireMovesNeverDeletes(t *testing.T) {
	d := ParseHeuristics(fixtures["heuristics.md"])
	beforeLines := strings.Count(SerializeHeuristics(d), "\n")
	id := d.LiveEntries()[1].ID
	if !d.Retire(id) {
		t.Fatal("retire refused")
	}
	out := SerializeHeuristics(d)
	if strings.Count(out, "\n") < beforeLines {
		t.Fatal("retire reduced line count")
	}
	if !strings.Contains(strings.SplitN(out, "## retired", 2)[1], "Morale is the most valuable") {
		t.Fatal("entry not under ## retired")
	}
	// retiring from a doc with no retired heading creates it
	d2 := ParseHeuristics("- only one [first:: 2026-01-01]\n")
	if !d2.Retire(d2.LiveEntries()[0].ID) {
		t.Fatal("retire refused on headingless doc")
	}
	out2 := SerializeHeuristics(d2)
	if !strings.Contains(out2, "## retired") {
		t.Fatalf("no retired heading:\n%s", out2)
	}
	if SerializeHeuristics(ParseHeuristics(out2)) != out2 {
		t.Fatal("post-retire not a fixpoint")
	}
}

func TestReorderIsPermutationOnly(t *testing.T) {
	d := ParseHeuristics(fixtures["heuristics.md"])
	ids := []string{d.LiveEntries()[1].ID, d.LiveEntries()[0].ID}
	if !d.Reorder(ids) {
		t.Fatal("valid permutation refused")
	}
	if d.LiveEntries()[0].Text != "Morale is the most valuable resource we have" {
		t.Fatal("order not applied")
	}
	if d.Reorder([]string{"bogus"}) {
		t.Fatal("non-permutation accepted")
	}
	if d.Reorder(append(ids, "extra")) {
		t.Fatal("wrong-length accepted")
	}
}

func TestAppendBacklogItemExactlyOneLine(t *testing.T) {
	current := fixtures["backlog.md"]
	p := ProposalPayload{
		Kind: KindTask, Title: "Test spheroids in the ultrasound setup",
		Owner: "HZ", Status: StatusOpen,
		Sources: []string{"2026-08-03 aion sync"}, Captured: "2026-08-03",
	}
	out, err := AppendBacklogItem(current, p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != strings.Count(current, "\n")+1 {
		t.Fatalf("expected exactly one added line")
	}
	if !strings.Contains(out, "- [ ] Test spheroids in the ultrasound setup [kind:: task] [owner:: HZ] [source:: [[2026-08-03 aion sync]]] [captured:: 2026-08-03] [status:: open]") {
		t.Fatalf("rendered line wrong:\n%s", out)
	}
	// idempotency: same payload again refuses
	if _, err := AppendBacklogItem(out, p); err == nil {
		t.Fatal("duplicate append accepted")
	}
	// result is a fixpoint
	if SerializeBacklog(ParseBacklog(out)) != out {
		t.Fatal("post-append not a fixpoint")
	}
	// new task lands under ## Tasks, before the blank line separating sections
	tasksPart := strings.SplitN(out, "## Decisions", 2)[0]
	if !strings.Contains(tasksPart, "Test spheroids") {
		t.Fatal("task not under ## Tasks")
	}
}

func TestApplyHeuristicNewAndReinforce(t *testing.T) {
	current := fixtures["heuristics.md"]
	// new
	pNew := ProposalPayload{
		Kind: KindHeuristic, Title: "Solve the read problem first",
		Sources: []string{"aion master plan"}, Captured: "2026-07-30",
		Heuristic: HeuristicIntent{Mode: HeuristicModeNew},
	}
	out, err := ApplyHeuristic(current, pNew)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- Solve the read problem first [first:: 2026-07-30]") ||
		!strings.Contains(out, "    - [[aion master plan]] [date:: 2026-07-30]") {
		t.Fatalf("new heuristic wrong:\n%s", out)
	}
	// the new entry must land in the LIVE zone, not after ## retired
	if livePart := strings.SplitN(out, "## retired", 2)[0]; !strings.Contains(livePart, "Solve the read problem") {
		t.Fatal("new heuristic landed in retired zone")
	}
	// duplicate new refuses, pointing at reinforce
	if _, err := ApplyHeuristic(out, pNew); err == nil {
		t.Fatal("duplicate new accepted")
	}
	// reinforce
	pRe := ProposalPayload{
		Kind: KindHeuristic, Title: "anything — reinforcement re-expression",
		Sources: []string{"2026-07-31 jack ruhl sync"}, Captured: "2026-07-31",
		Heuristic: HeuristicIntent{Mode: HeuristicModeReinforce,
			Target: "Take the longer path that gets you there faster"},
	}
	out2, err := ApplyHeuristic(out, pRe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "    - [[2026-07-31 jack ruhl sync]] [date:: 2026-07-31]") {
		t.Fatalf("reinforcement missing:\n%s", out2)
	}
	// reinforce is idempotent per note+date
	out3, err := ApplyHeuristic(out2, pRe)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out3, "[[2026-07-31 jack ruhl sync]]") != 1 {
		t.Fatal("duplicate reinforcement added")
	}
	// missing target refuses loudly
	pBad := pRe
	pBad.Heuristic.Target = "A statement that does not exist"
	if _, err := ApplyHeuristic(out2, pBad); err == nil {
		t.Fatal("missing target accepted")
	}
}

func TestPayloadFenceRoundTrip(t *testing.T) {
	p := ProposalPayload{
		Kind: KindTask, Title: "A task", Owner: "HZ",
		Sources: []string{"note a"}, Captured: "2026-08-07",
		Confidence: 0.92, Quote: "the justifying quote",
	}
	body := "A one-line summary\n\n> \"the justifying quote\" — [[note a]]\n\n" + RenderPayloadFence(p) + "\n"
	got, ok := ParsePayload(body)
	if !ok || got.Title != "A task" || got.Confidence != 0.92 {
		t.Fatalf("parse: %+v ok=%v", got, ok)
	}
	// edit in place: fence replaced, prose untouched
	got.Owner = "BA"
	got.Kind = KindDecision
	edited, ok := ReplacePayloadFence(body, got)
	if !ok {
		t.Fatal("replace failed")
	}
	if !strings.Contains(edited, "A one-line summary") || !strings.Contains(edited, "the justifying quote") {
		t.Fatal("prose lost")
	}
	re, ok := ParsePayload(edited)
	if !ok || re.Owner != "BA" || re.Kind != KindDecision {
		t.Fatalf("re-parse: %+v", re)
	}
	// RenderItemLine never leaks quote/confidence
	line := RenderItemLine(got)
	if strings.Contains(line, "quote") || strings.Contains(line, "0.92") {
		t.Fatalf("leak: %s", line)
	}
}

func TestValidatePayload(t *testing.T) {
	people := []*Person{{Initials: "BA"}, {Initials: "HZ"}}
	ok := ProposalPayload{Kind: KindTask, Title: "x", Owner: "BA/HZ", Captured: "2026-08-07"}
	if err := ok.Validate(people); err != nil {
		t.Fatal(err)
	}
	bad := []ProposalPayload{
		{Kind: "note", Title: "x"},
		{Kind: KindTask, Title: " "},
		{Kind: KindTask, Title: "x", Due: "aug 3"},
		{Kind: KindHeuristic, Title: "x", Heuristic: HeuristicIntent{Mode: "maybe"}},
		{Kind: KindHeuristic, Title: "x", Heuristic: HeuristicIntent{Mode: HeuristicModeReinforce}},
		{Kind: KindTask, Title: "x", Owner: "ZZ"},
	}
	for i, p := range bad {
		if err := p.Validate(people); err == nil {
			t.Errorf("bad[%d] accepted: %+v", i, p)
		}
	}
	// free-text owner names pass (non-roster people keep names)
	free := ProposalPayload{Kind: KindTask, Title: "x", Owner: "Ken Cron"}
	if err := free.Validate(people); err != nil {
		t.Fatal(err)
	}
}

// A TASK that arrived with a stray [status:: decided] (extraction drift) must
// stay editable — only decided DECISIONS are permanent. The guard once matched
// on status alone, locking such tasks forever.
func TestUpdateTaskWithStrayDecidedStatus(t *testing.T) {
	dir := t.TempDir()
	raw := "# AION — backlog\n\n## inbox\n- [ ] Execute advisor agreement [kind:: task] [owner:: BA] [status:: decided] [rock:: aion/operations-health]\n"
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st := NewStore(dir, "", func(p string, b []byte) error { return os.WriteFile(p, b, 0o644) })
	id := ItemID(KindTask, "Execute advisor agreement")
	if err := st.UpdateItem(id, map[string]string{"status": "done"}, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("update refused: %v", err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if !strings.Contains(string(out), "- [x] Execute advisor agreement") || !strings.Contains(string(out), "[status:: done]") {
		t.Fatalf("task not marked done:\n%s", out)
	}
	// a decided DECISION stays permanent
	raw2 := "# AION — backlog\n\n## inbox\n- [x] Pick the vendor [kind:: decision] [status:: decided] [outcome:: chose X]\n"
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(raw2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateItem(ItemID(KindDecision, "Pick the vendor"), map[string]string{"owner": "HZ"}, time.Now()); err == nil {
		t.Fatal("decided decision must refuse edits")
	}
}
