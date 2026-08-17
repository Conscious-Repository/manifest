package chatthreads

import (
	"testing"
	"time"
)

func ts() time.Time { return time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC) }

func TestThreadCRUDAndPrune(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ben := Identity{ID: "ben@aion.bio", Name: "Benjamin Anderson"}
	if _, err := st.CreateThread("th/pig-site", "pig site", "aion/mouse-to-pig", ben, ts()); err != nil {
		t.Fatal(err)
	}
	// idempotent create
	if _, err := st.CreateThread("th/pig-site", "x", "y", ben, ts()); err != nil {
		t.Fatal(err)
	}
	if len(st.Threads()) != 1 {
		t.Fatalf("create must be idempotent: %+v", st.Threads())
	}
	// an untitled empty thread gets pruned; a titled one survives
	if _, err := st.CreateThread("th/new-1", "untitled", "", ben, ts()); err != nil {
		t.Fatal(err)
	}
	dropped := st.PruneEmpty("th/pig-site")
	if len(dropped) != 1 || dropped[0] != "th/new-1" {
		t.Fatalf("prune: %+v", dropped)
	}
	if len(st.Threads()) != 1 {
		t.Fatalf("prune left: %+v", st.Threads())
	}
	// archive / reopen round-trip
	if _, err := st.PatchThread("th/pig-site", map[string]any{"archived": true}, ben, ts()); err != nil {
		t.Fatal(err)
	}
	if !st.Threads()[0].Archived {
		t.Fatal("archive did not stick")
	}
	if _, err := st.PatchThread("th/pig-site", map[string]any{"archived": false, "rock": "aion/series-a-15m"}, ben, ts()); err != nil {
		t.Fatal(err)
	}
	if st.Threads()[0].Archived || st.Threads()[0].Rock != "aion/series-a-15m" {
		t.Fatalf("reopen/rescope: %+v", st.Threads()[0])
	}
}

func TestMessagesAndHasRun(t *testing.T) {
	st, _ := New(t.TempDir())
	ben := Identity{ID: "ben@aion.bio", Name: "Benjamin Anderson"}
	_, _ = st.CreateThread("th/x", "x", "", ben, ts())
	if _, err := st.AddMessage(Message{Thread: "th/x", Kind: "ask", Author: ben.ID, AuthName: ben.Name, Text: "what's the setback?"}, ts()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddMessage(Message{Thread: "th/x", Kind: "kairos", Author: "agent:kairos", AuthName: "kairos", Text: "25ft", Run: "r1", Outcome: "completed"}, ts()); err != nil {
		t.Fatal(err)
	}
	if len(st.Messages("th/x")) != 2 {
		t.Fatalf("messages: %d", len(st.Messages("th/x")))
	}
	if !st.HasRun("th/x", "r1") {
		t.Fatal("HasRun must find r1")
	}
	if st.HasRun("th/x", "r2") {
		t.Fatal("HasRun must not find r2")
	}
}

func TestPendingLifecycle(t *testing.T) {
	st, _ := New(t.TempDir())
	if err := st.SetPending(Pending{OrderID: "o1", Thread: "th/x", By: "Ben", ByEmail: "ben@aion.bio", Ritual: "ask", At: ts()}, ts()); err != nil {
		t.Fatal(err)
	}
	if len(st.Pending()) != 1 {
		t.Fatal("pending not set")
	}
	st.ClearPending("o1")
	if len(st.Pending()) != 0 {
		t.Fatal("pending not cleared")
	}
}

func TestDecideProposal(t *testing.T) {
	st, _ := New(t.TempDir())
	ben := Identity{ID: "ben@aion.bio", Name: "Benjamin Anderson"}
	_, _ = st.CreateThread("th/x", "x", "", ben, ts())
	m, _ := st.AddMessage(Message{Thread: "th/x", Kind: "kairos", Author: "agent:kairos", AuthName: "kairos",
		Text: "done", Run: "r1", Outcome: "completed",
		Props: []Proposal{{Verb: "set due", Type: "set-field", ItemID: "bl/x", Field: "due", Value: "2026-09-01", State: "pending"}},
	}, ts())
	p, err := st.DecideProposal("th/x", m.ID, 0, true, ben, ts())
	if err != nil || p.State != "applied" || p.By != ben.ID {
		t.Fatalf("decide: %+v %v", p, err)
	}
	// second decide refuses
	if _, err := st.DecideProposal("th/x", m.ID, 0, false, ben, ts()); err == nil {
		t.Fatal("re-decide must refuse")
	}
	if st.Messages("th/x")[0].Props[0].State != "applied" {
		t.Fatal("state not persisted")
	}
}

func TestParseProposals(t *testing.T) {
	brief := "Here's the plan.\n\n```manifest-proposal\n{\"type\":\"replace-section\",\"item\":\"bl/eth-loi\",\"section\":\"plan\",\"body\":\"1. Draft\\n2. Send\"}\n```\n\nAnd a field:\n```manifest-proposal\n{\"type\":\"set-field\",\"item\":\"bl/eth-loi\",\"field\":\"status\",\"value\":\"in_progress\"}\n```\n"
	clean, props, warns := ParseProposals(brief)
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if len(props) != 2 {
		t.Fatalf("props: %+v", props)
	}
	if props[0].Type != "replace-section" || props[0].Section != "plan" || props[0].ItemID != "bl/eth-loi" {
		t.Fatalf("prop0: %+v", props[0])
	}
	if props[1].Type != "set-field" || props[1].Field != "status" || props[1].Value != "in_progress" {
		t.Fatalf("prop1: %+v", props[1])
	}
	if !contains(clean, "→ proposed: replace ## plan") || !contains(clean, "→ proposed: set status") {
		t.Fatalf("clean missing placeholders:\n%s", clean)
	}
	// a malformed block warns, no proposal; a clean brief passes through
	_, p2, w2 := ParseProposals("```manifest-proposal\n{not json}\n```")
	if len(p2) != 0 || len(w2) != 1 {
		t.Fatalf("malformed: props=%v warns=%v", p2, w2)
	}
	c3, p3, _ := ParseProposals("just a normal answer, no blocks")
	if len(p3) != 0 || c3 != "just a normal answer, no blocks" {
		t.Fatalf("passthrough: %q %+v", c3, p3)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
