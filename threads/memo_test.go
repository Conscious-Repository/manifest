package threads

import (
	"testing"
	"time"
)

// The read-only accessors serve the parsed state from memory and still see
// every entry added through the store; a caller cannot corrupt the memo
// through the slice Thread hands back.
func TestStoreMemoTracksWrites(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 3, 0, 0, 0, time.UTC)
	me := Identity{ID: "me", Name: "Me"}
	if len(st.TaskIDs()) != 0 || st.HasAction("t1", ActPlan, "r1") {
		t.Fatal("fresh store not empty")
	}
	if _, err := st.Add(me, "t1", ActPlan, "the plan", nil, nil, map[string]any{"run": "r1"}, now); err != nil {
		t.Fatal(err)
	}
	if !st.HasAction("t1", ActPlan, "r1") || len(st.TaskIDs()) != 1 {
		t.Fatal("first write not visible")
	}
	got := st.Thread("t1")
	if len(got) != 1 {
		t.Fatal(got)
	}
	got[0].Text = "mutated by a caller"
	if st.Thread("t1")[0].Text != "the plan" {
		t.Fatal("memo shared with the caller")
	}
	if _, err := st.Add(me, "t1", ActComment, "answer", nil, nil, nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if th := st.Thread("t1"); len(th) != 2 || th[1].Text != "answer" {
		t.Fatal("second write not visible", th)
	}
}
