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
	"manifest/realestate"
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

// UseChatThreads wires the AION chat store.
func (s *Server) UseChatThreads(store *chatthreads.Store) { s.chat = store }

// UseOodaChat wires the OODA portal's chat store + its agent (ooda-portal
// plan, Stage D). Same machinery, second identity — the alternative was a
// second copy of this file, and a chat bridge is exactly the kind of thing
// that rots when duplicated.
func (s *Server) UseOodaChat(store *chatthreads.Store) { s.oodaChat = store }

// chatAgent is ONE conversational agent: which harness spools its work, what
// it calls itself in the preamble, and which thread store holds its
// conversation. Everything below takes an agent rather than assuming kairos.
type chatAgent struct {
	Name     string // spirit + harness name — "kairos" | "zeck"
	Display  string // "Kairos" | "Zeck"
	Identity string // the preamble sentence, minus the thread clause
	Host     string // engine sidebar: where it runs
	Root     string // engine sidebar: its harness tree
	Store    *chatthreads.Store
}

// kairosAgent is the AION team agent (nil when chat is unwired).
func (s *Server) kairosAgent() *chatAgent {
	if s.chat == nil {
		return nil
	}
	return &chatAgent{
		Name: "kairos", Display: "Kairos",
		Identity: "You are kairos, the AION team agent, answering in the team portal chat",
		Host:     "lab-apps · kairos", Root: "/shared/apps/kairos", Store: s.chat,
	}
}

// zeckAgent is the OODA real-estate agent (nil when the OODA chat is unwired).
func (s *Server) zeckAgent() *chatAgent {
	if s.oodaChat == nil {
		return nil
	}
	return &chatAgent{
		Name: "zeck", Display: "Zeck",
		Identity: "You are zeck, the OODA real-estate agent, answering in the OODA portal chat",
		Host:     "metis · zeck", Root: "/private/harnesses/zeck", Store: s.oodaChat,
	}
}

// chatAgents is every wired agent — chatSweep walks them all.
func (s *Server) chatAgents() []*chatAgent {
	var out []*chatAgent
	for _, ag := range []*chatAgent{s.kairosAgent(), s.zeckAgent()} {
		if ag != nil {
			out = append(out, ag)
		}
	}
	return out
}

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
		case strings.HasPrefix(id, "prop/") && s.oodaLive != nil:
			// A REAL-ESTATE property (ooda-portal plan, Stage D). This branch is
			// the whole reason zeck can be useful without reading the vault:
			// manifest, running as the owner, resolves the id into real content
			// and spools THAT into the work order. The agent sees the excerpt it
			// was handed, never the tree.
			slug, workID, _ := strings.Cut(strings.TrimPrefix(id, "prop/"), "#")
			b.WriteString(s.oodaChatContext(id, slug, workID))
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
func (s *Server) spoolChatOrder(ag *chatAgent, threadID, threadTitle, ritual, intent, text string, contextIDs []string, who string) (string, error) {
	h := s.findHarness(ag.Name)
	if h == nil || h.Spirits == nil {
		return "", errBadRequest(ag.Name + " is not configured")
	}
	orderID := fmt.Sprintf("%d", time.Now().UnixNano())
	var b strings.Builder
	if p, ok := s.persona(intent); ok {
		b.WriteString("PERSONA (how to respond):\n" + p.Prompt + "\n")
	}
	b.WriteString(ag.Identity)
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
	if err := h.Spirits.SpoolRunNow(ag.Name, ritual, b.String(), ""); err != nil {
		return "", err
	}
	return orderID, nil
}

// chatSweep ingests completed/failed kairos runs carrying a [chat::] token into
// their thread as kairos messages (parsing proposals on delegate turns). Runs
// on the AgentLoopTicker; idempotent by run id.
func (s *Server) chatSweep() {
	if !s.chatSweepMu.TryLock() { // ticker + read-driven sweeps must not double-ingest
		return
	}
	defer s.chatSweepMu.Unlock()
	for _, ag := range s.chatAgents() {
		s.sweepAgent(ag)
	}
}

