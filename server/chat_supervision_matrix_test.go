package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/hermes"
	"manifest/threads"
)

// Every state transition in chat_supervision.go has a named fixture here (or
// names the existing test that owns it). The adapters are fakes — constructed
// journal/receipt/marker records, the herdr fixture daemon, the Hermes stub —
// and the names below are the transitions a live-provider journey would have
// to reproduce; those live journeys are not run in this environment.
//
// Owned elsewhere (not duplicated):
//   native restart → disconnected, no replay .......... TestSupervisionRestartDisconnectsWithoutReplay
//   native completed needs its reply heading ......... TestSupervisionCompletedRequiresReplyTurn
//   native running, no invocation → disconnected ..... TestSupervisionRunningWithoutInvocationIsDisconnected
//   herdr working/idle/gone/outbox staged ............ TestTerminalSupervisionProjectionAndRefusals
//   herdr unconfirmed recovered from provider record . TestTerminalUnconfirmedReceiptRecoversFromProviderRecord
//   herdr dispatch window → submitted/running ........ TestTerminalDispatchInProgressIsNotDisconnected
//   task-thread re-dispatch chain → abandoned ........ TestHermesTurnSweepRedispatchIsVisibleAndPinned

func TestSupervisionNativeTransitionMatrix(t *testing.T) {
	body := "## Turn 1 — you · 2026-09-25T10:00:00Z\n\nhello\n\n## Turn 2 — alfred · 2026-09-25T10:00:05Z\n\nhi\n"
	owning := New(nil, nil, nil)
	owning.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: "/bin/false"}), "")
	owning.agentChat = &agentChatCfg{running: map[string]agentChatInvocation{"alfred/s1": {requestID: "req-live"}}}
	readOnly := New(nil, nil, nil) // cannot own turns: another writer may hold the call
	readOnly.agentChat = &agentChatCfg{running: map[string]agentChatInvocation{}}
	cases := []struct {
		name     string
		srv      *Server
		d        agentchat.Delivery
		body     string
		turns    int
		want     string
		evidence string
	}{
		{"queued→submitted", owning, agentchat.Delivery{ID: "req-q", State: agentchat.DeliveryQueued}, body, 2, supervisionSubmitted, "queued delivery receipt"},
		{"running+live invocation→running", owning, agentchat.Delivery{ID: "req-live", State: agentchat.DeliveryRunning, UserTurn: 1}, body, 2, supervisionRunning, "live invocation"},
		{"running, process cannot own turns→unknown", readOnly, agentchat.Delivery{ID: "req-r", State: agentchat.DeliveryRunning}, body, 2, supervisionUnknown, "cannot own turns"},
		{"running, no invocation→disconnected", owning, agentchat.Delivery{ID: "req-orphan", State: agentchat.DeliveryRunning}, body, 2, supervisionDisconnected, "no live invocation"},
		{"completed with reply heading→ready_for_review", owning, agentchat.Delivery{ID: "req-c", State: agentchat.DeliveryCompleted, UserTurn: 1, ReplyTurn: 2}, body, 2, supervisionReady, "reply heading"},
		{"completed, reply heading missing→unknown", owning, agentchat.Delivery{ID: "req-c", State: agentchat.DeliveryCompleted, UserTurn: 1, ReplyTurn: 4}, body, 2, supervisionUnknown, "no such"},
		{"completed, reply not after user turn→unknown", owning, agentchat.Delivery{ID: "req-c", State: agentchat.DeliveryCompleted, UserTurn: 2, ReplyTurn: 2}, body, 2, supervisionUnknown, "no such"},
		{"rail row: completed within turn count→ready_for_review", owning, agentchat.Delivery{ID: "req-c", State: agentchat.DeliveryCompleted, UserTurn: 1, ReplyTurn: 2}, "", 2, supervisionReady, "reply turn 2 of 2"},
		{"rail row: completed beyond turn count→unknown", owning, agentchat.Delivery{ID: "req-c", State: agentchat.DeliveryCompleted, UserTurn: 1, ReplyTurn: 3}, "", 2, supervisionUnknown, "no such"},
		{"failed→failed", owning, agentchat.Delivery{ID: "req-f", State: agentchat.DeliveryFailed, Error: "runner exit 1"}, body, 2, supervisionFailed, "runner exit 1"},
		{"interrupted by restart→disconnected", owning, agentchat.Delivery{ID: "req-i", State: agentchat.DeliveryInterrupted, Disconnected: true, Error: "server restarted"}, body, 2, supervisionDisconnected, "server restarted"},
		{"interrupted by owner→interrupted", owning, agentchat.Delivery{ID: "req-i", State: agentchat.DeliveryInterrupted, Error: "stopped by owner"}, body, 2, supervisionInterrupted, "interrupted"},
		{"cancelled before dispatch→cancelled", owning, agentchat.Delivery{ID: "req-x", State: agentchat.DeliveryCancelled}, body, 2, supervisionCancelled, "cancelled before dispatch"},
		{"unrecognised journal state→unknown", owning, agentchat.Delivery{ID: "req-u", State: "exploded"}, body, 2, supervisionUnknown, "unrecognised state"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sess := agentchat.Session{Agent: "alfred", ID: "s1", Turns: c.turns, Deliveries: []agentchat.Delivery{c.d}}
			sv := c.srv.nativeChatSupervision(sess, c.body)
			if len(sv.Runs) != 1 || sv.Runs[0].State != c.want || !strings.Contains(sv.Runs[0].Evidence, c.evidence) {
				t.Fatalf("%+v", sv.Runs)
			}
			if sv.Runs[0].RunID != supervisionRunID(adapterHermesOneshot, sv.Conversation, c.d.ID) {
				t.Fatalf("run identity: %s", sv.Runs[0].RunID)
			}
		})
	}
	for _, c := range []struct {
		name string
		sess agentchat.Session
		want string
	}{
		{"no receipts, legacy thinking flag→unknown", agentchat.Session{Agent: "alfred", ID: "s2", Status: agentchat.StatusThinking, Turns: 1}, "legacy transport"},
		{"no receipts, no turns→unknown", agentchat.Session{Agent: "alfred", ID: "s3"}, "no instruction accepted"},
		{"no receipts, legacy turns→unknown", agentchat.Session{Agent: "alfred", ID: "s4", Turns: 2}, "completion unproven"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sv := owning.nativeChatSupervision(c.sess, "")
			if sv.State != supervisionUnknown || !strings.Contains(sv.Evidence, c.want) {
				t.Fatalf("%+v", sv)
			}
		})
	}
}

