package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/vault"
)

// jul15 is a fixed clock in 2026-Q3 for deterministic migration tests.
var jul15 = time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)

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
	if !strings.Contains(s, "### 1-year") || !strings.Contains(s, "### Rocks (90-day)") {
		t.Fatalf("seed missing the horizon sections:\n%s", s)
	}
	if strings.Contains(s, "### 90-day\n") || strings.Contains(s, "### 30-day") {
		t.Fatalf("seed must not emit legacy cascade headings:\n%s", s)
	}
}

func TestFixpoint(t *testing.T) {
	inputs := []string{
		"# Goals\n\n## Aion\n> North Star: Bring aging under complete biomedical control.\n\n### 1-year — 2026\n" +
			"- [ ] Series A closed + first program in vivo [goal:: aion/2026]\n\n### Rocks (90-day)\n" +
			"- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3] [serves:: aion/2026]\n" +
			"    - [x] Soft lead identified\n" +
			"    - [ ] Term sheet [status:: at-risk]\n" +
			"        - [ ] Send updated deck\n",
		"# Goals\n## Sidequests\n- [ ] loose todo with no section\n* [ ] star bullet [priority:: high]\n",
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

// TestRockStageTaskDepth pins the literal depth rule: under a Rock, one level is a
// stage and two is a task; a lone checkbox under a Rock parses as a (small) stage.
func TestRockStageTaskDepth(t *testing.T) {
	in := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n" +
		"- [ ] Series A 15M\n" +
		"    - [ ] Term sheet\n" +
		"        - [ ] Send deck\n" +
		"- [ ] Lonely rock\n" +
		"    - [ ] just one checkbox\n"
	a := Parse(in).FindArea("Aion")
	if a == nil || len(a.Rocks) != 2 {
		t.Fatalf("want 2 Rocks, got %+v", a)
	}
	rock := a.Rocks[0]
	if len(rock.Children) != 1 || rock.Children[0].Text != "Term sheet" {
		t.Fatalf("Rock should own one stage, got %+v", rock.Children)
	}
	if len(rock.Children[0].Children) != 1 || rock.Children[0].Children[0].Text != "Send deck" {
		t.Fatalf("stage should own one task, got %+v", rock.Children[0].Children)
	}
	// The lone checkbox under the second Rock is a stage, not lost.
	if len(a.Rocks[1].Children) != 1 || a.Rocks[1].Children[0].Text != "just one checkbox" {
		t.Fatalf("lone checkbox under a Rock must parse as a stage: %+v", a.Rocks[1])
	}
}

// TestFieldEmission checks the §1 canonical emission rules.
func TestFieldEmission(t *testing.T) {
	in := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n" +
		"- [ ] Rock A [quarter:: 2026-Q3] [serves:: aion/2026] [status:: active] [owner:: me]\n" +
		"- [ ] Rock B [status:: blocked] [owner:: team] [rolled-from:: 2026-Q2]\n" +
		"    - [ ] a stage [owner:: me]\n"
	out := Serialize(Parse(in))
	// Rock A: goal (identity) + quarter + serves; status active and owner me are dropped.
	if !strings.Contains(out, "[goal:: aion/rock-a] [quarter:: 2026-Q3] [serves:: aion/2026]") {
		t.Fatalf("Rock A emission wrong:\n%s", out)
	}
	if strings.Contains(out, "status:: active") {
		t.Fatalf("active status must not be written:\n%s", out)
	}
	// Rock B: blocked status + team owner + rolled-from kept.
	if !strings.Contains(out, "[status:: blocked]") || !strings.Contains(out, "[rolled-from:: 2026-Q2]") || !strings.Contains(out, "[owner:: team]") {
		t.Fatalf("Rock B emission wrong:\n%s", out)
	}
	// The stage's owner==me is dropped, and a stage gets no quarter/goal by default.
	if strings.Contains(out, "a stage [owner:: me]") || strings.Contains(out, "a stage [goal::") {
		t.Fatalf("stage should be bare:\n%s", out)
	}
}

func TestDueRetired(t *testing.T) {
	in := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Rock [due:: 2026-09-30] [priority:: high]\n"
	out := Serialize(Parse(in))
	if strings.Contains(out, "due::") {
		t.Fatalf("due:: must be dropped on save:\n%s", out)
	}
	if !strings.Contains(out, "[priority:: high]") {
		t.Fatalf("unknown fields must still survive:\n%s", out)
	}
}

func TestNeedsMigration(t *testing.T) {
	legacy := "# Goals\n## Aion\n### 90-day\n- [ ] x\n"
	legacyDue := "# Goals\n## Aion\n### Rocks (90-day)\n- [ ] x [due:: 2026-01-01]\n"
	fresh := "# Goals\n## Aion\n### 1-year — 2026\n### Rocks (90-day)\n- [ ] x [quarter:: 2026-Q3]\n"
	if !needsMigration(legacy) || !needsMigration(legacyDue) {
		t.Fatal("legacy formats must need migration")
	}
	if needsMigration(fresh) {
		t.Fatal("already-migrated file must not need migration")
	}
}

func TestMigrateFromLegacy(t *testing.T) {
	in := "# Goals\n\n## Aion\n> North Star: Bring aging under complete biomedical control.\n\n### 90-day\n" +
		"- [ ] Series A 15M [owner:: me] [goal:: aion/series-a-15m] [due:: 2026-09-30]\n" +
		"    - [ ] Soft lead identified [owner:: me]\n" +
		"        - [ ] Shoumik sync\n"
	doc := Parse(in)
	doc.migrateFromLegacy(jul15)
	out := Serialize(doc)

	for _, want := range []string{
		"### 1-year — 2026",
		"### Rocks (90-day)",
		"- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3]",
		"    - [ ] Soft lead identified",
		"        - [ ] Shoumik sync",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("migration missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "due::") {
		t.Fatalf("migration must strip due::\n%s", out)
	}
	if strings.Contains(out, "### 90-day\n") {
		t.Fatalf("legacy 90-day heading must be gone:\n%s", out)
	}
	// Idempotent: re-parsing/serializing is a fixpoint and needs no further migration.
	if needsMigration(out) {
		t.Fatalf("migrated output should not need migration again:\n%s", out)
	}
}

func TestStoreMigrateWritesBackup(t *testing.T) {
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	st := NewStore(idx, dir, "goals.md")
	legacy := "# Goals\n\n## Aion\n\n### 90-day\n- [ ] Ship it [owner:: me] [due:: 2026-09-30]\n"
	path := filepath.Join(dir, "goals.md")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	migrated, err := st.Migrate(jul15)
	if err != nil || !migrated {
		t.Fatalf("Migrate: migrated=%v err=%v", migrated, err)
	}
	// A backup preserving the original bytes must exist.
	if b, err := os.ReadFile(path + ".pre-migration"); err != nil || string(b) != legacy {
		t.Fatalf("backup missing or wrong: err=%v", err)
	}
	// The live file is now the new format.
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "### Rocks (90-day)") || strings.Contains(string(got), "due::") {
		t.Fatalf("migrated file wrong:\n%s", got)
	}
	// Second Migrate is a no-op (already migrated).
	if again, err := st.Migrate(jul15); err != nil || again {
		t.Fatalf("second Migrate should be a no-op: again=%v err=%v", again, err)
	}
}

func TestUnknownFieldAndProseSurvive(t *testing.T) {
	in := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Rock one [priority:: high]\nthis is prose the app did not write\n"
	out := Serialize(Parse(in))
	if !strings.Contains(out, "[priority:: high]") {
		t.Fatalf("unknown field lost:\n%s", out)
	}
	if !strings.Contains(out, "this is prose the app did not write") {
		t.Fatalf("prose lost:\n%s", out)
	}
}

func TestMyPlateAndPool(t *testing.T) {
	in := "# Goals\n\n## A\n\n### Rocks (90-day)\n" +
		"- [ ] mineRock [owner:: me]\n" +
		"    - [ ] mineStage [owner:: me]\n" +
		"        - [ ] mineTask\n" +
		"        - [x] doneTask\n" +
		"- [ ] teamRock [owner:: team]\n"
	doc := Parse(in)

	var plate []string
	for _, g := range doc.MyPlate() {
		for _, it := range g.Items {
			plate = append(plate, it.Text)
		}
	}
	joined := strings.Join(plate, "|")
	for _, want := range []string{"mineRock", "mineStage", "mineTask"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("MyPlate missing %q: %v", want, plate)
		}
	}
	if strings.Contains(joined, "teamRock") || strings.Contains(joined, "doneTask") {
		t.Fatalf("non-me or checked item leaked into My Plate: %v", plate)
	}

	pool := doc.Pool()
	if len(pool) != 1 || pool[0].Text != "mineStage" {
		t.Fatalf("Pool should be the open owner==me stage tier: %+v", pool)
	}
}

func TestAddGoalSections(t *testing.T) {
	doc := Parse("# Goals\n\n## Aion\n### 1-year — 2026\n### Rocks (90-day)\n")
	annual, ok := doc.AddGoal("Aion", "", "annual", "Series A closed", "me")
	if !ok || annual == nil {
		t.Fatal("add annual failed")
	}
	rock, ok := doc.AddGoal("Aion", "", "rock", "Series A 15M", "me")
	if !ok {
		t.Fatal("add rock failed")
	}
	stage, ok := doc.AddGoal("", rock.ID, "", "Term sheet", "me")
	if !ok {
		t.Fatal("add stage failed")
	}
	if _, ok := doc.AddGoal("", stage.ID, "", "Send deck", ""); !ok {
		t.Fatal("add task failed")
	}
	a := doc.FindArea("Aion")
	if len(a.Annuals) != 1 || len(a.Rocks) != 1 {
		t.Fatalf("sections wrong: annuals=%d rocks=%d", len(a.Annuals), len(a.Rocks))
	}
	if len(a.Rocks[0].Children) != 1 || len(a.Rocks[0].Children[0].Children) != 1 {
		t.Fatalf("stage/task nesting wrong: %+v", a.Rocks[0])
	}
	// Round-trips as a fixpoint.
	out := Serialize(doc)
	if Serialize(Parse(out)) != out {
		t.Fatalf("added goals not idempotent:\n%s", out)
	}
}

func TestFinishLineFields(t *testing.T) {
	// until/verify on annuals + Rocks + stages; kpi on Rocks + stages; canonical
	// order (goal, quarter, serves, status, rolled-from, moved, until, verify, kpi,
	// owner). A hand-written file re-emits canonically and is a fixpoint after.
	in := "# Goals\n\n## Aion\n\n### 1-year — 2026\n" +
		"- [ ] Big goal [until:: shipped v1] [verify:: users in prod]\n" +
		"\n### Rocks (90-day)\n" +
		"- [ ] Series A [kpi:: LOIs 4/10] [goal:: aion/series-a] [until:: countersigned term sheet >= 15M] [quarter:: 2026-Q3] [verify:: PDF in data room]\n" +
		"    - [ ] LOIs [verify:: 10 signed emails] [kpi:: 4/10]\n"
	doc := Parse(in)
	_, rock := doc.FindGoal("aion/series-a")
	if rock == nil || rock.Until != "countersigned term sheet >= 15M" || rock.Verify != "PDF in data room" || rock.Kpi != "LOIs 4/10" {
		t.Fatalf("rock finish-line fields not lifted: %+v", rock)
	}
	out := Serialize(doc)
	// canonical order: goal before quarter before until before verify before kpi
	wantRock := "- [ ] Series A [goal:: aion/series-a] [quarter:: 2026-Q3] [until:: countersigned term sheet >= 15M] [verify:: PDF in data room] [kpi:: LOIs 4/10]"
	if !strings.Contains(out, wantRock) {
		t.Fatalf("rock not re-emitted in canonical order:\n%s", out)
	}
	if !strings.Contains(out, "- [ ] Big goal [goal:: aion/big-goal] [until:: shipped v1] [verify:: users in prod]") {
		t.Fatalf("annual until/verify wrong:\n%s", out)
	}
	if !strings.Contains(out, "- [ ] LOIs [verify:: 10 signed emails] [kpi:: 4/10]") {
		t.Fatalf("stage verify/kpi wrong:\n%s", out)
	}
	// fixpoint after canonicalization
	if twice := Serialize(Parse(out)); twice != out {
		t.Fatalf("not a fixpoint:\n--out--\n%s\n--twice--\n%s", out, twice)
	}
	// kpi is dropped on an annual (not a valid role for it)
	an := "# Goals\n\n## X\n\n### 1-year — 2026\n- [ ] G [kpi:: 3/5]\n"
	if o := Serialize(Parse(an)); strings.Contains(o, "kpi::") {
		t.Fatalf("kpi should not emit on an annual:\n%s", o)
	}
	// EditGoal strips a `]` from a value so it can't break the field regex
	doc.EditGoal("aion/series-a", GoalEdit{Until: strptr("has a ] bracket")})
	if _, g := doc.FindGoal("aion/series-a"); g.Until != "has a  bracket" {
		t.Fatalf("bracket not stripped: %q", g.Until)
	}
}

func strptr(s string) *string { return &s }
