package server

// Native chat with kairos (chat-kairos handoff) — the server core. A chat
// message that asks kairos spools a REAL run order (kind:"") into the kairos
// harness tree; the runner (cmd/kairos-runner) executes it headless and writes
// a report + brief; chatSweep (on the AgentLoopTicker) ingests the answer as a
// kairos message. There is no token stream — the surface shows honest discrete
// states. kairos is OS-sandboxed on lab-apps (it can't write the vault), so the
// ask/delegate distinction is a PROMPT distinction composed here: ask answers
// from context and proposes nothing; delegate produces a work order and emits
// fenced manifest-proposal blocks a person approves in-portal.

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"manifest/chatthreads"
	"manifest/ledger"
)

var chatTokenRe = regexp.MustCompile(`\[chat::\s*([^\]#]+)#([^\]]+)\]`)

// chatDelegateProtocol tells kairos to return proposals rather than act — the
// spool-path sibling of hermesGoProtocol, but scoped to the two in-portal
// change types (item field patch, plan/description section swap).
const chatDelegateProtocol = "\nCHANGES PROTOCOL: you have NO write access — nothing you say is applied until a person approves it. " +
	"For anything you would change, emit a fenced ```manifest-proposal``` block (one per change), JSON, exactly these shapes:\n" +
	"  {\"type\":\"set-field\",\"item\":\"<item-id>\",\"field\":\"status|due|needed_by\",\"value\":\"<v>\"}\n" +
	"  {\"type\":\"replace-section\",\"item\":\"<item-id>\",\"section\":\"plan|description\",\"body\":\"<full new markdown>\"}\n" +
	"Use the exact item ids from the CONTEXT above. Put your reasoning in prose; put every change in a block. Propose nothing you cannot ground in the context.\n"

const chatAskProtocol = "\nPROTOCOL: answer from the CONTEXT above and your read-only tools. Cite the files/records you drew on. " +
	"Do NOT propose or make changes — this is a read-only ask.\n"

// UseChatThreads wires the chat store.
func (s *Server) UseChatThreads(store *chatthreads.Store) { s.chat = store }

// resolveChatContext turns structural ids into real content the work order
// carries (the client sends ids, never prose).
func (s *Server) resolveChatContext(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("CONTEXT (what this ask is grounded in):\n")
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		switch {
		case strings.HasPrefix(id, "aion:") && s.aion != nil:
			bare := strings.TrimPrefix(id, "aion:")
			if it := s.aion.LoadBacklog().Find(bare); it != nil {
				b.WriteString("- item [" + id + "]: " + it.Text)
				if it.Rock != "" {
					b.WriteString(" (rock: " + it.Rock + ")")
				}
				b.WriteString("\n")
				if rec := s.readPlanRecord(id); rec.Exists && strings.TrimSpace(rec.Plan) != "" {
					b.WriteString("  its ## plan:\n" + indent(rec.Plan, "    ") + "\n")
				}
				continue
			}
			b.WriteString("- item [" + id + "]\n")
		default:
			// a rock / goal id — resolve the title if we can
			title := id
			if s.goals != nil {
				if _, g := s.goals.Load().FindGoal(id); g != nil {
					title = g.Text
				}
			}
			b.WriteString("- rock [" + id + "]: " + title + "\n")
		}
	}
	return b.String()
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// spoolChatOrder composes and spools one chat run order into the kairos tree.
// Returns the order id (for pending attribution) or an error (incl.
// spirits.ErrAlreadyActive when a run is in flight).
func (s *Server) spoolChatOrder(threadID, threadTitle, ritual, intent, text string, contextIDs []string, who string) (string, error) {
	h := s.findHarness("kairos")
	if h == nil || h.Spirits == nil {
		return "", errBadRequest("kairos is not configured")
	}
	orderID := fmt.Sprintf("%d", time.Now().UnixNano())
	var b strings.Builder
	if p, ok := s.persona(intent); ok {
		b.WriteString("PERSONA (how to respond):\n" + p.Prompt + "\n")
	}
	b.WriteString("You are kairos, the AION team agent, answering in the team portal chat")
	if threadTitle != "" {
		b.WriteString(" (thread: " + threadTitle + ")")
	}
	b.WriteString(".\n")
	if ctx := s.resolveChatContext(contextIDs); ctx != "" {
		b.WriteString(ctx)
	}
	b.WriteString("MESSAGE (from " + orStr(who, "a teammate") + "):\n" + text + "\n")
	if ritual == "delegate" {
		b.WriteString(chatDelegateProtocol)
	} else {
		b.WriteString(chatAskProtocol)
	}
	b.WriteString("[chat:: " + threadID + "#" + orderID + "]")
	if err := h.Spirits.SpoolRunNow("kairos", ritual, b.String(), ""); err != nil {
		return "", err
	}
	return orderID, nil
}

