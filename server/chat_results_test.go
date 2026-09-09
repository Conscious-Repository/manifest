package server

import (
	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/threads"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodingResultSnapshotPinsDiscussedBytes(t *testing.T) {
	s := codingFixture(t)
	chats := agentchat.New(filepath.Join(t.TempDir(), "chats"))
	s.UseAgentChat(chats)
	id, err := chats.Create("alfred", "", "Planning", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = chats.SetTask("alfred", id, "inbox/current"); err != nil {
		t.Fatal(err)
	}
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	s.UseArtifactRegistry(reg)
	h := s.findHarness("codex")
	run := boardRunID()
	write := func(body string) {
		t.Helper()
		if err := boardReport(h, run, "inbox/current", "go", "", "completed", body, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	write("Original deliverable")
	sess, _, _, _ := chats.Get("alfred", id)
	result := s.chatCodingResults(sess)[0]
	endpoint := "/api/agents/chat/alfred/sessions/" + id + "/coding-result"
	capture := func(hash string) (int, map[string]any) {
		return agentChatJSON(t, s, "POST", endpoint, map[string]any{"agent": "codex", "run": run, "hash": hash})
	}
	code, ref := capture(result.Hash)
	if code != 200 {
		t.Fatal(code, ref)
	}
	refs := []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}
	text, err := s.taskArtifactContext("inbox/current", refs)
	if err != nil || !strings.Contains(text, "Original deliverable") {
		t.Fatal(text, err)
	}
	write("Revised deliverable")
	if code, _ = capture(result.Hash); code != 409 {
		t.Fatal("silently captured newer bytes", code)
	}
	newResult := s.chatCodingResults(sess)[0]
	code, newRef := capture(newResult.Hash)
	if code != 200 || newRef["revision"] == ref["revision"] {
		t.Fatal(code, newRef)
	}
	text, err = s.taskArtifactContext("inbox/current", refs)
	if err != nil || !strings.Contains(text, "Original deliverable") || strings.Contains(text, "Revised deliverable") {
		t.Fatal("old context changed", text, err)
	}
	if err = chats.SetTask("alfred", id, "inbox/unrelated"); err != nil {
		t.Fatal(err)
	}
	if code, _ = capture(newResult.Hash); code != 404 {
		t.Fatal("unrelated result accessible", code)
	}
	_, body, _, _ := chats.Get("alfred", id)
	if strings.Contains(body, "deliverable") {
		t.Fatal("snapshot wrote transcript")
	}
}

func TestCodingResultsReturnOnlyToExplicitOrigin(t *testing.T) {
	s := codingFixture(t)
	s.threads = loopFixture(t).threads
	sess := agentchat.Session{Agent: "alfred", ID: "20260909-100000-abcd", Task: "inbox/current"}
	link := func(task, id string) {
		t.Helper()
		_, err := s.addThreadEntry(s.ownerIdentity(), task, threads.ActComment, "Origin", nil, nil, map[string]any{"chat": map[string]any{"agent": "alfred", "id": id, "canonical": true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	link("inbox/older", sess.ID)
	link("inbox/conflict", sess.ID)
	link("inbox/conflict", "20260909-100001-abcd")
	h := s.findHarness("codex")
	for i, task := range []string{"inbox/current", "inbox/older", "inbox/conflict", "inbox/unrelated", "inbox/older] [todo:: inbox/unrelated"} {
		if err := boardReport(h, boardRunID(), task, "go", "", "completed", "Result for "+task, time.Now().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	running := boardRunID()
	if err := boardReport(h, running, "inbox/current", "go", "", "running", "Not a result", time.Now()); err != nil {
		t.Fatal(err)
	}
	results := s.chatCodingResults(sess)
	if len(results) != 2 || results[0].Task != "inbox/current" || results[1].Task != "inbox/older" {
		t.Fatalf("wrong results: %+v", results)
	}
	for _, r := range results {
		if r.Agent != "codex" || r.Body == "" || r.Outcome != "completed" {
			t.Fatal(r)
		}
	}
	// Projection reads must not create copied task comments or mutate reports.
	if got := len(s.listThread("inbox/older")); got != 1 {
		t.Fatal("copied result", got)
	}
	if err := boardReport(h, results[0].ID, "inbox/unrelated", "go", "", "completed", "Reattributed", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := s.chatCodingResults(sess); len(got) != 1 || got[0].Task != "inbox/older" {
		t.Fatal("stale relationship", got)
	}
	sess.Task = "inbox/conflict"
	if got := s.chatCodingResults(sess); len(got) != 1 {
		t.Fatal("pointer overruled explicit origins", got)
	}
}
