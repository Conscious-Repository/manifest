package realestate

import (
	"strings"
	"testing"
	"time"

	"manifest/tasks"
)

// The property `## tasks` section rides the shared to-do line grammar; the
// fixpoint contract is what lets hand edits and app writes coexist.
func TestPropertyTasksFixpoint(t *testing.T) {
	src := strings.Join([]string{
		"- [ ] gutters front + back [added:: 2026-08-01]",
		"- [ ] pull electrical permit [added:: 2026-08-02] [owner:: acme-gc] [rank:: 2]",
		"a hand-written context line",
		"- [x] order windows [added:: 2026-07-20] [done:: 2026-08-01] [work:: rough-in/order-windows]",
		"- [ ] gutters front + back [todo:: gutters-2] [added:: 2026-08-05]",
	}, "\n") + "\n"
	list := ParsePropertyTasks(strings.Split(strings.TrimRight(src, "\n"), "\n"))
	if out := EmitPropertyTasks(list); out != src {
		t.Fatalf("fixpoint broken:\n--- in ---\n%s--- out ---\n%s", src, out)
	}
	// a hand-written field order normalizes ONCE, then stays byte-stable
	hand := []string{"- [x] order windows [work:: w1] [added:: 2026-07-20]"}
	once := EmitPropertyTasks(ParsePropertyTasks(hand))
	twice := EmitPropertyTasks(ParsePropertyTasks(strings.Split(strings.TrimRight(once, "\n"), "\n")))
	if once != twice {
		t.Fatalf("normalize-once broken:\n%q\n%q", once, twice)
	}
	items := list.Tasks()
	if len(items) != 4 {
		t.Fatalf("want 4 todos, got %d", len(items))
	}
	if items[0].ID != "gutters-front-back" {
		t.Fatalf("derived id: %q", items[0].ID)
	}
	if items[1].Owner != "acme-gc" || items[1].RankN() != 2 {
		t.Fatalf("owner/rank: %+v", items[1])
	}
	if items[2].FieldValue("work") != "rough-in/order-windows" {
		t.Fatalf("work tether lost: %+v", items[2])
	}
	// explicit [todo:: id] pin wins over the colliding text slug
	if items[3].ID != "gutters-2" {
		t.Fatalf("explicit pin: %q", items[3].ID)
	}
	if list.Find("gutters-front-back") != items[0] {
		t.Fatal("Find by derived id")
	}
}

// Migration copies OPEN work todos with a [work::] back-tether, skips checked
// ones and already-tethered ids (idempotent), and never touches ## work.
func TestMigrateWorkTasks(t *testing.T) {
	p := Property{Work: ParseWork([]string{
		"- [ ] Rough-in",
		"    - [x] Rough plumbing",
		"    - [ ] Rough electrical",
		"    - [ ] Order windows",
	})}
	list := ParsePropertyTasks([]string{
		"- [ ] order windows [work:: rough-in/order-windows] [added:: 2026-08-01]",
	})
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	next, added := MigrateWorkTasks(p, list, now)
	if len(added) != 1 || added[0] != "Rough electrical" {
		t.Fatalf("added = %v", added)
	}
	items := next.Tasks()
	last := items[len(items)-1]
	if last.FieldValue("work") != "rough-in/rough-electrical" || last.Added != "2026-08-09" {
		t.Fatalf("migrated line: %+v", last)
	}
	// re-running adds nothing
	if _, again := MigrateWorkTasks(p, next, now); len(again) != 0 {
		t.Fatalf("not idempotent: %v", again)
	}
	_ = tasks.EmitLine(last) // the migrated line emits through the shared grammar
}
