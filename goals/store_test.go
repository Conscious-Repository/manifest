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

func TestCaptureTask(t *testing.T) {
	seed := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n" +
		"- [ ] Rock [goal:: aion/rock] [quarter:: 2026-Q3]\n" +
		"    - [ ] Stage [goal:: aion/rock/stage]\n" +
		"        - [ ] Existing task\n"
	st, dir := tempStore(t, seed)

	// Capture a new task under the stage.
	text, id, err := st.CaptureTask("aion/rock/stage", "  Lee sync  ", jul15)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if text != "Lee sync" || id != "aion/rock/stage/lee-sync" {
		t.Fatalf("got text=%q id=%q", text, id)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "goals.md"))
	if !strings.Contains(string(b), "        - [ ] Lee sync [goal:: aion/rock/stage/lee-sync]") {
		t.Fatalf("task line missing durable id:\n%s", b)
	}
	if !strings.Contains(string(b), "[moved:: 2026-07-15]") {
		t.Fatalf("rock moved not stamped:\n%s", b)
	}
	// Byte-stability: the file is a serialize fixpoint.
	if got := Serialize(Parse(string(b))); got != string(b) {
		t.Fatalf("file is not a serialize fixpoint after capture:\ngot:\n%s\nfile:\n%s", got, b)
	}

	// Dedupe: same text (case/space-insensitive) reuses the task — one line, same id.
	_, id2, err := st.CaptureTask("aion/rock/stage", "lee SYNC", jul15)
	if err != nil || id2 != id {
		t.Fatalf("dedupe: id2=%q err=%v", id2, err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "goals.md"))
	if n := strings.Count(string(b), "Lee sync"); n != 1 {
		t.Fatalf("dedupe wrote %d lines", n)
	}

	// Dedupe-promote: an existing id-less open task gains a durable id.
	_, id3, err := st.CaptureTask("aion/rock/stage", "Existing task", jul15)
	if err != nil || id3 != "aion/rock/stage/existing-task" {
		t.Fatalf("promote-on-dedupe: id3=%q err=%v", id3, err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "goals.md"))
	if !strings.Contains(string(b), "- [ ] Existing task [goal:: aion/rock/stage/existing-task]") {
		t.Fatalf("existing task not promoted:\n%s", b)
	}

	// Depth guard: a rock id, a task id, and an unknown id are all refused.
	for _, bad := range []string{"aion/rock", "aion/rock/stage/lee-sync", "aion/nope"} {
		if _, _, err := st.CaptureTask(bad, "x", jul15); err == nil {
			t.Fatalf("capture under %q should be refused", bad)
		}
	}
	// Empty text refused.
	if _, _, err := st.CaptureTask("aion/rock/stage", "   ", jul15); err == nil {
		t.Fatal("empty text should be refused")
	}
}
