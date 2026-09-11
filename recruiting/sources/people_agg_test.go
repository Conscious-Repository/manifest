package sources

import (
	"strings"
	"testing"
)

func mention(paper int, name, byline string, full bool, orcid string, affiliations ...string) authorMention {
	return authorMention{Paper: paper, Name: name, Byline: byline, FullName: full, ORCID: orcid, Affiliations: affiliations, Position: 1, Total: 1}
}

func keys(people []person) string {
	out := make([]string, 0, len(people))
	for _, p := range people {
		s := p.Key.String()
		if p.Key.Ambiguous {
			s += "?"
		}
		out = append(out, s+"×"+itoa(len(p.Mentions)))
	}
	return strings.Join(out, " ")
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n))) }

// The precedence, one rung at a time, on hand-built mentions.
func TestPeopleAggregationPrecedence(t *testing.T) {
	people := aggregatePeople([]authorMention{
		// rung 1: ORCID wins over everything, whatever the name or org says
		mention(0, "Dana M Reyes", "Reyes DM", true, "0000-0001-2345-6789", "University of Aberdeen"),
		mention(1, "Dana Reyes", "Reyes D", true, "0000-0001-2345-6789", "Somewhere Else"),
		// rung 3: full name + org
		mention(0, "Priya Natarajan", "Natarajan P", true, "", "Department of Radiology, University of Aberdeen, UK"),
		mention(2, "Priya Natarajan", "Natarajan P", true, "", "Aberdeen Biomedical Imaging Centre, The University of Aberdeen"),
		// same full name, different org: a different key, no merge
		mention(3, "Priya Natarajan", "Natarajan P", true, "", "University of Oxford"),
		// rung 4: initials only — one row per byline+org, never merged up
		mention(1, "Reyes DM", "Reyes DM", false, "", "University of Aberdeen"),
		mention(2, "Reyes DM", "Reyes DM", false, "", "University of Aberdeen"),
		mention(3, "Reyes DM", "Reyes DM", false, "", "University of Oxford"),
		// rung 4: a full name with no affiliation at all
		mention(4, "P James Ross", "Ross PJ", true, ""),
		mention(4, "P James Ross", "Ross PJ", true, ""),
	})
	want := "orcid/0000-0001-2345-6789×2 name/priya natarajan/university of aberdeen×2 name/priya natarajan/university of oxford×1 byline/reyes dm/university of aberdeen?×2 byline/reyes dm/university of oxford?×1 name/p james ross/?×2"
	if got := keys(people); got != want {
		t.Fatalf("keys:\n got %s\nwant %s", got, want)
	}
	if people[0].Name != "Dana M Reyes" || people[0].Org != "University of Aberdeen" || people[0].ORCID != "0000-0001-2345-6789" {
		t.Errorf("the first mention keeps the last word on display facts: %+v", people[0])
	}
	if !strings.Contains(people[3].Key.Reason, "initials only (Reyes DM)") || !strings.Contains(people[5].Key.Reason, "no affiliation") {
		t.Errorf("reasons: %q / %q", people[3].Key.Reason, people[5].Key.Reason)
	}
}

// The one bridge: a name+org key adopts the ORCID it coincides with when
// there is exactly one; two ORCIDs behind one name+org is a namesake pair
// and nobody bridges. Initials never bridge.
func TestPeopleAggregationBridgesExactlyOneStrongerKey(t *testing.T) {
	people := aggregatePeople([]authorMention{
		mention(0, "Samuel Okafor", "Okafor S", true, "", "University of Oxford"),
		mention(1, "Samuel Okafor", "Okafor S", true, "0000-0003-4444-5555", "Institute of Biomedical Engineering, University of Oxford"),
		mention(2, "Samuel Okafor", "Okafor S", true, "", "University of Oxford"),
		mention(2, "Okafor S", "Okafor S", false, "", "University of Oxford"),
	})
	if got := keys(people); got != "orcid/0000-0003-4444-5555×3 byline/okafor s/university of oxford?×1" {
		t.Fatalf("bridge: %s", got)
	}
	if people[0].ORCID != "0000-0003-4444-5555" || people[0].Org != "University of Oxford" {
		t.Errorf("the ORCID rides the bridged row: %+v", people[0])
	}

	// two ORCIDs, one name+org: the ORCID-less mention stays its own row
	people = aggregatePeople([]authorMention{
		mention(0, "Wei Wang", "Wang W", true, "0000-0001-0000-0001", "Peking University"),
		mention(1, "Wei Wang", "Wang W", true, "0000-0001-0000-0002", "Peking University"),
		mention(2, "Wei Wang", "Wang W", true, "", "Peking University"),
	})
	if got := keys(people); got != "orcid/0000-0001-0000-0001×1 orcid/0000-0001-0000-0002×1 name/wei wang/peking university×1" {
		t.Fatalf("namesakes: %s", got)
	}
}

// Rung 2 exists for a caller that resolved an OpenAlex author id
// deterministically; PubMed never supplies one. It sits below ORCID and
// above the name, and bridges up to an ORCID the same way.
func TestPeopleAggregationOpenAlexRung(t *testing.T) {
	a := mention(0, "Guang Yu", "Yu G", true, "", "Example University")
	a.OpenAlexID = "A1234"
	b := mention(1, "Guang Yu", "Yu G", true, "0000-0002-0000-0002", "Example University")
	b.OpenAlexID = "A1234"
	c := mention(2, "Guang Yu", "Yu G", true, "", "Example University")
	d := mention(3, "G Yu", "Yu G", true, "", "Other University")
	d.OpenAlexID = "A9999"
	people := aggregatePeople([]authorMention{a, b, c, d})
	if got := keys(people); got != "orcid/0000-0002-0000-0002×3 openalex/A9999×1" {
		t.Fatalf("openalex rung: %s", got)
	}
	if primaryKey(a).Kind != keyOpenAlex || primaryKey(b).Kind != keyORCID || primaryKey(c).Kind != keyName {
		t.Errorf("precedence: %v %v %v", primaryKey(a), primaryKey(b), primaryKey(c))
	}
}

