package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func questionCallFixture() string {
	return `{"type":"response_item","payload":{"type":"function_call","name":"request_user_input_async","call_id":"call_one","arguments":"{\"questions\":[{\"title\":\"Which option?\",\"options\":[\"One\",\"Two\"]},{\"title\":\"Anything else?\"}]}"}}` + "\n"
}
func questionRollout(t *testing.T, s *Server, se termSession) termSession {
	t.Helper()
	se.Kind = "codex"
	se.LaunchPhase = "active"
	se.Started = true
	se.Runtime = herdrFixtureID(t, s.terminal.herdr)
	se.ResumeID = "01a084b2-78a3-73e0-82a1-150d710697a7"
	s.terminal.codexSessions = t.TempDir()
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": se.ResumeID, "cwd": se.Cwd}})
	path := filepath.Join(s.terminal.codexSessions, "rollout-test-"+se.ResumeID+".jsonl")
	if err := os.WriteFile(path, append(append(meta, '\n'), []byte(questionCallFixture())...), 0600); err != nil {
		t.Fatal(err)
	}
	s.terminal.upsert(se)
	return se
}
func TestQuestionProjectionAndNativeReply(t *testing.T) {
	tr := parseCodexTranscript(strings.NewReader(questionCallFixture()))
	if len(tr.Questions) != 2 || tr.Questions[0].ID != `["request_user_input_async","call_one",0]` || len(tr.Questions[0].Options) != 2 || len(tr.Questions[1].Options) != 0 {
		t.Fatalf("questions lost: %+v", tr.Questions)
	}
	raw, _ := json.Marshal([]nativeQuestionReply{{ID: tr.Questions[0].ID, Question: tr.Questions[0].Title, Answer: "My own answer"}})
	text := questionReplyOpen + "\n" + string(raw) + "\n" + questionReplyClose
	reply, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": text}}}})
	full := parseCodexTranscript(strings.NewReader(questionCallFixture() + string(reply) + "\n"))
	if full.Questions[0].State != "answered" || full.Questions[1].State != "pending" {
		t.Fatal(full.Questions)
	}
	if full.Turns[len(full.Turns)-1].Text != text {
		t.Fatal("reply bytes must survive for receipt matching")
	}
	for _, args := range []string{`bad json`, `{"questions":[{}]}`} {
		if got := projectQuestionCall("request_user_input_async", "call", args); len(got) != 0 {
			t.Fatal(got)
		}
	}
	if got := projectQuestionCall("other_tool", "call", `{"questions":[{"title":"Not a question tool"}]}`); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestQuestionAnswerDeliveryAndReplay(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "sent", true: "uncertain"}[uncertain], func(t *testing.T) {
			s, se, prompts := terminalReceiptFixture(t, uncertain)
			se = questionRollout(t, s, se)
			answers := []terminalQuestionAnswer{{ID: `["request_user_input_async","call_one",0]`, Answer: "Two"}}
			payload, _ := json.Marshal(terminalInput{RequestID: "receipt-input-001", QuestionAnswers: answers})
			first := receiptInput(s, se.ID, string(payload))
			if first.Code != 200 && first.Code != 202 {
				t.Fatal(first.Code, first.Body.String())
			}
			if prompts.Load() != 1 {
				t.Fatal("missing send")
			}
			receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
			if err != nil || len(receipt.QuestionAnswers) != 1 {
				t.Fatal(receipt, err)
			}
			text, err := s.prepareQuestionAnswers(se, answers)
			if err == nil || text != "" {
				t.Fatal("duplicate logical answer allowed")
			}
			again := receiptInput(s, se.ID, string(payload))
			if again.Code != first.Code || prompts.Load() != 1 {
				t.Fatal("retry replayed answer", again.Body.String())
			}
			tr, _ := readTranscript("codex", s.terminal.transcriptPath(se), 0)
			qs := s.terminalQuestions(se, tr)
			want := "sent"
			if uncertain {
				want = "unconfirmed"
			}
			if qs[0].State != want || qs[1].State != "pending" {
				t.Fatal(qs)
			}
			// Projection must not mutate the cached transcript.
			if tr.Questions[0].State != "pending" {
				t.Fatal("cache was mutated")
			}
		})
	}
}
func TestQuestionAnswerValidation(t *testing.T) {
	s, se, prompts := terminalReceiptFixture(t, false)
	se = questionRollout(t, s, se)
	id := `["request_user_input_async","call_one",0]`
	for _, answers := range [][]terminalQuestionAnswer{{{ID: "another-session", Answer: "x"}}, {{ID: id, Answer: "  "}}, {{ID: id, Answer: "a"}, {ID: id, Answer: "b"}}} {
		if _, err := s.prepareQuestionAnswers(se, answers); err == nil {
			t.Fatal("invalid answer accepted", answers)
		}
	}
	body, _ := json.Marshal(terminalInput{RequestID: "receipt-input-001", Text: "unrelated", QuestionAnswers: []terminalQuestionAnswer{{ID: id, Answer: "x"}}})
	if w := receiptInput(s, se.ID, string(body)); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if prompts.Load() != 0 {
		t.Fatal("invalid response crossed runtime boundary")
	}
}
