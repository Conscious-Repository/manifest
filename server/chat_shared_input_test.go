package server

import (
	"context"
	"encoding/json"
	"manifest/agentchat"
	"manifest/chatthreads"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sharedInputFixture(t *testing.T, uncertain bool, prepare ...func(*chatShareReview, termSession, *Server)) (*Server, termSession, string, *atomic.Int32) {
	t.Helper()
	s, se, sends := terminalReceiptFixture(t, uncertain)
	private, store, _ := agentChatFixture(t, echoStub)
	s.agentChat = private.agentChat
	team, _ := chatFixture(t)
	s.chat = team.chat
	id, err := store.Create("kairos-private", "kairos-private", "Shared code", "")
	if err != nil {
		t.Fatal(err)
	}
	source, body, _, _ := store.Get("kairos-private", id)
	se.Origin = &agentchat.Origin{Mode: "continue", Agent: source.Agent, ID: source.ID}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	review := s.chatShareReview(context.Background(), source, body, []codingContinuationView{{ID: se.ID, Agent: se.Kind, Process: "not-started", HistoryAvailable: true}})
	if len(review.Blockers) != 0 {
		t.Fatal(review.Blockers)
	}
	for _, f := range prepare {
		f(&review, se, s)
	}
	review.Revision = ""
	unsigned, _ := json.Marshal(review)
	review.Revision = hashTerminalText(string(unsigned))
	payload, _ := json.Marshal(review)
	thread := chatthreads.Thread{ID: "team-input-one", Created: time.Now().UTC(), ImportSource: sessionConversation(source).Key, ImportRevision: review.Revision, SharedSource: &chatthreads.SharedSource{Agent: source.Agent, ID: source.ID}}
	if _, _, err := store.BeginReviewedShare(source.Agent, source.ID, "shared-input-consent", review.SourceRevision, "kairos", thread.ID, payload); err != nil {
		t.Fatal(err)
	}
	msg := chatthreads.Message{ID: "shared-initial", Thread: thread.ID, Kind: "ask", Author: "member@aion.bio", AuthName: "Member", At: thread.Created, Text: "TEAM_CONTEXT_MARKER"}
	if _, err := s.chat.ImportSharedThread(thread, []chatthreads.Message{msg}, thread.Created); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteShare(source.Agent, source.ID, "shared-input-consent", review.SourceRevision); err != nil {
		t.Fatal(err)
	}
	return s, se, thread.ID, sends
}

func postSharedInput(s *Server, se termSession, thread, email, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/chat/threads/"+thread+"/terminals/"+se.ID+"/input", strings.NewReader(body))
	r.SetPathValue("thread", thread)
	r.SetPathValue("terminal", se.ID)
	s.AionChatTerminalInput(w, r, email, "Member")
	return w
}

func TestSharedInputAttributionAndLostResponseRecovery(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{true: "uncertain", false: "sent"}[uncertain], func(t *testing.T) {
			s, se, thread, sends := sharedInputFixture(t, uncertain)
			scope := &sharedTerminalInputScope{s.kairosAgent(), thread, se.ID, "member@aion.bio", "Member"}
			prompt, _, _, err := s.sharedInputContext(context.Background(), scope, nil)
			if err != nil || !strings.Contains(prompt, "TEAM_CONTEXT_MARKER") {
				t.Fatal("missing current team history", prompt, err)
			}
			body := `{"text":"one instruction","requestId":"receipt-input-001"}`
			want := 200
			if uncertain {
				want = 202
			}
			if w := postSharedInput(s, se, thread, scope.Email, body); w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
			if err != nil || receipt.ActorEmail != scope.Email || receipt.SharedThread != thread || receipt.SharedAgent != "kairos" || receipt.Text != "one instruction" {
				t.Fatal(receipt, err)
			}
			if receipt.ContextSource != agentConversation("portal", "kairos", thread, "team:aion", "").Key {
				t.Fatal("private context key retained", receipt)
			}
			// Native transcript rendering restores the member's own message and
			// keeps their identity instead of displaying the generated envelope.
			projection := receipt
			projection.ID, projection.SubmittedHash = "projection-message", hashTerminalText("wrapped shared instruction")
			if err := s.terminal.writeInputReceipt(se.ID, projection); err != nil {
				t.Fatal(err)
			}
			turns, submissions := s.projectConversationNativeTurns(se, agentConversation("hermes", se.Origin.Agent, se.Origin.ID, "private", "").Key, []termTurn{{ID: "native-projection", Who: "user", Text: "wrapped shared instruction"}})
			if turns[0].Text != "one instruction" || submissions["native-projection"].ActorEmail != scope.Email {
				t.Fatal("lost member attribution", turns, submissions)
			}
			timeline := conversationTimeline(agentchat.Session{}, "", []codingContinuationView{{ID: se.ID, Agent: se.Kind, Turns: turns, Submissions: submissions}})
			contextText, _ := timelineContinuationContext("shared", timeline)
			if !strings.Contains(contextText, `author "member@aion.bio"`) {
				t.Fatal("member attributed as owner", contextText)
			}
			// No runtime is needed to recover an accepted or uncertain request.
			s.terminal = &termCfg{regPath: s.terminal.regPath}
			if w := postSharedInput(s, se, thread, scope.Email, body); w.Code != want {
				t.Fatal("retry", w.Code, w.Body.String())
			}
			if sends.Load() != 1 {
				t.Fatal("duplicate runtime send", sends.Load())
			}
			if w := postSharedInput(s, se, thread, "other@aion.bio", body); w.Code != 409 {
				t.Fatal("another member reused receipt", w.Code)
			}
			if w := postSharedInput(s, se, thread, scope.Email, `{"text":"changed","requestId":"receipt-input-001"}`); w.Code != 409 {
				t.Fatal("changed instruction reused receipt", w.Code)
			}
		})
	}
}