// chatSweep ingests completed/failed kairos runs carrying a [chat::] token into
// their thread as kairos messages (parsing proposals on delegate turns). Runs
// on the AgentLoopTicker; idempotent by run id.
func (s *Server) chatSweep() {
	if s.chat == nil {
		return
	}
	if !s.chatSweepMu.TryLock() { // ticker + read-driven sweeps must not double-ingest
		return
	}
	defer s.chatSweepMu.Unlock()
	h := s.findHarness("kairos")
	if h == nil || h.Spirits == nil {
		return
	}
	lib := harnessLibrary(*h)
	for _, r := range h.Spirits.Runs() {
		if r.Outcome != "completed" && r.Outcome != "failed" {
			continue
		}
		m := chatTokenRe.FindStringSubmatch(r.Request)
		if m == nil {
			continue
		}
		threadID, orderID := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if s.chat.HasRun(threadID, r.Run) {
			continue
		}
		// the answer: library brief, else the report body
		doc, ok := libraryDocForRun(*h, r.ID, lib)
		body := strings.TrimSpace(doc.Body)
		if !ok || body == "" {
			if _, rb, ok2 := h.Spirits.Run(r.ID); ok2 {
				body = strings.TrimSpace(rb)
			}
		}
		var props []chatthreads.Proposal
		if r.Ritual == "delegate" && r.Outcome == "completed" {
			clean, parsed, _ := chatthreads.ParseProposals(body)
			body, props = clean, parsed
		}
		msg := chatthreads.Message{
			Thread: threadID, Kind: "kairos", Author: "agent:kairos", AuthName: "Kairos",
			Text: body, At: time.Now(), Ritual: r.Ritual, Outcome: r.Outcome,
			Elapsed: elapsed(r.Started, r.Finished), Run: r.Run,
			Report: "artifacts/runs/" + r.ID + ".md", Brief: doc.Ref, Props: props,
		}
		if _, err := s.chat.AddMessage(msg, time.Now()); err != nil {
			continue
		}
		s.chat.ClearPending(orderID)
		kind := "chat.completed"
		if r.Outcome == "failed" {
			kind = "chat.failed"
		}
		s.ledger(ledger.Entry{Source: "chat", Kind: kind, Actor: "agent:kairos",
			Run: r.Run, Harness: "kairos", Text: ledger.Snip(body, 280)})
	}
}

func elapsed(started, finished string) string {
	if started == "" || finished == "" {
		return ""
	}
	st, e1 := time.Parse(time.RFC3339, started)
	fn, e2 := time.Parse(time.RFC3339, finished)
	if e1 != nil || e2 != nil {
		return ""
	}
	d := fn.Sub(st)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
}

// chatEngine reports the kairos engine liveness + queue for the sidebar, with
// requester attribution from the chat store's pending records.
func (s *Server) chatEngine() map[string]any {
	out := map[string]any{
		"harness": "kairos", "runtime": "hermes-agent", "host": "lab-apps · kairos",
		"model": "deepseek-v4-flash-0731", "root": "/shared/apps/kairos",
		"ceiling": "10m · poll 3s", "live": false, "beat": -1,
	}
	h := s.findHarness("kairos")
	if h == nil || h.Spirits == nil {
		return out
	}
	alive, at := h.Spirits.EngineAlive()
	out["live"] = alive
	if !at.IsZero() {
		out["beat"] = int(time.Since(at).Seconds())
	}
	// pending attribution from the chat store
	var pend []map[string]any
	if s.chat != nil {
		for _, p := range s.chat.Pending() {
			pend = append(pend, map[string]any{
				"thread": p.Thread, "by": p.By, "byEmail": p.ByEmail,
				"ritual": p.Ritual, "text": p.Text, "at": p.At,
			})
		}
	}
	// a running report → the active claim, attributed via its [chat::] token's
	// thread matched against a pending record
	active := map[string]any(nil)
	for _, r := range h.Spirits.Runs() {
		if r.Outcome != "running" {
			continue
		}
		active = map[string]any{"ritual": r.Ritual, "run": r.Run}
		if m := chatTokenRe.FindStringSubmatch(r.Request); m != nil {
			thread := strings.TrimSpace(m[1])
			active["thread"] = thread
			for _, p := range pend {
				if p["thread"] == thread {
					active["by"] = p["by"]
					active["byEmail"] = p["byEmail"]
					active["text"] = p["text"]
					break
				}
			}
		}
		break
	}
	out["active"] = active
	out["pending"] = pend
	return out
}

// chatBusy reports whether kairos has a run in flight (one-run-at-a-time gate).
func (s *Server) chatBusy() bool {
	h := s.findHarness("kairos")
	if h == nil || h.Spirits == nil {
		return false
	}
	return h.Spirits.IsActive("kairos", "ask") || h.Spirits.IsActive("kairos", "delegate")
}
