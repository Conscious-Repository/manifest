package server

import (
	"manifest/agentchat"
	"manifest/approvals"
	"manifest/threads"
	"path/filepath"
	"testing"
)

func TestCodingChatProjectsOnlyLinkedTaskApprovals(t *testing.T) {
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	s := &Server{approvals: store}
	se := termSession{ID: "abcdef123456", Kind: "codex", Origin: &agentchat.Origin{Task: "inbox/electricians", Agent: "alfred", ID: "source"}}
	p, err := store.Propose(approvals.Proposal{Type: "approval", Agent: "alfred", Action: "Review draft [todo:: inbox/electricians]", Body: "Exact recipients and draft"})
	if err != nil {
		t.Fatal(err)
	}
	rows := s.terminalTaskProposals(se)
	if len(rows) != 1 || rows[0].ID != p.ID || rows[0].Body != p.Body {
		t.Fatalf("coding chat lost shared proposal: %+v", rows)
	}
	if len(s.terminalTaskProposals(termSession{ID: se.ID, Kind: se.Kind, Name: "electricians"})) != 0 {
		t.Fatal("title inferred a task link")
	}
	if err := store.Confirm(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.terminalTaskProposals(se)) != 0 || len(s.taskProposals(se.Origin.Task)) != 0 || len(s.approvalRows(nil)) != 0 {
		t.Fatal("Feed decision did not settle coding chat and task")
	}
}

func TestCanonicalChatProjectsLinkedTaskApprovalsAndSettlesWithFeed(t *testing.T) {
	s, chats, _ := agentChatFixture(t, echoStub)
	s.threads = loopFixture(t).threads
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	s.UseApprovals(store)
	id, err := chats.Create("alfred", "", "Shared work", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = chats.SetTask("alfred", id, "inbox/current"); err != nil {
		t.Fatal(err)
	}
	link := func(task, source string) {
		t.Helper()
		_, err := s.addThreadEntry(s.ownerIdentity(), task, threads.ActComment, "Origin", nil, nil, map[string]any{"chat": map[string]any{"agent": "alfred", "id": source, "canonical": true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	link("inbox/older", id)
	link("inbox/ambiguous", id)
	link("inbox/ambiguous", "20260909-100000-abcd")
	want := map[string]bool{}
	for _, task := range []string{"inbox/current", "inbox/older", "inbox/unrelated", "inbox/ambiguous"} {
		p, err := store.Propose(approvals.Proposal{Type: "approval", Agent: "alfred", Action: "Review [todo:: " + task + "]", Body: "Review exact evidence for " + task})
		if err != nil {
			t.Fatal(err)
		}
		if task == "inbox/current" || task == "inbox/older" {
			want[p.ID] = true
		}
	}
	sess, _, _, _ := chats.Get("alfred", id)
	rows := s.chatTaskProposals(sess)
	if len(rows) != 2 {
		t.Fatalf("wrong linked approvals: %+v", rows)
	}
	for _, row := range rows {
		if !want[row.ID] || row.Action != "Review" || row.Body == "" {
			t.Fatalf("invalid projection: %+v", row)
		}
	}
	code, body := agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions/"+id, nil)
	if code != 200 || len(body["proposals"].([]any)) != 2 {
		t.Fatalf("missing API proposals: %d %v", code, body)
	}
	// The same decision record used by Feed removes it from the chat projection.
	if err = store.Confirm(rows[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := s.chatTaskProposals(sess); len(got) != 1 || got[0].ID == rows[0].ID {
		t.Fatal("stale chat approval", got)
	}
	// The convenience pointer cannot overrule conflicting explicit origins.
	sess.Task = "inbox/ambiguous"
	for _, row := range s.chatTaskProposals(sess) {
		if !want[row.ID] {
			t.Fatal("ambiguous origin exposed", row)
		}
	}
	final, _, _, _ := chats.Get("alfred", id)
	if final.Turns != 0 || len(final.Deliveries) != 0 {
		t.Fatal("projection dispatched work")
	}
}
