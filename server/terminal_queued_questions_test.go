package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHerdrQueuedQuestionFollowup(t *testing.T) {
	const ready = "• Queued follow-up inputs\n  ? 3 questions\n    alt + ↑ to answer\n› Ask Codex to do anything"
	for _, tc := range []struct {
		name, screen, code, kind string
		sent                     bool
	}{
		{"queued questions", ready, "agent_blocked", "codex", true},
		{"approval dialog", ready + "\nEnter to confirm · Esc to cancel", "agent_blocked", "codex", false},
		{"existing draft", strings.ReplaceAll(ready, "› Ask Codex to do anything", "› existing draft"), "agent_blocked", "codex", false},
		{"timeout cannot replay", ready, "timeout", "codex", false},
		{"other agent", ready, "agent_blocked", "claude", false},
		{"other blocked screen", "waiting for approval", "agent_blocked", "codex", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pastes, enters atomic.Int32
			h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
				switch r.Method {
				case "session.snapshot":
					pane := herdrFixturePane("blocked", 2)
					pane["agent"] = tc.kind
					herdrFixtureReply(c, map[string]any{"snapshot": map[string]any{"version": "0.9.0", "protocol": 22, "panes": []any{pane}}})
				case "pane.read":
					herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": tc.screen}})
				case "agent.prompt":
					_ = json.NewEncoder(c).Encode(map[string]any{"id": "manifest", "error": map[string]any{"code": tc.code, "message": "not accepted"}})
				case "pane.send_input":
					pastes.Add(1)
					if r.Params["text"] != "one\nfollow-up" || r.Params["keys"] != nil {
						t.Error("incorrect paste", r.Params)
					}
					herdrFixtureReply(c, map[string]any{})
				case "pane.send_keys":
					enters.Add(1)
					if pastes.Load() != 1 {
						t.Error("enter before paste")
					}
					herdrFixtureReply(c, map[string]any{})
				default:
					t.Error("unexpected method", r.Method)
				}
			})
			s := &Server{terminal: &termCfg{herdr: h}}
			id := herdrFixtureID(t, h)
			readyErr := s.herdrPromptReady(context.Background(), termSession{Runtime: id, Backend: "herdr"})
			wantReady := tc.kind == "codex" && tc.screen == ready
			if (readyErr == nil) != wantReady {
				t.Fatalf("readiness: %v", readyErr)
			}
			err := h.SendText(context.Background(), id, "one\nfollow-up")
			if (err == nil) != tc.sent || (pastes.Load() == 1) != tc.sent || (enters.Load() == 1) != tc.sent {
				t.Fatalf("error=%v pastes=%d enters=%d", err, pastes.Load(), enters.Load())
			}
		})
	}
}

// Run only against an isolated scratch daemon; never an existing user session.
func TestHerdrLiveQueuedQuestionFollowup(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_QUESTION_PROBE")
	if !strings.HasPrefix(session, "manifest-question-probe-") {
		t.Skip("requires dedicated scratch herdr daemon")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: os.Getenv("MANIFEST_HERDR_TEST_CWD")}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config/herdr/sessions", session, "herdr.sock")}
	s.terminal.herdr = h
	w := httptest.NewRecorder()
	s.handleTermCreate(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"kind":"codex","model":"gpt-6-astra","name":"isolated-question-probe"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var se termSession
	if err := json.Unmarshal(w.Body.Bytes(), &se); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if row, ok := s.terminal.find(se.ID); ok {
			_ = s.closeTerm(context.Background(), row)
		}
	}()
	w = receiptInput(s, se.ID, `{"text":"This is an isolated chat transport test. Do not access files or run tools other than request_user_input_async. Use request_user_input_async to ask one question titled Transport probe with options Ready and Wait. Then end your turn with the text Waiting for transport probe.","requestId":"live-question-probe-start"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		se, _ = s.terminal.find(se.ID)
		lines, err := h.Screen(context.Background(), se.Runtime)
		if err == nil && codexQueuedQuestionComposer(lines) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued question composer did not appear: %v %v", lines, err)
		}
		time.Sleep(time.Second)
	}
	w = receiptInput(s, se.ID, `{"text":"Transport probe answer: Ready. Reply only TRANSPORT_FOLLOWUP_OK. Do not use tools or access files.","requestId":"live-question-probe-followup"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetPathValue("id", se.ID)
		tw := httptest.NewRecorder()
		s.handleTermTranscript(tw, r)
		var tr termTranscript
		_ = json.Unmarshal(tw.Body.Bytes(), &tr)
		for _, turn := range tr.Turns {
			if turn.Who == "assistant" {
				for _, block := range turn.Blocks {
					if block.T == "say" && strings.TrimSpace(block.Text) == "TRANSPORT_FOLLOWUP_OK" {
						t.Log("native assistant replied to follow-up")
						return
					}
				}
			}
		}
		if time.Now().After(deadline) {
			lines, _ := h.Screen(context.Background(), se.Runtime)
			t.Fatalf("native reply absent: %v", lines)
		}
		time.Sleep(time.Second)
	}
}
