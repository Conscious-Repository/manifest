package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/agentchat"
	"manifest/ledger"
	"manifest/threads"
)

// AGENT CHAT ↔ TASK BOARD — the two bridges (agent-chat plan Phase 4, §3.4f).
//
// "→ task" promotes a conversation into a todo: the line is created through
// the capture path (Inbox, or a named personal domain), its id is pinned, the
// conversation window is copied into the todo's thread as the opening entries
// (the owner's turns as the owner, the agent's as the agent, every entry
// tagged meta.from=chat and the first one carrying meta.chat = {agent, id,
// title}), and the agent is assigned. The assignment is RECORD-ONLY — no turn
// is spent (the reply guard, Q6): the thread already holds the context, and
// Ask ✦ / Do ✦ in the panel is the explicit next word. Hermes-family sessions
// remember the task in their frontmatter (`task:`) so the transcript head
// links back; portal threads have no field for it, so their link lives on the
// task side only.
//
// "open in chat" is the reverse: the panel payload names the chat behind a
// task (the promoted entries' meta.chat, else the assignee's rail section),
// and an agent's rail section lists the open todos it holds.

// chatPromoteWindow bounds the turns copied into the thread (newest kept).
const chatPromoteWindow = 12

// chatPromoteTextMax caps one copied entry (under threads.maxCommentLen).
const chatPromoteTextMax = 7800

// chatTurnsFor reads one session on either backend into the shared turn
// grammar: the Hermes-family store directly, a portal thread through its
// transcript renderer. label is the agent's display name.
func (s *Server) chatTurnsFor(agent, id string) (turns []agentchat.Turn, title, label string, ok bool) {
	if ag, isPortal := s.portalChatAgent(agent); isPortal {
		if ag == nil {
			return nil, "", "", false
		}
		t, found := portalChatThread(ag, id)
		if !found {
			return nil, "", "", false
		}
		self, _ := s.portalChatIdentity()
		return agentchat.ParseTurns(portalChatBody(ag, ag.Store.Messages(t.ID), self)), t.Title, ag.Display, true
	}
	if s.agentChat == nil {
		return nil, "", "", false
	}
	sess, body, _, found := s.agentChat.store.Get(agent, id)
	if !found {
		return nil, "", "", false
	}
	return agentchat.ParseTurns(body), sess.Title, agentDisplayName("agent:" + agent), true
}

// chatSlugFor maps an assignee token to its CHAT rail section: alfred/hermes
// → alfred, anything else its bare name ("" for a person).
func chatSlugFor(token string) string {
	name := strings.TrimPrefix(token, "agent:")
	if name == token || name == "" || strings.Contains(name, "::") {
		return ""
	}
	if name == "hermes" {
		return alfredAgent
	}
	return name
}

// taskChatLink is the panel's pointer from a task to its conversation.
type taskChatLink struct {
	Agent string `json:"agent"`           // rail section slug
	Label string `json:"label"`           // display name
	ID    string `json:"id,omitempty"`    // the promoted session ("" = the section only)
	Title string `json:"title,omitempty"` // that session's title
}

// taskChatLink finds the chat a task came from: the first thread entry
// tagged meta.chat (the promote copy), else — when an agent holds the task —
// the agent's section with no session.
func (s *Server) taskChatLink(id string, thread []threads.Comment, assignee string) *taskChatLink {
	for _, c := range thread {
		m, _ := c.Meta["chat"].(map[string]any)
		if m == nil {
			continue
		}
		agent, _ := m["agent"].(string)
		if agent == "" {
			continue
		}
		sid, _ := m["id"].(string)
		title, _ := m["title"].(string)
		return &taskChatLink{Agent: agent, Label: agentDisplayName("agent:" + agent), ID: sid, Title: title}
	}
	if slug := chatSlugFor(assignee); slug != "" && s.agentHarness(assignee) != "" {
		return &taskChatLink{Agent: slug, Label: agentDisplayName(assignee)}
	}
	return nil
}

