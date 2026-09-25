package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"manifest/agentchat"
	"manifest/threads"
)

// Supervision is the ONE state vocabulary every chat adapter projects into.
// It is a read-only projection over the records that already exist — the
// agentchat delivery journal, terminal input receipts, the chat outbox and the
// live runtime observation — never a second store. Every projected state names
// the artifact that proves it (Evidence); a state that cannot be proven is
// reported as unknown, not guessed.
//
//	submitted        accepted durably, not started
//	running          a provider call is in flight and this process owns it
//	disconnected     the call outlived its client or process; outcome uncertain, never replayed
//	ready_for_review the provider returned and the reply landed; NOT owner acceptance
//	failed           the adapter reported failure (or nothing was ever invoked)
//	interrupted      stopped at owner request; already-started effects may be uncertain
//	cancelled        withdrawn before dispatch
//	unknown          no artifact proves any of the above
const (
	supervisionSubmitted    = "submitted"
	supervisionRunning      = "running"
	supervisionDisconnected = "disconnected"
	supervisionReady        = "ready_for_review"
	supervisionFailed       = "failed"
	supervisionInterrupted  = "interrupted"
	supervisionCancelled    = "cancelled"
	supervisionUnknown      = "unknown"
)

// supervisionRun is one durable run identity across adapters:
// RunID = <adapter>:<conversation key>#<request ID>. The request ID is the same
// idempotency key the delivery/receipt/outbox already carry, so a run is found
// by one identity in the journal, the receipt directory, the outbox and the
// ledger (Meta.runId).
type supervisionRun struct {
	RunID         string `json:"runId"`
	Adapter       string `json:"adapter"`
	Conversation  string `json:"conversation"`
	RequestID     string `json:"requestId"`
	State         string `json:"state"`
	Evidence      string `json:"evidence"`
	StopRequested bool   `json:"stopRequested,omitempty"`
	Updated       string `json:"updated,omitempty"`
	// Attempt/ReplayOf are set only on a sweep re-dispatch of a task-thread
	// turn: attempt n (the original is 1) of the run whose request is ReplayOf.
	Attempt  int    `json:"attempt,omitempty"`
	ReplayOf string `json:"replayOf,omitempty"`
}

type chatSupervision struct {
	Adapter      string           `json:"adapter"`
	Conversation string           `json:"conversation"`
	State        string           `json:"state"`
	Evidence     string           `json:"evidence"`
	Capabilities chatCapabilities `json:"capabilities"`
	Runs         []supervisionRun `json:"runs"`
}

func supervisionRunID(adapter, conversation, requestID string) string {
	return adapter + ":" + conversation + "#" + requestID
}

// supervisionStale marks a native question whose run is no longer live: the
// CLI process that asked it is gone, so an answer could only reach a resumed
// or new process as an ordinary message. Such a question is not answerable.
const questionStale = "stale"

// ---- native (hermes-oneshot) ----

