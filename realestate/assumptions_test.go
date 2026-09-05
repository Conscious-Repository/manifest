package realestate

import (
	"strings"
	"testing"
)

// The seed parses to the full default set and round-trips byte-identically.
func TestAssumptionsSeedFixpoint(t *testing.T) {
	seed := SeedAssumptions()
	a := ParseAssumptions(seed)
	for _, k := range AssumptionKeys {
		if a.Values[k] != DefaultAssumptions[k] {
			t.Fatalf("%s: parsed %v want %v", k, a.Values[k], DefaultAssumptions[k])
		}
	}
	if got := EmitAssumptions(a); got != seed {
		t.Fatalf("seed not a fixpoint:\n--- seed\n%s\n--- emit\n%s", seed, got)
	}
}

// The ARCHITECTURE §3 fixpoint, golden: an untouched document round-trips
// byte-identically no matter how its numbers are spelled (0.0500 is not
// rebuilt to 0.05) or how it ends (a doubled EOF newline, or none, survives);
// only a REAL edit rewrites its line, and then in canonical form.
func TestAssumptionsGoldenRoundTrip(t *testing.T) {
	for _, in := range []string{
		"- [exit_cap_rate:: 0.0500]\n",
		"- [exit_cap_rate:: 0.0500]\n\n",
		"- [exit_cap_rate:: 0.0500]",     // no final newline
		"- [exit_cap_rate:: 0.0500]  \n", // trailing whitespace inside the line
		"- [construction_interest_rate:: 0.10]\n- [hold_years:: 5.0]\n\n\n",
		"- [foo:: bar]\n", // control: an unrecognized line
		"\n\n",
		"",
	} {
		if out := EmitAssumptions(ParseAssumptions(in)); out != in {
			t.Errorf("not byte-identical:\n--- in\n%q\n--- out\n%q", in, out)
		}
	}
	// a real edit still normalizes ITS line and leaves the rest byte-identical
	a := ParseAssumptions("- [exit_cap_rate:: 0.0500]\n- [perm_ltv:: 0.750]\n\n")
	if err := a.SetAssumption("exit_cap_rate", 0.06); err != nil {
		t.Fatal(err)
	}
	if got, want := EmitAssumptions(a), "- [exit_cap_rate:: 0.06]\n- [perm_ltv:: 0.750]\n\n"; got != want {
		t.Fatalf("edit emit:\n--- got\n%q\n--- want\n%q", got, want)
	}
	// setting a value equal to the literal's value is not an edit
	b := ParseAssumptions("- [exit_cap_rate:: 0.0500]\n")
	if err := b.SetAssumption("exit_cap_rate", 0.05); err != nil {
		t.Fatal(err)
	}
	if got := EmitAssumptions(b); got != "- [exit_cap_rate:: 0.0500]\n" {
		t.Fatalf("no-op edit rewrote the literal: %q", got)
	}
	// an appended line on a newline-less doc terminates the file
	c := ParseAssumptions("# empty")
	if err := c.SetAssumption("perm_ltv", 0.7); err != nil {
		t.Fatal(err)
	}
	if got := EmitAssumptions(c); got != "# empty\n- [perm_ltv:: 0.7]\n" {
		t.Fatalf("append on newline-less doc: %q", got)
	}
}

// Prose and unknown lines survive verbatim; an edit rebuilds only its line.
func TestAssumptionsEditPreservesProse(t *testing.T) {
	raw := "---\ncategories: [assumptions]\n---\n\n# underwriting assumptions\n\nsome prose the owner wrote\n\n- [vacancy_rate:: 0.08]\n- [exit_cap_rate:: 0.0725]\n- [unknown_key:: 42]\n"
	a := ParseAssumptions(raw)
	if got := EmitAssumptions(a); got != raw {
		t.Fatalf("no-edit emit diverged:\n%s", got)
	}
	if err := a.SetAssumption("vacancy_rate", 0.1); err != nil {
		t.Fatal(err)
	}
	out := EmitAssumptions(a)
	if !strings.Contains(out, "- [vacancy_rate:: 0.1]") {
		t.Fatalf("edit did not land:\n%s", out)
	}
	if !strings.Contains(out, "some prose the owner wrote") || !strings.Contains(out, "- [unknown_key:: 42]") {
		t.Fatalf("prose/unknown lost:\n%s", out)
	}
	// unknown keys are refused
	if err := a.SetAssumption("nope", 1); err == nil {
		t.Fatal("unknown key accepted")
	}
	// a key with no line yet gets one appended
	a2 := ParseAssumptions("# empty\n")
	if err := a2.SetAssumption("perm_ltv", 0.7); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(EmitAssumptions(a2), "- [perm_ltv:: 0.7]") {
		t.Fatal("missing-key line not appended")
	}
}