func TestSupervisionConversationStateRule(t *testing.T) {
	run := func(state string) supervisionRun { return supervisionRun{State: state, Evidence: state} }
	cases := []struct {
		name string
		runs []supervisionRun
		want string
	}{
		{"no runs→unknown", nil, supervisionUnknown},
		{"only cancelled→unknown", []supervisionRun{run(supervisionCancelled)}, supervisionUnknown},
		{"latest finished speaks", []supervisionRun{run(supervisionFailed), run(supervisionReady)}, supervisionReady},
		{"older disconnected does not hide a later landed run", []supervisionRun{run(supervisionDisconnected), run(supervisionReady)}, supervisionReady},
		{"in-flight wins over newer finished", []supervisionRun{run(supervisionRunning), run(supervisionReady)}, supervisionRunning},
		{"submitted wins over newer finished", []supervisionRun{run(supervisionSubmitted), run(supervisionFailed)}, supervisionSubmitted},
		{"trailing cancelled is skipped", []supervisionRun{run(supervisionInterrupted), run(supervisionCancelled)}, supervisionInterrupted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, _ := conversationSupervisionState(c.runs); got != c.want {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func TestSupervisionTerminalReceiptTransitionMatrix(t *testing.T) {
	submitted := "2026-09-25T10:00:00Z"
	after := "2026-09-25T10:01:00Z"
	before := "2026-09-25T09:59:00Z"
	live := terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle", ObservedAt: time.Now()}
	working := live
	working.AgentState = "working"
	blocked := live
	blocked.AgentState = "blocked"
	stopped := terminalObservation{Connectivity: "connected", Process: "stopped", AgentState: "unknown"}
	sent := terminalInputReceipt{ID: "r-sent", State: "sent", Updated: submitted}
	hash := hashTerminalText("do the thing")
	cases := []struct {
		name     string
		r        terminalInputReceipt
		tr       termTranscript
		ob       terminalObservation
		want     string
		evidence string
	}{
		{"unconfirmed with runtime error→failed", terminalInputReceipt{ID: "r1", State: "unconfirmed", Error: "pane gone"}, termTranscript{}, live, supervisionFailed, "pane gone"},
		{"unconfirmed, no provider record→disconnected", terminalInputReceipt{ID: "r2", State: "unconfirmed", SubmittedHash: hash, Updated: submitted}, termTranscript{}, live, supervisionDisconnected, "not replayed"},
		{"unconfirmed, provider user turn has the exact bytes→sent rules apply", terminalInputReceipt{ID: "r3", State: "unconfirmed", SubmittedHash: hash, Updated: submitted},
			termTranscript{Turns: []termTurn{{ID: "u1", Who: "user", Text: "do the thing"}, {ID: "a1", Who: "assistant", TS: after}}}, live, supervisionReady, "recorded by the provider as user turn u1"},
		{"unconfirmed, similar but different bytes→disconnected", terminalInputReceipt{ID: "r4", State: "unconfirmed", SubmittedHash: hash, Updated: submitted},
			termTranscript{Turns: []termTurn{{ID: "u1", Who: "user", Text: "do the thing!"}}}, live, supervisionDisconnected, "not replayed"},
		{"sent, provider run completed after→ready_for_review", sent, termTranscript{Run: &terminalRunEvidence{ID: "run1", State: "completed", At: after, Evidence: "task_complete"}}, live, supervisionReady, "provider run run1 completed"},
		{"sent, provider run failed after→failed", sent, termTranscript{Run: &terminalRunEvidence{ID: "run1", State: "failed", At: after, Evidence: "abort", Error: "rate limited"}}, live, supervisionFailed, "rate limited"},
		{"sent, provider run completed BEFORE submission is not its answer", sent, termTranscript{Run: &terminalRunEvidence{ID: "run0", State: "completed", At: before, Evidence: "old"}}, live, supervisionUnknown, "no provider record"},
		// ws-finish audit B/C3: a provider run still open (tool call, permission
		// prompt) with an assistant turn already written is not the answer.
		{"sent, provider run still open + assistant turn, idle→unknown (not ready)", sent, termTranscript{Run: &terminalRunEvidence{ID: "run1", State: "running", At: after, Evidence: "rec9"}, Turns: []termTurn{{ID: "a1", Who: "assistant", TS: after}}}, live, supervisionUnknown, "an open run is not completion"},
		{"sent, provider run still open + assistant turn, blocked→running", sent, termTranscript{Run: &terminalRunEvidence{ID: "run1", State: "running", At: after, Evidence: "rec9"}, Turns: []termTurn{{ID: "a1", Who: "assistant", TS: after}}}, blocked, supervisionRunning, "blocked on interactive input"},
		{"sent, provider run still open + assistant turn, process gone→disconnected", sent, termTranscript{Run: &terminalRunEvidence{ID: "run1", State: "running", At: after, Evidence: "rec9"}, Turns: []termTurn{{ID: "a1", Who: "assistant", TS: after}}}, stopped, supervisionDisconnected, "still open"},
		{"sent, provider run still open, working→running", sent, termTranscript{Run: &terminalRunEvidence{ID: "run1", State: "running", At: after, Evidence: "rec9"}}, working, supervisionRunning, "still open"},
		{"sent, runtime working→running", sent, termTranscript{}, working, supervisionRunning, "runtime observation working"},
		{"sent, assistant turn after, live→ready_for_review", sent, termTranscript{Turns: []termTurn{{ID: "a1", Who: "assistant", TS: after}}}, live, supervisionReady, "after submission"},
		{"sent, assistant turn after, process gone→ready_for_review (runtime named)", sent, termTranscript{Turns: []termTurn{{ID: "a1", Who: "assistant", TS: after}}}, stopped, supervisionReady, "runtime now stopped"},
		{"sent, assistant turn before submission is not its answer", sent, termTranscript{Turns: []termTurn{{ID: "a0", Who: "assistant", TS: before}}}, live, supervisionUnknown, "no provider record"},
		{"sent, untimestamped assistant turn proves nothing", sent, termTranscript{Turns: []termTurn{{ID: "a0", Who: "assistant"}}}, live, supervisionUnknown, "no provider record"},
		{"sent, no record, process gone→disconnected", sent, termTranscript{}, stopped, supervisionDisconnected, "no provider record answers it"},
		{"sent, no record, blocked on input→running", sent, termTranscript{}, blocked, supervisionRunning, "blocked on interactive input"},
		{"sent, no record, idle→unknown (idle is never completion)", sent, termTranscript{}, live, supervisionUnknown, "no provider record"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := terminalReceiptState(c.r, c.tr, c.ob)
			if got != c.want || !strings.Contains(why, c.evidence) {
				t.Fatalf("got %s: %s", got, why)
			}
		})
	}
}

func TestSupervisionTerminalConversationMatrix(t *testing.T) {
	s, se, _, _ := herdrSupervisionFixture(t, "codex")
	live := terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle"}
	t.Run("draft session→unknown", func(t *testing.T) {
		if sv := s.terminalChatSupervision(se, termTranscript{}, live); sv.State != supervisionUnknown || !strings.Contains(sv.Evidence, "draft") {
			t.Fatalf("%+v", sv)
		}
	})
	t.Run("legacy tmux is observation-only→unknown", func(t *testing.T) {
		legacy := termSession{ID: "legacy000000001", Kind: "claude"}
		if sv := s.terminalChatSupervision(legacy, termTranscript{}, terminalObservation{Process: "running", AgentState: "idle"}); sv.State != supervisionUnknown || !strings.Contains(sv.Evidence, "not completion evidence") {
			t.Fatalf("%+v", sv)
		}
	})
	started := se
	started.LaunchPhase, started.Started = "", true
	started.Runtime = terminalIdentity{Workspace: "w1", Pane: "p1"}
	s.terminal.upsert(started)
	t.Run("no receipts, working→running (not submitted through Manifest)", func(t *testing.T) {
		sv := s.terminalChatSupervision(started, termTranscript{}, terminalObservation{Connectivity: "connected", Process: "running", AgentState: "working"})
		if sv.State != supervisionRunning || !strings.Contains(sv.Evidence, "no receipt") {
			t.Fatalf("%+v", sv)
		}
	})
	t.Run("no receipts, disconnected→unknown", func(t *testing.T) {
		if sv := s.terminalChatSupervision(started, termTranscript{}, terminalObservation{Connectivity: "unreachable", Process: "unknown"}); sv.State != supervisionUnknown || !strings.Contains(sv.Evidence, "unreachable") {
			t.Fatalf("%+v", sv)
		}
	})
	t.Run("no receipts, idle→unknown", func(t *testing.T) {
		if sv := s.terminalChatSupervision(started, termTranscript{}, live); sv.State != supervisionUnknown || !strings.Contains(sv.Evidence, "idle process proves nothing") {
			t.Fatalf("%+v", sv)
		}
	})
	key := s.terminalConversation(started).Key
	writeOutbox := func(items map[string]map[string]any) {
		t.Helper()
		value := chatQueueValue{Items: map[string]json.RawMessage{}}
		for id, item := range items {
			item["stateKey"], item["url"], item["agent"], item["at"] = key, "/api/terminal/session/"+started.ID+"/input", "codex", "2026-09-25T10:00:00Z"
			item["payload"] = map[string]any{"text": "later " + id, "requestId": id}
			raw, _ := json.Marshal(item)
			value.Items[id] = raw
		}
		raw, _ := json.Marshal(value)
		snap, _ := s.chatState.Read(key, "deliveries")
		if _, err := s.chatState.Write(key, "deliveries", snap.Revision, raw); err != nil {
			t.Fatal(err)
		}
	}
	writeOutbox(map[string]map[string]any{
		"outbox-refused": {"staged": true, "stagedError": "runtime refused"},
		"outbox-claimed": {},
		"outbox-inflite": {},
		"outbox-receipt": {},
	})
	if err := s.terminal.writeInputReceipt(started.ID, terminalInputReceipt{ID: "outbox-receipt", State: "sent", Updated: "2026-09-25T10:00:01Z", Fingerprint: strings.Repeat("b", 64), SubmittedHash: hashTerminalText("later outbox-receipt")}); err != nil {
		t.Fatal(err)
	}
	if err := s.terminal.writeInputReceipt(started.ID, terminalInputReceipt{ID: "receipt-flight", State: "unconfirmed", Updated: "2026-09-25T10:00:02Z", Fingerprint: strings.Repeat("c", 64), SubmittedHash: hashTerminalText("in flight")}); err != nil {
		t.Fatal(err)
	}
	doneA := s.terminal.markInflight(started.ID, "outbox-inflite")
	doneB := s.terminal.markInflight(started.ID, "receipt-flight")
	sv := s.terminalChatSupervision(started, termTranscript{}, live)
	for _, c := range []struct{ name, id, want, evidence string }{
		{"outbox refused by runtime→failed", "outbox-refused", supervisionFailed, "runtime refused"},
		{"outbox claimed, no receipt, not in flight→disconnected", "outbox-claimed", supervisionDisconnected, "not replayed"},
		{"outbox claimed, dispatch in this process→submitted", "outbox-inflite", supervisionSubmitted, "dispatch in progress"},
		{"unconfirmed receipt, send in this process→running", "receipt-flight", supervisionRunning, "send in progress"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := runState(sv, c.id); got.State != c.want || !strings.Contains(got.Evidence, c.evidence) {
				t.Fatalf("%+v", got)
			}
		})
	}
	t.Run("outbox entry with a receipt is not a second run", func(t *testing.T) {
		n := 0
		for _, r := range sv.Runs {
			if r.RequestID == "outbox-receipt" {
				n++
				if !strings.HasPrefix(r.Evidence, "input receipt") {
					t.Fatalf("the receipt, not the outbox entry, is the record: %+v", r)
				}
			}
		}
		if n != 1 {
			t.Fatalf("want one run for the request, got %d", n)
		}
	})
	doneA()
	doneB()
	t.Run("restart empties the dispatch table: in-flight becomes disconnected", func(t *testing.T) {
		sv := s.terminalChatSupervision(started, termTranscript{}, live)
		if got := runState(sv, "receipt-flight"); got.State != supervisionDisconnected {
			t.Fatalf("%+v", got)
		}
		if got := runState(sv, "outbox-inflite"); got.State != supervisionDisconnected {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("pending question, process stopped→stale", func(t *testing.T) {
		tr := termTranscript{Questions: []terminalQuestion{{ID: "q1", State: "pending"}}}
		for _, q := range s.terminalQuestionsObserved(started, tr, terminalObservation{Process: "stopped"}) {
			if q.State == "pending" {
				t.Fatalf("a question whose process is gone must not stay answerable: %+v", q)
			}
		}
	})
}

// Task-thread turn states, one record set per transition (the re-dispatch
// chain itself is TestHermesTurnSweepRedispatchIsVisibleAndPinned).
func TestSupervisionTaskThreadTransitionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		build    func(srv *Server, id string)
		inFlight bool
		want     string
		evidence string
	}{
		{"open, live invocation→running", func(srv *Server, id string) { openTurn(srv, id) }, true, supervisionRunning, "invocation live"},
		{"open, no close, no invocation→disconnected (owed)", func(srv *Server, id string) { openTurn(srv, id) }, false, supervisionDisconnected, "owed"},
		{"closed with the agent's reply→ready_for_review", func(srv *Server, id string) {
			openTurn(srv, id)
			agentReply(srv, id, "ANSWER: R-1")
			srv.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": "agent:alfred"})
		}, false, supervisionReady, "on the thread"},
		{"close repaired by the sweep→ready_for_review", func(srv *Server, id string) {
			openTurn(srv, id)
			agentReply(srv, id, "ANSWER: R-1")
			srv.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": "agent:alfred", "repaired": true})
		}, false, supervisionReady, "repaired"},
		{"closed with a ⚠ failure note→failed", func(srv *Server, id string) {
			openTurn(srv, id)
			agentReply(srv, id, "⚠ Alfred couldn't finish that — timed out")
			srv.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": "agent:alfred"})
		}, false, supervisionFailed, "failure note"},
		{"closed with no visible reply→unknown", func(srv *Server, id string) {
			openTurn(srv, id)
			srv.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": "agent:alfred"})
		}, false, supervisionUnknown, "without a visible agent reply"},
		{"re-dispatch refused before a turn opened→failed", func(srv *Server, id string) {
			openTurn(srv, id)
			srv.hermesTurnMark(id, actTurnRedispatch, map[string]any{"attempt": 2, "of": "c-x", "error": "harness disabled"})
		}, false, supervisionFailed, "harness disabled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := loopFixture(t)
			id := "inbox/research-zoning"
			if _, ok := srv.pinTaskID(id); !ok {
				t.Fatal("pin")
			}
			c.build(srv, id)
			if c.inFlight {
				srv.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: "/bin/false"}), "")
				srv.hermes.running[id] = hermesTurn{Phase: "comment", Agent: "agent:alfred", Since: time.Now()}
			}
			sv := srv.taskThreadSupervision(id)
			if sv.State != c.want || !strings.Contains(sv.Evidence, c.evidence) {
				t.Fatalf("%+v", sv)
			}
		})
	}
}

