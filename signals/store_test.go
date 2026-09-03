package signals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The snooze-lapse GC must be judged against the now the reader supplies, never
// the wall clock. Fixed times throughout — no real-clock dependence, so this
// holds regardless of the calendar date the suite runs on.
func TestSnoozeGCUsesCallerNow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // long past real "now"
	until := base.Add(48 * time.Hour)

	if err := store.Snooze("sig", until); err != nil {
		t.Fatal(err)
	}
	// Snooze must survive save even though until < time.Now().
	if !store.Suppressed("sig", "h", base) {
		t.Fatal("snooze honored while now.Before(until)")
	}
	if got := readSnoozed(t, dir); got["sig"] != until.Format(time.RFC3339) {
		t.Fatalf("snooze should persist untouched, file has %v", got)
	}
	// Exactly at until: no longer honored, and pruned from the file.
	if store.Suppressed("sig", "h", until) {
		t.Fatal("snooze must lapse once now reaches until")
	}
	if got := readSnoozed(t, dir); len(got) != 0 {
		t.Fatalf("lapsed snooze should be pruned from the file, got %v", got)
	}
}

// A reader with an earlier now must not prune snoozes that are still live from
// its point of view, even if they have lapsed on the wall clock.
func TestSnoozeGCDoesNotPruneFutureRelativeToNow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = store.Snooze("a", base.Add(24*time.Hour))
	_ = store.Snooze("b", base.Add(72*time.Hour))

	// now = base+48h: a lapsed, b still live.
	if store.Suppressed("a", "h", base.Add(48*time.Hour)) {
		t.Fatal("a should have lapsed")
	}
	if !store.Suppressed("b", "h", base.Add(48*time.Hour)) {
		t.Fatal("b should still be snoozed")
	}
	got := readSnoozed(t, dir)
	if _, ok := got["a"]; ok {
		t.Fatalf("a should be pruned, file has %v", got)
	}
	if _, ok := got["b"]; !ok {
		t.Fatalf("b should be kept, file has %v", got)
	}

	// Reloading from disk sees the same picture (format unchanged: RFC3339 map).
	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Suppressed("b", "h", base.Add(48*time.Hour)) {
		t.Fatal("b should still be snoozed after reload")
	}
}

func readSnoozed(t *testing.T, dir string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "feed-signals.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st sigState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	return st.Snoozed
}