func TestSharedInputRejectsUnsharedContextBeforeRuntime(t *testing.T) {
	s, se, thread, sends := sharedInputFixture(t, false)
	for _, body := range []string{`{"text":"read this","requestId":"private-file-001","artifacts":[{"id":"private-file","revision":"unshared"}]}`, `{"text":"read this","requestId":"private-file-001","conversationId":"other-private-session"}`} {
		w := postSharedInput(s, se, thread, "member@aion.bio", body)
		if w.Code < 400 || sends.Load() != 0 {
			t.Fatal("unshared context reached terminal", w.Code, sends.Load())
		}
	}
	if w := postSharedInput(s, se, thread, "", `{"text":"impersonate","requestId":"no-identity-001"}`); w.Code != 401 {
		t.Fatal(w.Code)
	}
	called := false
	p := &portalAPI{opt: PortalOptions{ChatTerminalInput: func(_ http.ResponseWriter, _ *http.Request, _, _ string) { called = true }}}
	w := httptest.NewRecorder()
	p.handleChatTerminalInput(w, httptest.NewRequest("POST", "/", strings.NewReader(`{}`)))
	if called || w.Code != 503 {
		t.Fatal("unauthenticated input callback", called, w.Code)
	}
}

func TestSharedKeysUseDurableReceiptsAndNoPrivateSelectors(t *testing.T) {
	s, se, thread, _ := sharedInputFixture(t, false)
	// Start the reviewed runtime, then replace the daemon fixture with one
	// that counts key delivery and checks the pre-send receipt.
	if w := postSharedInput(s, se, thread, "member@aion.bio", `{"text":"start","requestId":"receipt-input-001"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var keys atomic.Int32
	h := herdrFixture(t, func(c net.Conn, req herdrFixtureRequest) {
		switch req.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "pane.send_keys":
			keys.Add(1)
			r, err := s.terminal.readInputReceipt(se.ID, "shared-key-001")
			if err != nil || r.State != "unconfirmed" || r.ActorEmail != "member@aion.bio" {
				t.Error("key crossed boundary before attribution/receipt", r, err)
			}
			herdrFixtureReply(c, map[string]any{})
		default:
			t.Errorf("unexpected runtime method %s", req.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	// The replacement test daemon has a different socket generation; bind the
	// fixture row to it explicitly rather than bypassing the runtime's guard.
	current, _ := s.terminal.find(se.ID)
	current.Runtime.Generation, _ = h.generation()
	if err := s.terminal.upsertChecked(current); err != nil {
		t.Fatal(err)
	}
	body := `{"key":"\u0003","requestId":"shared-key-001"}`
	for i := 0; i < 2; i++ {
		if w := postSharedInput(s, se, thread, "member@aion.bio", body); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if keys.Load() != 1 {
		t.Fatal("duplicate key", keys.Load())
	}
	for _, invalid := range []string{`{"text":"x"}`, `{"text":"x","requestId":"new-input-001","task":"private-task"}`, `{"text":"x","key":"y","requestId":"new-input-001"}`} {
		if w := postSharedInput(s, se, thread, "member@aion.bio", invalid); w.Code != 400 {
			t.Fatal("invalid shared selectors", w.Code, w.Body.String())
		}
	}
	if keys.Load() != 1 {
		t.Fatal("invalid request touched runtime")
	}
}
