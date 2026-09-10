package server

import (
	"context"
	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/threads"
	"os"
	"strings"
	"testing"
)

func TestChatShareReviewIncludesExactVersionsWithoutPublishing(t *testing.T) {
	s, store, _ := agentChatFixture(t, echoStub)
	team, _ := chatFixture(t)
	s.chat = team.chat
	var err error
	s.artifacts, err = artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.artifactReg, err = artifacts.NewRegistry(s.artifacts)
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := threads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.threads = &threadsCfg{private: blobs}
	file, err := blobs.SaveBlob(strings.NewReader("reviewed attachment"), "notes.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.artifactReg.Put(artifacts.Put{Kind: artifacts.KindPlan, Ref: "plans/old.md", Content: []byte("reviewed plan")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.artifactReg.Put(artifacts.Put{ID: old.Artifact.ID, Ref: "plans/new.md", Content: []byte("newer unselected plan")})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Create("kairos-private", "kairos-private", "Review this", "")
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("full history ", 700)
	_, err = store.AppendTurn("kairos-private", id, "user", long+"\n[file:: "+file.Hash+" notes.txt]\n", 0)
	if err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := store.Get("kairos-private", id)
	ref := agentchat.ArtifactReference{ID: old.Artifact.ID, Revision: old.Revision.Hash}
	sess.Origin = &agentchat.Origin{Agent: "alfred", ID: id, Context: "explicitly retained origin context", Artifacts: []agentchat.ArtifactReference{ref}}
	sess.Deliveries = []agentchat.Delivery{{ID: "completed-turn", State: agentchat.DeliveryCompleted, Context: &agentchat.MessageContext{Artifacts: []agentchat.ArtifactReference{ref}}}}
	views := []codingContinuationView{{ID: "abcdef012345", Agent: "codex", Process: "stopped", HistoryAvailable: true, Turns: []termTurn{{ID: "native-one", Who: "assistant", Text: "native reply", Blocks: []termBlock{{T: "say", Text: "retained native block"}}}}, Submissions: map[string]terminalInputReceipt{
		"input-b": {ID: "native-input-b", State: "sent", Artifacts: []artifactContextRef{ref}},
		"input-a": {ID: "native-input-a", State: "sent", Artifacts: []artifactContextRef{ref}},
	}}}
	before := len(s.chat.Threads())
	r := s.chatShareReview(context.Background(), sess, body, views)
	if len(r.Blockers) != 0 {
		t.Fatal(r.Blockers)
	}
	if r.Audience != "AION team" || !r.FutureMessages || r.TargetAgent != "kairos" {
		t.Fatal(r)
	}
	if len(r.Timeline) != 2 || !strings.Contains(r.Body, long) || r.Session.Origin.Context != sess.Origin.Context {
		t.Fatal("lost history or origin")
	}
	if len(r.Files) != 2 {
		t.Fatalf("files: %+v", r.Files)
	}
	for _, f := range r.Files {
		if f.ArtifactID != "" && (f.Hash != old.Revision.Hash || f.Name != "plans/old.md" || len(f.References) != 4) {
			t.Fatalf("replaced or lost selected version: %+v", f)
		}
		if s.artifacts.Owns("aion", f.Hash) || s.artifacts.Owns("ooda", f.Hash) {
			t.Fatal("review granted team access")
		}
	}
	if len(s.chat.Threads()) != before {
		t.Fatal("review published history")
	}
	for i := 0; i < 20; i++ {
		if next := s.chatShareReview(context.Background(), sess, body, views); next.Revision != r.Revision {
			t.Fatal("unstable review revision")
		}
	}
	views[0].Turns[0].Text = "changed native reply"
	if next := s.chatShareReview(context.Background(), sess, body, views); next.Revision == r.Revision {
		t.Fatal("native change not bound to review")
	}
	if err := os.WriteFile(s.artifacts.BlobPath(old.Revision.Hash), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if next := s.chatShareReview(context.Background(), sess, body, views); len(next.Blockers) == 0 {
		t.Fatal("corrupt revision accepted")
	}
}

func TestChatShareReviewRouteAndUncertainWork(t *testing.T) {
	s, store, _ := agentChatFixture(t, echoStub)
	team, _ := chatFixture(t)
	s.chat = team.chat
	id, err := store.Create("kairos-private", "kairos-private", "Sharing", "")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/agents/chat/kairos-private/sessions/" + id + "/share-review"
	code, out := agentChatJSON(t, s, "GET", path, nil)
	if code != 200 || out["futureMessages"] != true || out["revision"] == "" {
		t.Fatal(code, out)
	}
	if code, _ := agentChatJSON(t, s, "GET", strings.Replace(path, "kairos-private", "kairos", 1), nil); code != 400 {
		t.Fatal("team route accepted", code)
	}
	if _, err := store.Accept("kairos-private", id, "pending-input", "wait"); err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := store.Get("kairos-private", id)
	views := []codingContinuationView{{ID: "abcdef012345", Process: "unknown", Submissions: map[string]terminalInputReceipt{"uncertain": {ID: "uncertain-input", State: "unconfirmed"}}}}
	r := s.chatShareReview(context.Background(), sess, body, views)
	joined := strings.Join(r.Blockers, " ")
	for _, want := range []string{"pending conversation deliveries", "terminal continuation", "uncertain terminal delivery"} {
		if !strings.Contains(joined, want) {
			t.Fatal("missing blocker", want, joined)
		}
	}
	current, _, _, _ := store.Get("kairos-private", id)
	if current.Sharing != nil {
		t.Fatal("review fenced the source")
	}
}
