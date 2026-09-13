package portals

import (
	"testing"
	"time"
)

// The read-only accessors serve the parsed file from memory and still see
// every write made through the cache.
func TestCacheMemoTracksWrites(t *testing.T) {
	c := newCache(t.TempDir(), "benchling")
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	if c.Dismissed("x") {
		t.Fatal("fresh cache dismissed nothing")
	}
	c.Commit(now, true, []Event{{ID: "e1", Title: "one", At: now}}, map[string]string{"entry": now.Format(time.RFC3339)}, nil, "")
	if ev := c.Events(); len(ev) != 1 || ev[0].ID != "e1" {
		t.Fatal("commit not visible", ev)
	}
	if c.Cursor("entry").IsZero() {
		t.Fatal("cursor not visible")
	}
	c.Dismiss("e1", now)
	if !c.Dismissed("e1") {
		t.Fatal("dismissal not visible after write")
	}
	c.Commit(now.Add(time.Hour), true, []Event{{ID: "e2", Title: "two", At: now.Add(time.Hour)}}, nil, nil, "")
	if ev := c.Events(); len(ev) != 2 || ev[0].ID != "e2" {
		t.Fatal("second commit not visible", ev)
	}
	if ok, _ := c.Status(); ok.IsZero() {
		t.Fatal("status not visible")
	}
}
