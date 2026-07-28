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
	return NewStore(idx, dir, "goals.md", testWrite), dir
}

func TestSyncChecks(t *testing.T) {
	st, _ := tempStore(t, "# Goals\n\n## Aion\n\n### Rocks (90-day)\n"+
		"- [ ] Rock [goal:: aion/rock] [quarter:: 2026-Q3]\n"+
		"    - [ ] Stage\n"+
		"        - [ ] Task [goal:: aion/rock/stage/task]\n")

	// task-substrate split: task depth is frozen history — a legacy daily
	// [goal:: task-id] tick MISSES (→ approvals note upstream), never a guess.
	missed := st.SyncChecks(map[string]bool{"aion/rock/stage/task": true, "aion/gone": true}, jul15)
	if !missed["aion/gone"] || !missed["aion/rock/stage/task"] {
		t.Fatalf("missed set wrong: %+v", missed)
	}
	// stage-level ids still sync (stages remain live goals)
	missed = st.SyncChecks(map[string]bool{"aion/rock/stage": true}, jul15)
	if len(missed) != 0 {
		t.Fatalf("stage sync missed: %+v", missed)
	}
	doc := st.Load()
	if _, stg := doc.FindGoal("aion/rock/stage"); stg == nil || !stg.Checked {
		t.Fatal("stage not checked via write-back")
	}
	if rock := doc.RockOf("aion/rock/stage"); rock == nil || rock.Moved != "2026-07-15" {
		t.Fatalf("Rock moved not stamped: %+v", rock)
	}
	// the frozen task line survives verbatim through the sync save
	raw, _ := os.ReadFile(st.Path())
	if !strings.Contains(string(raw), "        - [ ] Task [goal:: aion/rock/stage/task]") {
		t.Fatalf("frozen line lost on save:\n%s", raw)
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

// testWrite is the tests' plain write path (prod injects a vaultwriter capability).
func testWrite(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
