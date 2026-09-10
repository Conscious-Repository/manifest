package server

import (
	"context"
	"encoding/json"
	"errors"
	"manifest/agentchat"
	"manifest/approvals"
	"manifest/artifacts"
	"manifest/chatthreads"
	"manifest/threads"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type sharePublishFixture struct {
	s                        *Server
	agent, id, teamDir, body string
	review                   chatShareReview
	file                     threads.FileRef
	native                   termSession
}

func publicationFixture(t *testing.T, agent string) sharePublishFixture {
	t.Helper()
	s, store, _ := agentChatFixture(t, echoStub)
	teamDir := t.TempDir()
	team, err := chatthreads.New(teamDir)
	if err != nil {
		t.Fatal(err)
	}
	if agent == "kairos-private" {
		s.chat = team
	} else {
		s.oodaChat = team
	}
	s.artifacts, err = artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	private, err := threads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.UseThreads(private, nil, nil, nil, "owner@example.test")
	file, err := private.SaveBlob(strings.NewReader("exact reviewed bytes"), "plan.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Create(agent, agent, "Shared work", "")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("Untruncated history. ", 400) + "\n[file:: " + file.Hash + " plan.txt]\n"
	if _, err := store.AppendTurn(agent, id, "user", body, 0); err != nil {
		t.Fatal(err)
	}
	s.UseTerminal(filepath.Join(t.TempDir(), "terminals.json"), t.TempDir(), t.TempDir())
	native := termSession{ID: "0123456789abcdef", Kind: "codex", Backend: "herdr", LaunchPhase: "draft", CreatedAt: time.Now().UTC().Format(time.RFC3339), Origin: &agentchat.Origin{Mode: "continue", Agent: agent, ID: id}}
	if err := s.terminal.upsertChecked(native); err != nil {
		t.Fatal(err)
	}
	source, text, _, _ := store.Get(agent, id)
	review := s.chatShareReview(context.Background(), source, text, s.codingContinuations(context.Background(), source))
	if len(review.Blockers) > 0 {
		t.Fatal(review.Blockers)
	}
	return sharePublishFixture{s, agent, id, teamDir, body, review, file, native}
}
func (f sharePublishFixture) publish(request chatShareRequest) (chatShareResult, error) {
	return f.s.publishChatShare(httptest.NewRequest("POST", "/", nil), f.agent, f.id, request)
}

func TestPublishWholeConversationAndRecoverReceipt(t *testing.T) {
	for _, agent := range []string{"kairos-private", "zeck-private"} {
		t.Run(agent, func(t *testing.T) {
			f := publicationFixture(t, agent)
			request := chatShareRequest{"share-confirmation-001", f.review.Revision}
			result, err := f.publish(request)
			if err != nil || result.State != "shared" {
				t.Fatal(result, err)
			}
			ag, _ := f.s.portalChatAgent(f.review.TargetAgent)
			threads := ag.Store.Threads()
			if len(threads) != 1 {
				t.Fatal(threads)
			}
			msgs := ag.Store.Messages(threads[0].ID)
			if len(msgs) != 1 || len(msgs[0].Text) < 6000 || len(msgs[0].Files) != 1 || msgs[0].Files[0].Hash != f.file.Hash {
				t.Fatal("incomplete history import")
			}
			if !f.s.artifacts.Owns(ag.Domain, f.file.Hash) {
				t.Fatal("attachment inaccessible")
			}
			other := "ooda"
			if ag.Domain == "ooda" {
				other = "aion"
			}
			if f.s.artifacts.Owns(other, f.file.Hash) {
				t.Fatal("wrong audience got file")
			}
			if _, err := f.s.sharedTerminal(ag, threads[0].ID, f.native.ID); err != nil {
				t.Fatal("original runtime not shared", err)
			}
			// Lost response/restart: recover even while a writer uses the team session.
			f.s.agentChat.store = agentchat.New(f.s.agentChat.store.Root())
			release, err := f.s.chatShareMutation(f.agent, f.id)
			if err != nil {
				t.Fatal(err)
			}
			again, err := f.publish(request)
			release()
			if err != nil || !reflect.DeepEqual(again, result) {
				t.Fatal("receipt changed on recovery", again, err)
			}
			if len(ag.Store.Threads()) != 1 || len(ag.Store.Messages(threads[0].ID)) != 1 || len(f.s.artifacts.List(ag.Domain)) != 1 {
				t.Fatal("retry duplicated publication")
			}
			if _, err := f.publish(chatShareRequest{"another-share-request", f.review.Revision}); !errors.Is(err, agentchat.ErrRequestConflict) {
				t.Fatal("new request accepted over existing share", err)
			}
		})
	}
}

func TestShareRefusesStaleReviewAndActiveWriterBeforePublishing(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	request := chatShareRequest{"share-confirmation-001", f.review.Revision}
	release, err := f.s.chatShareMutation(f.agent, f.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(request); !errors.Is(err, agentchat.ErrShareBusy) {
		t.Fatal(err)
	}
	// A writer for this conversation does not lock a different source.
	other := f.s.chatShareMutex(f.agent, "20260910-120000-abcdef")
	if !other.TryLock() {
		t.Fatal("unrelated source blocked")
	}
	other.Unlock()
	release()
	if _, err := f.s.agentChat.store.AppendTurn(f.agent, f.id, "user", "not in the approved review", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.publish(request); !errors.Is(err, agentchat.ErrShareChanged) {
		t.Fatal(err)
	}
	source, _, _, _ := f.s.agentChat.store.Get(f.agent, f.id)
	if source.Sharing != nil || len(f.s.chat.Threads()) != 0 || f.s.artifacts.Owns("aion", f.file.Hash) {
		t.Fatal("refused review published state")
	}
}

func TestPreparedShareRecoversAfterTargetStorageFailure(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	request := chatShareRequest{"share-confirmation-001", f.review.Revision}
	target := filepath.Join(f.teamDir, "chat.json")
	if err := os.WriteFile(target, []byte("broken existing history"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := f.publish(request)
	if err == nil || result.State != "prepared" {
		t.Fatal(result, err)
	}
	if raw, _ := os.ReadFile(target); string(raw) != "broken existing history" {
		t.Fatal("replaced corrupt target")
	}
	if err := f.s.agentChat.store.Rename(f.agent, f.id, "must remain frozen"); !errors.Is(err, agentchat.ErrShared) {
		t.Fatal(err)
	}
	w := receiptInput(f.s, f.native.ID, `{"text":"no racing input","requestId":"receipt-input-001"}`)
	if w.Code != 409 {
		t.Fatal("prepared source accepted terminal input", w.Code, w.Body.String())
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	f.s.agentChat.store = agentchat.New(f.s.agentChat.store.Root())
	result, err = f.publish(request)
	if err != nil || result.State != "shared" {
		t.Fatal(result, err)
	}
	if len(f.s.chat.Threads()) != 1 || len(f.s.artifacts.List("aion")) != 1 {
		t.Fatal("recovery duplicated artifacts/history")
	}
}

func TestShareRecoversAfterImportBeforeSourceCompletion(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	request := chatShareRequest{"share-confirmation-001", f.review.Revision}
	id := "shared-" + artifacts.Hash([]byte(f.agent + "/" + f.id))[:24]
	payload, _ := json.Marshal(f.review)
	if _, _, err := f.s.agentChat.store.BeginReviewedShare(f.agent, f.id, request.RequestID, f.review.SourceRevision, "kairos", id, payload); err != nil {
		t.Fatal(err)
	}
	ag := f.s.kairosAgent()
	if err := f.s.stageChatShareFiles(ag, id, f.review); err != nil {
		t.Fatal(err)
	}
	thread, msgs, err := buildChatShareImport(f.review, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ag.Store.ImportSharedThread(thread, msgs, thread.Created); err != nil {
		t.Fatal(err)
	}
	if err := f.s.chatAskFor(ag, id, "wait for recovery", "ask", nil, "member@example.test", "Member"); err == nil || !strings.Contains(err.Error(), "sharing is not complete") {
		t.Fatal("early target input accepted", err)
	}
	result, err := f.publish(request)
	if err != nil || result.State != "shared" {
		t.Fatal(result, err)
	}
	if len(ag.Store.Messages(id)) != len(msgs) {
		t.Fatal("reimport duplicated history")
	}
}

func TestSharePublicationHTTPConfirmation(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	path := "/api/agents/chat/" + f.agent + "/sessions/" + f.id + "/share"
	if code, out := agentChatJSON(t, f.s, "GET", path, nil); code != 200 || out["state"] != "private" {
		t.Fatal(code, out)
	}
	payload, _ := json.Marshal(chatShareRequest{"share-confirmation-http", f.review.Revision})
	req := httptest.NewRequest("POST", path, strings.NewReader(string(payload)))
	req.Header.Set("Origin", "https://unrelated.example.test")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, req)
	if w.Code != 403 || len(f.s.chat.Threads()) != 0 {
		t.Fatal("cross-origin publication", w.Code)
	}
	if code, out := agentChatJSON(t, f.s, "POST", path, chatShareRequest{"share-confirmation-http", f.review.Revision}); code != 200 || out["state"] != "shared" {
		t.Fatal(code, out)
	}
	if code, out := agentChatJSON(t, f.s, "GET", path, nil); code != 200 || out["state"] != "shared" {
		t.Fatal(code, out)
	}
}

func TestRawTerminalHandleRetainsShareFence(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	runtime := terminalIdentity{Backend: "herdr", Host: "fixture", Session: "session", Generation: "generation", Workspace: "workspace", Pane: "pane", Occupant: "occupant"}
	f.native.Runtime = runtime
	f.native.Runtime.ManifestID = f.native.ID
	if err := f.s.terminal.upsertChecked(f.native); err != nil {
		t.Fatal(err)
	}
	// Raw handles may omit registry-only metadata but still identify the same pane.
	found, err := f.s.terminalForHandle(runtime)
	if err != nil || found.ID != f.native.ID {
		t.Fatal(found, err)
	}
	payload, _ := json.Marshal(f.review)
	if _, _, err := f.s.agentChat.store.BeginReviewedShare(f.agent, f.id, "raw-handle-fence", f.review.SourceRevision, "kairos", "shared-fixture", payload); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	release, allowed := f.s.guardTerminalShare(w, found)
	release()
	if allowed || w.Code != 409 {
		t.Fatal("raw handle bypassed prepared fence", w.Code)
	}
	duplicate := f.native
	duplicate.ID = "abcdef0123456789"
	if err := f.s.terminal.upsertChecked(duplicate); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.terminalForHandle(runtime); err == nil {
		t.Fatal("ambiguous handle selected an arbitrary source")
	}
}

func TestShareReviewRecoveryReturnsOriginalEnvelope(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	request := chatShareRequest{"review-recovery-fixture", f.review.Revision}
	target := filepath.Join(f.teamDir, "chat.json")
	if err := os.WriteFile(target, []byte("corrupt target"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := f.publish(request)
	if err == nil || result.State != "prepared" {
		t.Fatal(result, err)
	}
	path := "/api/agents/chat/" + f.agent + "/sessions/" + f.id + "/share-review"
	code, out := agentChatJSON(t, f.s, "GET", path, nil)
	if code != 200 || out["revision"] != f.review.Revision {
		t.Fatal(code, out)
	}
	raw, _ := json.Marshal(out)
	var got chatShareReview
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Blockers) != 0 || got.Session.Sharing != nil || got.Body != f.review.Body {
		t.Fatal("recovery substituted a current or blocked review")
	}
}

func TestSharedOwnerChatRetainsTaskAndCanonicalApproval(t *testing.T) {
	f := publicationFixture(t, "kairos-private")
	if err := f.s.agentChat.store.SetTask(f.agent, f.id, "inbox/share-fixture"); err != nil {
		t.Fatal(err)
	}
	store := approvals.NewStore(filepath.Join(t.TempDir(), "artifacts"))
	f.s.UseApprovals(store)
	proposal, err := store.Propose(approvals.Proposal{Type: "approval", Agent: "kairos-private", Action: "Review [todo:: inbox/share-fixture]", Body: "Original decision evidence"})
	if err != nil {
		t.Fatal(err)
	}
	source, body, _, _ := f.s.agentChat.store.Get(f.agent, f.id)
	f.review = f.s.chatShareReview(context.Background(), source, body, f.s.codingContinuations(context.Background(), source))
	if _, err = f.publish(chatShareRequest{"owner-context-fixture", f.review.Revision}); err != nil {
		t.Fatal(err)
	}
	thread := f.s.chat.Threads()[0].ID
	path := "/api/agents/chat/kairos/sessions/" + thread
	code, out := agentChatJSON(t, f.s, "GET", path, nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	session := out["session"].(map[string]any)
	if session["task"] != "inbox/share-fixture" {
		t.Fatal("lost task backlink", session)
	}
	raw, _ := json.Marshal(out["proposals"])
	var rows []approvalRow
	if err = json.Unmarshal(raw, &rows); err != nil || len(rows) != 1 || rows[0].ID != proposal.ID {
		t.Fatal("lost canonical decision", string(raw), err)
	}
	if err = store.Confirm(proposal.ID); err != nil {
		t.Fatal(err)
	}
	code, out = agentChatJSON(t, f.s, "GET", path, nil)
	raw, _ = json.Marshal(out["proposals"])
	if code != 200 || json.Unmarshal(raw, &rows) != nil || len(rows) != 0 {
		t.Fatal("feed decision did not settle shared owner chat", out)
	}
}
