package server

import (
	"crypto/sha256"
	"fmt"
	"manifest/agentchat"
	"manifest/artifacts"
	"net/http"
	"sort"
	"strings"
	"time"
)

type chatCodingResult struct {
	ID      string `json:"id"`
	Agent   string `json:"agent"`
	Task    string `json:"task"`
	Outcome string `json:"outcome"`
	Started string `json:"started"`
	Body    string `json:"body"`
	Hash    string `json:"hash"`
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
				out = append(out, chatCodingResult{run.ID, agent, task, run.Outcome, run.Started, body, fmt.Sprintf("%x", sha256.Sum256([]byte(body)))})
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

func (s *Server) handleChatCodingResult(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	if s.artifactReg == nil {
		http.Error(w, "artifact registry unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Agent, Run, Hash string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	sess, _, _, ok := s.agentChat.store.Get(r.PathValue("agent"), r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	for _, result := range s.chatCodingResults(sess) {
		if result.Agent != b.Agent || result.ID != b.Run {
			continue
		}
		if b.Hash == "" || result.Hash != b.Hash {
			http.Error(w, "The coding result changed. Reopen its current version.", http.StatusConflict)
			return
		}
		res, err := s.artifactReg.Put(artifacts.Put{
			Kind: artifacts.KindReport, Title: result.Agent + " coding result", Harness: result.Agent, Ref: "artifacts/runs/" + result.ID + ".md",
			Content: []byte(result.Body), Actor: result.Agent, At: time.Now(),
			Provenance: artifacts.Provenance{Source: "run", Task: result.Task, Run: result.ID},
		})
		if err != nil {
			httpError(w, err)
			return
		}
		s.artifactEvent(res, result.Agent)
		writeJSON(w, map[string]any{"id": res.Artifact.ID, "revision": res.Artifact.Head, "task": result.Task})
		return
	}
	http.NotFound(w, r)
}