// nativeChatSupervision projects the delivery journal. body may be "" (rail
// rows); then a completed delivery is proven by its recorded reply turn being
// within the session's turn count, and with the body by the actual heading.
func (s *Server) nativeChatSupervision(sess agentchat.Session, body string) chatSupervision {
	conversation := sessionConversation(sess).Key
	out := chatSupervision{Adapter: adapterHermesOneshot, Conversation: conversation, Capabilities: nativeChatCapabilities(), Runs: []supervisionRun{}}
	var turns []agentchat.Turn
	if body != "" {
		turns = agentchat.ParseTurns(body)
	}
	liveInvocation := ""
	if s.agentChat != nil {
		s.agentChat.runMu.Lock()
		if inv, ok := s.agentChat.running[sess.Agent+"/"+sess.ID]; ok {
			liveInvocation = inv.requestID
		}
		s.agentChat.runMu.Unlock()
	}
	for _, d := range sess.Deliveries {
		run := supervisionRun{RunID: supervisionRunID(adapterHermesOneshot, conversation, d.ID), Adapter: adapterHermesOneshot, Conversation: conversation, RequestID: d.ID, StopRequested: d.StopRequested, Updated: d.Updated}
		switch d.State {
		case agentchat.DeliveryQueued:
			run.State, run.Evidence = supervisionSubmitted, "queued delivery receipt "+d.ID+" accepted "+d.Accepted+"; no user turn appended yet"
		case agentchat.DeliveryRunning:
			switch {
			case liveInvocation == d.ID:
				run.State, run.Evidence = supervisionRunning, fmt.Sprintf("running delivery receipt %s (user turn %d) with a live invocation in this process", d.ID, d.UserTurn)
			case !s.hermesEnabled():
				run.State, run.Evidence = supervisionUnknown, "running delivery receipt "+d.ID+"; this process cannot own turns, so another writer may hold the call"
			default:
				run.State, run.Evidence = supervisionDisconnected, "running delivery receipt "+d.ID+" has no live invocation in this process; outcome uncertain and not replayed"
			}
		case agentchat.DeliveryCompleted:
			who := sess.Agent
			if d.Context != nil && d.Context.Recipient != nil && d.Context.Recipient.Agent != "" {
				who = d.Context.Recipient.Agent
			}
			if proof, ok := nativeReplyEvidence(sess, d, who, turns); ok {
				run.State, run.Evidence = supervisionReady, proof
			} else {
				run.State, run.Evidence = supervisionUnknown, fmt.Sprintf("completed receipt %s names reply turn %d but no such %s turn is recorded", d.ID, d.ReplyTurn, who)
			}
		case agentchat.DeliveryFailed:
			run.State, run.Evidence = supervisionFailed, "failed delivery receipt "+d.ID+": "+d.Error
		case agentchat.DeliveryInterrupted:
			if d.Disconnected {
				run.State, run.Evidence = supervisionDisconnected, "delivery receipt "+d.ID+" was running when the server restarted; "+d.Error
			} else {
				run.State, run.Evidence = supervisionInterrupted, "delivery receipt "+d.ID+" interrupted: "+d.Error
			}
		case agentchat.DeliveryCancelled:
			run.State, run.Evidence = supervisionCancelled, "delivery receipt "+d.ID+" cancelled before dispatch"
		default:
			run.State, run.Evidence = supervisionUnknown, "delivery receipt "+d.ID+" has unrecognised state "+d.State
		}
		out.Runs = append(out.Runs, run)
	}
	out.State, out.Evidence = conversationSupervisionState(out.Runs)
	if len(out.Runs) == 0 {
		if sess.Status == agentchat.StatusThinking {
			out.State, out.Evidence = supervisionUnknown, "session flag thinking without a delivery receipt (legacy transport); nothing proves a run"
		} else if sess.Turns == 0 {
			out.State, out.Evidence = supervisionUnknown, "no instruction accepted yet"
		} else {
			out.State, out.Evidence = supervisionUnknown, "transcript turns without delivery receipts (legacy transport); completion unproven"
		}
	}
	return out
}

func nativeReplyEvidence(sess agentchat.Session, d agentchat.Delivery, who string, turns []agentchat.Turn) (string, bool) {
	if d.ReplyTurn <= 0 || d.ReplyTurn <= d.UserTurn {
		return "", false
	}
	detail := ""
	if d.Result != nil && (d.Result.SessionID != "" || d.Result.ReportedModel != "") {
		detail = " · runner session " + d.Result.SessionID + " · reported model " + d.Result.ReportedModel
	}
	if turns == nil {
		if d.ReplyTurn > sess.Turns {
			return "", false
		}
		return fmt.Sprintf("completed receipt %s: reply turn %d of %d recorded%s", d.ID, d.ReplyTurn, sess.Turns, detail), true
	}
	for _, t := range turns {
		if t.N == d.ReplyTurn && t.Who == who {
			return fmt.Sprintf("completed receipt %s: reply heading “Turn %d — %s · %s” present%s", d.ID, t.N, t.Who, t.At, detail), true
		}
	}
	return "", false
}

