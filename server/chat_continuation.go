package server

import (
	"context"
	"encoding/json"
	"fmt"
	"manifest/agentchat"
	"sort"
	"strings"
	"time"
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
	Cwd              string                          `json:"cwd"`
	ID               string                          `json:"id"`
	Agent            string                          `json:"agent"`
	Model            string                          `json:"model"`
	Created          string                          `json:"created"`
	Conversation     conversationDescriptor          `json:"conversation"`
	Turns            []termTurn                      `json:"turns"`
	Process          string                          `json:"process"`
	AgentState       string                          `json:"agentState"`
	Connectivity     string                          `json:"connectivity"`
	HistoryOmitted   int                             `json:"historyOmitted"`
	HistoryAvailable bool                            `json:"historyAvailable"`
	Submissions      map[string]terminalInputReceipt `json:"submissions,omitempty"`
	PlanRevisions    map[string]chatPlanRevision     `json:"planRevisions,omitempty"`
}

// Project only explicitly continued native sessions. Neither shared task IDs
// nor ordinary related origins authorize sibling history to enter this view.
func (s *Server) codingContinuations(ctx context.Context, source agentchat.Session) []codingContinuationView {
	return s.codingContinuationsFor(ctx, "", source.Agent, source.ID, sessionConversation(source).Key)
}

func (s *Server) terminalCodingContinuations(ctx context.Context, source termSession) []codingContinuationView {
	return s.codingContinuationsFor(ctx, "terminal", source.Kind, source.ID, s.terminalConversation(source).Key)
}

