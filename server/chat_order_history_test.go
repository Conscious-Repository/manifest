package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/chatthreads"
	"manifest/spirits"
)

func TestChatOrderCarriesCompleteHistoryAndRejectsTruncation(t *testing.T) {
	s, _ := chatFixture(t)
	now := time.Now()
	_, err := s.chat.CreateThread("history", "History", "", chatthreads.Identity{ID: "member@aion.bio"}, now)
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("previous exact context ", 1000)
	_, err = s.chat.AddMessage(chatthreads.Message{Thread: "history", Kind: "ask", Author: "member@aion.bio", Text: long, At: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AionChatAsk("history", "What did we decide?", "ask", nil, "member@aion.bio", "Member"); err != nil {
		t.Fatal(err)
	}
	q := s.findHarness("kairos").Spirits.Queued()
	if len(q) != 1 || !strings.Contains(q[0].Request, "CONVERSATION HISTORY:") || len(q[0].Request) > spirits.MaxRequestChars {
		t.Fatal(q)
	}
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(s.attachDir(s.kairosAgent())), "chat-history", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Messages []chatthreads.Message `json:"messages"`
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil || len(snapshot.Messages) != 1 || snapshot.Messages[0].Text != long {
		t.Fatal("history truncated", err)
	}
	if !strings.Contains(q[0].Request, paths[0]) {
		t.Fatal("snapshot not reachable from request")
	}
	if err = s.AionChatAsk("history", strings.Repeat("x", spirits.MaxRequestChars), "ask", nil, "member@aion.bio", "Member"); err == nil || !strings.Contains(err.Error(), "request limit") {
		t.Fatal("oversized instruction queued or truncated", err)
	}
	if len(s.findHarness("kairos").Spirits.Queued()) != 1 || len(s.chat.Messages("history")) != 2 {
		t.Fatal("rejected request changed messages/queue")
	}
}

func TestSharedTeamAgentHistoryIncludesFreshNativeAndRefusesMissing(t *testing.T) {
	s, se, thread, _ := sharedInputFixture(t, false)
	harness, _ := chatFixture(t)
	s.harnessList = harness.harnessList
	s.terminal.claudeProjects = t.TempDir()
	s.terminal.herdr = nil
	se.LaunchPhase = "active"
	se.Started = true
	se.ResumeID = "abcdefab-1234-1234-1234-abcdefabcdef"
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	path := s.terminal.transcriptPath(se)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"assistant","uuid":"new-native","timestamp":"2026-09-10T11:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"LATEST_NATIVE_DECISION"}]}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	block, err := s.chatOrderHistory(s.kairosAgent(), thread)
	if err != nil || !strings.Contains(block, "LATEST_NATIVE_DECISION") || !strings.Contains(block, "TEAM_CONTEXT_MARKER") {
		t.Fatal(block, err)
	}
	if len(s.chat.Messages(thread)) != 1 {
		t.Fatal("history read persisted native copy")
	}
	if _, err = s.chatOrderHistory(s.zeckAgent(), thread); err == nil {
		t.Fatal("wrong audience got history")
	}
	if err = s.AionChatAsk(thread, "Discuss that decision", "ask", nil, "member@aion.bio", "Member"); err != nil {
		t.Fatal(err)
	}
	queue := s.findHarness("kairos").Spirits.Queued()
	if len(queue) != 1 || !strings.Contains(queue[0].Request, "LATEST_NATIVE_DECISION") {
		t.Fatal("team harness did not receive native history", queue)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.chatOrderHistory(s.kairosAgent(), thread); err == nil || !strings.Contains(err.Error(), "temporarily unavailable") {
		t.Fatal("missing native history accepted", err)
	}
	if err = s.AionChatAsk(thread, "Must wait", "ask", nil, "member@aion.bio", "Member"); err == nil {
		t.Fatal("incomplete history dispatched")
	}
	if len(s.findHarness("kairos").Spirits.Queued()) != 1 || len(s.chat.Messages(thread)) != 2 {
		t.Fatal("failed history read changed queue or messages")
	}
}