// conversationSupervisionState: the latest non-cancelled run speaks for the
// conversation, except that an in-flight or waiting run always wins over an
// older finished one. An older disconnected run stays visible in Runs; it
// does not hide a later run that did land.
func conversationSupervisionState(runs []supervisionRun) (string, string) {
	latest := -1
	for i, r := range runs {
		switch r.State {
		case supervisionRunning, supervisionSubmitted:
			return r.State, r.Evidence
		case supervisionCancelled:
			continue
		}
		latest = i
	}
	if latest < 0 {
		return supervisionUnknown, "no run recorded"
	}
	return runs[latest].State, runs[latest].Evidence
}

// ---- terminal (herdr / tmux) ----

// terminalChatSupervision projects input receipts + the chat outbox against
// the live observation and the provider transcript. An idle process is never
// proof of completion: ready_for_review needs a provider record (run evidence
// or an assistant turn) later than the submission it answers.
func (s *Server) terminalChatSupervision(se termSession, tr termTranscript, ob terminalObservation) chatSupervision {
	caps := terminalChatCapabilities(se)
	conversation := s.terminalConversation(se).Key
	out := chatSupervision{Adapter: caps.Adapter, Conversation: conversation, Capabilities: caps, Runs: []supervisionRun{}}
	if se.isDraft() {
		out.State, out.Evidence = supervisionUnknown, "draft session; no process launched and nothing submitted"
		return out
	}
	if caps.Supervision == "observation-only" {
		out.State, out.Evidence = supervisionUnknown, "legacy runtime keeps no input receipts; observation "+ob.Process+"/"+ob.AgentState+" is not completion evidence"
		return out
	}
	receipts := s.terminal.inputReceiptList(se.ID)
	receiptByID := map[string]bool{}
	for _, r := range receipts {
		receiptByID[r.ID] = true
	}
	// Waiting outbox entries are submitted runs: durable, not started. Once a
	// receipt exists for the same request the receipt is the record; the entry
	// only awaits the sweep's cleanup and must not project a second run.
	for _, item := range s.terminalQueuedFollowups(conversation, se.ID) {
		if receiptByID[item.Payload.RequestID] {
			continue
		}
		run := supervisionRun{RunID: supervisionRunID(caps.Adapter, conversation, item.Payload.RequestID), Adapter: caps.Adapter, Conversation: conversation, RequestID: item.Payload.RequestID, Updated: item.At}
		switch {
		case item.Staged && item.StagedError != "" && !item.WaitingForAgent:
			run.State, run.Evidence = supervisionFailed, "outbox entry refused by the runtime: "+item.StagedError
		case item.Staged:
			run.State, run.Evidence = supervisionSubmitted, "outbox entry "+item.Payload.RequestID+" waiting for an idle prompt; nothing sent"
		default:
			if since, ok := s.terminal.inflightSince(se.ID, item.Payload.RequestID); ok {
				run.State, run.Evidence = supervisionSubmitted, "outbox entry "+item.Payload.RequestID+" claimed; dispatch in progress in this process since "+since.Format(time.RFC3339)+"; the receipt follows the send"
			} else {
				run.State, run.Evidence = supervisionDisconnected, "outbox entry "+item.Payload.RequestID+" claimed for dispatch without a confirming receipt; not replayed"
			}
		}
		out.Runs = append(out.Runs, run)
	}
	ordered := receipts
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Updated == ordered[j].Updated {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Updated < ordered[j].Updated
	})
	for _, r := range ordered {
		run := supervisionRun{RunID: supervisionRunID(caps.Adapter, conversation, r.ID), Adapter: caps.Adapter, Conversation: conversation, RequestID: r.ID, Updated: r.Updated}
		if since, ok := s.terminal.inflightSince(se.ID, r.ID); ok && r.State == "unconfirmed" && r.Error == "" {
			// The receipt is written before the daemon call; until that call
			// returns in this process the send is running here, not lost.
			run.State, run.Evidence = supervisionRunning, "input receipt "+r.ID+" unconfirmed; send in progress in this process since "+since.Format(time.RFC3339)
		} else {
			run.State, run.Evidence = terminalReceiptState(r, tr, ob)
		}
		out.Runs = append(out.Runs, run)
	}
	out.State, out.Evidence = conversationSupervisionState(out.Runs)
	if len(out.Runs) == 0 {
		switch {
		case ob.Process == "running" && ob.AgentState == "working":
			out.State, out.Evidence = supervisionRunning, "runtime observation working; the run was not submitted through Manifest (no receipt)"
		case ob.Connectivity != "connected" || ob.Process != "running":
			out.State, out.Evidence = supervisionUnknown, "no input receipts; runtime observation "+ob.Process+"/"+ob.Connectivity
		default:
			out.State, out.Evidence = supervisionUnknown, "no input receipts; an idle process proves nothing about a run"
		}
	}
	return out
}

