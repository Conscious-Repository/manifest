package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Questions are a projection of native tool calls, not a second conversation store.
type terminalQuestion struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Options []string `json:"options,omitempty"`
	State   string   `json:"state"` // pending, sent, unconfirmed, answered
	Answer  string   `json:"answer,omitempty"`
	Async   bool     `json:"async"`
}
type terminalQuestionAnswer struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}
type nativeQuestionReply struct {
	ID       string `json:"questionItemId"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

const questionReplyOpen = "<send_user_message_question_reply>"
const questionReplyClose = "</send_user_message_question_reply>"

func projectQuestionCall(name, call, args string) []terminalQuestion {
	name = strings.TrimPrefix(name, "functions.")
	if call == "" || (name != "request_user_input_async" && name != "request_user_input") {
		return nil
	}
	var input struct {
		Questions []struct {
			ID       string            `json:"id"`
			Title    string            `json:"title"`
			Question string            `json:"question"`
			Options  []json.RawMessage `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal([]byte(args), &input) != nil {
		return nil
	}
	var out []terminalQuestion
	for i, q := range input.Questions {
		title := q.Title
		if title == "" {
			title = q.Question
		}
		if strings.TrimSpace(title) == "" {
			continue
		}
		id, _ := json.Marshal([]any{name, call, i})
		row := terminalQuestion{ID: string(id), Title: title, State: "pending", Async: name == "request_user_input_async"}
		for _, raw := range q.Options {
			var label string
			if json.Unmarshal(raw, &label) != nil {
				var option struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				}
				if json.Unmarshal(raw, &option) == nil {
					label = option.Label
					if option.Description != "" {
						label += " — " + option.Description
					}
				}
			}
			if strings.TrimSpace(label) != "" {
				row.Options = append(row.Options, label)
			}
		}
		out = append(out, row)
	}
	return out
}
func parseQuestionReply(text string) []nativeQuestionReply {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, questionReplyOpen) || !strings.HasSuffix(text, questionReplyClose) {
		return nil
	}
	var replies []nativeQuestionReply
	if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, questionReplyOpen), questionReplyClose))), &replies) != nil {
		return nil
	}
	return replies
}
func (s *Server) terminalQuestions(se termSession, tr termTranscript) []terminalQuestion {
	out := append([]terminalQuestion(nil), tr.Questions...)
	for _, receipt := range s.terminal.continuationReceipts(se.ID, "") {
		for _, a := range receipt.QuestionAnswers {
			for i := range out {
				if out[i].ID == a.ID && out[i].State == "pending" {
					out[i].State = receipt.State
					out[i].Answer = a.Answer
				}
			}
		}
	}
	return out
}

// Called under the existing session input lock, after idempotent receipt lookup.
func (s *Server) prepareQuestionAnswers(se termSession, answers []terminalQuestionAnswer) (string, error) {
	if se.Kind != "codex" || len(answers) == 0 || len(answers) > 20 {
		return "", fmt.Errorf("unsupported question response")
	}
	tr, ok := readTranscript(se.Kind, s.terminal.transcriptPath(se), 0)
	if !ok {
		return "", fmt.Errorf("question history is unavailable; nothing sent")
	}
	questions := s.terminalQuestions(se, tr)
	byID := map[string]terminalQuestion{}
	for _, q := range questions {
		byID[q.ID] = q
	}
	seen := map[string]bool{}
	replies := []nativeQuestionReply{}
	for _, a := range answers {
		q, exists := byID[a.ID]
		if !exists || !q.Async || q.State != "pending" || seen[a.ID] {
			return "", fmt.Errorf("question is no longer awaiting an answer; refresh the conversation")
		}
		if strings.TrimSpace(a.Answer) == "" || len(a.Answer) > 32000 {
			return "", fmt.Errorf("each answer must contain between 1 and 32000 bytes")
		}
		seen[a.ID] = true
		replies = append(replies, nativeQuestionReply{ID: q.ID, Question: q.Title, Answer: a.Answer})
	}
	raw, _ := json.Marshal(replies)
	return questionReplyOpen + "\n" + string(raw) + "\n" + questionReplyClose, nil
}
