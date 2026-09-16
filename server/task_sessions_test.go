package server

import (
	"encoding/json"
	"testing"
	"time"

	"manifest/chatstate"
	"manifest/threads"
)

// A session's turns project into the task timeline as comments from the
// owner and the agent, tagged with the session; the launch prompt and empty
// turns stay out.
func TestSessionTurnComments(t *testing.T) {
	se := termSession{ID: "abc123", Kind: "codex"}
	owner := threads.Identity{ID: "owner", Name: "Benjamin"}
	turns := []termTurn{
		{ID: "u0", Who: "user", TS: "2026-09-16T02:19:25Z", Text: "Read the complete work order at /x/brief.md and carry it through."},
		{ID: "a1", Who: "assistant", TS: "2026-09-16T02:45:10.5Z", Blocks: []termBlock{{T: "step", Cast: "exec", Input: "ls"}, {T: "say", Text: "The plan is ready."}}},
		{ID: "u2", Who: "user", TS: "2026-09-16T02:47:43Z", Text: "cool"},
		{ID: "a3", Who: "assistant", TS: "2026-09-16T02:47:45Z", Blocks: []termBlock{{T: "step", Cast: "exec", Input: "git status"}}},
		{ID: "x", Who: "user", TS: "not a time", Text: "dropped"},
	}
	got := sessionTurnComments(se, "manifest/task", turns, owner)
	if len(got) != 2 {
		t.Fatalf("want 2 comments, got %d: %+v", len(got), got)
	}
	if got[0].Author != "agent:codex" || got[0].AuthorName != "Codex" || got[0].Text != "The plan is ready." || got[0].ID != "session:abc123:a1" {
		t.Fatalf("assistant turn: %+v", got[0])
	}
	if got[1].Author != "owner" || got[1].AuthorName != "Benjamin" || got[1].Text != "cool" {
		t.Fatalf("owner turn: %+v", got[1])
	}
	chat, _ := got[0].Meta["chat"].(map[string]any)
	if got[0].Meta["from"] != "session" || chat["agent"] != "codex" || chat["id"] != "abc123" {
		t.Fatalf("session tag: %+v", got[0].Meta)
	}
}

// The task thread projects into the session's chat as system lines — owner
// comments and markers only, agent comments left to the transcript — and a
// full read interleaves them by time.
func TestBoardThreadTurnsAndMerge(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	s := &Server{}
	thread := []threads.Comment{
		{ID: "c1", Action: "assign", Author: "owner", AuthorName: "Benjamin", Text: "assigned to agent:codex (asked)", At: at("2026-09-16T02:19:00Z")},
		{ID: "c2", Action: "comment", Author: "owner", AuthorName: "Benjamin", Text: "work on a plan for this", At: at("2026-09-16T02:19:01Z")},
		{ID: "c3", Action: "comment", Author: "agent:codex", AuthorName: "Codex", Text: "Started with model x", At: at("2026-09-16T02:19:02Z")},
		{ID: "c4", Action: "result", Author: "system", AuthorName: "system", At: at("2026-09-16T02:50:00Z")},
	}
	var lines []termTurn
	for _, c := range thread {
		lines = append(lines, projectThreadComment(c)...)
	}
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %+v", lines)
	}
	if lines[0].ID != "thread:c1" || lines[0].Who != "system" || lines[0].Text != "Task · Benjamin · assign — assigned to agent:codex (asked)" {
		t.Fatalf("assign line: %+v", lines[0])
	}
	if lines[1].Text != "Task · Benjamin: work on a plan for this" || lines[2].Text != "Task · system · result" {
		t.Fatalf("lines: %+v", lines[1:])
	}
	native := []termTurn{
		{ID: "u0", Who: "user", TS: "2026-09-16T02:19:25Z", Text: "Read the complete work order…"},
		{ID: "a1", Who: "assistant", TS: "2026-09-16T02:45:10Z"},
	}
	merged := mergeThreadTurns(native, lines, true)
	order := []string{}
	for _, m := range merged {
		order = append(order, m.ID)
	}
	want := "thread:c1 thread:c2 u0 a1 thread:c4"
	if got := joinIDs(order); got != want {
		t.Fatalf("full read order: %s (want %s)", got, want)
	}
	tail := mergeThreadTurns([]termTurn{{ID: "a9", Who: "assistant", TS: "2026-09-16T03:00:00Z"}}, lines, false)
	if tail[0].ID != "a9" || len(tail) != 4 {
		t.Fatalf("tail keeps arrival order: %+v", tail)
	}
	_ = s
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " "
		}
		out += id
	}
	return out
}

// projectThreadComment is boardThreadTurns for one comment (the store-free
// half of it), so the projection rules can be tested without a thread store.
func projectThreadComment(c threads.Comment) []termTurn {
	s := &Server{}
	return s.projectThreadComments([]threads.Comment{c})
}

// Done archives the task's sessions in the inbox lifecycle slot, with the
// rail's own record shape, and never overrides an explicit state.
func TestArchiveChatKeys(t *testing.T) {
	s := &Server{chatState: chatstate.New(t.TempDir())}
	s.archiveChatKeys([]string{"terminal:codex/aaa", "terminal:codex/bbb"})
	snap, err := s.chatState.Read("inbox", "lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Items map[string]string `json:"items"`
	}
	if err := json.Unmarshal(snap.Value, &value); err != nil {
		t.Fatal(err)
	}
	if value.Items["terminal:codex/aaa"] != "archived" || value.Items["terminal:codex/bbb"] != "archived" {
		t.Fatalf("not archived: %+v", value.Items)
	}
	// a chat the owner had deleted stays deleted; one already archived is untouched
	raw, _ := json.Marshal(map[string]any{"items": map[string]string{"terminal:codex/aaa": "deleted"}})
	if _, err := s.chatState.Write("inbox", "lifecycle", snap.Revision, raw); err != nil {
		t.Fatal(err)
	}
	s.archiveChatKeys([]string{"terminal:codex/aaa", "terminal:codex/ccc"})
	snap, _ = s.chatState.Read("inbox", "lifecycle")
	_ = json.Unmarshal(snap.Value, &value)
	if value.Items["terminal:codex/aaa"] != "deleted" || value.Items["terminal:codex/ccc"] != "archived" {
		t.Fatalf("explicit state overridden: %+v", value.Items)
	}
}
