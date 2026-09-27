package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A long coding chat reads snappily: a first read carries the latest turns
// only, with no tool results (their size and owner instead); earlier turns
// and any one result come on demand; nothing is lost from the record.
func TestTranscriptLiteTailOlderAndStepResult(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	rec.live = false
	cwd := "/home/benjamin/src/manifest"
	projDir := filepath.Join(s.terminal.claudeProjects, claudeProjectDir(cwd))
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rid := "63e55a17-b5b5-464e-8a00-714a410c4422"
	var b strings.Builder
	big := strings.Repeat("x", 1200)
	for i := 0; i < 30; i++ {
		ts := fmt.Sprintf("2026-09-26T10:%02d:00Z", i)
		fmt.Fprintf(&b, `{"type":"user","timestamp":"%s","message":{"role":"user","content":"question %d"}}`+"\n", ts, i)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool%d","name":"Bash","input":{"command":"ls %d"}}]}}`+"\n", ts, i, i)
		fmt.Fprintf(&b, `{"type":"user","timestamp":"%s","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool%d","content":"%s-%d"}]}}`+"\n", ts, i, big, i)
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"%s","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"answer %d"}]}}`+"\n", ts, i)
	}
	if err := os.WriteFile(filepath.Join(projDir, rid+".jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	se := termSession{ID: "abcdef0123456789", Kind: "claude", Cwd: cwd, ResumeID: rid, Started: true, Name: "cc1"}
	s.terminal.upsert(se)
	call := func(handler string, query string) (int, map[string]any, int) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/terminal/session/"+se.ID+"/"+handler+"?"+query, nil)
		r.SetPathValue("id", se.ID)
		switch handler {
		case "transcript":
			s.handleTermTranscript(w, r)
		case "step":
			s.handleTermStepResult(w, r)
		case "turns":
			s.handleTermOlderTurns(w, r)
		}
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out, w.Body.Len()
	}
	_, full, fullBytes := call("transcript", "")
	if n := len(full["turns"].([]any)); n != 60 {
		t.Fatalf("a plain read still carries every turn: %d", n)
	}
	code, lite, liteBytes := call("transcript", "lite=1&tail=10")
	turns := lite["turns"].([]any)
	if code != 200 || len(turns) != 10 || lite["older"].(float64) != 50 {
		t.Fatalf("tail: %d turns, older %v", len(turns), lite["older"])
	}
	if liteBytes*5 > fullBytes {
		t.Fatalf("a lite tail should be a fraction of the full read: %d vs %d bytes", liteBytes, fullBytes)
	}
	var step map[string]any
	for _, tt := range turns {
		blocks, _ := tt.(map[string]any)["blocks"].([]any)
		for _, bl := range blocks {
			if m := bl.(map[string]any); m["t"] == "step" {
				step = m
			}
		}
	}
	if step == nil || step["result"] != nil || step["resultBytes"].(float64) < 1200 || step["sid"] != se.ID || step["done"] != true {
		t.Fatalf("lite step: %+v", step)
	}
	if code, out, _ := call("step", "id="+step["id"].(string)); code != 200 || !strings.HasPrefix(out["result"].(string), big) {
		t.Fatalf("step result on demand: %d", code)
	}
	if code, _, _ := call("step", "id=nope"); code != 404 {
		t.Fatalf("unknown step: %d", code)
	}
	first := turns[0].(map[string]any)["id"].(string)
	code, older, _ := call("turns", "limit=20&before="+first)
	if code != 200 || len(older["turns"].([]any)) != 20 || older["older"].(float64) != 30 {
		t.Fatalf("older: %d %v", code, older["older"])
	}
	if last := older["turns"].([]any)[19].(map[string]any)["id"]; last == first {
		t.Fatal("older turns must end before the first one shown")
	}
	// an incremental poll is untouched by tail: it carries what is new
	if _, poll, _ := call("transcript", fmt.Sprintf("lite=1&tail=10&after=%v", lite["offset"])); len(poll["turns"].([]any)) != 0 {
		t.Fatal("a caught-up poll carries no turns")
	}
}

// The timeline is stripped the same way (its owner per entry) and hashed so
// an unchanged window is not re-sent.
func TestLiteTimelineOwnersAndStableHash(t *testing.T) {
	items := []conversationTimelineTurn{
		{N: 1, Who: "user", TS: "a", Text: "go"},
		{N: "terminal:x:1", Who: "claude", TS: "b", Blocks: []termBlock{{T: "step", ID: "s1", Result: "long output"}}, Native: &continuationNativeSource{ID: "childsession"}},
	}
	lite := liteTimeline(items, "root")
	if b := lite[1].Blocks[0]; b.Result != "" || b.ResultBytes != 11 || b.SID != "childsession" {
		t.Fatalf("timeline step owner: %+v", b)
	}
	if items[1].Blocks[0].Result != "long output" {
		t.Fatal("liteTimeline mutated its input (a cached projection)")
	}
	if timelineHash(lite) != timelineHash(liteTimeline(items, "root")) {
		t.Fatal("hash must be stable for the same window")
	}
	window, older := tailOf(items, 1)
	if len(window) != 1 || older != 1 {
		t.Fatalf("tailOf: %d %d", len(window), older)
	}
}