func (s *Server) sweepAgent(ag *chatAgent) {
	h := s.findHarness(ag.Name)
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
		if ag.Store.HasRun(threadID, r.Run) {
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
		if r.Outcome == "completed" { // parse any turn: kairos may volunteer a fence in an ask; ParseProposals no-ops without one
			clean, parsed, _ := chatthreads.ParseProposals(body)
			body, props = clean, parsed
		}
		msg := chatthreads.Message{
			Thread: threadID, Kind: ag.Name, Author: "agent:" + ag.Name, AuthName: ag.Display,
			Text: body, At: time.Now(), Ritual: r.Ritual, Outcome: r.Outcome,
			Elapsed: elapsed(r.Started, r.Finished), Run: r.Run,
			Report: "artifacts/runs/" + r.ID + ".md", Brief: doc.Ref, Props: props,
		}
		if _, err := ag.Store.AddMessage(msg, time.Now()); err != nil {
			continue
		}
		ag.Store.ClearPending(orderID)
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
func (s *Server) chatEngine(ag *chatAgent) map[string]any {
	out := map[string]any{
		"harness": ag.Name, "runtime": "hermes-agent", "host": ag.Host,
		"model": "deepseek-v4-flash-0731", "root": ag.Root,
		"ceiling": "10m · poll 3s", "live": false, "beat": -1,
	}
	h := s.findHarness(ag.Name)
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
	if ag.Store != nil {
		for _, p := range ag.Store.Pending() {
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
func (s *Server) chatBusy(ag *chatAgent) bool {
	h := s.findHarness(ag.Name)
	if h == nil || h.Spirits == nil {
		return false
	}
	return h.Spirits.IsActive(ag.Name, "ask") || h.Spirits.IsActive(ag.Name, "delegate")
}

// oodaChatContext renders one property (optionally one rock-tree node) as the
// grounding block a chat order carries. Money, current rock, open work, and
// the node's own line — enough to answer a real question, and nothing the
// member could not already see in the portal.
func (s *Server) oodaChatContext(id, slug, workID string) string {
	snap := s.oodaLive.Snapshot()
	if snap == nil {
		return "- property [" + id + "]\n"
	}
	for i := range snap.Properties {
		p := snap.Properties[i]
		if !strings.EqualFold(p.Slug, slug) {
			continue
		}
		f := oodaPropertyFacts(p, oodaToday())
		var b strings.Builder
		fmt.Fprintf(&b, "- property [%s]: %s — %s, %s\n", id, f.Short, orStr(p.Entity, "no entity"),
			strings.ReplaceAll(p.Status, "_", " "))
		fmt.Fprintf(&b, "    money: plan %.0f · committed %.0f · paid %.0f · to go %.0f\n",
			f.Plan, f.Committed, f.Paid, f.ToGo)
		if f.Rock != "" {
			fmt.Fprintf(&b, "    current rock: %s%s\n", f.Rock,
				map[bool]string{true: " (due " + f.DoneBy + ")", false: ""}[f.DoneBy != ""])
		}
		fmt.Fprintf(&b, "    open work: %d\n", f.Open)
		if workID != "" {
			realestate.WalkNodes(p.Work, func(st *realestate.WorkStage, n *realestate.WorkNode) {
				if n.ID != workID || n.Task == nil {
					return
				}
				fmt.Fprintf(&b, "    the node in question [%s]: %s (rock: %s, owner: %s%s)\n",
					workID, n.Task.Text, st.Text, orStr(n.Task.Owner, "unassigned"),
					map[bool]string{true: ", done", false: ""}[n.Task.Checked])
			})
		}
		for _, c := range snap.Contracts {
			for _, al := range c.Allocations {
				if strings.EqualFold(al.Property, p.Slug) {
					fmt.Fprintf(&b, "    contract: %s — %s, %.0f (%s)\n", c.Name, c.Contractor, al.Amount, c.Status)
					break
				}
			}
		}
		return b.String()
	}
	return "- property [" + id + "] (no record)\n"
}
