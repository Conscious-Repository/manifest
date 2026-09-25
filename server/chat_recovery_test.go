package server

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Restart between the receipt and the daemon's reply: the receipt stays
// `unconfirmed` for ever, but the provider's own transcript can prove the
// prompt landed. Only the exact submitted bytes count; nothing is replayed and
// the receipt file is not rewritten.
func TestTerminalUnconfirmedReceiptRecoversFromProviderRecord(t *testing.T) {
	s, se, state, _ := herdrSupervisionFixture(t, "codex")
	if w := receiptInput(s, se.ID, `{"text":"first landed","requestId":"unconf-000"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	se, _ = s.terminal.find(se.ID)
	// The crash: handleTermInput wrote the receipt and died before SendText
	// returned (the shape prepareReceipt persists, minus the finalisation).
	text := "second crossed the boundary"
	submitted := terminalInput{Text: text + "\n", RequestID: "unconf-001"}
	unconfirmed := terminalInputReceipt{ID: "unconf-001", Fingerprint: submitted.fingerprint(), State: "unconfirmed", Runtime: se.Runtime, SubmittedHash: hashTerminalText(submitted.Text)}
	if err := s.terminal.writeInputReceipt(se.ID, unconfirmed); err != nil {
		t.Fatal(err)
	}
	// RESTART: a fresh process over the same registry, receipts and chat state.
	fresh := &Server{terminal: &termCfg{regPath: s.terminal.regPath, defaultWd: s.terminal.defaultWd, herdr: s.terminal.herdr}}
	fresh.UseChatState(filepath.Dir(s.chatFilesRoot))
	idle := terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle"}
	if got := runState(fresh.terminalChatSupervision(se, termTranscript{}, idle), "unconf-001"); got.State != supervisionDisconnected || !strings.Contains(got.Evidence, "unconfirmed") {
		t.Fatalf("no provider record: %+v", got)
	}
	// A user turn with different bytes is not this submission.
	other := termTranscript{Turns: []termTurn{{ID: "u0", Who: "user", Text: "something else"}}}
	if got := runState(fresh.terminalChatSupervision(se, other, idle), "unconf-001"); got.State != supervisionDisconnected {
		t.Fatalf("similar text must not confirm: %+v", got)
	}
	// The provider recorded the exact bytes (Codex may drop the final LF).
	later := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	landed := termTranscript{Turns: []termTurn{{ID: "u1", Who: "user", Text: text, TS: later}}}
	state.Store("working")
	working := terminalObservation{Connectivity: "connected", Process: "running", AgentState: "working", ObservedAt: time.Now()}
	if got := runState(fresh.terminalChatSupervision(se, landed, working), "unconf-001"); got.State != supervisionRunning || !strings.Contains(got.Evidence, "user turn u1") || !strings.Contains(got.Evidence, "exact submitted bytes") {
		t.Fatalf("confirmed and working: %+v", got)
	}
	answered := termTranscript{Turns: append(landed.Turns, termTurn{ID: "a1", Who: "assistant", TS: later, Blocks: []termBlock{{T: "say", Text: "done"}}})}
	got := runState(fresh.terminalChatSupervision(se, answered, idle), "unconf-001")
	if got.State != supervisionReady || !strings.Contains(got.Evidence, "user turn u1") || !strings.Contains(got.Evidence, "assistant turn a1") {
		t.Fatalf("confirmed and answered: %+v", got)
	}
	// Nothing was replayed and the durable record is untouched.
	if r, err := fresh.terminal.readInputReceipt(se.ID, "unconf-001"); err != nil || r.State != "unconfirmed" {
		t.Fatalf("receipt rewritten: %+v %v", r, err)
	}
	retry, _ := json.Marshal(submitted)
	if w := receiptInput(fresh, se.ID, string(retry)); w.Code != 202 || !strings.Contains(w.Body.String(), `"state":"unconfirmed"`) {
		t.Fatalf("retry must return the receipt, not send again: %d %s", w.Code, w.Body.String())
	}
	// A different-content retry under the same ID is still a conflict.
	if w := receiptInput(fresh, se.ID, `{"text":"changed","requestId":"unconf-001"}`); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	// The transcript route carries the same projection.
	row := terminalTranscriptJSON(t, fresh, se.ID)["supervision"].(map[string]any)
	if row["state"] == supervisionReady {
		t.Fatalf("fixture transcript has no provider record; must not read done: %v", row)
	}
}

// While this process is inside the send, the submission is in progress here:
// a claimed outbox entry is submitted, an unconfirmed receipt is running, and
// neither flickers into the Disconnected filter. Once the send returns the
// receipt is the single record for the request.
func TestTerminalDispatchInProgressIsNotDisconnected(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	s.UseChatState(t.TempDir())
	var gateRead, gatePrompt atomic.Bool
	readHeld, promptHeld := make(chan struct{}, 8), make(chan struct{}, 8)
	releaseRead, releasePrompt := make(chan struct{}), make(chan struct{})
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.read":
			if gateRead.Load() {
				readHeld <- struct{}{}
				<-releaseRead
			}
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		case "agent.prompt":
			if gatePrompt.Load() {
				promptHeld <- struct{}{}
				<-releasePrompt
			}
			herdrFixtureReply(c, map[string]any{})
		case "pane.send_input", "pane.send_keys", "pane.process_info":
			herdrFixtureReply(c, map[string]any{})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	se := createCodingDraft(t, s, "codex")
	// Launch with an ordinary first message so the pane exists.
	if w := receiptInput(s, se.ID, `{"text":"launch","requestId":"flight-000"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	se, _ = s.terminal.find(se.ID)
	key := s.terminalConversation(se).Key
	// The sweep's claim: staged=false, no receipt yet.
	item := map[string]any{"stateKey": key, "scope": "codex/" + se.ID, "agent": "codex", "url": "/api/terminal/session/" + se.ID + "/input", "at": time.Now().UTC().Format(time.RFC3339Nano), "staged": false, "payload": map[string]any{"text": "held follow-up", "requestId": "flight-001", "afterRun": true}}
	raw, _ := json.Marshal(item)
	value, _ := json.Marshal(chatQueueValue{Items: map[string]json.RawMessage{"flight-001": raw}})
	snap, _ := s.chatState.Read(key, "deliveries")
	if _, err := s.chatState.Write(key, "deliveries", snap.Revision, value); err != nil {
		t.Fatal(err)
	}
	idle := terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle"}
	if got := runState(s.terminalChatSupervision(se, termTranscript{}, idle), "flight-001"); got.State != supervisionDisconnected {
		t.Fatalf("claimed entry with nobody dispatching it is uncertain: %+v", got)
	}
	gateRead.Store(true)
	gatePrompt.Store(true)
	done := make(chan int, 1)
	go func() {
		done <- receiptInput(s, se.ID, `{"text":"held follow-up","requestId":"flight-001","afterRun":true}`).Code
	}()
	<-readHeld // inside herdrPromptReady: no receipt exists yet
	got := runState(s.terminalChatSupervision(se, termTranscript{}, idle), "flight-001")
	if got.State != supervisionSubmitted || !strings.Contains(got.Evidence, "dispatch in progress in this process") {
		t.Fatalf("claimed and dispatching: %+v", got)
	}
	if _, err := s.terminal.readInputReceipt(se.ID, "flight-001"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("receipt must not exist before prompt readiness", err)
	}
	gateRead.Store(false)
	close(releaseRead)
	<-promptHeld // receipt written (unconfirmed), daemon call in flight
	if r, err := s.terminal.readInputReceipt(se.ID, "flight-001"); err != nil || r.State != "unconfirmed" {
		t.Fatalf("receipt before the daemon reply: %+v %v", r, err)
	}
	sv := s.terminalChatSupervision(se, termTranscript{}, idle)
	got = runState(sv, "flight-001")
	if got.State != supervisionRunning || !strings.Contains(got.Evidence, "send in progress in this process") {
		t.Fatalf("unconfirmed but in flight here: %+v", got)
	}
	if n := 0; true {
		for _, r := range sv.Runs {
			if r.RequestID == "flight-001" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("one run per request, got %d: %+v", n, sv.Runs)
		}
	}
	if sv.State == supervisionDisconnected {
		t.Fatalf("conversation flickered to disconnected during dispatch: %+v", sv)
	}
	close(releasePrompt)
	if code := <-done; code != 200 {
		t.Fatal(code)
	}
	after := s.terminalChatSupervision(se, termTranscript{}, idle)
	got = runState(after, "flight-001")
	if got.State == supervisionDisconnected || got.State == supervisionRunning || !strings.Contains(got.Evidence, "sent") {
		t.Fatalf("after the send: %+v", got)
	}
	// A restart forgets the in-flight table: an unconfirmed receipt left by a
	// dead process is disconnected again, never "in progress".
	if err := s.terminal.writeInputReceipt(se.ID, terminalInputReceipt{ID: "flight-002", Fingerprint: strings.Repeat("c", 64), State: "unconfirmed", Runtime: se.Runtime, SubmittedHash: hashTerminalText("lost")}); err != nil {
		t.Fatal(err)
	}
	fresh := &Server{terminal: &termCfg{regPath: s.terminal.regPath, defaultWd: s.terminal.defaultWd, herdr: h}}
	fresh.UseChatState(filepath.Dir(s.chatFilesRoot))
	if got := runState(fresh.terminalChatSupervision(se, termTranscript{}, idle), "flight-002"); got.State != supervisionDisconnected {
		t.Fatalf("restart: %+v", got)
	}
}

// The legacy runtime has no receipts, and the browser retries 502/503 on its
// own (a proxy 502 means Manifest was never reached). A send that failed
// part-way must therefore not answer 502: that retry would type the text again.
func TestLegacyTerminalSendFailureIsNotAutoRetried(t *testing.T) {
	var calls []string
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir(), run: func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "list-sessions" {
			return []byte(tmuxName("abcdef1234567890") + "\n"), nil
		}
		if args[0] == "send-keys" && args[len(args)-1] == "Enter" {
			return nil, errors.New("tmux: pane vanished")
		}
		return nil, nil
	}}}
	legacy := termSession{ID: "abcdef1234567890", Kind: "claude", Cwd: s.terminal.defaultWd, Started: true}
	s.terminal.upsert(legacy)
	w := receiptInput(s, legacy.ID, `{"text":"typed once"}`)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "no automatic retry") || !strings.Contains(w.Body.String(), "uncertain") {
		t.Fatalf("partial send must refuse an automatic retry: %d %s", w.Code, w.Body.String())
	}
	typed := 0
	for _, c := range calls {
		if strings.Contains(c, "typed once") {
			typed++
		}
	}
	if typed != 1 {
		t.Fatalf("text reached the pane %d times: %v", typed, calls)
	}
}