// POST /api/agents/chat/{agent}/sessions/{id}/promote {turn?, text?, domain?}
// — "→ task". turn is the turn number the affordance was pressed on (0 = the
// whole conversation); text is the todo line ("" = the session title).
func (s *Server) handleAgentChatPromote(w http.ResponseWriter, r *http.Request) {
	agent, id := r.PathValue("agent"), r.PathValue("id")
	if !agentchat.ValidAgent(agent) {
		http.Error(w, "bad agent name", http.StatusBadRequest)
		return
	}
	var b struct {
		Turn   int    `json:"turn"`
		Text   string `json:"text"`
		Domain string `json:"domain"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	turns, title, label, ok := s.chatTurnsFor(agent, id)
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	text := strings.TrimSpace(b.Text)
	if text == "" {
		text = title
	}
	if text == "" || len(turns) == 0 {
		httpError(w, errBadRequest("nothing to promote yet — say something first"))
		return
	}
	taskID, err := s.addPersonalTask(personalTaskAdd{Domain: b.Domain, Text: text})
	if err != nil {
		httpError(w, err)
		return
	}
	if taskID == "" {
		httpError(w, errBadRequest("the todo was written but its id could not be resolved"))
		return
	}
	if pinned, ok := s.pinTaskID(taskID); ok {
		taskID = pinned
	}

	// the window: up to the pressed turn, newest chatPromoteWindow turns
	if b.Turn > 0 {
		for i, t := range turns {
			if t.N == b.Turn {
				turns = turns[:i+1]
				break
			}
		}
	}
	if len(turns) > chatPromoteWindow {
		turns = turns[len(turns)-chatPromoteWindow:]
	}
	token := "agent:" + agent
	copied := 0
	for _, t := range turns {
		if t.Who == "system" {
			continue
		}
		author := s.ownerIdentity()
		body := t.Text
		var files []threads.FileRef
		if t.Who == "user" {
			body, files = s.chatTurnFiles(agent, body)
		} else {
			// the same author id the agent's live turns post under (Alfred's
			// canonical thread id is agent:hermes) — one identity per thread
			author = agentTokenIdentity(token)
			body = agentchat.SayBody(body)
		}
		body = strings.TrimSpace(body)
		if body == "" && len(files) == 0 {
			continue
		}
		if len(body) > chatPromoteTextMax {
			body = body[:chatPromoteTextMax] + "…"
		}
		meta := map[string]any{"from": "chat", "turn": t.N}
		if copied == 0 {
			meta["chat"] = map[string]any{"agent": agent, "id": id, "title": title}
		}
		if _, err := s.addThreadEntry(author, taskID, threads.ActComment, body, nil, files, meta); err != nil {
			httpError(w, err)
			return
		}
		copied++
	}

	// assign — record only, the dispatchAssign shape without a relay
	assigned := s.agentHarness(token) != ""
	if assigned {
		if err := s.setTaskOwner(taskID, token); err != nil {
			assigned = false
		} else {
			_ = s.setPlanAssignee(taskID, token)
			_, _ = s.addThreadEntry(s.ownerIdentity(), taskID, threads.ActAssign,
				"assigned to "+token+" (from chat)", nil, nil, map[string]any{"assignee": token})
		}
	}
	if s.agentChat != nil {
		if _, isPortal := s.portalChatAgent(agent); !isPortal {
			_ = s.agentChat.store.SetTask(agent, id, taskID)
		}
	}
	s.ledger(ledger.Entry{Source: "chat", Kind: "chat.promoted", Actor: "owner", Session: id, Task: taskID,
		Text: ledger.Snip(text, 280), Meta: map[string]any{"agent": agent, "copied": copied, "assigned": assigned}})
	writeJSON(w, map[string]any{"created": taskID, "agent": token, "name": label, "assigned": assigned, "copied": copied})
}

// chatTurnFiles lifts the [file::] tokens off a user turn into thread refs
// when the blob lives in the private thread store (Hermes-family uploads);
// otherwise (portal pool) the name stays in the text as "(attached: name)".
func (s *Server) chatTurnFiles(agent, text string) (string, []threads.FileRef) {
	var files []threads.FileRef
	blobs := s.threads != nil && s.threads.private != nil
	_, isPortal := s.portalChatAgent(agent)
	out := fileTokenRe.ReplaceAllStringFunc(text, func(tok string) string {
		m := fileTokenRe.FindStringSubmatch(tok)
		if m == nil {
			return tok
		}
		if !isPortal && blobs && s.threads.private.BlobPath(m[1]) != "" {
			files = append(files, threads.FileRef{Hash: m[1], Name: m[2]})
			return ""
		}
		return "(attached: " + m[2] + ")"
	})
	return strings.TrimSpace(out), files
}

// agentChatTask is one open todo an agent holds, for its rail section.
type agentChatTask struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Container string `json:"container"`
	State     string `json:"state,omitempty"`  // delegation state (plan-ready, running, …)
	ChatID    string `json:"chatId,omitempty"` // the session it was promoted from
}

// GET /api/agents/chat/{agent}/tasks — the open todos assigned to this agent
// (alfred spans both of its tokens), newest-added first. Backend-agnostic.
func (s *Server) handleAgentChatTasks(w http.ResponseWriter, r *http.Request) {
	agent := r.PathValue("agent")
	if !agentchat.ValidAgent(agent) {
		http.Error(w, "bad agent name", http.StatusBadRequest)
		return
	}
	out := []agentChatTask{}
	tokens := map[string]bool{"agent:" + agent: true}
	if agent == alfredAgent {
		tokens["agent:hermes"] = true
	}
	if s.tasksStore != nil {
		doc, _ := s.tasksStore.Load()
		rows := s.unifiedRows(doc, time.Now())
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Added > rows[j].Added })
		for _, row := range rows {
			if !tokens[row.Owner] {
				continue
			}
			t := agentChatTask{ID: row.ID, Text: row.Text, Container: row.Container.Name}
			if row.Delegation != nil {
				t.State = row.Delegation.State
			}
			if link := s.taskChatLink(row.ID, s.listThread(row.ID), row.Owner); link != nil && link.Agent == agent {
				t.ChatID = link.ID
			}
			out = append(out, t)
		}
	}
	writeJSON(w, map[string]any{"agent": agent, "tasks": out})
}
