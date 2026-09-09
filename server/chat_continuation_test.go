package server

import (
	"context"
	"manifest/agentchat"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodingContinuationSnapshotRetainsAuthorsAndDisclosesOmission(t *testing.T) {
	source := agentchat.Session{Agent: "alfred", ID: "20260909-120000-abcd"}
	body := "## Turn 1 — user · 2026-09-09T12:00:00Z\n\n" + strings.Repeat("older", 8000) + "\n\n## Turn 2 — agent:zeck · 2026-09-09T12:01:00Z\n\nReviewed finding\n\n## Turn 3 — user · 2026-09-09T12:02:00Z\n\nPlease build this\n"
	text, omitted := codingContinuationContext(source, body)
	if omitted != 1 || !strings.Contains(text, `author "agent:zeck"`) || !strings.Contains(text, "Reviewed finding") || !strings.Contains(text, "1 earlier turns omitted") || strings.Contains(text, "olderolder") {
		t.Fatal(omitted, text)
	}
	if !strings.Contains(text, "Tool traces and attachment contents are excluded") {
		t.Fatal("missing scope disclosure")
	}
	long := "## Turn 1 — user · 2026-09-09T12:00:00Z\n\n" + strings.Repeat("x", agentChatWindowChars+1)
	text, omitted = codingContinuationContext(source, long)
	if omitted != 1 || len(text) > agentChatWindowChars {
		t.Fatal("unbounded context or silent truncation", omitted, len(text))
	}
}

func TestCodingContinuationCreationFreezesContextWithoutWritingTurns(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}
	id, _ := st.Create("alfred", "", "Source", "")
	st.AppendTurn("alfred", id, "user", "INITIAL_CONTEXT", 0)
	endpoint := "/api/agents/chat/alfred/sessions/" + id + "/related"
	payload := map[string]any{"backend": "terminal", "mode": "continue", "agent": "codex", "requestId": "continuation-001"}
	code, out := agentChatJSON(t, s, "POST", endpoint, payload)
	if code != 200 {
		t.Fatal(code, out)
	}
	childID := out["id"].(string)
	child, _ := s.terminal.find(childID)
	if !child.isDraft() || child.Origin.Mode != "continue" || !strings.Contains(child.Origin.Context, "INITIAL_CONTEXT") {
		t.Fatal(child)
	}
	st.AppendTurn("alfred", id, "user", "LATER_CONTEXT", 0)
	code, retry := agentChatJSON(t, s, "POST", endpoint, payload)
	if code != 200 || retry["id"] != childID {
		t.Fatal(code, retry)
	}
	child, _ = s.terminal.find(childID)
	if strings.Contains(child.Origin.Context, "LATER_CONTEXT") {
		t.Fatal("retry changed approved context")
	}
	payload["mode"] = ""
	if code, _ := agentChatJSON(t, s, "POST", endpoint, payload); code != 409 {
		t.Fatal("mode change not detected", code)
	}
	parent, body, queue, _ := st.Get("alfred", id)
	if parent.Turns != 2 || len(queue) != 0 || strings.Contains(body, "codex") {
		t.Fatal("continuation rewrote source")
	}
}

func TestCodingContinuationProjectionExcludesRelatedAndUnrelatedHistories(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir(), claudeProjects: t.TempDir()}
	id, _ := st.Create("alfred", "", "Canonical source", "")
	st.AppendTurn("alfred", id, "user", "source text stays here", 0)
	source, body, _, _ := st.Get("alfred", id)
	se := termSession{ID: "abcd1234", Kind: "claude", Backend: "herdr", LaunchPhase: "active", Started: true, Cwd: s.terminal.defaultWd, ResumeID: "01234567-abcd", Origin: &agentchat.Origin{Mode: "continue", Agent: "alfred", ID: id, Context: "frozen snapshot"}}
	path := s.terminal.transcriptPath(se)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/claude_session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s.terminal.upsert(se)
	for i, origin := range []*agentchat.Origin{{Agent: "alfred", ID: id}, {Mode: "continue", Agent: "alfred", ID: "20260909-120000-ffff"}, {Mode: "continue", Agent: "another", ID: id}} {
		other := se
		other.ID = []string{"abcd1235", "abcd1236", "abcd1237"}[i]
		other.Origin = origin
		s.terminal.upsert(other)
	}
	views := s.codingContinuations(context.Background(), source)
	native, _ := readTranscript("claude", path, 0)
	if len(views) != 1 || views[0].ID != se.ID || !reflect.DeepEqual(views[0].Turns, native.Turns) {
		t.Fatalf("wrong sources or changed native IDs: %+v", views)
	}
	_, after, _, _ := st.Get("alfred", id)
	afterNative, _ := os.ReadFile(path)
	if body != after || string(raw) != string(afterNative) {
		t.Fatal("projection modified source data")
	}
	if views[0].Process != "unknown" {
		t.Fatal("missing daemon mistaken for stopped")
	}
}