func terminalReceiptState(r terminalInputReceipt, tr termTranscript, ob terminalObservation) (string, string) {
	if r.State == "unconfirmed" {
		if r.Error != "" {
			return supervisionFailed, "input receipt " + r.ID + " unconfirmed with runtime error: " + r.Error
		}
		// A process that died between the receipt and the daemon's reply left
		// the outcome unknown. The provider's own transcript can settle it: the
		// exact submitted bytes recorded as a user turn prove the prompt landed,
		// after which the ordinary sent-receipt rules apply. Without that record
		// the run stays disconnected; it is never replayed.
		turn, ok := receiptConfirmedByTranscript(r, tr)
		if !ok {
			return supervisionDisconnected, "input receipt " + r.ID + " unconfirmed: the send crossed the runtime boundary without a reply; not replayed"
		}
		confirmed := "input receipt " + r.ID + " unconfirmed by this process but recorded by the provider as user turn " + turn.ID + " (exact submitted bytes); "
		state, evidence := terminalReceiptState(terminalInputReceipt{ID: r.ID, Fingerprint: r.Fingerprint, State: "sent", Updated: r.Updated, Submitted: r.Submitted, Runtime: r.Runtime, SubmittedHash: r.SubmittedHash}, tr, ob)
		return state, confirmed + evidence
	}
	// Provider lifecycle records after the submission are the proof of a result.
	submitted := r.submittedAt()
	if tr.Run != nil && tr.Run.Evidence != "" && !laterConversationTimestamp(submitted, tr.Run.At) {
		switch tr.Run.State {
		case "completed":
			return supervisionReady, "input receipt " + r.ID + " sent; provider run " + tr.Run.ID + " " + tr.Run.State + " (" + tr.Run.Evidence + ")"
		case "failed":
			return supervisionFailed, "input receipt " + r.ID + " sent; provider run " + tr.Run.ID + " failed: " + tr.Run.Error
		case "running":
			// The provider's own record says the turn is still open (a tool
			// call, a permission prompt): an assistant turn written mid-run is
			// not its answer, so the observation decides and never "ready".
			open := "input receipt " + r.ID + " sent; provider run " + tr.Run.ID + " still open (" + tr.Run.Evidence + ")"
			switch {
			case ob.Connectivity != "connected" || ob.Process != "running":
				return supervisionDisconnected, open + " and the runtime is " + ob.Process + "/" + ob.Connectivity + "; not replayed"
			case ob.AgentState == "working":
				return supervisionRunning, open + "; runtime observation working"
			case ob.AgentState == "blocked":
				return supervisionRunning, open + "; runtime blocked on interactive input"
			default:
				return supervisionUnknown, open + "; runtime " + ob.AgentState + " — an open run is not completion"
			}
		}
	}
	if ob.Connectivity == "connected" && ob.Process == "running" && ob.AgentState == "working" {
		return supervisionRunning, "input receipt " + r.ID + " sent; runtime observation working at " + ob.ObservedAt.UTC().Format(time.RFC3339)
	}
	for i := len(tr.Turns) - 1; i >= 0; i-- {
		t := tr.Turns[i]
		if t.Who == "assistant" && t.TS != "" && laterConversationTimestamp(t.TS, submitted) {
			if ob.Connectivity == "connected" && ob.Process == "running" {
				return supervisionReady, "input receipt " + r.ID + " sent; assistant turn " + t.ID + " recorded at " + t.TS + " after submission"
			}
			return supervisionReady, "input receipt " + r.ID + " sent; assistant turn " + t.ID + " recorded at " + t.TS + "; runtime now " + ob.Process + "/" + ob.Connectivity
		}
	}
	if ob.Connectivity != "connected" || ob.Process != "running" {
		return supervisionDisconnected, "input receipt " + r.ID + " sent; no provider record answers it and the runtime is " + ob.Process + "/" + ob.Connectivity
	}
	if ob.AgentState == "blocked" {
		return supervisionRunning, "input receipt " + r.ID + " sent; runtime blocked on interactive input"
	}
	return supervisionUnknown, "input receipt " + r.ID + " sent; runtime " + ob.AgentState + " but no provider record answers the submission"
}

