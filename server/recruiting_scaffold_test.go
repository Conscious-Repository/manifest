package server

import (
	"strings"
	"testing"
)

// The citation rule is the whole trust model of the model's pass, and it is
// testable without a model: a suggestion survives only what the fetched
// material actually says.

const scaffoldMaterialFixture = `SOURCE: web
URL: https://bme.washu.edu/labs/smith
NAME AS FETCHED: Smith Lab — Biomedical Engineering
FIELD site: bme.washu.edu (https://bme.washu.edu/labs/smith)
PERSON: Jane Smith — Washington University in St. Louis
PAGE TEXT:
The Smith Lab builds low-field MRI hardware in the Department of Biomedical Engineering.`

func TestCitationRuleKeepsOnlyWhatWasFetched(t *testing.T) {
	got, dropped := enforceCitationRule(scaffoldSuggestion{
		Class: "lab",
		Name:  "Smith Lab",
		Org:   "Washington University in St. Louis",
		Title: "Associate Professor of Radiology", // nowhere in the material
		Note:  "A hardware lab worth sweeping for MRI engineers.",
		People: []string{
			"Jane Smith",
			"Robert Nguyen", // the model knows this person; the page does not name them
		},
	}, scaffoldMaterialFixture)

	if got.Name != "Smith Lab" || got.Org != "Washington University in St. Louis" {
		t.Fatalf("supported fields must survive: %+v", got)
	}
	if got.Title != "" {
		t.Fatalf("a title nothing fetched said must be dropped: %q", got.Title)
	}
	if len(got.People) != 1 || got.People[0] != "Jane Smith" {
		t.Fatalf("only named people survive: %+v", got.People)
	}
	if got.Note == "" {
		t.Fatal("the note is the one place the model may write its own sentence")
	}
	if len(dropped) != 2 {
		t.Fatalf("every drop is reported: %+v", dropped)
	}
	joined := strings.Join(dropped, " | ")
	if !strings.Contains(joined, "title") || !strings.Contains(joined, "Robert Nguyen") {
		t.Fatalf("the reasons must name what was dropped: %q", joined)
	}
}

// The check must survive the ordinary ways one string is spelled twice.
func TestCitationRuleNormalizesSpelling(t *testing.T) {
	material := "NAME AS FETCHED: The   Smith  Lab — Biomedical Engineering"
	got, dropped := enforceCitationRule(scaffoldSuggestion{
		Name: "the smith lab - biomedical engineering",
	}, material)
	if got.Name == "" {
		t.Fatalf("case, whitespace runs and dash style are not differences of fact: %+v", dropped)
	}
}

// A class outside the closed set is refused with its reason, not silently
// coerced.
func TestCitationRuleRefusesAnUnknownClass(t *testing.T) {
	got, dropped := enforceCitationRule(scaffoldSuggestion{Class: "university"}, scaffoldMaterialFixture)
	if got.Class != "" {
		t.Fatalf("class: %q", got.Class)
	}
	if len(dropped) != 1 || !strings.Contains(dropped[0], "university") {
		t.Fatalf("dropped: %+v", dropped)
	}
	if ok, _ := enforceCitationRule(scaffoldSuggestion{Class: "LAB"}, scaffoldMaterialFixture); ok.Class != "lab" {
		t.Fatalf("a valid class in the wrong case is still that class: %q", ok.Class)
	}
}

func TestParseScaffoldReply(t *testing.T) {
	fenced := "Here is what I found.\n\n```json\n{\"class\":\"lab\",\"name\":\"Smith Lab\"}\n```\n"
	got, err := parseScaffoldReply(fenced)
	if err != nil || got.Name != "Smith Lab" || got.Class != "lab" {
		t.Fatalf("fenced: %+v %v", got, err)
	}
	bare := `{"name":"Smith Lab","people":["Jane Smith"]}`
	if got, err := parseScaffoldReply("thinking… " + bare); err != nil || len(got.People) != 1 {
		t.Fatalf("bare: %+v %v", got, err)
	}
	if _, err := parseScaffoldReply("I could not find anything useful."); err == nil {
		t.Fatal("prose with no JSON is an error, not an empty suggestion")
	}
	if _, err := parseScaffoldReply("```json\n{not json}\n```"); err == nil {
		t.Fatal("malformed JSON is an error")
	}
}

// With no runner wired, the ask refuses in words rather than hanging.
func TestScaffoldAskRefusesWithoutARunner(t *testing.T) {
	_, mux := testIntakeServer(t, nil)
	w := sourcesDo(t, mux, "POST", "/api/aion/recruiting/intake/ask", `{"text":"https://bme.washu.edu"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Hermes runner is not enabled") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := sourcesDo(t, mux, "GET", "/api/aion/recruiting/intake/ask/nope", ""); w.Code != 404 {
		t.Fatalf("an unknown job is a 404: %d", w.Code)
	}
}
