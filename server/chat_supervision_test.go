package server

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/hermes"
	"manifest/ledger"
)

// gatedStub is a fake `hermes` that blocks while a gate file named by a token
// in the prompt exists, appends every prompt it saw to calls.log, then answers.
// It lets a test hold a provider call open ("the process died with work in
// flight") and count invocations (a replay would append a second line).
func gatedStub(dir string) string {
	return "#!/bin/sh\nprompt=\"\"\nusage=\"\"\nwhile [ $# -gt 0 ]; do case \"$1\" in -z) prompt=\"$2\"; shift 2;; --usage-file) usage=\"$2\"; shift 2;; *) shift;; esac; done\n" +
		"token=$(printf '%s' \"$prompt\" | grep -o 'gate-[a-z]*' | tail -n 1)\n" +
		"printf '%s\\n' \"$token\" >> '" + filepath.Join(dir, "calls.log") + "'\n" +
		"while [ -n \"$token\" ] && [ -e '" + dir + "/'\"$token\" ]; do sleep 0.02; done\n" +
		"printf 'reply for %s' \"$token\"\n" +
		"[ -n \"$usage\" ] && printf '{\"estimated_cost_usd\":0.001,\"model\":\"stub-model\",\"session_id\":\"20260925_%s\"}' \"$token\" > \"$usage\"\nexit 0\n"
}

// agentChatFixtureAt is agentChatFixture over an existing store root and
// ledger directory: the shape of a restarted process on the same files.
func agentChatFixtureAt(t *testing.T, chatsRoot, ledgerDir, script string) (*Server, *agentchat.Store, *ledger.Store) {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil)
	s.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: stub}), "web,memory")
	var hosts HostsInfo
	hosts.Hermes.Enabled, hosts.Hermes.Bin = true, stub
	s.UseHosts(hosts)
	st := agentchat.New(chatsRoot)
	s.UseAgentChat(st)
	led, err := ledger.New(ledgerDir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	s.UseLedger(led)
	return s, st, led
}

func callsLogged(t *testing.T, dir, token string) int {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	return strings.Count(string(raw), token+"\n")
}

func waitInvocation(t *testing.T, s *Server, st *agentchat.Store, agent, id, requestID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		d, _ := st.Receipt(agent, id, requestID)
		s.agentChat.runMu.Lock()
		inv, live := s.agentChat.running[agent+"/"+id]
		s.agentChat.runMu.Unlock()
		if d.State == agentchat.DeliveryRunning && d.ToolScope != nil && live && inv.requestID == requestID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("invocation never started for", requestID)
}

func supervisionOf(t *testing.T, s *Server, agent, id string) chatSupervision {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/agents/chat/"+agent+"/sessions/"+id, nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Supervision chatSupervision `json:"supervision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Supervision
}

func runState(sv chatSupervision, requestID string) supervisionRun {
	for _, r := range sv.Runs {
		if r.RequestID == requestID {
			return r
		}
	}
	return supervisionRun{}
}

