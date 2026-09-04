package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The claude fixture (a real session, redacted) projects into the turn
// grammar: user text → user turn; assistant text/thinking/tool_use → one
// assistant turn of ordered blocks; the tool_result carrier row pairs with
// its step instead of becoming a turn; ai-title / cost-state surface as
// title / cost; attachment / queue-operation / last-prompt / mode rows vanish.
func TestClaudeTranscriptGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "claude_session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := parseClaudeTranscript(f)

	if tr.Title != "Repo status and layout check" {
		t.Fatalf("title = %q", tr.Title)
	}
	if tr.Cost != 0.0421 {
		t.Fatalf("cost = %v", tr.Cost)
	}
	if tr.Offset == 0 {
		t.Fatal("offset not advanced")
	}
	want := []struct {
		who    string
		blocks []string // t:cast
		text   string
	}{
		{who: "user", text: "model"},
		{who: "assistant", blocks: []string{"say", "step:Bash", "think", "say"}},
	}
	if len(tr.Turns) != len(want) {
		b, _ := json.MarshalIndent(tr.Turns, "", " ")
		t.Fatalf("got %d turns, want %d:\n%s", len(tr.Turns), len(want), b)
	}
	for i, w := range want {
		got := tr.Turns[i]
		if got.Who != w.who {
			t.Fatalf("turn %d who = %q, want %q", i, got.Who, w.who)
		}
		if w.text != "" && got.Text != w.text {
			t.Fatalf("turn %d text = %q, want %q", i, got.Text, w.text)
		}
		var kinds []string
		for _, bl := range got.Blocks {
			k := bl.T
			if bl.Cast != "" {
				k += ":" + bl.Cast
			}
			kinds = append(kinds, k)
		}
		if strings.Join(kinds, ",") != strings.Join(w.blocks, ",") {
			t.Fatalf("turn %d blocks = %v, want %v", i, kinds, w.blocks)
		}
		if got.TS == "" {
			t.Fatalf("turn %d has no timestamp", i)
		}
	}
	// the step: one-line input, paired (non-error) result, thinking text kept
	a := tr.Turns[1]
	step := a.Blocks[1]
	if !strings.Contains(step.Input, "git status --short") || strings.Contains(step.Input, "\n") {
		t.Fatalf("step input = %q", step.Input)
	}
	if !strings.HasPrefix(step.Result, "?? config.json.bak") || step.Error {
		t.Fatalf("step result = %q (error=%v)", step.Result, step.Error)
	}
	if a.Blocks[2].Text != "Check git status first, then list the tree." {
		t.Fatalf("thinking = %q", a.Blocks[2].Text)
	}
	if !strings.HasPrefix(a.Blocks[0].Text, "I'll take a look") || !strings.HasPrefix(a.Blocks[3].Text, "I don't have a request") {
		t.Fatalf("say blocks = %q / %q", a.Blocks[0].Text, a.Blocks[3].Text)
	}
	if strings.Contains(step.Result, "(redacted)") {
		t.Fatal("toolUseResult (the row-level copy) leaked into the step; content must come from the tool_result block")
	}
}

// The codex rollout projects the same grammar: developer rows and the CLI's
// <…> boilerplate user row are skipped; custom_tool_call pairs with its
// _output by call_id; reasoning summaries become think blocks.
func TestCodexTranscriptGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "codex_rollout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := parseCodexTranscript(f)
	if len(tr.Turns) != 2 {
		b, _ := json.MarshalIndent(tr.Turns, "", " ")
		t.Fatalf("got %d turns, want 2:\n%s", len(tr.Turns), b)
	}
	if tr.Turns[0].Who != "user" || !strings.HasPrefix(tr.Turns[0].Text, "Summarise framework.md") {
		t.Fatalf("turn 0 = %+v", tr.Turns[0])
	}
	a := tr.Turns[1]
	if a.Who != "assistant" {
		t.Fatalf("turn 1 who = %q", a.Who)
	}
	var steps, thinks, says int
	for _, bl := range a.Blocks {
		switch bl.T {
		case "step":
			steps++
			if bl.Cast != "exec" || bl.Result == "" {
				t.Fatalf("step %+v not paired", bl)
			}
		case "think":
			thinks++
		case "say":
			says++
		}
	}
	if steps != 3 || thinks != 2 || says == 0 {
		t.Fatalf("steps=%d thinks=%d says=%d", steps, thinks, says)
	}
}