func TestPositionLabel(t *testing.T) {
	for _, c := range []struct {
		pos, total int
		want       string
	}{{1, 1, "sole"}, {1, 3, "first"}, {2, 3, "middle"}, {3, 3, "last"}, {1, 2, "first"}, {2, 2, "last"}, {0, 0, "sole"}} {
		if got := (authorMention{Position: c.pos, Total: c.total}).positionLabel(); got != c.want {
			t.Errorf("%d of %d = %q want %q", c.pos, c.total, got, c.want)
		}
	}
}

func TestForenameIsFull(t *testing.T) {
	for _, c := range []struct {
		fore, initials string
		want           bool
	}{
		{"Dana M", "DM", true}, {"Dana", "D", true}, {"Wu", "W", true}, {"Li", "L", true}, {"P James", "PJ", true},
		{"Jean-Pierre", "JP", true}, {"D M", "DM", false}, {"D.M.", "DM", false}, {"DM", "DM", false}, {"J-P", "JP", false},
		{"D", "D", false}, {"", "", false}, {"", "DM", false}, {"D M", "", false},
	} {
		if got := forenameIsFull(c.fore, c.initials); got != c.want {
			t.Errorf("forenameIsFull(%q, %q) = %v want %v", c.fore, c.initials, got, c.want)
		}
	}
}

func TestOrgToken(t *testing.T) {
	for in, want := range map[string]string{
		"Department of Radiology, University of Aberdeen, Aberdeen, UK.":            "university of aberdeen",
		"Aberdeen Biomedical Imaging Centre, University of Aberdeen, Aberdeen, UK.": "university of aberdeen",
		"The University of Aberdeen, Aberdeen, UK":                                  "university of aberdeen",
		"Institut für Radiologie, Universität Heidelberg, Germany":                  "universität heidelberg",
		"Institute of Biomedical Engineering, University of Oxford, Oxford, UK.":    "university of oxford",
		"RIKEN Center for Biosystems Dynamics Research, Kobe, Japan.":               "riken center for biosystems dynamics research",
		"Acme Therapeutics Inc, Boston, MA, USA":                                    "acme therapeutics inc",
		"Division of Cardiology; Massachusetts General Hospital; Boston":            "massachusetts general hospital",
		"Department of Physics, Kobe, Japan":                                        "department of physics",
		"Kobe, Japan":                                                               "kobe",
		"":                                                                          "",
		"   ,  ":                                                                    "",
	} {
		if got := orgToken(in); got != want {
			t.Errorf("orgToken(%q) = %q want %q", in, got, want)
		}
	}
	if got := orgDisplay("Department of Radiology, University of Aberdeen, Aberdeen, UK."); got != "University of Aberdeen" {
		t.Errorf("orgDisplay: %q", got)
	}
	if got := orgDisplay("Kobe, Japan."); got != "Kobe" {
		t.Errorf("orgDisplay bare: %q", got)
	}
}

// Email-shaped text leaves; everything else stays as printed.
func TestStripAddresses(t *testing.T) {
	for in, want := range map[string]string{
		"Department of Radiology, University of Aberdeen, Aberdeen, UK. Electronic address: dana@example.test.": "Department of Radiology, University of Aberdeen, Aberdeen, UK.",
		"University of Oxford, Oxford, UK. samuel.okafor@example.test":                                          "University of Oxford, Oxford, UK.",
		"University of Aberdeen, Aberdeen, UK. E-mail: d.lurie@example.test":                                    "University of Aberdeen, Aberdeen, UK.",
		"Email: a@b.org, University X, Town":                                                                    "University X, Town",
		"Lab of Y (contact: <y@lab.example>), University Z":                                                     "Lab of Y (contact: ), University Z",
		"University of Nowhere":                   "University of Nowhere",
		"someone@example.test":                    "",
		"@mention without a domain, University Q": "@mention without a domain, University Q",
		"": "",
	} {
		got := stripAddresses(in)
		if got != want {
			t.Errorf("stripAddresses(%q) = %q want %q", in, got, want)
		}
		if containsAddress(got) {
			t.Errorf("stripAddresses(%q) still carries an address: %q", in, got)
		}
	}
}

func TestPubMedORCID(t *testing.T) {
	for in, want := range map[string]string{
		"0000-0001-2345-6789":                   "0000-0001-2345-6789",
		"https://orcid.org/0000-0001-2345-678X": "0000-0001-2345-678X",
		"http://orcid.org/0000-0001-2345-678x":  "0000-0001-2345-678X",
		"0000000123456789":                      "0000-0001-2345-6789",
		"  0000-0001-2345-6789 ":                "0000-0001-2345-6789",
		"0000-0001-2345":                        "",
		"not an orcid":                          "",
		"":                                      "",
	} {
		if got := pubmedORCID(in); got != want {
			t.Errorf("pubmedORCID(%q) = %q want %q", in, got, want)
		}
	}
}