func (s *Server) codingContinuationsFor(ctx context.Context, backend, agent, id, key string) []codingContinuationView {
	out := []codingContinuationView{}
	if s.terminal == nil {
		return out
	}
	for _, se := range s.terminal.load() {
		o := se.Origin
		if o == nil || o.Mode != "continue" || o.Backend != backend || o.Agent != agent || o.ID != id || se.Device != "" || !isCodingAgent(se.Kind) {
			continue
		}
		se, tr, ob, _ := s.projectTerminalTranscript(ctx, se, 0)
		// Never modify the native parser cache. Only an exact submitted-text hash
		// permits the canonical view to show the owner's text without its envelope.
		turns, submissions := s.terminal.projectContinuationTurns(se.ID, key, tr.Turns)
		out = append(out, codingContinuationView{ID: se.ID, Agent: se.Kind, Model: se.Model, Cwd: se.Cwd, Created: se.CreatedAt, Conversation: s.terminalConversation(se), Turns: turns, Process: ob.Process, AgentState: ob.AgentState, Connectivity: ob.Connectivity, HistoryOmitted: o.HistoryOmitted, HistoryAvailable: tr.Available, Submissions: submissions, PlanRevisions: s.nativePlanRevisions(se, tr.Turns)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (c *termCfg) projectContinuationTurns(id, key string, native []termTurn) ([]termTurn, map[string]terminalInputReceipt) {
	turns := append([]termTurn{}, native...)
	receipts := c.continuationReceipts(id, key)
	submissions := map[string]terminalInputReceipt{}
	for i, t := range turns {
		if t.Who == "user" {
			if receipt, ok := matchingInputReceipt(receipts, t.Text); ok {
				turns[i].Text = receipt.Text
				submissions[t.ID] = receipt
			}
		}
	}
	return turns, submissions
}

type continuationNativeSource struct {
	Agent string `json:"agent"`
	Model string `json:"model"`
	ID    string `json:"id"`
	Route string `json:"route"`
}

func laterConversationTimestamp(candidate, current string) bool {
	a, err := time.Parse(time.RFC3339Nano, candidate)
	if err != nil {
		return false
	}
	b, err := time.Parse(time.RFC3339Nano, current)
	return err != nil || a.After(b)
}

type conversationTimelineTurn struct {
	N            any                       `json:"n"`
	Who          string                    `json:"who"`
	TS           string                    `json:"ts"`
	USD          string                    `json:"usd,omitempty"`
	Text         string                    `json:"text,omitempty"`
	Blocks       []termBlock               `json:"blocks,omitempty"`
	Native       *continuationNativeSource `json:"native,omitempty"`
	Submission   *terminalInputReceipt     `json:"submission,omitempty"`
	Delivery     *agentchat.Delivery       `json:"delivery,omitempty"`
	PlanRevision *chatPlanRevision         `json:"planRevision,omitempty"`
	orderTime    time.Time
}

func conversationTimeline(source agentchat.Session, body string, views []codingContinuationView) []conversationTimelineTurn {
	out := []conversationTimelineTurn{}
	stamp := func(at, fallback string) time.Time {
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			t, _ = time.Parse(time.RFC3339Nano, fallback)
		}
		return t
	}
	for _, t := range agentchat.ParseTurns(body) {
		item := conversationTimelineTurn{N: t.N, Who: t.Who, TS: t.At, USD: t.USD, Text: t.Text, orderTime: stamp(t.At, source.Created)}
		for _, delivery := range source.Deliveries {
			if t.Who == "user" && delivery.UserTurn == t.N {
				d := delivery
				item.Delivery = &d
				break
			}
		}
		out = append(out, item)
	}
	for _, v := range views {
		for _, t := range v.Turns {
			who := t.Who
			if who == "assistant" {
				who = "agent:" + v.Agent
			}
			item := conversationTimelineTurn{N: "terminal:" + v.ID + ":" + t.ID, Who: who, TS: t.TS, Text: t.Text, Blocks: t.Blocks, Native: &continuationNativeSource{Agent: v.Agent, Model: v.Model, ID: v.ID, Route: v.Conversation.Route}, orderTime: stamp(t.TS, v.Created)}
			if r, ok := v.Submissions[t.ID]; ok {
				item.Submission = &r
			}
			if p, ok := v.PlanRevisions[t.ID]; ok {
				item.PlanRevision = &p
			}
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].orderTime.Before(out[j].orderTime) })
	return out
}

func logicalContinuationContext(source agentchat.Session, body string, views []codingContinuationView) (string, int) {
	turns := conversationTimeline(source, body, views)
	return timelineContinuationContext(sessionConversation(source).Key, turns)
}

// Native-root conversations include only explicit continuations. Never
// turn ordinary related chats or task membership into implicit shared context.
func (s *Server) terminalPlanningChildren(root termSession) []agentchat.Session {
	if s.agentChat == nil {
		return nil
	}
	var children []agentchat.Session
	for _, agent := range s.agentChat.store.Agents() {
		for _, child := range s.agentChat.store.List(agent) {
			o := child.Origin
			if o != nil && o.Mode == "continue" && o.Backend == "terminal" && o.Agent == root.Kind && o.ID == root.ID {
				children = append(children, child)
			}
		}
	}
	return children
}

func (s *Server) terminalPlanningTimeline(ctx context.Context, root termSession) ([]conversationTimelineTurn, bool) {
	children := s.terminalPlanningChildren(root)
	coding := s.terminalCodingContinuations(ctx, root)
	if len(children) == 0 && len(coding) == 0 {
		return nil, false
	}
	return s.terminalRootTimeline(ctx, root, children, coding), true
}

func (s *Server) terminalRootTimeline(ctx context.Context, root termSession, children []agentchat.Session, coding []codingContinuationView) []conversationTimelineTurn {
	_, tr, _, _ := s.projectTerminalTranscript(ctx, root, 0)
	turns, submissions := s.terminal.projectContinuationTurns(root.ID, s.terminalConversation(root).Key, tr.Turns)
	views := append([]codingContinuationView{{ID: root.ID, Agent: root.Kind, Model: root.Model, Created: root.CreatedAt, Conversation: s.terminalConversation(root), Turns: turns, Submissions: submissions, PlanRevisions: s.nativePlanRevisions(root, tr.Turns)}}, coding...)
	timeline := conversationTimeline(agentchat.Session{}, "", views)
	for _, child := range children {
		fresh, body, _, ok := s.agentChat.store.Get(child.Agent, child.ID)
		if !ok {
			continue
		}
		proposals := s.chatPlanRevisions(fresh, body)
		for _, turn := range conversationTimeline(fresh, body, nil) {
			for _, proposal := range proposals {
				if proposal.ReplyTurn == turn.N {
					p := proposal
					turn.PlanRevision = &p
					break
				}
			}
			turn.N = fmt.Sprintf("chat:%s/%s:%v", child.Agent, child.ID, turn.N)
			timeline = append(timeline, turn)
		}
	}
	sort.SliceStable(timeline, func(i, j int) bool { return timeline[i].orderTime.Before(timeline[j].orderTime) })
	return timeline
}

func timelineContinuationContext(key string, turns []conversationTimelineTurn) (string, int) {
	parts := make([]string, len(turns))
	for i, t := range turns {
		text := t.Text
		if t.Who != "user" && t.Who != "system" {
			if t.Native != nil {
				var b strings.Builder
				for _, block := range t.Blocks {
					if block.T == "say" {
						b.WriteString(block.Text + "\n")
					}
				}
				text = b.String()
			} else {
				text = agentchat.SayBody(text)
			}
		}
		text = fileTokenRe.ReplaceAllString(text, "(attachment: $2; content not included)")
		if t.Delivery != nil && t.Delivery.Context != nil {
			manifest, _ := json.Marshal(t.Delivery.Context)
			text = "Recorded message context (references only): " + string(manifest) + "\n" + text
		}
		parts[i] = fmt.Sprintf("\n[source-turn %v; author %q; at %q]\n%s\n", t.N, t.Who, t.TS, text)
	}
	start, total := len(parts), 0
	for i := len(parts) - 1; i >= 0; i-- {
		if total+len(parts[i]) > agentChatWindowChars {
			break
		}
		total += len(parts[i])
		start = i
	}
	return fmt.Sprintf("Read-only conversation context from %s. Original source IDs and authors follow. Tool traces and attachment contents are excluded unless selected separately. %d earlier turns omitted.\n%s", key, start, strings.Join(parts[start:], "")), start
}
