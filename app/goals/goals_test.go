package goals

import (
	"strings"
	"testing"
)

func TestSeedRoundTrip(t *testing.T) {
	s := Serialize(seedDoc())
	if got := Serialize(Parse(s)); got != s {
		t.Fatalf("seed not idempotent:\n--want--\n%s\n--got--\n%s", s, got)
	}
	// Seeded areas should be present with their horizon scaffolding.
	for _, name := range []string{"Aion", "OODA Group", "House", "Personal", "Sidequests"} {
		if !strings.Contains(s, "## "+name) {
			t.Fatalf("seed missing area %q:\n%s", name, s)
		}
	}
	if !strings.Contains(s, "### 90-day") || !strings.Contains(s, "### 30-day") {
		t.Fatalf("seed missing horizons:\n%s", s)
	}
}

func TestFixpoint(t *testing.T) {
	inputs := []string{
		"# Goals\n\n## Aion\n> North Star: Bring aging under biomedical control.\n\n### 90-day\n- [ ] Go/no-go on IPR/ICR [owner:: me] [due:: 2026-09-15]\n- [x] Animal data package [owner:: team]\n\n### 30-day\n- [ ] Draft Murugan/Picard contract [owner:: me] [due:: 2026-07-14]\n",
		"# Goals\n## Sidequests\n- [ ] Build a thing\n* [ ] Star bullet [priority:: high]\n",
		"# Goals\n\n## Messy\n> just a quote\nsome prose line\n### backlog\n- [ ] x [foo:: bar] [owner:: Olga]\n",
		"",
	}
	for _, in := range inputs {
		once := Serialize(Parse(in))
		twice := Serialize(Parse(once))
		if once != twice {
			t.Fatalf("fixpoint failed for %q:\n--once--\n%s\n--twice--\n%s", in, once, twice)
		}
	}
}

func TestUnknownFieldAndProseSurvive(t *testing.T) {
	in := "# Goals\n\n## Aion\n\n### 90-day\n- [ ] Goal one [owner:: me] [priority:: high]\nthis is prose the app did not write\n"
	out := Serialize(Parse(in))
	if !strings.Contains(out, "[priority:: high]") {
		t.Fatalf("unknown field lost:\n%s", out)
	}
	if !strings.Contains(out, "this is prose the app did not write") {
		t.Fatalf("prose lost:\n%s", out)
	}
}

func TestOwnerFiltering(t *testing.T) {
	in := "# Goals\n\n## A\n\n### 30-day\n- [ ] mine [owner:: me]\n- [ ] implicit\n- [ ] teamwork [owner:: team]\n- [ ] olga [owner:: Olga]\n- [x] done mine [owner:: me]\n"
	var texts []string
	for _, g := range Parse(in).MyPlate() {
		for _, it := range g.Items {
			texts = append(texts, it.Text)
		}
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "mine") || !strings.Contains(joined, "implicit") {
		t.Fatalf("me/implicit items missing: %v", texts)
	}
	if strings.Contains(joined, "teamwork") || strings.Contains(joined, "olga") {
		t.Fatalf("non-me items leaked into My Plate: %v", texts)
	}
	if strings.Contains(joined, "done mine") {
		t.Fatalf("checked item leaked into My Plate: %v", texts)
	}
}

func TestDueSort(t *testing.T) {
	in := "# Goals\n\n## A\n\n### 30-day\n- [ ] later [owner:: me] [due:: 2026-09-01]\n- [ ] nodue [owner:: me]\n- [ ] sooner [owner:: me] [due:: 2026-07-01]\n"
	items := Parse(in).MyPlate()[0].Items
	if items[0].Text != "sooner" || items[1].Text != "later" || items[2].Text != "nodue" {
		t.Fatalf("due sort wrong (want sooner,later,nodue): %+v", items)
	}
}

func TestMutationsAndIsolation(t *testing.T) {
	doc := Parse("# Goals\n\n## Aion\n> North Star: A\n\n### 90-day\n- [ ] Existing [owner:: me]\n\n## House\n> North Star: B\n")
	houseBefore := Serialize(doc)
	houseIdx := strings.Index(houseBefore, "## House")

	g, ok := doc.AddGoal("Aion", H30, "New goal", "me", "2026-07-14")
	if !ok || g == nil {
		t.Fatal("AddGoal failed")
	}
	doc.assignIDs()
	out := Serialize(doc)
	if !strings.Contains(out, "### 30-day") || !strings.Contains(out, "- [ ] New goal [owner:: me] [due:: 2026-07-14]") {
		t.Fatalf("added goal not serialized:\n%s", out)
	}
	// Editing Aion must not change the House section.
	if got := out[strings.Index(out, "## House"):]; got != houseBefore[houseIdx:] {
		t.Fatalf("House section changed by an Aion edit:\n--before--\n%s\n--after--\n%s", houseBefore[houseIdx:], got)
	}

	if _, gg := doc.FindGoal(g.ID); gg == nil {
		t.Fatalf("goal not addressable by id %q", g.ID)
	}
	doc.CheckGoal(g.ID, true)
	if !strings.Contains(Serialize(doc), "- [x] New goal") {
		t.Fatal("CheckGoal did not persist")
	}
}
