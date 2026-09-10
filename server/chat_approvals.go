package server

import (
	"manifest/agentchat"
	"manifest/approvals"
	"manifest/manifestmcp"
)

// Project existing decision records; continuation does not copy or re-key them.
func (s *Server) terminalPlanningOperations(root termSession) []map[string]any {
	conversations := map[string]bool{}
	for _, child := range s.terminalPlanningChildren(root) {
		conversations[child.ID] = true
	}
	out := []map[string]any{}
	if len(conversations) == 0 {
		return out
	}
	for _, operation := range s.syncManifestOperations() {
		if conversations[operation.Conversation] {
			out = append(out, map[string]any{"record": operation, "proposal": manifestmcp.Proposal(operation)})
		}
	}
	return out
}

// Canonical chat redirects must not hide the approvals for the task that
// redirected here. Several tasks can explicitly originate in one conversation.
func (s *Server) chatTaskProposals(sess agentchat.Session) []approvalRow {
	return s.linkedTaskProposals(map[string]bool{approvals.TypeManifestOperation: true}, s.chatTaskMatcher(sess))
}

func (s *Server) terminalTaskProposals(se termSession) []approvalRow {
	tasks := map[string]bool{}
	for _, link := range s.terminalConversation(se).Links {
		if link.Kind == "task" {
			tasks[link.ID] = true
		}
	}
	return s.linkedTaskProposals(map[string]bool{approvals.TypeManifestOperation: true}, func(task string) bool { return tasks[task] })
}

func (s *Server) chatTaskMatcher(sess agentchat.Session) func(string) bool {
	matched := map[string]bool{}
	return func(task string) bool {
		if match, seen := matched[task]; seen {
			return match
		}
		thread := s.listThread(task)
		link := s.taskChatLink(task, thread, "")
		// A conflicting explicit origin must not be overridden by the session's
		// convenience task pointer. Native origin parsing marks ambiguity nil.
		explicit := false
		for _, c := range thread {
			if m, ok := c.Meta["chat"].(map[string]any); ok {
				if agent, _ := m["agent"].(string); agent != "" {
					explicit = true
					break
				}
			}
		}
		match := link != nil && link.Agent == sess.Agent && link.ID == sess.ID
		if !explicit {
			match = task == sess.Task
		}
		matched[task] = match
		return match
	}
}
