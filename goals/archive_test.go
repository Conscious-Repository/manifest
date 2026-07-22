package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/vault"
)

func TestArchiveRoundTrip(t *testing.T) {
	entries := []ArchiveEntry{
		{Area: "Aion", Text: "Series A 15M", GoalID: "aion/series-a-15m", Outcome: "win", Closed: "2026-08-14", Reached: "Term sheet", Serves: "aion/2026"},
		{Area: "Aion", Text: "Consumer MRI", GoalID: "aion/consumer-mri", Outcome: "learn", Closed: "2026-09-30", Reached: "Diligence", Note: "deprioritized behind Series A"},
		{Area: "Home", Text: "Backyard", GoalID: "home/backyard", Outcome: "win", Closed: "2026-08-01", Reached: "Pavers"},
	}
	once := serializeArchive("2026-Q3", entries)
	twice := serializeArchive("2026-Q3", parseArchive(once))
	if once != twice {
		t.Fatalf("archive not a fixpoint:\n--once--\n%s\n--twice--\n%s", once, twice)
	}
	got := parseArchive(once)
	if len(got) != 3 || got[0].Outcome != "win" || got[1].Note != "deprioritized behind Series A" {
		t.Fatalf("parse wrong: %+v", got)
	}
	if !strings.Contains(once, "## Aion") || !strings.Contains(once, "## Home") {
		t.Fatalf("area grouping lost:\n%s", once)
	}
}

func TestCloseGoalArchivesAndRemoves(t *testing.T) {
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	st := NewStore(idx, dir, "goals.md")
	goalsMD := "# Goals\n\n## Aion\n\n### 1-year — 2026\n- [ ] Series A closed [goal:: aion/2026]\n\n### Rocks (90-day)\n" +
		"- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3] [serves:: aion/2026]\n" +
		"    - [x] Soft lead\n" +
		"    - [ ] Term sheet\n"
	if err := os.WriteFile(filepath.Join(dir, "goals.md"), []byte(goalsMD), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only a Rock closes — closing the annual is rejected.
	if err := st.CloseGoal("aion/2026", "win", "", "proof", jul15); err == nil {
		t.Fatal("closing an annual should fail")
	}

	if err := st.CloseGoal("aion/series-a-15m", "win", "", "[[series a data room]]", jul15); err != nil {
		t.Fatal(err)
	}
	// a win with no evidence is refused (§5)
	if err := st.CloseGoal("home/backyard", "win", "", "  ", jul15); err == nil {
		t.Fatal("a win with empty evidence must be refused")
	}
	gm, _ := os.ReadFile(filepath.Join(dir, "goals.md"))
	if strings.Contains(string(gm), "Series A 15M") {
		t.Fatalf("Rock not removed from goals.md:\n%s", gm)
	}
	arch, err := os.ReadFile(filepath.Join(dir, "goals 2026-Q3.md"))
	if err != nil {
		t.Fatalf("archive not created: %v", err)
	}
	for _, want := range []string{
		"# goals 2026-Q3", "## Aion", "Series A 15M",
		"[outcome:: win]", "[closed:: 2026-07-15]", "[reached:: Term sheet]", "[serves:: aion/2026]",
	} {
		if !strings.Contains(string(arch), want) {
			t.Fatalf("archive missing %q:\n%s", want, arch)
		}
	}
	all := st.LoadAllArchives()
	if len(all) != 1 || all[0].Quarter != "2026-Q3" || len(all[0].Entries) != 1 {
		t.Fatalf("LoadAllArchives wrong: %+v", all)
	}
}

func TestMovedEmission(t *testing.T) {
	in := "# Goals\n\n## A\n\n### Rocks (90-day)\n- [ ] Rock [moved:: 2026-07-01]\n"
	out := Serialize(Parse(in))
	if !strings.Contains(out, "[moved:: 2026-07-01]") {
		t.Fatalf("moved not emitted on a Rock:\n%s", out)
	}
	if Serialize(Parse(out)) != out {
		t.Fatalf("moved not a fixpoint:\n%s", out)
	}
}

func TestArchiveEvidenceWikilinkRoundTrip(t *testing.T) {
	// A win's evidence may be a [[wikilink]] (contains ]]). It must survive the
	// archive round-trip even alongside a note, and re-serialize as a fixpoint.
	entries := []ArchiveEntry{
		{Area: "Aion", Text: "Series A", GoalID: "aion/series-a", Outcome: "win",
			Closed: "2026-07-22", Reached: "term sheet", Serves: "aion/2026",
			Note: "closed hot", Evidence: "[[series a data room]]"},
		{Area: "Aion", Text: "Old rock", Outcome: "learn", Closed: "2026-07-01", Note: "pivoted"},
	}
	s := serializeArchive("2026-Q3", entries)
	if !strings.Contains(s, "[evidence:: [[series a data room]]]") {
		t.Fatalf("evidence not emitted intact:\n%s", s)
	}
	got := parseArchive(s)
	if got[0].Evidence != "[[series a data room]]" {
		t.Fatalf("evidence not parsed back: %q\n%s", got[0].Evidence, s)
	}
	if got[0].Note != "closed hot" || got[0].Reached != "term sheet" || got[0].Serves != "aion/2026" {
		t.Fatalf("other fields corrupted by evidence extraction: %+v", got[0])
	}
	if twice := serializeArchive("2026-Q3", got); twice != s {
		t.Fatalf("archive not a fixpoint:\n--s--\n%s\n--twice--\n%s", s, twice)
	}
}