func openTurn(srv *Server, id string) {
	srv.hermesTurnMark(id, actTurnOpen, map[string]any{"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "zoning?"})
	time.Sleep(2 * time.Millisecond)
}

func agentReply(srv *Server, id, text string) {
	_, _ = srv.addThreadEntry(agentTokenIdentity("agent:alfred"), id, threads.ActComment, text, nil, nil, map[string]any{"hermes": true})
	time.Sleep(2 * time.Millisecond)
}

// The CHAT rail's task row carries the turn projection when a turn is owed
// or failed, and nothing otherwise.
func TestTaskThreadRowCarriesInterruptedTurn(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActComment, "what is it zoned?", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	row := func() taskThreadRow {
		for _, r := range srv.taskThreads() {
			if r.ID == id {
				return r
			}
		}
		t.Fatal("row missing")
		return taskThreadRow{}
	}
	if r := row(); r.Supervision != nil {
		t.Fatalf("no turn, no projection: %+v", r.Supervision)
	}
	openTurn(srv, id)
	if r := row(); r.Supervision == nil || r.Supervision.State != supervisionDisconnected {
		t.Fatalf("owed turn must reach the rail: %+v", r.Supervision)
	}
	agentReply(srv, id, "ANSWER: R-1")
	srv.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": "agent:alfred"})
	if r := row(); r.Supervision != nil {
		t.Fatalf("an answered turn adds nothing to the row: %+v", r.Supervision)
	}
}

// Audit 2026-09-25: two identical sends are two runs. Receipts were read
// through the hash-keyed attribution map, so the newer send vanished behind
// the older one's result.
func TestTerminalIdenticalSendsAreSeparateRuns(t *testing.T) {
	s, se, _, _ := herdrSupervisionFixture(t, "codex")
	se.LaunchPhase, se.Started = "", true
	se.Runtime = terminalIdentity{Workspace: "w1", Pane: "p1"}
	s.terminal.upsert(se)
	hash := hashTerminalText("run the tests")
	for i, id := range []string{"same-text-first", "same-text-second"} {
		if err := s.terminal.writeInputReceipt(se.ID, terminalInputReceipt{ID: id, Fingerprint: strings.Repeat("d", 64), State: "sent", SubmittedHash: hash, Updated: fmt.Sprintf("2026-09-25T10:0%d:00Z", i)}); err != nil {
			t.Fatal(err)
		}
	}
	tr := termTranscript{Turns: []termTurn{{ID: "a1", Who: "assistant", TS: "2026-09-25T10:00:30Z"}}}
	sv := s.terminalChatSupervision(se, tr, terminalObservation{Connectivity: "connected", Process: "running", AgentState: "working"})
	if len(sv.Runs) != 2 {
		t.Fatalf("two sends, two runs: %+v", sv.Runs)
	}
	if got := runState(sv, "same-text-second"); got.State != supervisionRunning {
		t.Fatalf("the newer identical send must be visible as running: %+v", got)
	}
	if got := runState(sv, "same-text-first"); got.State != supervisionRunning && got.State != supervisionReady {
		t.Fatalf("%+v", got)
	}
}

// Audit 2026-09-25: a supervised send rewrites its receipt after the reply
// has landed, so comparing the reply with Updated read "no provider record"
// (unknown) for a finished run. The write-once submission time decides.
func TestSupervisedReceiptFinalisedAfterReplyIsReady(t *testing.T) {
	c := &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	r := terminalInputReceipt{ID: "supervised-1", Fingerprint: strings.Repeat("e", 64), State: "unconfirmed", SubmittedHash: hashTerminalText("do the thing")}
	if err := c.writeInputReceipt("t1", r); err != nil {
		t.Fatal(err)
	}
	first, _ := c.readInputReceipt("t1", r.ID)
	time.Sleep(5 * time.Millisecond)
	reply := time.Now().UTC().Format(time.RFC3339Nano)
	time.Sleep(5 * time.Millisecond)
	r.State = "sent" // finalised after the supervised prompt settled
	if err := c.writeInputReceipt("t1", r); err != nil {
		t.Fatal(err)
	}
	final, _ := c.readInputReceipt("t1", r.ID)
	if final.Submitted == "" || final.Submitted != first.Submitted || !laterConversationTimestamp(final.Updated, reply) {
		t.Fatalf("submission time must be write-once: first %+v final %+v", first, final)
	}
	tr := termTranscript{Turns: []termTurn{{ID: "a1", Who: "assistant", TS: reply}}}
	if got, why := terminalReceiptState(final, tr, terminalObservation{Connectivity: "connected", Process: "running", AgentState: "idle"}); got != supervisionReady {
		t.Fatalf("a reply after submission but before finalisation is the answer: %s %s", got, why)
	}
}

// Audit 2026-09-25: a herdr shell's matrix says it cannot steer or queue, but
// the input handler refused only non-herdr backends, so steer:true reached the
// shell as plain text. The handler now consults the matrix.
func TestHerdrShellRefusesSteerAndQueueInWords(t *testing.T) {
	s, _, _, prompts := herdrSupervisionFixture(t, "codex")
	se := termSession{ID: "5e11000000000001", Backend: "herdr", Kind: "shell", Cwd: s.terminal.defaultWd, LaunchPhase: "active", Started: true, Runtime: herdrFixtureID(t, s.terminal.herdr)}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	if caps := terminalChatCapabilities(se); caps.Steer != "unsupported" || caps.Queue != "none" || caps.Resume != "unsupported" {
		t.Fatalf("%+v", caps)
	}
	for _, body := range []string{`{"text":"x","steer":true,"requestId":"shell-steer-01"}`, `{"text":"x","afterRun":true,"requestId":"shell-queue-01"}`} {
		if w := receiptInput(s, se.ID, body); w.Code != 409 || !strings.Contains(w.Body.String(), adapterHerdrOther) || !strings.Contains(w.Body.String(), "nothing sent") {
			t.Fatal("shell steer/queue accepted", w.Code, w.Body.String())
		}
	}
	if prompts.Load() != 0 {
		t.Fatal("refused input reached the runtime")
	}
}
