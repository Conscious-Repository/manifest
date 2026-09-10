package server

import (
	"context"
	"encoding/json"
	"manifest/chatthreads"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func createSharedTerminal(s *Server, agent, thread, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.SetPathValue("agent", agent)
	r.SetPathValue("id", thread)
	w := httptest.NewRecorder()
	s.handleChatRelated(w, r)
	return w
}
func TestSharedTerminalCreationContinuityAndRecovery(t *testing.T) {
	s, original, thread, sends := sharedInputFixture(t, false)
	body := `{"agent":"claude","backend":"terminal","mode":"continue","title":"Shared builder","requestId":"shared-creation-fixture"}`
	w := createSharedTerminal(s, "kairos", thread, body)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created struct {
		ID           string                 `json:"id"`
		Conversation conversationDescriptor `json:"conversation"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &created); e != nil {
		t.Fatal(e)
	}
	if created.ID == original.ID || created.Conversation.Route != "#/chat/a/kairos/"+thread {
		t.Fatalf("wrong identity: %+v", created)
	}
	se, ok := s.terminal.find(created.ID)
	if !ok || se.Runtime.Session != "" || se.LaunchPhase != "draft" {
		t.Fatalf("creation started work: %+v", se)
	}
	if sends.Load() != 0 {
		t.Fatal("creation sent input")
	}
	again := createSharedTerminal(s, "kairos", thread, body)
	if again.Code != 200 || again.Body.String() != w.Body.String() || len(s.terminal.load()) != 2 {
		t.Fatal("creation retry duplicated runtime", again.Body.String())
	}
	changed := createSharedTerminal(s, "kairos", thread, strings.Replace(body, "Shared builder", "Changed", 1))
	if changed.Code != 409 {
		t.Fatal("changed request accepted", changed.Code)
	}
	if _, err := s.sharedTerminal(s.kairosAgent(), thread, se.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sharedTerminal(s.zeckAgent(), thread, se.ID); err == nil {
		t.Fatal("cross-team access")
	}
	if s.terminalSharedConversation(se) == nil {
		t.Fatal("owner terminal lost shared link")
	}
	bad := terminalInput{Task: "private-task"}
	if _, err := s.ownerSharedTerminalInput(se, &bad); err == nil {
		t.Fatal("private task expanded context")
	}
	good := terminalInput{ConversationAgent: "kairos", ConversationID: thread}
	scope, err := s.ownerSharedTerminalInput(se, &good)
	if err != nil || scope.Thread != thread || good.ConversationID != "" {
		t.Fatal("owner scope", scope, err)
	}
	review, err := s.sharedConversationReview(s.kairosAgent(), thread)
	if err != nil {
		t.Fatal(err)
	}
	views, err := s.sharedNativeViews(context.Background(), s.kairosAgent(), thread, review)
	if err != nil || len(views) != 2 {
		t.Fatal("new agent missing from shared recipient list", len(views), err)
	}
	// The fake daemon validates the receipt for the first registry row.
	s.terminal.mu.Lock()
	err = s.terminal.writeRowsLocked([]termSession{se, original})
	s.terminal.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	sent := postSharedInput(s, se, thread, "member@aion.bio", `{"text":"continue our plan","requestId":"receipt-input-001"}`)
	if sent.Code != 200 || sends.Load() != 1 {
		t.Fatal("team input failed", sent.Code, sent.Body.String())
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
	if err != nil || receipt.SharedThread != thread || receipt.ContextSource != created.Conversation.Key {
		t.Fatal("lost shared context", receipt, err)
	}
	repeat := postSharedInput(s, se, thread, "member@aion.bio", `{"text":"continue our plan","requestId":"receipt-input-001"}`)
	if repeat.Code != 200 || sends.Load() != 1 {
		t.Fatal("retry replayed input")
	}
	// Archive revokes new-session access and even old creation-request recovery.
	if _, err = s.chat.PatchThread(thread, map[string]any{"archived": true}, chatthreads.Identity{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := createSharedTerminal(s, "kairos", thread, body); w.Code != 403 {
		t.Fatal("archived creation recovered", w.Code)
	}
	if _, err = s.sharedTerminal(s.kairosAgent(), thread, se.ID); err == nil {
		t.Fatal("archived runtime still authorized")
	}

}
func TestSharedTerminalCreationRejectsImplicitContext(t *testing.T) {
	s, _, thread, _ := sharedInputFixture(t, false)
	for _, body := range []string{
		`{"agent":"claude","backend":"terminal","mode":"continue","task":"private","requestId":"private-context-fixture"}`,
		`{"agent":"claude","backend":"terminal","mode":"continue","prompt":"hidden","requestId":"private-context-fixture"}`,
		`{"agent":"claude","backend":"terminal","requestId":"private-context-fixture"}`,
	} {
		if w := createSharedTerminal(s, "kairos", thread, body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := createSharedTerminal(s, "kairos", "another-thread", `{"agent":"claude","backend":"terminal","mode":"continue","requestId":"wrong-thread-fixture"}`); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if len(s.terminal.load()) != 1 {
		t.Fatal("rejected request created a runtime")
	}
}