// A claimed hermes delivery is registered as this process's live invocation
// before the send returns, so no read can find a running receipt without an
// invocation and call it disconnected.
func TestHermesClaimIsLiveBeforeSendReturns(t *testing.T) {
	gates := t.TempDir()
	if err := os.WriteFile(filepath.Join(gates, "gate-hold"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, st, _ := agentChatFixtureAt(t, filepath.Join(t.TempDir(), "chats"), t.TempDir(), gatedStub(gates))
	id, err := st.Create("alfred", "", "claim", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.agentChatSendRequest("alfred", id, "request-hold", "please hold gate-hold", nil); err != nil {
		t.Fatal(err)
	}
	// Synchronously after the send: the receipt is running and the invocation
	// is already registered (before this change the goroutine registered it).
	d, _ := st.Receipt("alfred", id, "request-hold")
	s.agentChat.runMu.Lock()
	inv, live := s.agentChat.running["alfred/"+id]
	s.agentChat.runMu.Unlock()
	if d.State != "running" || !live || inv.requestID != "request-hold" {
		t.Fatalf("running receipt without a live invocation: %s live=%v %+v", d.State, live, inv)
	}
	if sv := supervisionOf(t, s, "alfred", id); runState(sv, "request-hold").State != supervisionRunning {
		t.Fatalf("projected %s while this process owns the turn: %+v", runState(sv, "request-hold").State, sv)
	}
	waitInvocation(t, s, st, "alfred", id, "request-hold")
	_ = os.Remove(filepath.Join(gates, "gate-hold"))
	waitIdle(t, st, "alfred", id)
	if callsLogged(t, gates, "gate-hold") != 1 {
		t.Fatal("replayed", callsLogged(t, gates, "gate-hold"))
	}
}
