package server

import (
	"encoding/json"
	"fmt"
	"manifest/agentchat"
	"regexp"
	"strings"
)

func (s *Server) planRevisionInstructions(refs []artifactContextRef) string {
	var out strings.Builder
	if s.artifactReg == nil {
		return ""
	}
	for _, ref := range refs {
		if a, ok := s.artifactReg.Get(ref.ID); ok && a.Provenance.Source == "task-plan" {
			fmt.Fprintf(&out, "\nPlan revision workflow: only when the owner requests an edit, propose the full replacement in one fenced manifest-plan-revision JSON block with artifactId=%q, baseRevision=%q, and content (full Markdown string). Do not write the file directly or execute the plan. The owner can review and save this as a new reversible version in Chat. Keep explanatory text outside the block concise. A discussion or summary request does not call for a revision.\n", ref.ID, ref.Revision)
			fmt.Fprintf(&out, "Format:\n```manifest-plan-revision\n{\"artifactId\":%q,\"baseRevision\":%q,\"content\":\"full replacement Markdown here\"}\n```\n", ref.ID, ref.Revision)
		}
	}
	return out.String()
}

var planRevisionFence = regexp.MustCompile("(?s)```(?:manifest-plan-revision|json)[ \\t]*\\r?\\n(.*?)\\r?\\n```")

type chatPlanRevision struct {
	ArtifactID   string `json:"artifactId"`
	BaseRevision string `json:"baseRevision"`
	Content      string `json:"content"`
	Task         string `json:"task"`
	ReplyTurn    int    `json:"replyTurn"`
}

// A proposal is projected from its original reply, never executed by parsing.
// The target must be the exact task-plan version delivered for this reply.
func (s *Server) chatPlanRevisions(sess agentchat.Session, body string) []chatPlanRevision {
	out := []chatPlanRevision{}
	if s.artifactReg == nil {
		return out
	}
	for _, turn := range agentchat.ParseTurns(body) {
		if turn.Who == "user" || turn.Who == "system" {
			continue
		}
		for _, delivery := range sess.Deliveries {
			if delivery.State != agentchat.DeliveryCompleted || delivery.ReplyTurn != turn.N || delivery.Context == nil || delivery.Context.Task == "" {
				continue
			}
			if proposal := s.validatedPlanRevision(agentchat.SayBody(turn.Text), delivery.Context); proposal != nil {
				proposal.ReplyTurn = turn.N
				out = append(out, *proposal)
			}
		}
	}
	return out
}

func (s *Server) validatedPlanRevision(text string, ctx *agentchat.MessageContext) *chatPlanRevision {
	if s.artifactReg == nil || ctx == nil || ctx.Task == "" {
		return nil
	}
	matches := planRevisionFence.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 {
		return nil
	}
	var p chatPlanRevision
	if json.Unmarshal([]byte(matches[0][1]), &p) != nil || strings.TrimSpace(p.Content) == "" || len(p.Content) > 64000 {
		return nil
	}
	for _, ref := range ctx.Artifacts {
		if ref.ID != p.ArtifactID || ref.Revision != p.BaseRevision {
			continue
		}
		a, ok := s.artifactReg.Get(ref.ID)
		if !ok || a.Provenance.Source != "task-plan" || a.Provenance.Task != ctx.Task {
			continue
		}
		if _, ok := a.Revision(ref.Revision); !ok {
			continue
		}
		p.Task = ctx.Task
		p.ReplyTurn = 0
		return &p
	}
	return nil
}

// Match native proposals only to a sent receipt for the immediately preceding
// owner turn. Full transcript inspection makes incremental reads safe too.
func (s *Server) nativePlanRevisions(se termSession, turns []termTurn) map[string]chatPlanRevision {
	out := map[string]chatPlanRevision{}
	if s.artifactReg == nil || s.terminal == nil {
		return out
	}
	receipts := s.terminal.continuationReceipts(se.ID, "")
	var current *terminalInputReceipt
	for _, turn := range turns {
		if turn.Who == "user" {
			current = nil
			if r, ok := receipts[hashTerminalText(turn.Text)]; ok && r.State == "sent" {
				current = &r
			}
			continue
		}
		if turn.Who != "assistant" || current == nil {
			continue
		}
		var text strings.Builder
		for _, block := range turn.Blocks {
			if block.T == "say" {
				text.WriteString(block.Text + "\n")
			}
		}
		if p := s.validatedPlanRevision(text.String(), &agentchat.MessageContext{Task: current.Task, Artifacts: current.Artifacts}); p != nil {
			out[turn.ID] = *p
		}
	}
	return out
}
