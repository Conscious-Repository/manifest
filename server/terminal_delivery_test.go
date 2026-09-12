package server

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func terminalReceiptFixture(t *testing.T, uncertain bool) (*Server, termSession, *atomic.Int32) {
	t.Helper()
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var prompts atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		case "agent.prompt":
			prompts.Add(1)
			rows := s.terminal.load()
			receipt, err := s.terminal.readInputReceipt(rows[0].ID, "receipt-input-001")
			if err != nil || receipt.State != "unconfirmed" || receipt.Runtime.Pane == "" {
				t.Errorf("prompt crossed boundary before receipt: %+v %v", receipt, err)
			}
			if uncertain {
				_ = json.NewEncoder(c).Encode(map[string]any{"id": "manifest", "error": map[string]any{"code": "timeout", "message": "reply lost"}})
			} else {
				herdrFixtureReply(c, map[string]any{})
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	return s, createCodingDraft(t, s, "claude"), &prompts
}

func receiptInput(s *Server, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.SetPathValue("id", id)
	s.handleTermInput(w, r)
	return w
}

func TestTerminalInputReceiptsRecoverWithoutReplay(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "sent", true: "unconfirmed"}[uncertain], func(t *testing.T) {
			s, se, prompts := terminalReceiptFixture(t, uncertain)
			body := `{"text":"one instruction","requestId":"receipt-input-001"}`
			want := 200
			if uncertain {
				want = 202
			}
			if w := receiptInput(s, se.ID, body); w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			// A restarted server with NO daemon can answer a lost-response retry.
			restarted := &Server{terminal: &termCfg{regPath: s.terminal.regPath}}
			for i := 0; i < 2; i++ {
				if w := receiptInput(restarted, se.ID, body); w.Code != want {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if prompts.Load() != 1 {
				t.Fatal("replayed", prompts.Load())
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/?request=receipt-input-001", nil)
			r.SetPathValue("id", se.ID)
			restarted.handleTermDelivery(w, r)
			if w.Code != want || !strings.Contains(w.Body.String(), `"state":"`+map[bool]string{true: "unconfirmed", false: "sent"}[uncertain]+`"`) {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := receiptInput(restarted, se.ID, `{"text":"changed","requestId":"receipt-input-001"}`); w.Code != 409 {
				t.Fatal("changed request accepted", w.Code)
			}
		})
	}
}

func TestTerminalInputReceiptConcurrentRetries(t *testing.T) {
	s, se, prompts := terminalReceiptFixture(t, false)
	var wg sync.WaitGroup
	results := make(chan int, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- receiptInput(s, se.ID, `{"text":"one instruction","requestId":"receipt-input-001"}`).Code
		}()
	}
	wg.Wait()
	close(results)
	for code := range results {
		if code != 200 {
			t.Fatal(code)
		}
	}
	if prompts.Load() != 1 {
		t.Fatal("duplicate instructions", prompts.Load())
	}
}

func TestTerminalInputReceiptStorageFailureSendsNothing(t *testing.T) {
	s, se, prompts := terminalReceiptFixture(t, false)
	// Prevent even the initial receipt directory from being created.
	if err := os.WriteFile(s.terminal.regPath+".inputs", []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	w := receiptInput(s, se.ID, `{"text":"never submit","requestId":"receipt-input-001"}`)
	if w.Code != 500 || prompts.Load() != 0 {
		t.Fatal(w.Code, w.Body.String(), prompts.Load())
	}
	if row, _ := s.terminal.find(se.ID); !row.isDraft() {
		t.Fatal("receipt lookup failure launched draft")
	}
}

func TestTerminalInputReceiptCrashBoundaryDoesNotReplay(t *testing.T) {
	s, se, prompts := terminalReceiptFixture(t, false)
	b := terminalInput{Text: "one instruction", RequestID: "receipt-input-001"}
	// Simulate persistence followed by a crash before the runtime response.
	if err := s.terminal.writeInputReceipt(se.ID, terminalInputReceipt{ID: b.RequestID, Fingerprint: b.fingerprint(), State: "unconfirmed"}); err != nil {
		t.Fatal(err)
	}
	w := receiptInput(s, se.ID, `{"text":"one instruction","requestId":"receipt-input-001"}`)
	if w.Code != 202 || prompts.Load() != 0 {
		t.Fatal(w.Code, w.Body.String(), prompts.Load())
	}
}

func TestTerminalSteeringWhileWorkingAndBlockedRejection(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	state := "working"
	reject := false
	prompts := 0
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, state, 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "›"}})
		case "agent.prompt":
			prompts++
			if reject {
				_ = json.NewEncoder(c).Encode(map[string]any{"id": "manifest", "error": map[string]any{"code": "agent_blocked", "message": "requires interactive input"}})
			} else {
				herdrFixtureReply(c, map[string]any{})
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	se := createCodingDraft(t, s, "codex")
	// steer:true is the deliberate mid-turn send; a plain message into a
	// working agent is held instead (TestTerminalInputHeldWhileAgentWorking)
	body := `{"text":"Change direction while working","requestId":"steering-working-001","steer":true}`
	if w := receiptInput(s, se.ID, body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if prompts != 1 {
		t.Fatal("working input was not dispatched exactly once", prompts)
	}
	state = "blocked"
	blocked := `{"text":"Follow up after questions","requestId":"steering-blocked-002","steer":true}`
	if w := receiptInput(s, se.ID, blocked); w.Code == 200 || !strings.Contains(w.Body.String(), "nothing sent") {
		t.Fatal(w.Code, w.Body.String())
	}
	if prompts != 1 {
		t.Fatal("blocked agent received input")
	}
	if _, err := s.terminal.readInputReceipt(se.ID, "steering-blocked-002"); !os.IsNotExist(err) {
		t.Fatal("blocked input became uncertain", err)
	}
	state = "working"
	reject = true
	if w := receiptInput(s, se.ID, blocked); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := s.terminal.readInputReceipt(se.ID, "steering-blocked-002"); !os.IsNotExist(err) {
		t.Fatal("pre-write rejection became uncertain", err)
	}
	reject = false
	if w := receiptInput(s, se.ID, blocked); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
