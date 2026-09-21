package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/threads"
)

// taskThreadRow is one task conversation as the CHAT rail lists it
// (2026-09-21): the task's own words, who holds it, the newest visible
// comment, and the live delegation state — so a task an agent is working
// stands in the chat list beside the agent and coding sessions instead of
// living only behind the TASKS board's panel.
type taskThreadRow struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Domain     string    `json:"domain,omitempty"`
	Agent      string    `json:"agent,omitempty"` // the assignee token ("agent:alfred"), when known
	Updated    time.Time `json:"updated"`
	Comments   int       `json:"comments"`
	LastAuthor string    `json:"lastAuthor,omitempty"`
	LastAction string    `json:"lastAction,omitempty"`
	LastText   string    `json:"lastText,omitempty"`
	State      string    `json:"state,omitempty"` // delegation state (plan-running, running, plan-ready, …)
	Phase      string    `json:"phase,omitempty"`
	Open       bool      `json:"open"` // the task record is open, or could not be resolved
}

// taskDomain is the id's namespace: "manifest/…" → manifest, "aion:…" → aion.
func taskDomain(id string) string {
	if i := strings.IndexAny(id, "/:"); i > 0 {
		return id[:i]
	}
	return ""
}

// taskThreads lists every task that has a conversation, newest activity
// first. A task the record resolves as closed drops out (its record stays
// under TASKS); an unresolvable id stays, since the thread is real.
func (s *Server) taskThreads() []taskThreadRow {
	if s.threads == nil {
		return []taskThreadRow{}
	}
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if s.threads.private != nil {
		for _, id := range s.threads.private.TaskIDs() {
			add(id)
		}
	}
	if s.threads.re != nil {
		for _, id := range s.threads.re.TaskIDs() {
			add(id)
		}
	}
	if s.threads.aion != nil {
		for item := range s.threads.aion.Ext().Comments {
			add("aion:" + item)
		}
	}
	doc := s.tasksDocOrNil()
	deleg := s.delegationIndex()
	out := []taskThreadRow{}
	for _, id := range ids {
		thread := s.listThread(id)
		if len(thread) == 0 {
			continue
		}
		text, open := s.openTaskTextIn(doc, id)
		known := text != id
		if known && !open {
			continue
		}
		last := thread[len(thread)-1]
		row := taskThreadRow{ID: id, Title: text, Domain: taskDomain(id), Updated: last.At, Comments: len(thread),
			LastAuthor: firstNonEmptyStr(last.AuthorName, last.Author), LastAction: last.Action, LastText: snip(last.Text, 140), Open: true}
		if rec := s.readPlanRecord(id); rec.Assignee != "" {
			row.Agent = rec.Assignee
		}
		if d, ok := deleg[id]; ok {
			row.State, row.Phase = d.State, d.Phase
			if row.Agent == "" && d.Agent != "" {
				row.Agent = d.Agent
			}
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func snip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

// GET /api/tasks/threads — every task conversation, for the CHAT rail.
func (s *Server) handleTaskThreads(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"threads": s.taskThreads()})
}

var _ = threads.ActComment // the row's LastAction is one of the threads actions
