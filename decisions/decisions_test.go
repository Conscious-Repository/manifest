package decisions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A hand-authored note: an unknown frontmatter key, headings in mixed case,
// an unknown section, a comment line inside a list, tabs, CRLF-free but
// irregular blank lines. Parse → Serialize must be byte-identical.
const handNote = `---
title: Pick the vendor
owner: BA
Status: Deliberating
captured: 2026-09-01
needed-by: 2026-09-20
mood: tense
---

Some preamble the owner typed.

## Why
The current vendor churns
and support is slow.


## Evidence
- [ref:: artifact:55c3a1c4a22f15b8] the brief measured 40% churn
- [[2026-09-01 vendor call]] they admitted the backlog
- https://example.com/status incident page
- heuristic:h1a2b3c4 solve the read problem first
<!-- a note to self -->
- just a note with no ref

## alternatives
- stay with acme [tradeoff:: churn continues]
- switch to beta [tradeoff:: 3-week migration]
- do nothing

## expected outcome
Churn halves by Q4.

## scratch
whatever the owner keeps here

## downstream
- [ref:: task:inbox/order-beta] the order

## sources
- [[meeting 2026-09-01]]
- [[vendor thread]]
`

func TestHandNoteIsAFixpointAndProjects(t *testing.T) {
	d := Parse(handNote)
	if got := Serialize(d); got != handNote {
		t.Fatalf("not a fixpoint:\n%s", got)
	}
	dec := d.Decision()
	if dec.Title != "Pick the vendor" || dec.Owner != "BA" || dec.Status != StatusDeliberating ||
		dec.Captured != "2026-09-01" || dec.NeededBy != "2026-09-20" || dec.Decided != "" {
		t.Fatalf("scalars: %+v", dec)
	}
	if len(dec.Unknown) != 1 || dec.Unknown[0].Key != "mood" || dec.Unknown[0].Value != "tense" {
		t.Fatalf("unknown frontmatter: %+v", dec.Unknown)
	}
	if dec.Why != "The current vendor churns\nand support is slow." {
		t.Fatalf("why: %q", dec.Why)
	}
	wantEv := []Link{
		{Ref: "artifact:55c3a1c4a22f15b8", Note: "the brief measured 40% churn"},
		{Ref: "[[2026-09-01 vendor call]]", Note: "they admitted the backlog"},
		{Ref: "https://example.com/status", Note: "incident page"},
		{Ref: "heuristic:h1a2b3c4", Note: "solve the read problem first"},
		{Note: "just a note with no ref"},
	}
	if !sameLinks(dec.Evidence, wantEv) {
		t.Fatalf("evidence:\n got %+v\nwant %+v", dec.Evidence, wantEv)
	}
	wantAlt := []Alternative{{"stay with acme", "churn continues"}, {"switch to beta", "3-week migration"}, {"do nothing", ""}}
	if !sameAlternatives(dec.Alternatives, wantAlt) {
		t.Fatalf("alternatives: %+v", dec.Alternatives)
	}
	if dec.ExpectedOutcome != "Churn halves by Q4." || dec.ActualOutcome != "" {
		t.Fatalf("outcomes: %+v", dec)
	}
	if !sameLinks(dec.Downstream, []Link{{Ref: "task:inbox/order-beta", Note: "the order"}}) {
		t.Fatalf("downstream: %+v", dec.Downstream)
	}
	if strings.Join(dec.Sources, "|") != "meeting 2026-09-01|vendor thread" {
		t.Fatalf("sources: %+v", dec.Sources)
	}
	// refs: kind:id splits, a wikilink and a bare note do not
	if k, id, ok := RefKind(dec.Evidence[0].Ref); !ok || k != "artifact" || id != "55c3a1c4a22f15b8" {
		t.Fatalf("ref kind: %s %s %v", k, id, ok)
	}
	if _, _, ok := RefKind(dec.Evidence[1].Ref); ok {
		t.Fatal("a wikilink is not a kind:id ref")
	}
	if k, _, ok := RefKind(dec.Evidence[2].Ref); !ok || k != "https" {
		t.Fatalf("a URL reads as kind https: %s %v", k, ok)
	}
}

