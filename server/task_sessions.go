package server

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"manifest/chatstate"
	"manifest/threads"
)

// One task, one timeline (owner report 2026-09-16).
//
// A task assigned to a coding agent from the board is worked in a terminal
// session with its own transcript, while the task's thread holds the
// assignment, the owner's comments and the run markers. The two views showed
// different halves of the same work: the task card knew nothing of what the
// agent actually said, and the agent's chat thread knew nothing of the
// assignment or the owner's notes. This is how Linear's agent sessions and
// GitHub's coding agent present it — the work item has one activity feed,
// the agent's session is an entry in it, and the session view carries the
// item's activity too. Both directions here are read-time projections: no
// line is copied into either store, so they cannot drift apart. When the task
// is done, the sessions that worked it are archived with it.

// taskBoardSessions returns the board sessions whose work order is this task,
// oldest first.
func (s *Server) taskBoardSessions(id string) []termSession {
	if s.terminal == nil {
		return nil
	}
	var out []termSession
	for _, se := range s.terminal.load() {
		if se.BoardBrief == "" || se.Device != "" {
			continue
		}
		for _, l := range s.terminalConversation(se).Links {
			if l.Kind == "task" && l.ID == id {
				out = append(out, se)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

// taskTimeline is the task thread with its board sessions' turns interleaved
// by time: the owner's prompts and the agent's replies as comments tagged
// meta.from="session" + meta.chat{agent,id}, so the panel can say where a
// line came from and open it there.
func (s *Server) taskTimeline(id string, thread []threads.Comment) []threads.Comment {
	out := append([]threads.Comment(nil), thread...)
	for _, se := range s.taskBoardSessions(id) {
		path := s.terminal.transcriptPath(se)
		if path == "" {
			continue
		}
		tr, ok := readTranscript(se.Kind, path, 0)
		if !ok {
			continue
		}
		out = append(out, sessionTurnComments(se, id, tr.Turns, s.ownerIdentity())...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// sessionTurnComments projects a session's turns as thread comments. The
// launch prompt is skipped (the thread already records the assignment),
// activity blocks stay with the chat view, and an assistant turn is what it
// said.
func sessionTurnComments(se termSession, task string, turns []termTurn, owner threads.Identity) []threads.Comment {
	var out []threads.Comment
	for _, t := range turns {
		at, err := parseTurnTime(t.TS)
		if err != nil {
			continue
		}
		meta := map[string]any{"from": "session", "chat": map[string]any{"agent": se.Kind, "id": se.ID}}
		id := "session:" + se.ID + ":" + t.ID
		switch t.Who {
		case "user":
			text := strings.TrimSpace(t.Text)
			if t.WorkOrder || strings.HasPrefix(text, "Read the complete work order at ") || text == "" {
				continue
			}
			out = append(out, threads.Comment{ID: id, TaskID: task, Action: threads.ActComment, Author: owner.ID, AuthorName: owner.Name, Text: t.Text, Meta: meta, At: at})
		case "assistant":
			text := turnSayText(t)
			if text == "" {
				continue
			}
			out = append(out, threads.Comment{ID: id, TaskID: task, Action: threads.ActComment, Author: "agent:" + se.Kind, AuthorName: agentDisplayName("agent:" + se.Kind), Text: text, Meta: meta, At: at})
		}
	}
	return out
}

// turnSayText is an assistant turn's prose: its say blocks joined, or its
// text when the projection carried none.
func turnSayText(t termTurn) string {
	var parts []string
	for _, b := range t.Blocks {
		if b.T == "say" {
			if txt := strings.TrimSpace(b.Text); txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	if len(parts) == 0 {
		return strings.TrimSpace(t.Text)
	}
	return strings.Join(parts, "\n\n")
}

func parseTurnTime(ts string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
	}
	return t, err
}

// boardThreadTurns projects the task thread into a board session's chat
// thread: the owner's comments and the structural markers (assign, fire,
// plan, questions, result) as system lines with stable ids (thread:<comment>)
// the client de-duplicates across tails. Agent-authored comments are left
// out — the transcript already speaks for the agent.
func (s *Server) boardThreadTurns(task string) []termTurn {
	return s.projectThreadComments(s.listThread(task))
}

func (s *Server) projectThreadComments(thread []threads.Comment) []termTurn {
	var out []termTurn
	for _, c := range thread {
		if strings.HasPrefix(c.Author, "agent:") {
			continue
		}
		if from, _ := c.Meta["from"].(string); from == "session" || from == "chat" {
			continue
		}
		who := c.AuthorName
		if who == "" {
			who = c.Author
		}
		text := strings.TrimSpace(c.Text)
		var label string
		switch {
		case c.Action == threads.ActComment || c.Action == "":
			if text == "" {
				continue
			}
			label = who + ": " + text
		case text != "":
			label = who + " · " + c.Action + " — " + text
		default:
			label = who + " · " + c.Action
		}
		out = append(out, termTurn{ID: "thread:" + c.ID, Who: "system", TS: c.At.UTC().Format(time.RFC3339Nano), Text: "Task · " + label})
	}
	return out
}

// mergeThreadTurns appends the projected thread lines to a transcript read.
// A full read is re-ordered by time so the lines land where they happened;
// a tail keeps its arrival order (the client de-duplicates by id).
func mergeThreadTurns(turns, extra []termTurn, full bool) []termTurn {
	if len(extra) == 0 {
		return turns
	}
	out := append(append([]termTurn(nil), turns...), extra...)
	if !full {
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, ea := parseTurnTime(out[i].TS)
		b, eb := parseTurnTime(out[j].TS)
		if ea != nil || eb != nil {
			return false // unparsable stamps keep their place
		}
		return a.Before(b)
	})
	return out
}

// conversationTaskLink is the single task a conversation descriptor links.
func conversationTaskLink(d conversationDescriptor) string {
	task := ""
	for _, l := range d.Links {
		if l.Kind == "task" {
			if task != "" {
				return ""
			}
			task = l.ID
		}
	}
	return task
}

// archiveTaskChats archives the board sessions that worked a task once it is
// done: the work is over, so its chats leave the active list (they come back
// from Archived chats). Nothing else about the sessions changes.
func (s *Server) archiveTaskChats(id string) {
	var keys []string
	for _, se := range s.taskBoardSessions(id) {
		keys = append(keys, "terminal:"+se.Kind+"/"+se.ID)
	}
	s.archiveChatKeys(keys)
}

// archiveChatKeys marks conversations archived in the inbox lifecycle slot —
// the same {items: key → status} record the chat rail keeps, written with its
// revision so a concurrent change from the rail is retried, not clobbered.
func (s *Server) archiveChatKeys(keys []string) {
	if s.chatState == nil || len(keys) == 0 {
		return
	}
	for attempt := 0; attempt < 4; attempt++ {
		snap, err := s.chatState.Read("inbox", "lifecycle")
		if err != nil {
			return
		}
		var value struct {
			Items map[string]string `json:"items"`
		}
		if len(snap.Value) > 0 {
			_ = json.Unmarshal(snap.Value, &value)
		}
		if value.Items == nil {
			value.Items = map[string]string{}
		}
		changed := false
		for _, key := range keys {
			if value.Items[key] == "" { // never un-delete, never re-archive a restored chat's explicit state
				value.Items[key] = "archived"
				changed = true
			}
		}
		if !changed {
			return
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return
		}
		if _, err := s.chatState.Write("inbox", "lifecycle", snap.Revision, raw); !errors.Is(err, chatstate.ErrConflict) {
			return
		}
	}
}
