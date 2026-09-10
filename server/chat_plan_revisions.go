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
		matches := planRevisionFence.FindAllStringSubmatch(agentchat.SayBody(turn.Text), -1)
		if len(matches) != 1 {
			continue
		}
		var proposal chatPlanRevision
		if json.Unmarshal([]byte(matches[0][1]), &proposal) != nil || strings.TrimSpace(proposal.Content) == "" || len(proposal.Content) > 64000 {
			continue
		}
		for _, delivery := range sess.Deliveries {
			if delivery.State != agentchat.DeliveryCompleted || delivery.ReplyTurn != turn.N || delivery.Context == nil || delivery.Context.Task == "" {
				continue
			}
			for _, ref := range delivery.Context.Artifacts {
				if ref.ID != proposal.ArtifactID || ref.Revision != proposal.BaseRevision {
					continue
				}
				a, ok := s.artifactReg.Get(ref.ID)
				if !ok || a.Provenance.Source != "task-plan" || a.Provenance.Task != delivery.Context.Task {
					continue
				}
				if _, ok := a.Revision(ref.Revision); !ok {
					continue
				}
				proposal.Task = delivery.Context.Task
				proposal.ReplyTurn = turn.N
				out = append(out, proposal)
			}
		}
	}
	return out
}
