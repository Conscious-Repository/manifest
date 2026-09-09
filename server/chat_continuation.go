package server

import (
	"context"
	"fmt"
	"manifest/agentchat"
	"sort"
	"strings"
)

// A continuation snapshot is an attributed context payload, not another copy
// of the source transcript. It is frozen with creation intent for review before
// the first send; ordinary related chats never receive this implicit context.
func codingContinuationContext(source agentchat.Session, body string) (string, int) {
	turns := agentchat.ParseTurns(body)
	parts := make([]string, len(turns))
	for i, t := range turns {
		text := t.Text
		if t.Who != "user" && t.Who != "system" {
			text = agentchat.SayBody(text)
		}
		text = fileTokenRe.ReplaceAllString(text, "(attachment: $2; content not included)")
		parts[i] = fmt.Sprintf("\n[source-turn %d; author %q; at %q]\n%s\n", t.N, t.Who, t.At, text)
	}
	start, total := len(parts), 0
	for i := len(parts) - 1; i >= 0; i-- {
		if total+len(parts[i]) > agentChatWindowChars {
			break
		}
		total += len(parts[i])
		start = i
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Read-only conversation context from %s. Original authors and turn numbers follow. Tool traces and attachment contents are excluded unless separately selected. %d earlier turns omitted.\n", sessionConversation(source).Key, start)
	for _, part := range parts[start:] {
		out.WriteString(part)
	}
	return out.String(), start
}

type codingContinuationView struct {
	ID             string                 `json:"id"`
	Agent          string                 `json:"agent"`
	Model          string                 `json:"model"`
	Created        string                 `json:"created"`
	Conversation   conversationDescriptor `json:"conversation"`
	Turns          []termTurn             `json:"turns"`
	Process        string                 `json:"process"`
	AgentState     string                 `json:"agentState"`
	Connectivity   string                 `json:"connectivity"`
	HistoryOmitted int                    `json:"historyOmitted"`
}

// Project only explicitly continued native sessions. Neither shared task IDs
// nor ordinary related origins authorize sibling history to enter this view.
func (s *Server) codingContinuations(ctx context.Context, source agentchat.Session) []codingContinuationView {
	out := []codingContinuationView{}
	if s.terminal == nil {
		return out
	}
	for _, se := range s.terminal.load() {
		o := se.Origin
		if o == nil || o.Mode != "continue" || o.Backend != "" || o.Agent != source.Agent || o.ID != source.ID || se.Device != "" || !isCodingAgent(se.Kind) {
			continue
		}
		se, tr, ob, _ := s.projectTerminalTranscript(ctx, se, 0)
		out = append(out, codingContinuationView{ID: se.ID, Agent: se.Kind, Model: se.Model, Created: se.CreatedAt, Conversation: s.terminalConversation(se), Turns: tr.Turns, Process: ob.Process, AgentState: ob.AgentState, Connectivity: ob.Connectivity, HistoryOmitted: o.HistoryOmitted})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}
