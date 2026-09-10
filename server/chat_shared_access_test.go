package server

import (
	"context"
	"encoding/json"
	"errors"
	"manifest/agentchat"
	"manifest/chatthreads"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedTerminalRequiresCompletedMatchingConsent(t *testing.T) {
	s, store, _ := agentChatFixture(t, echoStub)
	team, _ := chatFixture(t)
	s.chat = team.chat
	s.UseTerminal(filepath.Join(t.TempDir(), "terminals.json"), t.TempDir(), t.TempDir())
	id, err := store.Create("kairos-private", "kairos-private", "Team coding", "")
	if err != nil {
		t.Fatal(err)
	}
	source, body, _, _ := store.Get("kairos-private", id)
	native := termSession{ID: "0123456789abcdef", Kind: "codex", Backend: "herdr", LaunchPhase: "draft", Origin: &agentchat.Origin{Mode: "continue", Agent: source.Agent, ID: source.ID}}
	unrelated := termSession{ID: "fedcba9876543210", Kind: "codex", Backend: "herdr", Origin: native.Origin}
	if err := s.terminal.upsertChecked(native); err != nil {
		t.Fatal(err)
	}
	if err := s.terminal.upsertChecked(unrelated); err != nil {
		t.Fatal(err)
	}
	ag := s.kairosAgent()
	review := s.chatShareReview(context.Background(), source, body, []codingContinuationView{{ID: native.ID, Agent: "codex", Process: "stopped", HistoryAvailable: true}})
	if len(review.Blockers) != 0 {
		t.Fatal(review.Blockers)
	}
	payload, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	thread := chatthreads.Thread{ID: "shared-terminal-one", Title: "Team coding", Created: time.Now().UTC(), By: "owner", ImportSource: sessionConversation(source).Key, ImportRevision: review.Revision, SharedSource: &chatthreads.SharedSource{Agent: source.Agent, ID: source.ID}}
	deny := func(agent *chatAgent, tid, terminal string) {
		t.Helper()
		if _, err := s.sharedTerminal(agent, tid, terminal); !errors.Is(err, errSharedConversationAccess) {
			t.Fatal("unexpected access", tid, terminal, err)
		}
	}
	deny(ag, thread.ID, native.ID)
	if _, _, err := store.BeginReviewedShare(source.Agent, source.ID, "share-terminal-01", review.SourceRevision, "kairos", thread.ID, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := ag.Store.ImportSharedThread(thread, nil, thread.Created); err != nil {
		t.Fatal(err)
	}
	deny(ag, thread.ID, native.ID) // publication alone does not complete consent
	if _, err := store.CompleteShare(source.Agent, source.ID, "share-terminal-01", review.SourceRevision); err != nil {
		t.Fatal(err)
	}
	if got, err := s.sharedTerminal(ag, thread.ID, native.ID); err != nil || got.ID != native.ID {
		t.Fatal("approved runtime unavailable", got, err)
	}
	read := func(terminal string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/chat/threads/"+thread.ID+"/terminals/"+terminal+"/transcript", nil)
		r.SetPathValue("thread", thread.ID)
		r.SetPathValue("terminal", terminal)
		w := httptest.NewRecorder()
		s.AionChatTerminalRead(w, r)
		return w
	}
	if w := read(native.ID); w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := read(unrelated.ID); w.Code != http.StatusForbidden {
		t.Fatal("read route exposed unshared terminal", w.Code)
	}
	deny(ag, thread.ID, unrelated.ID) // same origin is not a reviewed runtime grant
	deny(&chatAgent{Name: "zeck", Domain: "ooda", Store: ag.Store}, thread.ID, native.ID)
	deny(ag, "another-thread", native.ID)
	changed := native
	changed.Origin = &agentchat.Origin{Mode: "continue", Agent: source.Agent, ID: "20260910-000000-aaaa"}
	if err := s.terminal.upsertChecked(changed); err != nil {
		t.Fatal(err)
	}
	deny(ag, thread.ID, native.ID)
	if err := s.terminal.upsertChecked(native); err != nil {
		t.Fatal(err)
	}
	// Recovery across source-store reopening must preserve the grant.
	s.agentChat.store = agentchat.New(store.Root())
	if _, err := s.sharedTerminal(ag, thread.ID, native.ID); err != nil {
		t.Fatal("restart lost consent", err)
	}
	frozen, _, _, _ := store.Get(source.Agent, source.ID)
	path := filepath.Join(store.Root(), ".shares", frozen.Sharing.EnvelopeHash+".json")
	if err := os.WriteFile(path, []byte(`{"changed":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	deny(ag, thread.ID, native.ID)
}

func TestPortalAuthenticatesBeforeSharedTerminalRead(t *testing.T) {
	called := false
	p := &portalAPI{opt: PortalOptions{ChatTerminalRead: func(w http.ResponseWriter, r *http.Request) { called = true }}}
	w := httptest.NewRecorder()
	p.handleChatTerminalRead(w, httptest.NewRequest("GET", "/api/chat/threads/a/terminals/b/transcript", nil))
	if called || w.Code != http.StatusServiceUnavailable {
		t.Fatal("unauthenticated callback", called, w.Code)
	}
}

func TestSharedConversationCannotSubstituteReviewOrAudience(t *testing.T) {
	s, store, _ := agentChatFixture(t, echoStub)
	team, _ := chatFixture(t)
	s.chat = team.chat
	id, _ := store.Create("kairos-private", "kairos-private", "Private", "")
	source, body, _, _ := store.Get("kairos-private", id)
	review := s.chatShareReview(context.Background(), source, body, nil)
	// The outer payload hash is valid, but the approved UI digest no longer
	// matches the contents. A downstream authorization check must detect this.
	review.FutureMessages = false
	payload, _ := json.Marshal(review)
	thread := chatthreads.Thread{ID: "substituted-review", Created: time.Now().UTC(), ImportSource: sessionConversation(source).Key, ImportRevision: review.Revision, SharedSource: &chatthreads.SharedSource{Agent: source.Agent, ID: source.ID}}
	if _, _, err := store.BeginReviewedShare(source.Agent, source.ID, "share-mismatch-01", review.SourceRevision, "kairos", thread.ID, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := s.chat.ImportSharedThread(thread, nil, thread.Created); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteShare(source.Agent, source.ID, "share-mismatch-01", review.SourceRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sharedConversationReview(s.kairosAgent(), thread.ID); !errors.Is(err, errSharedConversationAccess) {
		t.Fatal("unapproved payload accepted", err)
	}
}
