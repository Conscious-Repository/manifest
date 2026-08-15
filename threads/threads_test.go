package threads

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

func TestThreadRoundTrip(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	me := Identity{ID: "owner", Name: "Benjamin"}
	c1, err := s.Add(me, "inbox/thing", ActComment, "first", []string{"agent:hermes"}, nil, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(me, "inbox/thing", ActAssign, "assigned", nil, nil, map[string]any{"assignee": "agent:hermes"}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	th := s.Thread("inbox/thing")
	if len(th) != 2 || th[0].ID != c1.ID || th[0].Text != "first" || th[1].Action != ActAssign {
		t.Fatalf("thread: %+v", th)
	}
	if th[0].Mentions[0] != "agent:hermes" {
		t.Fatalf("mentions lost: %+v", th[0])
	}
	// state must land BEFORE the log line (crash ordering): both files exist,
	// log has exactly two lines, each valid JSON
	b, err := os.ReadFile(filepath.Join(s.Dir(), "activity.log"))
	if err != nil {
		t.Fatal("no activity log")
	}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	n := 0
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			t.Fatalf("bad log line: %s", sc.Text())
		}
		n++
	}
	if n != 2 {
		t.Fatalf("log lines = %d", n)
	}
	// empty comment refused
	if _, err := s.Add(me, "x", ActComment, "  ", nil, nil, nil, now); err == nil {
		t.Fatal("empty comment must refuse")
	}
}

func TestBlobDedupAndPath(t *testing.T) {
	s, _ := New(t.TempDir())
	r1, err := s.SaveBlob(strings.NewReader("same bytes"), "a.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.SaveBlob(strings.NewReader("same bytes"), "b.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if r1.Hash != r2.Hash {
		t.Fatalf("same bytes different hash: %s vs %s", r1.Hash, r2.Hash)
	}
	// one file on disk despite two saves
	matches, _ := filepath.Glob(filepath.Join(s.Dir(), "files", r1.Hash[:2], "*"))
	if len(matches) != 1 {
		t.Fatalf("dedup failed: %v", matches)
	}
	if p := s.BlobPath(r1.Hash); p == "" {
		t.Fatal("blob path missing")
	}
	if p := s.BlobPath("nothex"); p != "" {
		t.Fatal("malformed hash must resolve to nothing")
	}
}

func TestHasActionIdempotency(t *testing.T) {
	s, _ := New(t.TempDir())
	h := Identity{ID: "agent:hermes", Name: "Hermes"}
	if s.HasAction("id1", ActPlan, "run-9") {
		t.Fatal("phantom action")
	}
	_, _ = s.Add(h, "id1", ActPlan, "plan landed", nil, nil, map[string]any{"run": "run-9"}, now)
	if !s.HasAction("id1", ActPlan, "run-9") {
		t.Fatal("action not found by run id")
	}
	if s.HasAction("id1", ActPlan, "run-10") {
		t.Fatal("wrong run matched")
	}
}
