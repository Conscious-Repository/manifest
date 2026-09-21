package recruiting

import "testing"

// The uniform name of a place: the principal's surname + Lab (a given name
// precedes the surname; a discipline word is not a person), a centre's short
// name, then the institution's tag — the same shape for every seed the
// owner has actually catalogued (2026-09-21).
func TestLabLabel(t *testing.T) {
	washu := "Washington University in St. Louis, Biomedical Engineering"
	cases := []struct{ name, org, want string }{
		{"Yablonskiy Lab", "Mallinckrodt Institute of Radiology, WashU", "Yablonskiy Lab · WashU"},
		{"Chen Ultrasound Laboratory", washu, "Chen Lab · WashU"},
		{"Jianmin Cui Lab", washu, "Cui Lab · WashU"},
		{"CIMED — Center for Investigation of Membrane Excitability Diseases", "Washington University in St. Louis", "CIMED · WashU"},
		{"Rutz Bioelectronics Lab", washu, "Rutz Lab · WashU"},
		{"Huebsch Lab — Biomaterials and Tissue Engineering", washu, "Huebsch Lab · WashU"},
		{"Chao Zhou Lab", washu, "Zhou Lab · WashU"},
		{"Guilak Laboratory", "Washington University in St. Louis, Orthopaedic Surgery", "Guilak Lab · WashU"},
		{"WashU Center for Engineering MechanoBiology", "Washington University in St. Louis", "Center for Engineering MechanoBiology · WashU"},
		{"Setton Laboratory", washu, "Setton Lab · WashU"},
		{"Berkland Lab — Therapeutics and Biomaterials", washu, "Berkland Lab · WashU"},
		{"Rudra Lab — Immunoengineering & Materials Immunology", washu, "Rudra Lab · WashU"},
		{"Bhatia Lab", "Massachusetts Institute of Technology, Koch Institute", "Bhatia Lab · MIT"},
		{"Synthetic Neurobiology Group", "MIT Media Lab", "Synthetic Neurobiology Group · MIT"},
		{"Deisseroth Lab", "Stanford University", "Deisseroth Lab · Stanford"},
		{"Lab of Tissue Mechanics", "Universität Wien, Physics", "Lab of Tissue Mechanics · Universität Wien"},
		{"Whitesides Research Group", "Harvard University", "Whitesides Lab · Harvard"},
		{"Molecular Imaging Lab", "", "Molecular Imaging Lab"},
	}
	for _, c := range cases {
		got := LabLabel(Seed{Class: SeedLab, Name: c.name, Org: c.org})
		if got != c.want {
			t.Errorf("LabLabel(%q, %q) = %q, want %q", c.name, c.org, got, c.want)
		}
	}
	// an owner label overrides the rule; a company keeps its name; a nameless seed shows its id
	if got := LabLabel(Seed{Class: SeedLab, Name: "Chen Ultrasound Laboratory", Org: washu, Label: "Hong Chen Lab"}); got != "Hong Chen Lab" {
		t.Fatalf("override = %q", got)
	}
	if got := LabLabel(Seed{Class: SeedCompany, Name: "Hyperfine", Org: "Guilford, CT"}); got != "Hyperfine" {
		t.Fatalf("company = %q", got)
	}
	if got := LabLabel(Seed{ID: "seed/lab-x", Class: SeedLab}); got != "seed/lab-x" {
		t.Fatalf("nameless = %q", got)
	}
}

// The label rides the seed row as [label:: …], projects as Display, and
// survives a parse → serialize round trip.
func TestSeedLabelRoundTrip(t *testing.T) {
	doc := ParseSeeds("- [id:: seed/lab-chen-ultrasound-laboratory] [class:: lab] [name:: Chen Ultrasound Laboratory] [org:: Washington University in St. Louis] [url:: https://chenultrasoundlab.wustl.edu/] [label:: Hong Chen Lab]\n")
	seeds := doc.Seeds()
	if len(seeds) != 1 || seeds[0].Label != "Hong Chen Lab" || seeds[0].Display != "Hong Chen Lab" {
		t.Fatalf("seeds = %+v", seeds)
	}
	if got := SerializeSeeds(ParseSeeds(SerializeSeeds(doc))); got != SerializeSeeds(doc) {
		t.Fatalf("round trip drifted:\n%s\n%s", got, SerializeSeeds(doc))
	}
	// without a label the display is derived
	plain := ParseSeeds("- [id:: seed/lab-jianmin-cui-lab] [class:: lab] [name:: Jianmin Cui Lab] [org:: Washington University in St. Louis, Biomedical Engineering]\n").Seeds()
	if plain[0].Display != "Cui Lab · WashU" || plain[0].Label != "" {
		t.Fatalf("derived = %+v", plain[0])
	}
}