// terminalQueuedFollowups reads this terminal's waiting outbox entries from the
// existing chat-state store (the same records chatQueuedFollowupSweep drains).
func (s *Server) terminalQueuedFollowups(conversation, terminalID string) []chatQueuedFollowup {
	if s.chatState == nil {
		return nil
	}
	snapshot, err := s.chatState.Read(conversation, "deliveries")
	if err != nil {
		return nil
	}
	var value chatQueueValue
	if json.Unmarshal(snapshot.Value, &value) != nil {
		return nil
	}
	var out []chatQueuedFollowup
	for id, raw := range value.Items {
		var item chatQueuedFollowup
		if json.Unmarshal(raw, &item) != nil || item.Payload.RequestID != id || item.URL != "/api/terminal/session/"+terminalID+"/input" {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At == out[j].At {
			return out[i].Payload.RequestID < out[j].Payload.RequestID
		}
		return out[i].At < out[j].At
	})
	return out
}

// terminalQuestionsObserved is terminalQuestions plus the liveness truth: a
// pending question whose process is confirmed gone is stale, not answerable.
func (s *Server) terminalQuestionsObserved(se termSession, tr termTranscript, ob terminalObservation) []terminalQuestion {
	out := s.terminalQuestions(se, tr)
	if ob.Process == "stopped" || ob.Process == "not-started" {
		for i := range out {
			if out[i].State == "pending" {
				out[i].State = questionStale
			}
		}
	}
	return out
}

// questionRunLive refuses an answer whose run is gone. Called under the input
// mutex before launch resolution, so a stale answer can never trigger a resume
// and arrive in a fresh process as an ordinary message.
func (s *Server) questionRunLive(ctx context.Context, se termSession) error {
	ob, err := s.observeTerm(ctx, se)
	if err != nil {
		return fmt.Errorf("question run state is unknown (%s); nothing sent", strings.TrimSpace(err.Error()))
	}
	if ob.Process != "running" || ob.Connectivity != "connected" {
		return fmt.Errorf("question is stale: its run is no longer live (%s/%s); nothing sent", ob.Process, ob.Connectivity)
	}
	return nil
}

// ---- task thread (hermes Ask/Do turns) ----

