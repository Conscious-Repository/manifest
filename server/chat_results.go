package server

import (
	"manifest/agentchat"
	"sort"
	"strings"
)

type chatCodingResult struct {
	ID      string `json:"id"`
	Agent   string `json:"agent"`
	Task    string `json:"task"`
	Outcome string `json:"outcome"`
	Started string `json:"started"`
	Body    string `json:"body"`
}

// Project the durable report; never manufacture an assistant turn or copy its
// text into the planning transcript. Multiple tasks may originate in this chat.
func (s *Server) chatCodingResults(sess agentchat.Session) []chatCodingResult {
	out := []chatCodingResult{}
	matches := s.chatTaskMatcher(sess)
	for _, agent := range []string{"claude", "codex"} {
		h := s.findHarness(agent)
		if h == nil || h.Spirits == nil {
			continue
		}
		for _, run := range h.Spirits.Runs() {
			if run.Spirit != agent || run.Run != run.ID || (run.Outcome != "completed" && run.Outcome != "failed") {
				continue
			}
			tasks := map[string]bool{}
			for _, m := range todoTokenRe.FindAllStringSubmatch(run.Request, -1) {
				tasks[strings.TrimSpace(m[1])] = true
			}
			if len(tasks) != 1 {
				continue
			}
			for task := range tasks {
				if !matches(task) {
					continue
				}
				current, body, ok := h.Spirits.Run(run.ID)
				// A report replaced between list and read must be reconsidered on
				// the next poll, rather than attributed using stale task metadata.
				if !ok || current.Request != run.Request || current.Outcome != run.Outcome || current.Spirit != agent || current.Run != run.ID {
					continue
				}
				out = append(out, chatCodingResult{run.ID, agent, task, run.Outcome, run.Started, body})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Started != out[j].Started {
			return out[i].Started < out[j].Started
		}
		return out[i].Agent+out[i].ID < out[j].Agent+out[j].ID
	})
	return out
}
