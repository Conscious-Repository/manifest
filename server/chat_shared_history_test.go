package server

import (
	"context"
	"encoding/json"
	"manifest/chatthreads"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedConversationReadsNewNativeHistoryWithoutCopying(t *testing.T) {
	lines := `{"type":"assistant","uuid":"old-native","timestamp":"2026-09-10T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"DO_NOT_REIMPORT"}]}}
{"type":"user","uuid":"new-user","timestamp":"2026-09-10T11:00:00Z","message":{"role":"user","content":"wrapped member instruction"}}
{"type":"assistant","uuid":"new-native","timestamp":"2026-09-10T11:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"NEW_NATIVE_REPLY"}]}}
`
	s, se, thread, _ := sharedInputFixture(t, false, func(review *chatShareReview, se termSession, s *Server) {
		review.Continuations[0].Turns = []termTurn{parseClaudeTranscript(strings.NewReader(lines)).Turns[0]}
	})
	s.terminal.claudeProjects = t.TempDir()
	s.terminal.herdr = nil // transcripts remain readable without a live daemon
	se.LaunchPhase = "active"
	se.Started = true
	se.ResumeID = "abcdefab-1234-1234-1234-abcdefabcdef"
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	path := s.terminal.transcriptPath(se)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// Old native history was already reviewed/imported. The current file must
	// not introduce a second copy or overwrite its approved historical content.

	if err := os.WriteFile(path, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	receipt := terminalInputReceipt{ID: "member-after-share", Fingerprint: hashTerminalText("fixture"), SharedAgent: "kairos", SharedThread: thread, ActorEmail: "member@aion.bio", ActorName: "Member", Text: "MEMBER_MESSAGE", State: "sent", SubmittedHash: hashTerminalText("wrapped member instruction"), ContextSource: agentConversation("portal", "kairos", thread, "", "").Key}
	if err := s.terminal.writeInputReceipt(se.ID, receipt); err != nil {
		t.Fatal(err)
	}
	read := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetPathValue("thread", thread)
		w := httptest.NewRecorder()
		s.AionChatConversation(w, r)
		return w
	}
	var out struct {
		Messages  []chatthreads.Message    `json:"messages"`
		Terminals []codingContinuationView `json:"terminals"`
	}
	for i := 0; i < 2; i++ {
		w := read()
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Messages) != 3 || len(out.Terminals) != 1 || !out.Terminals[0].HistoryAvailable {
			t.Fatal(w.Body.String())
		}
		if strings.Contains(w.Body.String(), "DO_NOT_REIMPORT") || strings.Contains(w.Body.String(), "wrapped member instruction") || out.Terminals[0].Cwd != "" {
			t.Fatal("duplicated history or unnormalized member input", w.Body.String())
		}
		var member, native bool
		for _, m := range out.Messages {
			member = member || (m.Text == "MEMBER_MESSAGE" && m.Author == receipt.ActorEmail)
			native = native || (m.Text == "NEW_NATIVE_REPLY" && m.Author == "agent:claude")
		}
		if !member || !native {
			t.Fatal(out.Messages)
		}
	}
	if len(s.chat.Messages(thread)) != 1 {
		t.Fatal("read copied native messages into store")
	}
	// Missing native history is an explicit warning; team history remains readable.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	w := read()
	if w.Code != 200 || !strings.Contains(w.Body.String(), "temporarily unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, _, _, err := s.sharedInputContext(context.Background(), &sharedTerminalInputScope{s.kairosAgent(), thread, se.ID, "member@aion.bio", "Member"}, nil); err == nil {
		t.Fatal("input accepted incomplete context")
	}
	wrong := httptest.NewRequest("GET", "/", nil)
	wrong.SetPathValue("thread", thread)
	deny := httptest.NewRecorder()
	s.OodaChatConversation(deny, wrong)
	if deny.Code != 403 {
		t.Fatal("other team access", deny.Code)
	}
	called := false
	p := &portalAPI{opt: PortalOptions{ChatConversation: func(_ http.ResponseWriter, _ *http.Request) { called = true }}}
	deny = httptest.NewRecorder()
	p.handleChatConversation(deny, httptest.NewRequest("GET", "/", nil))
	if called || deny.Code < 400 {
		t.Fatal("unauthenticated history callback")
	}
}

func TestSharedHistoryKeepsStableNativeIDsAndUnknownTime(t *testing.T) {
	review := chatShareReview{OwnerEmail: "owner@aion.bio"}
	views := []codingContinuationView{{ID: "native", Agent: "codex", Created: "2026-09-10T10:00:00Z", Turns: []termTurn{{ID: "one", Who: "assistant", Blocks: []termBlock{{T: "say", Text: "first"}}}}}}
	msgs := sharedHistoryMessages(review, "team", nil, views)
	if len(msgs) != 1 || msgs[0].Source.TimestampKnown || msgs[0].At.Equal(time.Time{}) {
		t.Fatal(msgs)
	}
	views[0].Turns[0].Blocks[0].Text = "streamed update"
	next := sharedHistoryMessages(review, "team", nil, views)
	if msgs[0].ID != next[0].ID || next[0].Text != "streamed update" {
		t.Fatal("stream update created a new identity", next)
	}
}
