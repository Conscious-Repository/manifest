package server

import (
	"context"
	"manifest/agentchat"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSideChatSnapshotIsolationAndRecovery(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, target := range []string{"alfred", "codex"} {
			t.Run(target+map[bool]string{false: "-planning", true: "-native"}[native], func(t *testing.T) {
				s, st, _ := agentChatFixture(t, echoStub)
				s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir(), claudeProjects: t.TempDir()}
				id, _ := st.Create("alfred", "", "Parent", "")
				st.AppendTurn("alfred", id, "user", "PARENT_SNAPSHOT", 0)
				endpoint := "/api/agents/chat/alfred/sessions/" + id + "/related"
				root := termSession{ID: "abcdef123456", Kind: "claude", Backend: "herdr", LaunchPhase: "active", Started: true, Cwd: s.terminal.defaultWd, ResumeID: "01234567-abcd"}
				if native {
					s.terminal.upsert(root)
					path := s.terminal.transcriptPath(root)
					os.MkdirAll(filepath.Dir(path), 0700)
					if err := os.WriteFile(path, []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"PARENT_SNAPSHOT"}]}}`+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					endpoint = "/api/terminal/claude/session/" + root.ID + "/related"
				}
				payload := map[string]any{"agent": target, "mode": "side", "requestId": "side-request-001", "title": "Side chat · " + strings.Repeat("界", 228)}
				if target == "codex" {
					payload["backend"] = "terminal"
					payload["model"] = "gpt-6-astra"
				}
				code, out := agentChatJSON(t, s, "POST", endpoint, payload)
				if code != 200 {
					t.Fatal(code, out)
				}
				childID := out["id"].(string)
				if target == "codex" {
					child, _ := s.terminal.find(childID)
					if !child.isDraft() || child.Origin.Mode != "side" || !strings.Contains(child.Origin.Context, "PARENT_SNAPSHOT") || child.Model != "gpt-6-astra" {
						t.Fatal(child)
					}
					if len(s.terminalCodingContinuations(context.Background(), root)) != 0 {
						t.Fatal("side chat merged into parent timeline")
					}
				} else {
					child, body, q, _ := st.Get(target, childID)
					if child.Turns != 0 || body != "" || len(q) != 0 || child.Sharing != nil || child.Origin.Mode != "side" || !strings.Contains(child.Origin.Context, "PARENT_SNAPSHOT") {
						t.Fatal(child, body)
					}
					prompt := s.composeAgentChatPrompt(target, child, "## Turn 1 — user · 2026-09-11T12:00:00Z\n\nSIDE_QUESTION")
					if !strings.Contains(prompt, "PARENT_SNAPSHOT") || !strings.Contains(prompt, "SIDE_QUESTION") {
						t.Fatal(prompt)
					}
				}
				st.AppendTurn("alfred", id, "user", "LATER_PARENT_MESSAGE", 0)
				if native {
					s.terminal.remove(root.ID)
				}
				code, retry := agentChatJSON(t, s, "POST", endpoint, payload)
				if code != 200 || retry["id"] != childID {
					t.Fatal("lost-response recovery", code, retry)
				}
				if target == "alfred" {
					child, _, _, _ := st.Get(target, childID)
					if strings.Contains(child.Origin.Context, "LATER_PARENT_MESSAGE") {
						t.Fatal("snapshot mutated")
					}
				}
				payload["model"] = "different-model"
				if code, _ := agentChatJSON(t, s, "POST", endpoint, payload); code != 409 {
					t.Fatal("changed intent accepted", code)
				}
			})
		}
	}
}

func TestSideChatContextSurvivesExplicitRecipientSwitch(t *testing.T) {
	source := agentchat.Session{Agent: "alfred", ID: "20260911-120000-abcd", Origin: &agentchat.Origin{Mode: "side", Context: "FROZEN_PARENT"}}
	text, _ := logicalContinuationContext(source, "## Turn 1 — user · 2026-09-11T12:00:00Z\n\nSIDE_QUESTION", nil)
	if !strings.Contains(text, "FROZEN_PARENT") || !strings.Contains(text, "SIDE_QUESTION") {
		t.Fatal(text)
	}
	text, _ = codingContinuationContext(source, "## Turn 1 — user · 2026-09-11T12:00:00Z\n\nSIDE_QUESTION")
	if !strings.Contains(text, "FROZEN_PARENT") {
		t.Fatal(text)
	}
}
