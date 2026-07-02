package goals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/vault"
)

func tempStore(t *testing.T, goalsMD string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goals.md"), []byte(goalsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewStore(idx, dir, "goals.md"), dir
}

func TestSyncChecks(t *testing.T) {
	st, _ := tempStore(t, "# Goals\n\n## Aion\n\n### Rocks (90-day)\n"+
		"- [ ] Rock [goal:: aion/rock] [quarter:: 2026-Q3]\n"+
		"    - [ ] Stage\n"+
		"        - [ ] Task [goal:: aion/rock/stage/task]\n")

	missed := st.SyncChecks(map[string]bool{"aion/rock/stage/task": true, "aion/gone": true}, jul15)
	if !missed["aion/gone"] || missed["aion/rock/stage/task"] {
		t.Fatalf("missed set wrong: %+v", missed)
	}
	doc := st.Load()
	if _, task := doc.FindGoal("aion/rock/stage/task"); task == nil || !task.Checked {
		t.Fatal("linked task not checked via write-back")
	}
	if rock := doc.RockOf("aion/rock/stage/task"); rock == nil || rock.Moved != "2026-07-15" {
		t.Fatalf("Rock moved not stamped: %+v", rock)
	}
	// 2-way: unticking unchecks it.
	st.SyncChecks(map[string]bool{"aion/rock/stage/task": false}, jul15)
	if _, task := st.Load().FindGoal("aion/rock/stage/task"); task.Checked {
		t.Fatal("2-way uncheck failed")
	}
}

func TestCarryGoal(t *testing.T) {
	st, _ := tempStore(t, "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Rock [goal:: aion/rock] [quarter:: 2026-Q2]\n")
	if err := st.CarryGoal("aion/rock", jul15); err != nil {
		t.Fatal(err)
	}
	_, g := st.Load().FindGoal("aion/rock")
	if g.Quarter != "2026-Q3" || g.RolledFrom != "2026-Q2" {
		t.Fatalf("carry wrong: quarter=%s rolledFrom=%s", g.Quarter, g.RolledFrom)
	}
	if err := st.CarryGoal("aion/not-a-rock", jul15); err == nil {
		t.Fatal("carrying a non-Rock should fail")
	}
}

func TestSaveRetro(t *testing.T) {
	st, dir := tempStore(t, "# Goals\n")
	if err := st.SaveRetro("2026-Q3", "ship faster", "long meetings", "morning focus"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "goals 2026-Q3 review.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# goals 2026-Q3 review", "## Start", "ship faster", "## Stop", "long meetings", "## Keep", "morning focus"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("retro missing %q:\n%s", want, b)
		}
	}
}