// ?after= tails: the records past the offset project on their own, a
// trailing partial line is left for the next poll, and a whole-file read is
// served from the (path,size,mtime) cache.
func TestTranscriptTailAndCache(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "claude_session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(strings.TrimRight(string(src), "\n")+"\n", "\n")
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	head := strings.Join(lines[:4], "") // ai-title, 2 queue ops, the user row
	if err := os.WriteFile(path, []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	tr, ok := readTranscript("claude", path, 0)
	if !ok || len(tr.Turns) != 1 || tr.Turns[0].Who != "user" {
		t.Fatalf("head projection = %+v ok=%v", tr.Turns, ok)
	}
	if tr.Offset != int64(len(head)) {
		t.Fatalf("offset = %d, want %d", tr.Offset, len(head))
	}
	// append the rest plus a partial line (the CLI mid-write)
	rest := strings.Join(lines[4:], "")
	if err := os.WriteFile(path, []byte(head+rest+`{"type":"assistant","mess`), 0o600); err != nil {
		t.Fatal(err)
	}
	tail, ok := readTranscript("claude", path, tr.Offset)
	if !ok {
		t.Fatal("tail read failed")
	}
	if tail.Offset != int64(len(head)+len(rest)) {
		t.Fatalf("tail offset = %d, want %d (partial line must not be consumed)", tail.Offset, len(head)+len(rest))
	}
	if len(tail.Turns) != 1 || tail.Turns[0].Who != "assistant" || len(tail.Turns[0].Blocks) != 4 {
		t.Fatalf("tail turns = %+v", tail.Turns)
	}
	if tail.Cost != 0.0421 {
		t.Fatalf("tail cost = %v", tail.Cost)
	}
	// whole-file read twice: second hits the cache (same projection)
	full1, _ := readTranscript("claude", path, 0)
	full2, _ := readTranscript("claude", path, 0)
	if len(full1.Turns) != 2 || len(full2.Turns) != 2 || full1.Offset != full2.Offset {
		t.Fatalf("full reads differ: %d/%d turns", len(full1.Turns), len(full2.Turns))
	}
	transcriptCacheMu.Lock()
	_, hit := transcriptCache[path]
	transcriptCacheMu.Unlock()
	if !hit {
		t.Fatal("whole-file projection not cached")
	}
}

// The endpoint locates the file by (cwd-encoded, resumeId) under the
// projects root and reports live from the tmux runner.
func TestTranscriptEndpoint(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	rec.live = false
	cwd := "/home/benjamin/src/manifest"
	if got := claudeProjectDir(cwd); got != "-home-benjamin-src-manifest" {
		t.Fatalf("claudeProjectDir = %q", got)
	}
	projDir := filepath.Join(s.terminal.claudeProjects, claudeProjectDir(cwd))
	if err := os.MkdirAll(projDir, 0o700); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(filepath.Join("testdata", "claude_session.jsonl"))
	rid := "63e55a17-b5b5-464e-8a00-714a410c4422"
	if err := os.WriteFile(filepath.Join(projDir, rid+".jsonl"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	se := termSession{ID: "abcdef0123456789", Kind: "claude", Cwd: cwd, ResumeID: rid, Started: true, Name: "cc1"}
	s.terminal.upsert(se)

	req := httptest.NewRequest("GET", "/api/terminal/session/"+se.ID+"/transcript", nil)
	req.SetPathValue("id", se.ID)
	w := httptest.NewRecorder()
	s.handleTermTranscript(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Turns  []termTurn `json:"turns"`
		Title  string     `json:"title"`
		Cost   float64    `json:"cost"`
		Live   bool       `json:"live"`
		Offset int64      `json:"offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Turns) != 2 || out.Title == "" || out.Cost == 0 || out.Live || out.Offset == 0 {
		t.Fatalf("reply = %+v", out)
	}
	// a virgin session (no file yet) answers empty, not 404
	se2 := termSession{ID: "0123456789abcdef", Kind: "claude", Cwd: cwd, ResumeID: "11111111-2222-3333-4444-555555555555", Name: "cc2"}
	s.terminal.upsert(se2)
	req = httptest.NewRequest("GET", "/api/terminal/session/"+se2.ID+"/transcript", nil)
	req.SetPathValue("id", se2.ID)
	w = httptest.NewRecorder()
	s.handleTermTranscript(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"turns":[]`) {
		t.Fatalf("virgin session: %d %s", w.Code, w.Body.String())
	}
}
