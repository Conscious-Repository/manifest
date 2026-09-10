package server

import (
	"context"
	"encoding/json"
	"manifest/agentchat"
	"strings"
	"testing"
)

func TestOwnerSharedTerminalUsesTeamContextAndRecovers(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{true: "uncertain", false: "sent"}[uncertain], func(t *testing.T) {
			s, se, thread, sends := sharedInputFixture(t, uncertain)
			email, name := s.portalChatIdentity()
			expected, key, _, err := s.sharedInputContext(context.Background(), &sharedTerminalInputScope{s.kairosAgent(), thread, se.ID, email, name}, nil)
			if err != nil || !strings.Contains(expected, "TEAM_CONTEXT_MARKER") {
				t.Fatal(expected, err)
			}
			b := terminalInput{Text: "owner follow-up", RequestID: "receipt-input-001", ConversationAgent: se.Origin.Agent, ConversationID: se.Origin.ID}
			encoded, _ := json.Marshal(b)
			want := 200
			if uncertain {
				want = 202
			}
			if w := receiptInput(s, se.ID, string(encoded)); w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			r, err := s.terminal.readInputReceipt(se.ID, b.RequestID)
			if err != nil || r.SharedThread != thread || r.ActorEmail != email || r.ContextSource != key || r.ContextHash != hashTerminalText(expected) {
				t.Fatal(r, err)
			}
			s.terminal = &termCfg{regPath: s.terminal.regPath}
			s.agentChat.store = agentchat.New(s.agentChat.store.Root())
			if w := receiptInput(s, se.ID, string(encoded)); w.Code != want {
				t.Fatal("recovery", w.Code, w.Body.String())
			}
			// The terminal-only composer omits the old source selectors. Same input
			// and request still resolve to the identical team-scoped receipt.
			b.ConversationAgent, b.ConversationID = "", ""
			encoded, _ = json.Marshal(b)
			if w := receiptInput(s, se.ID, string(encoded)); w.Code != want {
				t.Fatal("terminal entry point", w.Code, w.Body.String())
			}
			if sends.Load() != 1 {
				t.Fatal("duplicate send", sends.Load())
			}
			for _, changed := range []terminalInput{
				{Text: "different", RequestID: b.RequestID},
				{Text: b.Text, RequestID: b.RequestID, ConversationAgent: "alfred", ConversationID: "other"},
				{Text: b.Text, RequestID: b.RequestID, Task: "private-unrelated-task"},
			} {
				data, _ := json.Marshal(changed)
				if w := receiptInput(s, se.ID, string(data)); w.Code != 409 {
					t.Fatal("changed or private input accepted", w.Code)
				}
			}
		})
	}
}

func TestOwnerTerminalRefusesPreparedShareBeforeRuntime(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false)
	private, store, _ := agentChatFixture(t, echoStub)
	s.agentChat = private.agentChat
	id, err := store.Create("kairos-private", "kairos-private", "Pending share", "")
	if err != nil {
		t.Fatal(err)
	}
	source, body, _, _ := store.Get("kairos-private", id)
	se.Origin = &agentchat.Origin{Agent: source.Agent, ID: id, Mode: "continue"}
	if err = s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.BeginShare(source.Agent, id, "prepare-owner-001", agentchat.ShareRevision(source, body), "kairos", "target"); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"text":"should wait","requestId":"receipt-input-001"}`, `{"key":"y","requestId":"receipt-input-001"}`} {
		if w := receiptInput(s, se.ID, body); w.Code != 409 || sends.Load() != 0 {
			t.Fatal(w.Code, w.Body.String(), sends.Load())
		}
	}
}

func TestOwnerRecoversReviewedPrivateReceiptAfterSharing(t *testing.T) {
	var original terminalInput
	s, se, thread, sends := sharedInputFixture(t, false, func(review *chatShareReview, se termSession, s *Server) {
		original = terminalInput{Text: "accepted before sharing", RequestID: "receipt-input-001", ConversationAgent: se.Origin.Agent, ConversationID: se.Origin.ID}
		receipt := terminalInputReceipt{ID: original.RequestID, Fingerprint: original.fingerprint(), State: "sent", Text: original.Text, SubmittedHash: hashTerminalText("original private envelope"), ContextSource: sessionConversation(review.Session).Key}
		if err := s.terminal.writeInputReceipt(se.ID, receipt); err != nil {
			t.Fatal(err)
		}
		review.NativeReceipts[se.ID] = map[string]terminalInputReceipt{"reviewed-turn": receipt}
	})
	// The historical receipt is authoritative even with no daemon after restart.
	s.terminal = &termCfg{regPath: s.terminal.regPath}
	data, _ := json.Marshal(original)
	if w := receiptInput(s, se.ID, string(data)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if sends.Load() != 0 {
		t.Fatal("historical send replayed")
	}
	if w := postSharedInput(s, se, thread, "member@aion.bio", `{"text":"accepted before sharing","requestId":"receipt-input-001"}`); w.Code != 409 {
		t.Fatal("member reused owner receipt", w.Code)
	}
	original.Text = "changed"
	data, _ = json.Marshal(original)
	if w := receiptInput(s, se.ID, string(data)); w.Code != 409 {
		t.Fatal("changed receipt accepted", w.Code)
	}
	// A private receipt not present in the approved review cannot be recovered
	// by this exception, even when its body fingerprint matches.
	original.RequestID = "unreviewed-receipt-001"
	receipt := terminalInputReceipt{ID: original.RequestID, Fingerprint: original.fingerprint(), State: "sent"}
	if err := s.terminal.writeInputReceipt(se.ID, receipt); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(original)
	if w := receiptInput(s, se.ID, string(data)); w.Code != 409 {
		t.Fatal("unreviewed receipt accepted", w.Code)
	}
}

func TestSharedSourceLinksResolveToTeamConversation(t *testing.T) {
	s, se, thread, _ := sharedInputFixture(t, false)
	code, result := agentChatJSON(t, s, "GET", "/api/agents/chat/"+se.Origin.Agent+"/sessions/"+se.Origin.ID, nil)
	if code != 200 {
		t.Fatal(code, result)
	}
	destination, ok := result["sharedConversation"].(map[string]any)
	if !ok || destination["scope"] != "team:aion" || destination["route"] != "#/chat/a/kairos/"+thread {
		t.Fatal(result)
	}
	if _, oldBody := result["body"]; oldBody {
		t.Fatal("old source still presents an isolated writable history")
	}
	d := s.terminalSharedConversation(se)
	if d == nil || d.Route != destination["route"] {
		t.Fatal("original terminal lost shared destination", d)
	}
	code, result = agentChatJSON(t, s, "GET", "/api/agents/chat/kairos/sessions/"+thread, nil)
	if code != 200 {
		t.Fatal(code, result)
	}
	session := result["session"].(map[string]any)
	if session["shared"] != true || len(result["continuations"].([]any)) != 1 || !strings.Contains(result["body"].(string), "TEAM_CONTEXT_MARKER") {
		t.Fatal(result)
	}
}