func TestSettersRewriteOnlyTheirSection(t *testing.T) {
	d := Parse(handNote)
	d.SetActual("Churn fell 30%, not 50%.")
	d.SetScalar("status", StatusRevisited)
	d.SetScalar("revisited", "2026-12-01")
	d.SetScalar("needed-by", "") // a cleared date drops the line
	d.SetEvidence(append(d.Decision().Evidence, Link{Ref: "artifact:aaaa", Note: "the [bracketed] retro"}))
	got := Serialize(d)
	// untouched sections are byte-identical, the unknown key and section survive
	for _, keep := range []string{"mood: tense\n", "Some preamble the owner typed.\n", "## Why\nThe current vendor churns\nand support is slow.\n\n\n## Evidence\n",
		"## scratch\nwhatever the owner keeps here\n", "## sources\n- [[meeting 2026-09-01]]\n- [[vendor thread]]\n"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("lost %q in:\n%s", keep, got)
		}
	}
	// the actual-outcome section landed at its canonical slot: after expected, before scratch
	if !strings.Contains(got, "## expected outcome\nChurn halves by Q4.\n\n## actual outcome\nChurn fell 30%, not 50%.\n\n## scratch\n") {
		t.Fatalf("actual outcome placement:\n%s", got)
	}
	// (the kernel's DocFM.Set rewrites a line with the caller's key: `Status:` → `status:`)
	if !strings.Contains(got, "status: revisited\ncaptured: 2026-09-01\nmood: tense\nrevisited: 2026-12-01\n") || strings.Contains(got, "needed-by") {
		t.Fatalf("frontmatter:\n%s", got)
	}
	// the evidence list re-emitted canonically (the comment line is gone — the list was replaced)
	if !strings.Contains(got, "## Evidence\n- the brief measured 40% churn [ref:: artifact:55c3a1c4a22f15b8]\n- [[2026-09-01 vendor call]] they admitted the backlog\n- incident page [ref:: https://example.com/status]\n- solve the read problem first [ref:: heuristic:h1a2b3c4]\n- just a note with no ref\n- the [bracketed] retro [ref:: artifact:aaaa]\n\n## alternatives\n") {
		t.Fatalf("evidence emit:\n%s", got)
	}
	// and what was written is itself a fixpoint that projects the same
	again := Parse(got)
	if Serialize(again) != got {
		t.Fatal("re-emit is not a fixpoint")
	}
	if p := again.Decision(); p.ActualOutcome != "Churn fell 30%, not 50%." || p.Status != StatusRevisited || len(p.Evidence) != 6 || p.NeededBy != "" {
		t.Fatalf("re-projection: %+v", p)
	}
}

func TestNewLaysDownTheCanonicalNote(t *testing.T) {
	d := New(Decision{Title: "Pick the vendor", Owner: "BA", Status: StatusOpen, Captured: "2026-09-05",
		Why: "churn", Evidence: []Link{{Ref: "artifact:1", Note: "brief"}}, Alternatives: []Alternative{{"acme", "churn"}},
		ExpectedOutcome: "less churn", Downstream: []Link{{Ref: "task:inbox/x"}}, Sources: []string{"[[call]]", "thread"}})
	want := "---\ntitle: Pick the vendor\nowner: BA\nstatus: open\ncaptured: 2026-09-05\n---\n\n## why\nchurn\n\n## evidence\n- brief [ref:: artifact:1]\n\n## alternatives\n- acme [tradeoff:: churn]\n\n## expected outcome\nless churn\n\n## actual outcome\n\n## downstream\n- [ref:: task:inbox/x]\n\n## sources\n- [[call]]\n- [[thread]]\n\n"
	if got := Serialize(d); got != want {
		t.Fatalf("canonical note:\n%s", got)
	}
	if Serialize(Parse(want)) != want {
		t.Fatal("the seed is not a fixpoint")
	}
	if p := Parse(want).Decision(); p.Title != "Pick the vendor" || len(p.Evidence) != 1 || len(p.Sources) != 2 || p.Sources[0] != "call" {
		t.Fatalf("seed projection: %+v", p)
	}
	// a note with no frontmatter and no sections still projects and takes a scalar
	bare := Parse("just a line\n")
	if p := bare.Decision(); p.Status != StatusOpen || p.Title != "" {
		t.Fatalf("bare: %+v", p)
	}
	bare.SetScalar("title", "T")
	bare.SetWhy("because")
	if got := Serialize(bare); got != "---\ntitle: T\n---\n\njust a line\n## why\nbecause\n\n" {
		t.Fatalf("bare after set:\n%q", got)
	}
}

func storeFixture(t *testing.T) (*Store, string) {
	t.Helper()
	vault := t.TempDir()
	write := func(abs string, data []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, data, 0o644)
	}
	return NewStore(vault, "system/decisions", write), vault
}