// The core restart contract: a provider call that outlived its process is
// disconnected — not running, not done — the queued instruction behind it
// survives and starts exactly once, and the disconnected call is never replayed.
func TestSupervisionRestartDisconnectsWithoutReplay(t *testing.T) {
	gates := t.TempDir()
	if err := os.WriteFile(filepath.Join(gates, "gate-first"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	chats, ledgerDir := filepath.Join(t.TempDir(), "chats"), t.TempDir()
	old, oldStore, _ := agentChatFixtureAt(t, chats, ledgerDir, gatedStub(gates))
	id, err := oldStore.Create("alfred", "", "restart", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.agentChatSendRequest("alfred", id, "request-first", "hold gate-first please", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := old.agentChatSendRequest("alfred", id, "request-second", "then answer gate-second", nil); err != nil {
		t.Fatal(err)
	}
	waitInvocation(t, old, oldStore, "alfred", id, "request-first")
	before := supervisionOf(t, old, "alfred", id)
	if before.State != supervisionRunning || runState(before, "request-first").State != supervisionRunning || runState(before, "request-second").State != supervisionSubmitted {
		t.Fatalf("live process: %+v", before)
	}
	if !strings.Contains(runState(before, "request-first").Evidence, "live invocation") {
		t.Fatal("running must name its evidence", runState(before, "request-first").Evidence)
	}

	// RESTART: a fresh process over the same files. The old goroutine is still
	// parked in the provider call (a real dead process would simply be gone).
	fresh, freshStore, led := agentChatFixtureAt(t, chats, ledgerDir, gatedStub(gates))
	after := supervisionOf(t, fresh, "alfred", id)
	first, second := runState(after, "request-first"), runState(after, "request-second")
	if first.State != supervisionDisconnected || !strings.Contains(first.Evidence, "restarted") {
		t.Fatalf("interrupted run must be disconnected, got %+v", first)
	}
	if second.State != supervisionSubmitted {
		t.Fatalf("queued instruction must survive as submitted, got %+v", second)
	}
	if after.State != supervisionDisconnected && after.State != supervisionSubmitted {
		t.Fatalf("conversation must not present as running or done: %s", after.State)
	}
	receipt, _ := freshStore.Receipt("alfred", id, "request-first")
	if receipt.State != agentchat.DeliveryInterrupted || !receipt.Disconnected {
		t.Fatalf("journal: %+v", receipt)
	}
	// Startup drain: only the unstarted instruction runs, and the old call is
	// never re-sent. Release the old goroutine afterwards; its late result is
	// refused by the journal (delivery is not running), so nothing doubles.
	fresh.ResumeAgentChats()
	sess := waitIdle(t, freshStore, "alfred", id)
	// The disconnection is a durable ledger event carrying the run identity
	// (main wires the ledger after the repair; the drain flushes it).
	entries, _ := led.Day(led.Today())
	var disconnected *ledger.Entry
	for i := range entries {
		if entries[i].Kind == "run.disconnected" && entries[i].Meta["requestId"] == "request-first" {
			disconnected = &entries[i]
		}
	}
	if disconnected == nil || disconnected.Meta["runId"] != first.RunID || disconnected.Meta["queuedRetained"] != float64(1) {
		t.Fatalf("missing run.disconnected event: %+v", disconnected)
	}
	if callsLogged(t, gates, "gate-second") != 1 || callsLogged(t, gates, "gate-first") != 1 {
		t.Fatalf("calls: first=%d second=%d", callsLogged(t, gates, "gate-first"), callsLogged(t, gates, "gate-second"))
	}
	done := supervisionOf(t, fresh, "alfred", id)
	if done.State != supervisionReady || runState(done, "request-second").State != supervisionReady || runState(done, "request-first").State != supervisionDisconnected {
		t.Fatalf("after drain: %+v", done)
	}
	if !strings.Contains(runState(done, "request-second").Evidence, "reply heading") {
		t.Fatal("ready_for_review must name the reply turn", runState(done, "request-second").Evidence)
	}
	_ = os.Remove(filepath.Join(gates, "gate-first"))
	waitFor(t, "the old process's goroutine to give up", func() bool {
		old.agentChat.runMu.Lock()
		defer old.agentChat.runMu.Unlock()
		_, live := old.agentChat.running["alfred/"+id]
		return !live
	})
	final, body, _, _ := freshStore.Get("alfred", id)
	if final.Turns != sess.Turns || strings.Contains(body, "reply for gate-first") {
		t.Fatalf("late result of a disconnected call landed:\n%s", body)
	}
	// Rail rows carry the same projection without the body.
	w := httptest.NewRecorder()
	fresh.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/agents/chat/alfred/sessions", nil))
	var list struct {
		Sessions []struct {
			ID          string          `json:"id"`
			Supervision chatSupervision `json:"supervision"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Sessions) != 1 || list.Sessions[0].Supervision.State != supervisionReady || runState(list.Sessions[0].Supervision, "request-first").State != supervisionDisconnected {
		t.Fatalf("rail rows: %v %s", err, w.Body.String())
	}
}

// No phantom completion: a completed receipt is only ready_for_review when the
// transcript actually holds the reply turn it names.
func TestSupervisionCompletedRequiresReplyTurn(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, _ := st.Create("alfred", "", "", "")
	if _, err := s.agentChatSendRequest("alfred", id, "request-one", "hello there", nil); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, "alfred", id)
	sv := supervisionOf(t, s, "alfred", id)
	if sv.State != supervisionReady || !strings.Contains(sv.Evidence, "Turn 2 — alfred") {
		t.Fatalf("%+v", sv)
	}
	// Corrupt the record the way a torn write would: receipt says completed with
	// reply turn 2, but the body holds only the user turn.
	path := filepath.Join(st.Root(), "alfred", id+".md")
	raw, _ := os.ReadFile(path)
	cut := strings.Index(string(raw), "## Turn 2")
	if cut < 0 {
		t.Fatal("no reply turn to remove")
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(string(raw)[:cut])), 0o644); err != nil {
		t.Fatal(err)
	}
	torn := supervisionOf(t, s, "alfred", id)
	if torn.State != supervisionUnknown || !strings.Contains(torn.Evidence, "no such alfred turn") {
		t.Fatalf("phantom completion: %+v", torn)
	}
	// The body-less rail projection also refuses it once the turn count disagrees.
	sess, _, _, _ := st.Get("alfred", id)
	sess.Turns = 1
	if rail := s.nativeChatSupervision(sess, ""); rail.State != supervisionUnknown {
		t.Fatalf("rail phantom completion: %+v", rail)
	}
}

// A running receipt with no live invocation in a process that owns turns is
// disconnected; on a box whose runner is off it is unknown, never running.
func TestSupervisionRunningWithoutInvocationIsDisconnected(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, _ := st.Create("alfred", "", "", "")
	if _, err := st.Accept("alfred", id, "request-orphan", "orphaned"); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := st.Claim("alfred", id); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	sv := supervisionOf(t, s, "alfred", id)
	if sv.State != supervisionDisconnected || !strings.Contains(sv.Evidence, "no live invocation") {
		t.Fatalf("%+v", sv)
	}
	twin := New(nil, nil, nil)
	twin.UseAgentChat(agentchat.New(st.Root()))
	sess, body, _, _ := twin.agentChat.store.Get("alfred", id)
	if got := twin.nativeChatSupervision(sess, body); got.State != supervisionUnknown || !strings.Contains(got.Evidence, "cannot own turns") {
		t.Fatalf("dev twin must not claim disconnected or running: %+v", got)
	}
	if receipt, _ := st.Receipt("alfred", id, "request-orphan"); receipt.State != agentchat.DeliveryRunning {
		t.Fatal("projection must not rewrite the record", receipt)
	}
}

// Two threads, interleaved events: each conversation's state follows its own
// receipts and invocation; finishing one never moves the other.
func TestSupervisionConcurrentThreadsInterleave(t *testing.T) {
	gates := t.TempDir()
	for _, g := range []string{"gate-a", "gate-b"} {
		if err := os.WriteFile(filepath.Join(gates, g), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, st, led := agentChatFixtureAt(t, filepath.Join(t.TempDir(), "chats"), t.TempDir(), gatedStub(gates))
	a, _ := st.Create("alfred", "", "thread a", "")
	b, _ := st.Create("alfred", "", "thread b", "")
	if _, err := s.agentChatSendRequest("alfred", a, "request-a", "hold gate-a", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.agentChatSendRequest("alfred", b, "request-b", "hold gate-b", nil); err != nil {
		t.Fatal(err)
	}
	waitInvocation(t, s, st, "alfred", a, "request-a")
	waitInvocation(t, s, st, "alfred", b, "request-b")
	if sa, sb := supervisionOf(t, s, "alfred", a), supervisionOf(t, s, "alfred", b); sa.State != supervisionRunning || sb.State != supervisionRunning || sa.Runs[0].RunID == sb.Runs[0].RunID {
		t.Fatalf("a=%+v b=%+v", sa, sb)
	}
	_ = os.Remove(filepath.Join(gates, "gate-b"))
	waitIdle(t, st, "alfred", b)
	sa, sb := supervisionOf(t, s, "alfred", a), supervisionOf(t, s, "alfred", b)
	if sa.State != supervisionRunning || sb.State != supervisionReady {
		t.Fatalf("interleaved: a=%s b=%s", sa.State, sb.State)
	}
	_ = os.Remove(filepath.Join(gates, "gate-a"))
	waitIdle(t, st, "alfred", a)
	if sa = supervisionOf(t, s, "alfred", a); sa.State != supervisionReady {
		t.Fatalf("a after release: %+v", sa)
	}
	if callsLogged(t, gates, "gate-a") != 1 || callsLogged(t, gates, "gate-b") != 1 {
		t.Fatal("each thread invoked exactly once")
	}
	ea, eb := waitAgentChatOutcome(t, led, "request-a"), waitAgentChatOutcome(t, led, "request-b")
	if ea.Meta["runId"] != sa.Runs[0].RunID || eb.Meta["runId"] != sb.Runs[0].RunID || ea.Meta["runId"] == eb.Meta["runId"] {
		t.Fatalf("ledger run identity: %v %v", ea.Meta["runId"], eb.Meta["runId"])
	}
}

// herdrSupervisionFixture: a herdr daemon whose pane state the test flips
// between idle / working / gone (no pane → process stopped).
func herdrSupervisionFixture(t *testing.T, kind string) (*Server, termSession, *atomic.Value, *atomic.Int32) {
	t.Helper()
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	s.UseChatState(t.TempDir())
	var prompts atomic.Int32
	var state atomic.Value
	state.Store("idle")
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			if state.Load().(string) == "gone" {
				herdrFixtureReply(c, map[string]any{"type": "session_snapshot", "snapshot": map[string]any{"version": "0.9.0", "protocol": 22, "panes": []any{}}})
				return
			}
			herdrFixtureSnapshot(c, state.Load().(string), 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input", "pane.send_keys":
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		case "agent.prompt":
			prompts.Add(1)
			herdrFixtureReply(c, map[string]any{})
		case "pane.process_info":
			herdrFixtureReply(c, map[string]any{}) // no rollout discovery in the fixture
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	return s, createCodingDraft(t, s, kind), &state, &prompts
}

func terminalTranscriptJSON(t *testing.T, s *Server, id string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("id", id)
	s.handleTermTranscript(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	out := map[string]any{}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A question whose run is gone is stale: not answerable, nothing sent, and
// the projection says so instead of offering an answer form.
func TestTerminalStaleQuestionRefused(t *testing.T) {
	s, se, state, prompts := herdrSupervisionFixture(t, "codex")
	se = questionRollout(t, s, se)
	live := terminalTranscriptJSON(t, s, se.ID)["questions"].([]any)
	if live[0].(map[string]any)["state"] != "pending" {
		t.Fatal(live)
	}
	state.Store("gone")
	stale := terminalTranscriptJSON(t, s, se.ID)["questions"].([]any)
	if stale[0].(map[string]any)["state"] != questionStale || stale[1].(map[string]any)["state"] != questionStale {
		t.Fatalf("questions of a dead run must be stale: %v", stale)
	}
	payload, _ := json.Marshal(terminalInput{RequestID: "stale-answer-001", QuestionAnswers: []terminalQuestionAnswer{{ID: `["request_user_input_async","call_one",0]`, Answer: "Two"}}})
	w := receiptInput(s, se.ID, string(payload))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "stale") || !strings.Contains(w.Body.String(), "nothing sent") {
		t.Fatal(w.Code, w.Body.String())
	}
	if prompts.Load() != 0 {
		t.Fatal("stale answer reached the runtime")
	}
	if _, err := s.terminal.readInputReceipt(se.ID, "stale-answer-001"); err == nil {
		t.Fatal("stale answer left a receipt")
	}
	if rows := s.terminal.load(); rows[0].Started != se.Started || rows[0].Runtime != se.Runtime {
		t.Fatal("stale answer resumed or relaunched the conversation", rows[0])
	}
}

// Capability truth on the wire: the legacy runtime refuses queue/steer in
// words; the projection for a herdr row follows receipts + observation and
// never reads an idle process as done.
func TestTerminalSupervisionProjectionAndRefusals(t *testing.T) {
	s, se, state, prompts := herdrSupervisionFixture(t, "codex")
	// Unsupported adapter action: a tmux-legacy row cannot queue or steer.
	legacy := termSession{ID: "abcdef1234567890", Kind: "claude", Cwd: s.terminal.defaultWd}
	s.terminal.upsert(legacy)
	for _, body := range []string{`{"text":"x","steer":true}`, `{"text":"x","afterRun":true}`} {
		if w := receiptInput(s, legacy.ID, body); w.Code != 409 || !strings.Contains(w.Body.String(), "tmux-legacy") || !strings.Contains(w.Body.String(), "nothing sent") {
			t.Fatal("legacy steer/queue accepted", w.Code, w.Body.String())
		}
	}
	if caps := terminalChatCapabilities(legacy); caps.Steer != "unsupported" || caps.Queue != "none" || caps.Supervision != "observation-only" {
		t.Fatalf("%+v", caps)
	}

	draft := terminalTranscriptJSON(t, s, se.ID)
	if sv := draft["supervision"].(map[string]any); sv["state"] != supervisionUnknown || sv["adapter"] != adapterHerdrCodex {
		t.Fatalf("draft: %v", sv)
	}
	if caps := draft["capabilities"].(map[string]any); caps["answerQuestions"] != "async-codex" || caps["liveSteering"] != true || caps["steer"] != "explicit" {
		t.Fatalf("capabilities: %v", caps)
	}
	if w := receiptInput(s, se.ID, `{"text":"start here","requestId":"sup-input-001"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if prompts.Load() != 1 {
		t.Fatal(prompts.Load())
	}
	se, _ = s.terminal.find(se.ID)
	state.Store("working")
	working := terminalTranscriptJSON(t, s, se.ID)["supervision"].(map[string]any)
	if working["state"] != supervisionRunning || !strings.Contains(working["evidence"].(string), "sup-input-001") {
		t.Fatalf("working: %v", working)
	}
	// Idle with no provider record answering the submission is unknown, not done.
	state.Store("idle")
	idle := terminalTranscriptJSON(t, s, se.ID)["supervision"].(map[string]any)
	if idle["state"] != supervisionUnknown || !strings.Contains(idle["evidence"].(string), "no provider record") {
		t.Fatalf("idle must not be completion: %v", idle)
	}
	// Process gone: the sent submission is disconnected, not running or done.
	state.Store("gone")
	gone := terminalTranscriptJSON(t, s, se.ID)["supervision"].(map[string]any)
	if gone["state"] != supervisionDisconnected || !strings.Contains(gone["evidence"].(string), "stopped") {
		t.Fatalf("gone: %v", gone)
	}
	// An unconfirmed receipt (the send crossed the boundary without a reply) is disconnected.
	unconfirmed := terminalInputReceipt{ID: "sup-input-002", Fingerprint: strings.Repeat("a", 64), State: "unconfirmed", Runtime: se.Runtime, SubmittedHash: hashTerminalText("x")}
	if err := s.terminal.writeInputReceipt(se.ID, unconfirmed); err != nil {
		t.Fatal(err)
	}
	state.Store("idle")
	sv := s.terminalChatSupervision(se, termTranscript{}, terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle"})
	if got := runState(sv, "sup-input-002"); got.State != supervisionDisconnected || !strings.Contains(got.Evidence, "unconfirmed") {
		t.Fatalf("unconfirmed: %+v", got)
	}
	if sv.State != supervisionDisconnected {
		t.Fatalf("an uncertain send wins over an older unknown one: %+v", sv)
	}
	// A provider assistant turn recorded after the submission proves a result.
	sent, _ := s.terminal.readInputReceipt(se.ID, "sup-input-001")
	later := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	tr := termTranscript{Turns: []termTurn{{ID: "t1", Who: "assistant", TS: later, Blocks: []termBlock{{T: "say", Text: "done"}}}}}
	if got, why := terminalReceiptState(sent, tr, terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle"}); got != supervisionReady || !strings.Contains(why, "assistant turn t1") {
		t.Fatalf("assistant record after submission: %s %s", got, why)
	}
	// A waiting outbox entry is a submitted run.
	key := s.terminalConversation(se).Key
	item := map[string]any{"stateKey": key, "scope": "codex/" + se.ID, "agent": "codex", "url": "/api/terminal/session/" + se.ID + "/input", "at": later, "staged": true, "payload": map[string]any{"text": "later", "requestId": "sup-queued-001"}}
	raw, _ := json.Marshal(item)
	value, _ := json.Marshal(chatQueueValue{Items: map[string]json.RawMessage{"sup-queued-001": raw}})
	snap, _ := s.chatState.Read(key, "deliveries")
	if _, err := s.chatState.Write(key, "deliveries", snap.Revision, value); err != nil {
		t.Fatal(err)
	}
	if got := runState(s.terminalChatSupervision(se, termTranscript{}, terminalObservation{Connectivity: "connected", Process: "running", AgentState: "working"}), "sup-queued-001"); got.State != supervisionSubmitted {
		t.Fatalf("outbox entry: %+v", got)
	}
}

// Capability claims are data, adapter by adapter; none of them promises an
// automatic retry, and an idle-process adapter never claims receipts.
func TestChatAdapterCapabilityMatrix(t *testing.T) {
	seen := map[string]chatCapabilities{}
	for _, c := range chatAdapterCapabilities() {
		if c.Retry != "explicit-resubmit" || c.SkillInventory != "not-reported" {
			t.Fatalf("invented capability: %+v", c)
		}
		if (c.Queue == "durable") != c.CancelQueued || (c.Steer == "explicit") != c.LiveSteering || (c.AnswerQuestions == "async-codex") != c.StructuredQuestions {
			t.Fatalf("inconsistent claims: %+v", c)
		}
		seen[c.Adapter] = c
	}
	if seen[adapterHermesOneshot].Resume != "fresh-session-per-turn" || seen[adapterHermesOneshot].Steer != "unsupported" || seen[adapterHermesOneshot].Stop != "request" {
		t.Fatalf("%+v", seen[adapterHermesOneshot])
	}
	if seen[adapterHerdrClaude].AnswerQuestions != "terminal-only" || seen[adapterHerdrClaude].StructuredQuestions {
		t.Fatalf("%+v", seen[adapterHerdrClaude])
	}
	if seen[adapterTmuxLegacy].Supervision != "observation-only" || seen[adapterRemoteKeep].Stop != "unsupported" || seen[adapterRemoteKeep].Resume != "unsupported" {
		t.Fatalf("%+v %+v", seen[adapterTmuxLegacy], seen[adapterRemoteKeep])
	}
	// Refusal rather than silence: an interrupt for a native turn that is not
	// running is a 409 with the reason, never a no-op 200.
	s, st, _ := agentChatFixture(t, echoStub)
	id, _ := st.Create("alfred", "", "", "")
	if _, err := s.agentChatSendRequest("alfred", id, "request-done", "hello", nil); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, st, "alfred", id)
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"requestId":"request-done"}`))
	r.SetPathValue("agent", "alfred")
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleAgentChatInterrupt(w, r)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "not running") {
		t.Fatal(w.Code, w.Body.String())
	}
}
