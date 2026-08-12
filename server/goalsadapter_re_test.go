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
// aion/ rocks over the aion backlog): mine + open only, TodoID "re:<id>".
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
	if got.Text != "call the bank about the term sheet" || len(got.TodoID) < 4 || got.TodoID[:3] != "re:" {
		t.Fatalf("offer wrong: %+v", got)
	}
}
