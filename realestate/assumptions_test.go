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

// Holdings derive from {entity, from}: owned and acquiring never sum; hidden
// and entity-less records count nowhere.
func TestHoldings(t *testing.T) {
	props := []Property{
		{Slug: "a", Entity: "The Garden SPE"},                        // owned
		{Slug: "b", Entity: "IGS MO LLC", From: "J. Halloran"},       // acquiring
		{Slug: "c", Entity: "ODA Group", From: "City LRA"},           // acquiring
		{Slug: "d", Entity: "ODA Group"},                             // owned
		{Slug: "e", Entity: "ODA Group", Hidden: true},               // hidden — skipped
		{Slug: "f"},                                                  // no destination — counts nowhere
	}
	h := Holdings(props)
	if h["The Garden SPE"].Owned != 1 || h["The Garden SPE"].Acquiring != 0 {
		t.Fatalf("garden: %+v", h["The Garden SPE"])
	}
	if h["IGS MO LLC"].Acquiring != 1 || h["IGS MO LLC"].Owned != 0 {
		t.Fatalf("igs: %+v", h["IGS MO LLC"])
	}
	if h["ODA Group"].Owned != 1 || h["ODA Group"].Acquiring != 1 {
		t.Fatalf("oda: %+v", h["ODA Group"])
	}
	if len(h) != 3 {
		t.Fatalf("unexpected entities: %v", h)
	}
}