// Holdings derive from {entity, control, status}: owned, acquiring and
// pipeline never sum; hidden and entity-less records count nowhere.
func TestHoldings(t *testing.T) {
	props := []Property{
		{Slug: "a", Entity: "The Garden SPE", Control: "owned", Status: "construction"},    // owned
		{Slug: "b", Entity: "IGS MO LLC", Control: "owned", Status: "under_contract"},      // acquiring
		{Slug: "c", Entity: "ODA Group", Control: "owned", Status: "under_contract"},       // acquiring
		{Slug: "d", Entity: "ODA Group", Control: "owned", Status: "leased"},               // owned
		{Slug: "e", Entity: "ODA Group", Control: "owned", Status: "leased", Hidden: true}, // hidden — skipped
		{Slug: "g", Entity: "ODA Group", Control: "owned", Status: "negotiating"},          // pipeline
		{Slug: "f", Control: "tracked", Status: "negotiating"},                             // no destination — counts nowhere
	}
	h := Holdings(props)
	if h["The Garden SPE"].Owned != 1 || h["The Garden SPE"].Acquiring != 0 {
		t.Fatalf("garden: %+v", h["The Garden SPE"])
	}
	if h["IGS MO LLC"].Acquiring != 1 || h["IGS MO LLC"].Owned != 0 {
		t.Fatalf("igs: %+v", h["IGS MO LLC"])
	}
	if o := h["ODA Group"]; o.Owned != 1 || o.Acquiring != 1 || o.Pipeline != 1 {
		t.Fatalf("oda: %+v", o)
	}
	if len(h) != 3 {
		t.Fatalf("unexpected entities: %v", h)
	}
}

// The published module: a no-edit render over the live portal file shape is
// byte-identical (0.10 keeps its trailing zero); an edit rewrites ONLY its
// line; content outside the defaults block never moves.
func TestRenderPortalDefaults(t *testing.T) {
	current := []byte(`export const defaults = {
  vacancy_rate: 0.08,
  opex_rate: 0.35,
  construction_interest_rate: 0.10,
  construction_loan_ltc: 0.70,
};

// Default itemized OpEx breakdown (sums to 0.35, matching opex_rate default)
export const default_opex_items = {
  property_tax_rate: 0.10,
};
`)
	vals := map[string]float64{
		"vacancy_rate": 0.08, "opex_rate": 0.35,
		"construction_interest_rate": 0.10, "construction_loan_ltc": 0.70,
	}
	same, err := RenderPortalDefaults(current, vals)
	if err != nil {
		t.Fatal(err)
	}
	if string(same) != string(current) {
		t.Fatalf("no-edit render not byte-stable:\n%s", same)
	}
	vals["vacancy_rate"] = 0.1
	edited, err := RenderPortalDefaults(current, vals)
	if err != nil {
		t.Fatal(err)
	}
	got := string(edited)
	if !strings.Contains(got, "  vacancy_rate: 0.1,") {
		t.Fatalf("edit did not land:\n%s", got)
	}
	if !strings.Contains(got, "  construction_interest_rate: 0.10,") ||
		!strings.Contains(got, "  property_tax_rate: 0.10,") {
		t.Fatalf("untouched literals/blocks rewritten:\n%s", got)
	}
	// a key with no line yet is appended inside the block
	vals["contingency_pct"] = 0.05
	withNew, err := RenderPortalDefaults(current, vals)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withNew), "  contingency_pct: 0.05,\n};") {
		t.Fatalf("missing key not appended before the close:\n%s", withNew)
	}
}