func TestStoreCreateUpdateList(t *testing.T) {
	st, vault := storeFixture(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if _, err := st.Create(Decision{}, now); err == nil {
		t.Fatal("a title is required")
	}
	if _, err := st.Create(Decision{Title: "x", Status: "maybe"}, now); err == nil {
		t.Fatal("status outside the set")
	}
	dec, err := st.Create(Decision{Title: "Pick the vendor", Owner: "BA", Why: "churn", Source: "owner"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if dec.ID != "pick-the-vendor" || dec.Status != StatusOpen || dec.Captured != "2026-09-05" || dec.Why != "churn" || dec.Ref() != "decision:pick-the-vendor" {
		t.Fatalf("created: %+v", dec)
	}
	raw, err := os.ReadFile(filepath.Join(vault, "system", "decisions", "pick-the-vendor.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "---\ntitle: Pick the vendor\nowner: BA\nstatus: open\ncaptured: 2026-09-05\nsource: owner\n---\n\n## why\nchurn\n") {
		t.Fatalf("file:\n%s", raw)
	}
	// an open twin is refused; a distinct title mints -2 on a slug collision
	if _, err := st.Create(Decision{Title: "pick the VENDOR"}, now); err == nil || !strings.Contains(err.Error(), "already in the ledger") {
		t.Fatalf("open twin: %v", err)
	}
	two, err := st.Create(Decision{Title: "Pick the vendor!"}, now.Add(24*time.Hour))
	if err != nil || two.ID != "pick-the-vendor-2" {
		t.Fatalf("collision suffix: %+v %v", two, err)
	}
	if _, err := st.Create(Decision{ID: "../escape", Title: "bad"}, now); err == nil {
		t.Fatal("an id must be a slug")
	}
	// list: newest captured first
	if ids := idsOf(st.List()); strings.Join(ids, ",") != "pick-the-vendor-2,pick-the-vendor" {
		t.Fatalf("list order: %v", ids)
	}

	// update: a no-op patch writes nothing
	ch, err := st.Update("pick-the-vendor", Patch{Why: sp("churn")}, now)
	if err != nil || ch.Changed() {
		t.Fatalf("no-op: %+v %v", ch, err)
	}
	// recording an outcome DECIDES an open decision (aion's Decide), stamping the date
	ch, err = st.Update("pick-the-vendor", Patch{Outcome: sp("beta, 12-month contract"), ExpectedOutcome: sp("churn halves")}, now.Add(48*time.Hour))
	if err != nil || ch.Transition != "decided" || strings.Join(ch.Fields, ",") != "outcome,expected outcome,status,decided" {
		t.Fatalf("decide: %+v %v", ch, err)
	}
	if ch.After.Status != StatusDecided || ch.After.Decided != "2026-09-07" || ch.After.Outcome != "beta, 12-month contract" || ch.Before.Status != StatusOpen {
		t.Fatalf("after decide: %+v", ch.After)
	}
	// recording the actual outcome REVISITS it
	ch, err = st.Update("pick-the-vendor", Patch{ActualOutcome: sp("churn fell 30%")}, now.Add(72*time.Hour))
	if err != nil || ch.Transition != "revisited" || ch.After.Status != StatusRevisited || ch.After.Revisited != "2026-09-08" || ch.After.Decided != "2026-09-07" {
		t.Fatalf("revisit: %+v %v", ch, err)
	}
	// an explicit status back to deliberating reopens; a bad status refuses
	if _, err := st.Update("pick-the-vendor", Patch{Status: sp("undecided")}, now); err == nil {
		t.Fatal("bad status")
	}
	ch, err = st.Update("pick-the-vendor", Patch{Status: sp(StatusDeliberating), Downstream: &[]Link{{Ref: "task:inbox/order-beta", Note: "the order"}}}, now)
	if err != nil || ch.Transition != "reopened" || len(ch.After.Downstream) != 1 {
		t.Fatalf("reopen: %+v %v", ch, err)
	}
	if _, err := st.Update("nope", Patch{}, now); err == nil {
		t.Fatal("unknown id")
	}
	// the file round-trips through a fresh parse
	got, _ := st.Get("pick-the-vendor")
	if got.ActualOutcome != "churn fell 30%" || got.Status != StatusDeliberating || got.Downstream[0].Ref != "task:inbox/order-beta" {
		t.Fatalf("reload: %+v", got)
	}
	// a now-open twin is refused again (the title is live)
	if _, err := st.Create(Decision{Title: "Pick the vendor"}, now); err == nil {
		t.Fatal("reopened twin must block")
	}
}

func TestStoreNilWriterFailsLoudly(t *testing.T) {
	st := NewStore(t.TempDir(), "system/decisions", nil)
	if _, err := st.Create(Decision{Title: "x"}, time.Now()); err == nil || !strings.Contains(err.Error(), "no vault writer") {
		t.Fatalf("nil writer: %v", err)
	}
	if _, ok := st.Load("../x"); ok {
		t.Fatal("a path is not an id")
	}
	var perr *os.PathError
	if _, err := os.Stat(st.Path("x")); !errors.As(err, &perr) {
		t.Fatal("nothing written")
	}
}

func TestMintID(t *testing.T) {
	if MintID("", nil) != "decision" || MintID("Pick the vendor!", map[string]bool{"pick-the-vendor": true, "pick-the-vendor-2": true}) != "pick-the-vendor-3" {
		t.Fatal("mint")
	}
	if !ValidID("pick-the-vendor-3") || ValidID("Pick") || ValidID("a/b") || ValidID("") {
		t.Fatal("valid id")
	}
}

func sp(s string) *string { return &s }

func idsOf(ds []Decision) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}
