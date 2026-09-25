package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/tasks"
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
	// Supervision is the turn-marker projection (taskThreadSupervision), only
	// when it names an interrupted or failed turn — the rail otherwise read
	// "Idle" while the thread said disconnected/failed.
	Supervision *chatSupervision `json:"supervision,omitempty"`
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
	now := time.Now()
	out := []taskThreadRow{}
	for _, id := range ids {
		thread := s.listThread(id)
		if len(thread) == 0 {
			continue
		}
		text, open, known := s.resolveTaskThread(doc, id)
		last := thread[len(thread)-1]
		d, delegated := deleg[id]
		if !keepTaskThread(known, open, delegated && activeDelegation(d.State), last.At, now) {
			continue
		}
		row := taskThreadRow{ID: id, Title: text, Domain: taskDomain(id), Updated: last.At, Comments: len(thread),
			LastAuthor: firstNonEmptyStr(last.AuthorName, last.Author), LastAction: last.Action, LastText: snip(last.Text, 140), Open: open}
		if rec := s.readPlanRecord(id); rec.Assignee != "" {
			row.Agent = rec.Assignee
		}
		if delegated {
			row.State, row.Phase = d.State, d.Phase
			if row.Agent == "" && d.Agent != "" {
				row.Agent = d.Agent
			}
		}
		if sv := s.taskThreadSupervision(id); sv.State == supervisionDisconnected || sv.State == supervisionFailed {
			row.Supervision = &sv
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// keepTaskThread is the rail's rule for one conversation: a task the record
// knows is listed whether open or ticked off — the rail files a done task's
// conversation under Archived on its own (owner rule 2026-09-21), and the
// row reports Open so it can; a task no record knows (deleted, archived, a
// QA probe) is no conversation — unless its store could not be read, in
// which case the thread is the only evidence and it stays.
func keepTaskThread(known, open, active bool, updated, now time.Time) bool {
	_, _, _ = active, updated, now
	if !known {
		return open // open doubles as "store unavailable" for an unknown id
	}
	return true
}

// resolveTaskThread reads the task behind a thread: its words, whether it is
// open, and whether any record knows it at all (known false with open true
// means the store that would know is unavailable).
func (s *Server) resolveTaskThread(doc *tasks.Doc, id string) (text string, open, known bool) {
	text, open = s.openTaskTextIn(doc, id)
	if text != id {
		return text, open, true
	}
	switch {
	case strings.HasPrefix(id, "aion:"), strings.HasPrefix(id, "re:"):
		_, _, ok := s.backlogStoreFor(id)
		return id, !ok, false
	case strings.HasPrefix(id, "prop:"):
		return id, s.realestate == nil, false
	default:
		return id, doc == nil, false
	}
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
