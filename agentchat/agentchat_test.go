package agentchat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAppendParseRoundTrip(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "chats"))
	id, err := st.Create("alfred", "", "gutter contractors", "")
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(id) {
		t.Fatalf("id %q not in the spirit grammar", id)
	}
	if _, err := os.Stat(filepath.Join(st.Root(), "alfred", id+".md")); err != nil {
		t.Fatalf("session file missing: %v", err)
	}
	if n, err := st.AppendTurn("alfred", id, "user", "find me 10 gutter contractors", 0); err != nil || n != 1 {
		t.Fatalf("append user: n=%d err=%v", n, err)
	}
	if err := st.SetStatus("alfred", id, StatusThinking); err != nil {
		t.Fatal(err)
	}
	if n, err := st.AppendTurn("alfred", id, "alfred", "### Step 1 — say\n\nHere are ten.", 0.0123); err != nil || n != 2 {
		t.Fatalf("append assistant: n=%d err=%v", n, err)
	}
	if err := st.SetHermesSession("alfred", id, "20260904_135845_2214c8"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus("alfred", id, StatusIdle); err != nil {
		t.Fatal(err)
	}
	sess, body, queued, ok := st.Get("alfred", id)
	if !ok {
		t.Fatal("get failed")
	}
	if sess.Turns != 2 || sess.Status != StatusIdle || sess.Agent != "alfred" || sess.Title != "gutter contractors" {
		t.Errorf("session = %+v", sess)
	}
	if sess.SpentUSD < 0.0122 || sess.SpentUSD > 0.0124 {
		t.Errorf("spent = %v", sess.SpentUSD)
	}
	if sess.HermesSession != "20260904_135845_2214c8" {
		t.Errorf("hermes session = %q", sess.HermesSession)
	}
	if len(queued) != 0 {
		t.Errorf("queued = %v", queued)
	}
	turns := ParseTurns(body)
	if len(turns) != 2 {
		t.Fatalf("turns = %+v\nbody:\n%s", turns, body)
	}
	if turns[0].Who != "user" || turns[0].Text != "find me 10 gutter contractors" {
		t.Errorf("turn 1 = %+v", turns[0])
	}
	if turns[1].Who != "alfred" || turns[1].USD != "0.0123" || SayBody(turns[1].Text) != "Here are ten." {
		t.Errorf("turn 2 = %+v", turns[1])
	}
	// the heading grammar is the spirit renderer's, byte for byte
	if !strings.Contains(body, "## Turn 2 — alfred · ") || !strings.Contains(body, " · $0.0123\n") {
		t.Errorf("heading grammar drifted:\n%s", body)
	}
	// no stray tmp files (tmp+rename)
	entries, _ := os.ReadDir(filepath.Join(st.Root(), "alfred"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("tmp file left behind: %s", e.Name())
		}
	}
}

func TestListAgentsRenameDelete(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "chats"))
	a, _ := st.Create("alfred", "", "first", "")
	b, _ := st.Create("scout", "scout", "second", "gpt-5")
	if got := st.Agents(); strings.Join(got, ",") != "alfred,scout" {
		t.Errorf("agents = %v", got)
	}
	if l := st.List("alfred"); len(l) != 1 || l[0].ID != a {
		t.Errorf("alfred list = %+v", l)
	}
	if l := st.List("scout"); len(l) != 1 || l[0].Profile != "scout" || l[0].Model != "gpt-5" {
		t.Errorf("scout list = %+v", l)
	}
	if err := st.Rename("alfred", a, "renamed"); err != nil {
		t.Fatal(err)
	}
	if s, _, _, _ := st.Get("alfred", a); s.Title != "renamed" {
		t.Errorf("rename lost: %+v", s)
	}
	if err := st.Rename("alfred", a, "  "); err == nil {
		t.Error("blank rename accepted")
	}
	if err := st.Delete("scout", b); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := st.Get("scout", b); ok {
		t.Error("deleted session still readable")
	}
	if _, _, _, ok := st.Get("../etc", "20260904-120000-ab"); ok {
		t.Error("path-ish agent accepted")
	}
	if _, err := st.Create("Bad Name", "", "", ""); err == nil {
		t.Error("bad agent name accepted")
	}
}

func TestSubmitQueueDrain(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "chats"))
	id, _ := st.Create("alfred", "", "", "")
	if !st.Submit("alfred", id, "one") {
		t.Fatal("first submit must claim")
	}
	if st.Submit("alfred", id, "two") {
		t.Fatal("second submit must queue")
	}
	if st.Submit("alfred", id, "three") {
		t.Fatal("third submit must queue")
	}
	if q := st.Queued("alfred", id); strings.Join(q, ",") != "two,three" {
		t.Errorf("queued = %v", q)
	}
	if !st.InFlight("alfred", id) {
		t.Error("should be in flight")
	}
	if err := st.Delete("alfred", id); err == nil {
		t.Error("delete while in flight must refuse")
	}
	if txt, more := st.Next("alfred", id); !more || txt != "two" {
		t.Errorf("next = %q %v", txt, more)
	}
	if txt, more := st.Next("alfred", id); !more || txt != "three" {
		t.Errorf("next = %q %v", txt, more)
	}
	if _, more := st.Next("alfred", id); more {
		t.Error("queue should be drained")
	}
	if st.InFlight("alfred", id) {
		t.Error("claim must release when the queue is empty")
	}
	if !st.Submit("alfred", id, "four") {
		t.Error("submit after release must claim again")
	}
	st.Release("alfred", id)
}

func TestRecoverRepairsStaleThinking(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "chats"))
	id, _ := st.Create("alfred", "", "", "")
	_, _ = st.AppendTurn("alfred", id, "user", "hello", 0)
	_ = st.SetStatus("alfred", id, StatusThinking)
	fixed := New(st.Root()).Recover()
	if len(fixed) != 1 {
		t.Fatalf("fixed = %v", fixed)
	}
	sess, body, _, _ := st.Get("alfred", id)
	if sess.Status != StatusIdle || sess.Turns != 2 {
		t.Errorf("session = %+v", sess)
	}
	turns := ParseTurns(body)
	if len(turns) != 2 || turns[1].Who != "system" || !strings.Contains(turns[1].Text, "interrupted") {
		t.Errorf("turns = %+v", turns)
	}
}

// SayBody finds the say step wherever it sits (a portal reply puts a trace
// step first) and stops at the next step.
func TestSayBodyAnyStep(t *testing.T) {
	cases := map[string]string{
		"### Step 1 — say\n\nHere are ten.":                                                "Here are ten.",
		"### Step 1 — ask\n\n- result: completed · 41s\n\n### Step 2 — say\n\nFifty feet.": "Fifty feet.",
		"### Step 1 — say\n\nFirst.\n\n### Step 2 — search\n\n- result: x":                 "First.",
		"plain reply, no steps": "plain reply, no steps",
	}
	for in, want := range cases {
		if got := SayBody(in); got != want {
			t.Errorf("SayBody(%q) = %q, want %q", in, got, want)
		}
	}
}
