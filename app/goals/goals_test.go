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
	for _, name := range []string{"Aion", "OODA Group", "House", "Personal", "Sidequests"} {
		if !strings.Contains(s, "## "+name) {
			t.Fatalf("seed missing area %q:\n%s", name, s)
		}
	}
	if !strings.Contains(s, "### 90-day") {
		t.Fatalf("seed missing 90-day cascade section:\n%s", s)
	}
	if strings.Contains(s, "### 30-day") {
		t.Fatalf("cascade seed must not emit a flat 30-day section:\n%s", s)
	}
}

func TestFixpoint(t *testing.T) {
	inputs := []string{
		"# Goals\n\n## Aion\n> North Star: Bring aging under biomedical control.\n\n### 90-day\n- [ ] Series A fundraise [owner:: me] [due:: 2026-09-30]\n    - [ ] Draft deck + diligence materials [owner:: me] [due:: 2026-07-31]\n        - [ ] Intro to Founders Fund\n        - [ ] Call with Lee\n",
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

// The doc §2 cascade example: a 90-day owns one 30-day which owns tasks. Nesting
// is preserved and ids are hierarchical paths.
func TestCascadeNestingAndIDs(t *testing.T) {
	in := "# Goals\n\n## Aion\n> North Star: Bring aging under biomedical control.\n\n### 90-day\n" +
		"- [ ] Series A fundraise [owner:: me] [due:: 2026-09-30]\n" +
		"    - [ ] Draft deck + diligence materials [owner:: me] [due:: 2026-07-31]\n" +
		"        - [ ] Intro to Founders Fund\n" +
		"        - [ ] Call with Lee\n"
	doc := Parse(in)
	if got := Serialize(Parse(Serialize(doc))); got != Serialize(doc) {
		t.Fatalf("cascade not idempotent")
	}
	a := doc.FindArea("Aion")
	if a == nil || len(a.Goals) != 1 {
		t.Fatalf("want one 90-day root, got %+v", a)
	}
	root := a.Goals[0]
	if len(root.Children) != 1 {
		t.Fatalf("90-day should own one 30-day, got %d", len(root.Children))
	}
	m := root.Children[0]
	if len(m.Children) != 2 {
		t.Fatalf("30-day should own 2 tasks, got %d", len(m.Children))
	}
	if root.ID != "aion/series-a-fundraise" {
		t.Fatalf("root id: %s", root.ID)
	}
	if m.ID != "aion/series-a-fundraise/draft-deck-diligence-materials" {
		t.Fatalf("milestone id: %s", m.ID)
	}
	if m.Children[0].ID != "aion/series-a-fundraise/draft-deck-diligence-materials/intro-to-founders-fund" {
		t.Fatalf("task id: %s", m.Children[0].ID)
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

func TestMyPlateAcrossTiers(t *testing.T) {
	in := "# Goals\n\n## A\n\n### 90-day\n- [ ] mine90 [owner:: me]\n    - [ ] mine30 [owner:: me]\n        - [ ] minetask\n        - [x] donetask\n- [ ] team90 [owner:: team]\n"
	var texts []string
	for _, g := range Parse(in).MyPlate() {
		for _, it := range g.Items {
			texts = append(texts, it.Text)
		}
	}
	joined := strings.Join(texts, "|")
	for _, want := range []string{"mine90", "mine30", "minetask"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("MyPlate missing %q across tiers: %v", want, texts)
		}
	}
	if strings.Contains(joined, "team90") {
		t.Fatalf("non-me item leaked into My Plate: %v", texts)
	}
	if strings.Contains(joined, "donetask") {
		t.Fatalf("checked item leaked into My Plate: %v", texts)
	}
}

func TestPoolIs30Day(t *testing.T) {
	in := "# Goals\n\n## A\n\n### 90-day\n- [ ] big [owner:: me]\n    - [ ] step [owner:: me]\n        - [ ] tiny\n"
	pool := Parse(in).Pool()
	if len(pool) != 1 || pool[0].Text != "step" {
		t.Fatalf("Pool should be the 30-day tier only: %+v", pool)
	}
}

func TestAddGoalNestedAndIsolation(t *testing.T) {
	doc := Parse("# Goals\n\n## Aion\n> North Star: A\n\n### 90-day\n- [ ] Existing [owner:: me]\n\n## House\n> North Star: B\n")
	houseBefore := Serialize(doc)
	houseIdx := strings.Index(houseBefore, "## House")

	root, ok := doc.AddGoal("Aion", "", "Series A 15M", "me", "")
	if !ok {
		t.Fatal("add root failed")
	}
	mile, ok := doc.AddGoal("", root.ID, "Draft deck", "me", "2026-07-31")
	if !ok {
		t.Fatal("add 30-day under root failed")
	}
	if _, ok := doc.AddGoal("", mile.ID, "Intro to FF", "", ""); !ok {
		t.Fatal("add task under 30-day failed")
	}
	out := Serialize(doc)
	want := "### 90-day\n- [ ] Existing [owner:: me]\n- [ ] Series A 15M [owner:: me]\n    - [ ] Draft deck [owner:: me] [due:: 2026-07-31]\n        - [ ] Intro to FF\n"
	if !strings.Contains(out, want) {
		t.Fatalf("nested add not serialized as cascade:\n%s", out)
	}
	// Editing Aion must not change the House section.
	if got := out[strings.Index(out, "## House"):]; got != houseBefore[houseIdx:] {
		t.Fatalf("House section changed by an Aion edit:\n--before--\n%s\n--after--\n%s", houseBefore[houseIdx:], got)
	}
	doc.CheckGoal(mile.ID, true)
	if !strings.Contains(Serialize(doc), "- [x] Draft deck") {
		t.Fatal("CheckGoal did not persist on a nested node")
	}
	doc.DeleteGoal(root.ID)
	if strings.Contains(Serialize(doc), "Series A 15M") || strings.Contains(Serialize(doc), "Draft deck") {
		t.Fatal("DeleteGoal must remove the whole subtree")
	}
}
