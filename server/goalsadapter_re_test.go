package server

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/aion"
	"manifest/goals"
	"manifest/vault"
)

// An ooda-group/ rock's day-focus offers come from the RE backlog (mirroring
// aion/ rocks over the aion backlog): mine + open only, TaskID "re:<id>".
func TestGoalsAdapterReRockOffersBacklogTasks(t *testing.T) {
	dir := t.TempDir()
	goalsMD := "# Goals\n\n## Real Estate\n\n### Rocks (90-day)\n" +
		"- [ ] Fund I closed [goal:: ooda-group/fund-i-closed] [quarter:: 2026-Q3]\n" +
		"    - [ ] Term sheet out\n" +
		"    - [ ] Wires in\n"
	if err := os.WriteFile(filepath.Join(dir, "goals.md"), []byte(goalsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	gs := goals.NewStore(idx, dir, "goals.md", testWrite)

	if err := os.MkdirAll(filepath.Join(dir, "system", "realestate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system", "realestate", "backlog.md"), []byte(aion.REBacklogSeed), 0o644); err != nil {
		t.Fatal(err)
	}
	re := aion.NewStore(dir, "system/realestate", func(abs string, data []byte) error {
		return os.WriteFile(abs, data, 0o644)
	})
	add := func(text, rock, owner, status string) {
		t.Helper()
		if err := re.AddItem(&aion.BacklogItem{
			Kind: aion.KindTask, Text: text, Rock: rock, Owner: owner,
			Status: status, Captured: "2026-08-12",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("call the bank about the term sheet", "ooda-group/fund-i-closed", "BA", aion.StatusOpen)
	add("someone else's task", "ooda-group/fund-i-closed", "BF", aion.StatusOpen)
	add("done already", "ooda-group/fund-i-closed", "BA", aion.StatusDone)
	add("other rock", "ooda-group/other", "BA", aion.StatusOpen)

	a := NewGoalsAdapter(gs, nil, nil, re, "BA")
	res, ok := a.ResolveFocus("ooda-group/fund-i-closed", "")
	if !ok {
		t.Fatal("rock not resolved")
	}
	if len(res.Tasks) != 1 {
		t.Fatalf("offers = %+v, want exactly the one mine+open task", res.Tasks)
	}
	got := res.Tasks[0]
	if got.Text != "call the bank about the term sheet" || len(got.TaskID) < 4 || got.TaskID[:3] != "re:" {
		t.Fatalf("offer wrong: %+v", got)
	}
}

// The 2026-08-24 miss: the extractor tethers a task to whichever ladder node
// it matched — a MILESTONE id, an alias, the plain title — and the picker's
// exact rock-id equality dropped all of them. Every dialect must offer.
func TestGoalsAdapterOffersTasksTetheredAnywhereOnTheLadder(t *testing.T) {
	dir := t.TempDir()
	goalsMD := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n" +
		"- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3] [alias:: fundraising]\n" +
		"    - [ ] Resources for raise ready [goal:: aion/series-a-15m/soft-lead-identified]\n" +
		"    - [ ] Soft lead identified\n"
	if err := os.WriteFile(filepath.Join(dir, "goals.md"), []byte(goalsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	gs := goals.NewStore(idx, dir, "goals.md", testWrite)
	if err := os.MkdirAll(filepath.Join(dir, "system", "aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system", "aion", "backlog.md"), []byte(aion.SeedFiles["backlog.md"]), 0o644); err != nil {
		t.Fatal(err)
	}
	ai := aion.NewStore(dir, "system/aion", func(abs string, data []byte) error {
		return os.WriteFile(abs, data, 0o644)
	})
	add := func(text, rock string) {
		t.Helper()
		if err := ai.AddItem(&aion.BacklogItem{
			Kind: aion.KindTask, Text: text, Rock: rock, Owner: "BA",
			Status: aion.StatusOpen, Captured: "2026-08-24",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("tethered to the rock id", "aion/series-a-15m")
	add("Finish investor portal market section", "aion/series-a-15m/soft-lead-identified") // the live miss
	add("tethered via alias", "fundraising")
	add("tethered via title", "Series A 15M")
	add("someone else's rock", "aion/mri-prototype")

	a := NewGoalsAdapter(gs, nil, ai, nil, "BA")
	res, ok := a.ResolveFocus("aion/series-a-15m", "")
	if !ok {
		t.Fatal("rock not resolved")
	}
	texts := map[string]bool{}
	for _, n := range res.Tasks {
		texts[n.Text] = true
	}
	for _, want := range []string{
		"tethered to the rock id",
		"Finish investor portal market section",
		"tethered via alias",
		"tethered via title",
	} {
		if !texts[want] {
			t.Errorf("picker missing %q; offers = %v", want, texts)
		}
	}
	if texts["someone else's rock"] {
		t.Error("a different rock's task leaked into the offers")
	}
}