// taskThreadSupervision projects a task thread's private turn markers: every
// turn-open is a run (request ID = the marker's comment ID); a turn-redispatch
// marker before it makes that run a replay, attempt n of the owed chain. The
// markers are the only record — nothing is written here.
func (s *Server) taskThreadSupervision(taskID string) chatSupervision {
	out := chatSupervision{Adapter: adapterHermesTaskThread, Conversation: "task:" + taskID, Capabilities: taskThreadCapabilities(), Runs: []supervisionRun{}}
	if s.threads == nil || s.threads.private == nil {
		out.State, out.Evidence = supervisionUnknown, "no private thread store; no turn markers"
		return out
	}
	entries := s.threads.private.Thread(taskID)
	visible := s.listThread(taskID)
	inFlight := false
	if s.hermes != nil {
		s.hermes.mu.Lock()
		_, inFlight = s.hermes.running[taskID]
		s.hermes.mu.Unlock()
	}
	var pending map[string]any // the redispatch marker awaiting its turn-open
	for i, c := range entries {
		switch c.Action {
		case actTurnRedispatch:
			if _, failed := c.Meta["error"]; failed {
				attempt := metaInt(c.Meta["attempt"])
				out.Runs = append(out.Runs, supervisionRun{
					RunID: supervisionRunID(adapterHermesTaskThread, out.Conversation, c.ID), Adapter: adapterHermesTaskThread,
					Conversation: out.Conversation, RequestID: c.ID, Attempt: attempt, ReplayOf: metaString(c.Meta["of"]),
					State: supervisionFailed, Updated: c.At.UTC().Format(time.RFC3339),
					Evidence: fmt.Sprintf("re-dispatch attempt %d refused before a turn opened: %s", attempt, metaString(c.Meta["error"])),
				})
				pending = nil
				continue
			}
			pending = c.Meta
		case actTurnOpen:
			run := supervisionRun{RunID: supervisionRunID(adapterHermesTaskThread, out.Conversation, c.ID), Adapter: adapterHermesTaskThread,
				Conversation: out.Conversation, RequestID: c.ID, Updated: c.At.UTC().Format(time.RFC3339)}
			label := "turn-open marker " + c.ID
			if pending != nil {
				run.Attempt, run.ReplayOf = metaInt(pending["attempt"]), metaString(pending["of"])
				label = fmt.Sprintf("re-dispatch attempt %d of %d (replays %s after an interruption) — turn-open marker %s", run.Attempt, metaInt(pending["cap"]), run.ReplayOf, c.ID)
				pending = nil
			}
			run.State, run.Evidence = taskTurnState(entries[i+1:], visible, c, label, inFlight, s.hermesEnabled())
			out.Runs = append(out.Runs, run)
		}
	}
	out.State, out.Evidence = conversationSupervisionState(out.Runs)
	return out
}

// taskTurnState settles one turn-open from what follows it: its close, a
// later open (the process died and the chain moved on), the agent's reply, or
// the in-memory invocation. Idle is never completion.
func taskTurnState(after, visible []threads.Comment, open threads.Comment, label string, inFlight, canRun bool) (string, string) {
	agent := metaString(open.Meta["agent"])
	who := agentTokenIdentity(agent).ID
	var reply *threads.Comment
	for i := range visible {
		if visible[i].Author == who && visible[i].At.After(open.At) {
			reply = &visible[i]
			break
		}
	}
	for _, c := range after {
		switch c.Action {
		case actTurnOpen, actTurnRedispatch:
			return supervisionDisconnected, label + ": the process ended before this turn closed; not answered by this attempt"
		case actTurnClosed:
			switch {
			case c.Meta["abandoned"] == true:
				return supervisionFailed, label + ": abandoned by the sweep after the retry cap; the thread says so"
			case reply == nil:
				return supervisionUnknown, label + ": closed without a visible agent reply"
			case strings.HasPrefix(reply.Text, "⚠"):
				return supervisionFailed, label + ": closed with failure note " + reply.ID
			case c.Meta["repaired"] == true:
				return supervisionReady, label + ": reply " + reply.ID + " on the thread; close marker repaired by the sweep"
			default:
				return supervisionReady, label + ": reply " + reply.ID + " on the thread"
			}
		}
	}
	if inFlight {
		return supervisionRunning, label + ": invocation live in this process"
	}
	// The reply lands before the close marker (runHermesTurn's defer); the
	// sweep closes such a turn in place rather than re-sending it.
	if reply != nil {
		if strings.HasPrefix(reply.Text, "⚠") {
			return supervisionFailed, label + ": failure note " + reply.ID + " on the thread; close marker pending"
		}
		return supervisionReady, label + ": reply " + reply.ID + " on the thread; close marker pending"
	}
	if !canRun {
		return supervisionUnknown, label + ": no close, and this process cannot run turns (runner off); another writer may hold it"
	}
	return supervisionDisconnected, label + ": owed — no close and no live invocation; the sweep re-dispatches within the retry cap"
}

func metaString(v any) string {
	s, _ := v.(string)
	return s
}

func metaInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
