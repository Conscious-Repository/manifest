package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"manifest/aion"
	"manifest/approvals"
	"manifest/goals"
	"manifest/hermes"
	"manifest/ledger"
	"manifest/spirits"
	"manifest/threads"
)

// Hermes routes the app's @hermes onto the owner's REAL do-bot — the local
// NousResearch Hermes Agent CLI (`hermes -z`), the same agent he pings on
// Telegram — instead of the compartmentalized excalibur harness copy. The
// delegation SEMANTICS are unchanged (persona+phase work order → plan record →
// thread → fire); only the transport swaps: rather than spooling a file for the
// excalibur engine, we invoke the CLI in-process and materialize its reply.
//
// The excalibur write/execution CONTRACT is preserved by SCOPE: plan/comment
// turns run with a read-only toolset (`-t`), so a planning turn cannot act;
// approval-gated execution for the `go` phase is Phase 2.

// hermesCfg holds the runner + in-flight bookkeeping (one field on Server).
type hermesCfg struct {
	runner    *hermes.Runner
	readTools string                // -t scope for plan/comment (read-only)
	mu        sync.Mutex            // guards running + digging
	running   map[string]hermesTurn // taskID → the turn in flight (presence, agent-chat plan §3.4d)
	digging   map[string]bool       // feed item id → a "dig →" turn in flight (hermes_dig.go)
}

// hermesTurn is one in-flight do-bot turn on a todo — derived presence for
// the thread ("✦ Alfred is working… since 14:02 (plan)"), never stored.
type hermesTurn struct {
	Phase string
	Agent string // the agent token the turn runs as (agent:alfred | agent:<profile>)
	Since time.Time
}

// UseHermes wires the do-bot runner. readTools is the read-only toolset scope
// applied to plan/comment turns (the approval-gate pre-stage).
func (s *Server) UseHermes(r *hermes.Runner, readTools string) {
	if r == nil || !r.Enabled() {
		return
	}
	s.hermes = &hermesCfg{runner: r, readTools: readTools, running: map[string]hermesTurn{}, digging: map[string]bool{}}
	s.agentChatRecover() // the chat store may have been wired first (main.go order)
}

// hermesForked reports whether this delegation should route to the do-bot CLI
// rather than the harness spool. Only the `hermes` harness identity forks.
func (s *Server) hermesForked(h *Harness) bool {
	return s.hermes != nil && h != nil && strings.EqualFold(h.Name, "hermes")
}

// hermesEnabled reports whether the do-bot runner is wired.
func (s *Server) hermesEnabled() bool { return s.hermes != nil }

// hermesRealHarness reports whether a real `hermes` harness tree is still in the
// federation. Once Phase 1c retires it from config, Hermes is a VIRTUAL agent —
// the runner-backed identity with no tree — so findHarness/rosterFor/agentHarness
// synthesize it. This guard just avoids a duplicate during the transition.
func (s *Server) hermesRealHarness() bool {
	for _, h := range s.eachHarness() {
		if strings.EqualFold(h.Name, "hermes") {
			return true
		}
	}
	return false
}

// startHermesTurn kicks off one agent turn in the background (a turn is slow —
// the tool loop). It coalesces: a second call while a turn is in flight for the
// same todo is refused, mirroring the harness double-spool guard.
//
// agent is the token the turn runs as: agent:alfred (the default profile) or
// agent:<profile> (`hermes -p <profile>`); "" means Alfred. A busy todo returns
// an error wrapping spirits.ErrAlreadyActive — the same signal the harness
// spool gives, so relays and fire treat both transports alike.
//
// extra is the owner's raw input the work order was composed from (the ask
// text, or the approved plan on a go phase) — it rides the durable turn-open
// marker so a turn the process dies on can be re-composed and re-dispatched
// by the sweep (hermesTurnSweep); the composed prompt itself is not stored.
func (s *Server) startHermesTurn(taskID, agent, phase, intent, extra, prompt string) error {
	if agent == "" || s.hermesProfileOf(agent) == "" {
		agent = "agent:" + alfredAgent
	}
	s.hermes.mu.Lock()
	if _, busy := s.hermes.running[taskID]; busy {
		s.hermes.mu.Unlock()
		return fmt.Errorf("%s is already working on this — wait for it to finish: %w", agentDisplayName(agent), spirits.ErrAlreadyActive)
	}
	s.hermes.running[taskID] = hermesTurn{Phase: phase, Agent: agent, Since: time.Now()}
	s.hermes.mu.Unlock()
	// the durable "owed" record — written BEFORE the goroutine exists, so a
	// turn that completes instantly can never close before it opened.
	s.hermesTurnMark(taskID, actTurnOpen, map[string]any{
		"agent": agent, "phase": phase, "intent": intent, "text": extra})
	// let the do-bot SEE what the owner attached on the thread — text files
	// inlined, images handed their path (vision reads them; already in scope).
	if att := s.hermesAttachments(taskID); att != "" {
		prompt += "\n" + att
	}
	// the go phase carries the proposals protocol (Phase 2): execution stays
	// read-only tool-wise; world-changes ride fenced blocks that manifest files
	// into the approvals inbox for the owner's confirm.
	if phase == "go" {
		prompt += "\n" + hermesGoProtocol
	}
	go s.runHermesTurn(taskID, agent, phase, intent, prompt)
	return nil
}

// hermesGoProtocol is the Phase-2 execution contract stamped on every fired
// ("go") work order: Hermes cannot change the owner's systems directly — each
// world-change is a fenced manifest-proposal block that manifest parses and
// files for approval (hermes.ParseProposals is the counterpart parser).
const hermesGoProtocol = `CHANGES PROTOCOL (hard rule): you have NO direct write access to the owner's
systems. To change anything in the world, emit one fenced block per change,
exactly like this, anywhere in your reply:

` + "```manifest-proposal\n" + `{"type":"create-vault-note","title":"<note title>","body":"<full note content>"}
` + "```" + `

Allowed types (one JSON object per block; no other types exist):
- {"type":"create-vault-note","title":"...","body":"..."}            — a new dated note in the owner's vault log
- {"type":"run-errand","errand":"<what to do>","account":"<optional>"} — a real-world errand the owner's effector runs
- {"type":"aion-backlog","kind":"task|decision","title":"...","owner":"<initials, optional>","due":"YYYY-MM-DD, optional"}
- {"type":"re-backlog","kind":"task|decision","title":"...","owner":"<initials, optional>"}
- {"type":"goals-item","mode":"add|edit|move|delete","level":"rock|milestone","area":"<## heading in goals.md>",
   "title":"...","parentId":"<rock id, for milestone add/move>",
   "targetId":"<goal id, edit/move/delete>","anchorText":"<the goal's current text, edit/move/delete>"} — place/edit/move/remove a goal line (owner's words only)

Each block is filed for the OWNER'S APPROVAL — nothing happens until he
confirms, so file the change and reference it in your brief rather than
claiming it is done. Everything else in your reply is the RESULT brief.
`

// runHermesTurn invokes the CLI and materializes the reply. Always clears the
// in-flight marker (in memory) and closes the durable turn-open marker.
func (s *Server) runHermesTurn(taskID, agent, phase, intent, prompt string) {
	defer func() {
		s.hermes.mu.Lock()
		delete(s.hermes.running, taskID)
		s.hermes.mu.Unlock()
		s.hermesTurnMark(taskID, actTurnClosed, map[string]any{"agent": agent, "phase": phase})
	}()
	who := agentTokenIdentity(agent)
	// Every turn is a fresh Hermes session (hermes -z has no working resume —
	// see package hermes); the prompt itself carries the thread's context.
	res, err := s.hermes.runner.Run(context.Background(), hermes.Request{
		Prompt:   prompt,
		Toolsets: s.hermes.readTools, // read-only scope until Phase 2's gated execution
		Profile:  s.hermesProfileOf(agent),
	})
	if err != nil {
		log.Printf("hermes turn %s (%s): %v", taskID, phase, err)
		_, _ = s.addThreadEntry(who, taskID, threads.ActComment,
			"⚠ "+who.Name+" couldn't finish that — "+err.Error(), nil, nil, map[string]any{"hermes": true})
		return
	}
	// The run record carries the Hermes-side session id and the spend, so the
	// turn can be found again (`hermes sessions search`) and costed.
	s.ledger(ledger.Entry{Source: "run", Kind: "run.completed", Actor: who.ID, Task: taskID, Harness: "hermes",
		Text: phase + " turn on " + taskID + ": " + ledger.Snip(res.Reply, 280),
		Meta: map[string]any{"task": taskID, "phase": phase, "sessionId": res.SessionID, "spentUsd": res.SpentUSD, "model": res.Model}})
	// the intent decides where the reply lands: any non-plan intent (an Ask)
	// is a thread answer, whether or not its persona is enabled — an Ask must
	// never write ## plan (spoolTaskWorkOrderAs stamps the same rule)
	persona := ""
	if _, ok := s.persona(intent); ok || (intent != "" && intent != "plan") {
		persona = intent
	}
	s.materializeHermesBrief(taskID, agent, phase, persona, res.Reply)
}

// agentTokenIdentity is the thread author for a Hermes-family agent token:
// alfred/hermes speak as Alfred under the canonical `agent:hermes` id (the
// relay/ingestion checks key on the `agent:` prefix, and existing threads
// carry that id); a profile speaks under its own token.
func agentTokenIdentity(agent string) threads.Identity {
	name := strings.TrimPrefix(agent, "agent:")
	if name == "" || name == alfredAgent || name == "hermes" {
		return threads.Identity{ID: "agent:hermes", Name: "Alfred"}
	}
	return threads.Identity{ID: "agent:" + name, Name: agentDisplayName(agent)}
}

// materializeHermesBrief turns the CLI's reply into the same surfaces the
// excalibur path produces (mirrors agentLoopSweep, but sourced directly from the
// reply rather than a harness run report): a plan brief → the plan record + a
// thread note; a questions brief → a thread question; a non-plan persona reply
// or a fired result → a thread comment.
// postAgentBrief lands an agent's reply in the thread COMPLETELY: a reply
// longer than the destination store's comment cap is split into numbered
// parts at paragraph seams instead of being refused — a completed turn must
// never vanish (2026-09-04: an ask answer over the 8000-byte cap was
// silently dropped by a discarded error; the ledger said run.completed and
// the owner saw nothing). Any residual post failure surfaces as a visible
// ⚠ comment plus a log line — never a silent no-op.
func (s *Server) postAgentBrief(who threads.Identity, taskID, text string, meta map[string]any) {
	max := threads.MaxCommentLen - 200 // headroom for the part prefix
	if s.threadKind(taskID) == "aion" {
		max = 3800 // teamportal comments cap at 4000
	}
	parts := splitBrief(text, max)
	for i, p := range parts {
		if len(parts) > 1 {
			p = fmt.Sprintf("(part %d/%d)\n%s", i+1, len(parts), p)
		}
		if _, err := s.addThreadEntry(who, taskID, threads.ActComment, p, nil, nil, meta); err != nil {
			log.Printf("agent brief post %s (part %d/%d): %v", taskID, i+1, len(parts), err)
			_, _ = s.addThreadEntry(who, taskID, threads.ActComment,
				"⚠ "+who.Name+" finished, but the reply could not be posted — "+err.Error(),
				nil, nil, meta)
			return
		}
	}
}

// splitBrief cuts text into ≤max-byte pieces, preferring paragraph seams,
// then line seams, then a hard cut.
func splitBrief(text string, max int) []string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return []string{text}
	}
	var parts []string
	for len(text) > max {
		cut := strings.LastIndex(text[:max], "\n\n")
		if cut < max/2 {
			cut = strings.LastIndex(text[:max], "\n")
		}
		if cut < max/2 {
			// a hard cut lands on a rune boundary — a byte cut through a
			// multibyte character would post an invalid-UTF-8 part
			cut = max
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
		}
		parts = append(parts, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}

func (s *Server) materializeHermesBrief(taskID, agent, phase, persona, brief string) {
	brief = strings.TrimSpace(brief)
	if brief == "" || s.threads == nil || s.todoPlans == nil || s.vault == nil {
		return
	}
	if agent == "" {
		agent = "agent:" + alfredAgent
	}
	who := agentTokenIdentity(agent)
	meta := map[string]any{"hermes": true}
	rec := s.readPlanRecord(taskID)
	if rec.Assignee == "" { // engagement implies assignment (mirrors the sweep)
		_ = s.setPlanAssignee(taskID, agent)
	}

	// go phase → the deliverable lands in the thread, and any manifest-proposal
	// blocks are filed into the approvals inbox (Phase 2: the approval gate —
	// nothing executes until the owner confirms in FEED).
	if phase == "go" {
		clean, specs, warns := hermes.ParseProposals(brief)
		filed := 0
		for _, sp := range specs {
			p, err := s.hermesProposal(taskID, sp)
			if err == nil && s.approvals == nil {
				err = fmt.Errorf("no approvals inbox is configured")
			}
			if err == nil {
				_, err = s.approvals.Propose(p)
			}
			if err != nil {
				warns = append(warns, "couldn't file a proposal — "+err.Error())
				continue
			}
			filed++
		}
		text := ledger.Snip(clean, 3600) +
			"\n\n— result delivered; review it, then close the item or send it back with a comment"
		if filed > 0 {
			text += fmt.Sprintf("\n⚑ %d change(s) filed for your approval — review them in FEED", filed)
		}
		for _, w := range warns {
			text += "\n⚠ " + w
		}
		s.postAgentBrief(who, taskID, text, meta)
		return
	}
	// non-plan persona (brief/info/…) → the whole reply IS the answer.
	if persona != "" && persona != "plan" {
		s.postAgentBrief(who, taskID, brief, meta)
		return
	}
	// questions-only → post them as dialog, leave the plan untouched.
	questions, questionsOnly := briefQuestions(brief)
	if questionsOnly && questions != "" {
		s.postAgentBrief(who, taskID, questions, meta)
		return
	}
	// plan brief → attach/update the canon plan + a thread note.
	hadPlan := strings.TrimSpace(rec.Plan) != ""
	if err := s.writePlanSection("todo-plans-agent", taskID, "plan", brief); err != nil {
		log.Printf("hermes plan write %s: %v", taskID, err)
		return
	}
	verb := "plan attached to this task — answer in the thread to refine it, edit it directly, or fire to execute"
	if hadPlan {
		verb = "plan updated on this task — answer in the thread to refine it, edit it directly, or fire to execute"
	}
	s.ledger(ledger.Entry{Source: "plan", Kind: "plan.materialized",
		Actor: who.ID, Task: taskID, Harness: "hermes", Ref: s.readPlanRecord(taskID).Rel})
	s.postAgentBrief(who, taskID, verb, meta)
	if questions != "" { // drift guard: embedded questions still surface as dialog
		s.postAgentBrief(who, taskID, questions, meta)
	}
}

// hermesProposal maps one validated ProposalSpec onto the approvals byte-
// contract — the SAME shapes the excalibur engine's write_approval emits, so
// Confirm applies through the existing lanes (vault log/ write, errand enqueue,
// backlog append) with their allow-lists and secret scans intact. The
// [todo:: <id>] token rides the Action so the todo panel shows state=proposed
// (delegationIndex matches it) and dedupe (id = sha1(action|body)) keeps a
// re-fired plan from double-filing.
func (s *Server) hermesProposal(taskID string, sp hermes.ProposalSpec) (approvals.Proposal, error) {
	token := " [todo:: " + taskID + "]"
	p := approvals.Proposal{Agent: "hermes", Ritual: "delegate"}
	switch sp.Type {
	case "create-vault-note":
		date := strings.TrimSpace(sp.Date)
		if date == "" {
			date = time.Now().Format("2006-01-02")
		} else if _, err := time.Parse("2006-01-02", date); err != nil {
			return p, fmt.Errorf("create-vault-note date %q is not YYYY-MM-DD", sp.Date)
		}
		p.Type = approvals.TypeCreateVaultNote
		p.ApplyPath = date + " " + strings.TrimSpace(sp.Title) + ".md"
		if !approvals.CreateVaultNotePathAllowed(p.ApplyPath) {
			return p, fmt.Errorf("create-vault-note title %q doesn't form an allowed note name", sp.Title)
		}
		p.Action = "create vault note " + p.ApplyPath + token
		// the proposed content rides the body as a ````proposed fence — that is
		// how the store persists it (parse() re-derives Proposed from the fence).
		p.Body = "Hermes (go phase) drafted this note while executing the fired plan.\n\n````proposed\n" + sp.Body + "\n````"
	case "run-errand":
		p.Type = approvals.TypeRunErrand
		p.ErrandText = strings.TrimSpace(sp.Errand)
		p.ErrandAccount = strings.TrimSpace(sp.Account)
		p.ErrandGoal = strings.TrimSpace(sp.Goal)
		p.Action = "run errand: " + p.ErrandText + token
		p.Body = "Hermes (go phase) requests this errand as part of the fired plan."
	case "aion-backlog", "re-backlog":
		payload := aion.ProposalPayload{
			Kind: sp.Kind, Title: strings.TrimSpace(sp.Title),
			Owner: strings.TrimSpace(sp.Owner), Rock: strings.TrimSpace(sp.Rock),
			Due: strings.TrimSpace(sp.Due), Sources: sp.Sources,
			Captured: time.Now().Format("2006-01-02"),
		}
		fenceTag := aion.PayloadFence
		p.Type, p.ApplyPath = approvals.TypeAionBacklog, approvals.AionBacklogPath
		if sp.Type == "re-backlog" {
			fenceTag = aion.REPayloadFence
			p.Type, p.ApplyPath = approvals.TypeReBacklog, approvals.ReBacklogPath
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return p, err
		}
		evidence := strings.TrimSpace(sp.Body)
		if evidence == "" {
			evidence = "Hermes (go phase) filed this while executing the fired plan."
		}
		p.Body = evidence + "\n\n````" + fenceTag + "\n" + string(raw) + "\n````"
		p.Action = sp.Type + " " + sp.Kind + ": " + payload.Title + token
	case "goals-item":
		payload := goals.PlacementPayload{
			Mode: sp.Mode, Level: sp.Level, Area: strings.TrimSpace(sp.Area),
			ParentID: strings.TrimSpace(sp.ParentID), TargetID: strings.TrimSpace(sp.TargetID),
			AnchorText: strings.TrimSpace(sp.AnchorText),
			Title:      strings.TrimSpace(sp.Title), Owner: strings.TrimSpace(sp.Owner),
			Due: strings.TrimSpace(sp.Due), Serves: sp.Serves,
		}
		if err := payload.Validate(); err != nil {
			return p, err
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return p, err
		}
		evidence := strings.TrimSpace(sp.Body)
		if evidence == "" {
			evidence = "Hermes (go phase) proposed this placement while executing the fired plan."
		}
		p.Type, p.ApplyPath = approvals.TypeGoalsItem, approvals.GoalsPath
		p.Body = evidence + "\n\n````" + goals.PayloadFence + "\n" + string(raw) + "\n````"
		p.Action = "goals " + sp.Mode + " " + sp.Level + ": " + firstNonEmpty(payload.Title, payload.TargetID) + token
	default: // unreachable — ParseProposals validated the type
		return p, fmt.Errorf("unknown proposal type %q", sp.Type)
	}
	return p, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// hermesInlineMax caps how big a text attachment we inline into the prompt;
// larger ones are handed by path instead.
const hermesInlineMax = 128 << 10

// hermesAttachments renders the owner's thread attachments into a prompt block
// the do-bot can consume — so "i attached a ui file" actually reaches Hermes.
// Text-decodable files are inlined verbatim; images (and other binaries) are
// handed their on-disk blob path (which keeps its extension), and Hermes's
// vision toolset — already in the read-only scope — reads local image paths.
// Only the OWNER's attachments (not agent posts) are surfaced, deduped by hash.
func (s *Server) hermesAttachments(taskID string) string {
	if s.threads == nil {
		return ""
	}
	st := s.threadStore(s.threadKind(taskID))
	if st == nil {
		return ""
	}
	seen := map[string]bool{}
	var b strings.Builder
	for _, c := range s.listThread(taskID) {
		if strings.HasPrefix(c.Author, "agent:") {
			continue // the owner's attachments only
		}
		for _, f := range c.Files {
			if f.Hash == "" || seen[f.Hash] {
				continue
			}
			seen[f.Hash] = true
			path := st.BlobPath(f.Hash)
			if path == "" {
				continue
			}
			if b.Len() == 0 {
				b.WriteString("\nATTACHMENTS the owner shared on this task — read them before you answer:\n")
			}
			if body, ok := readTextAttachment(path, f); ok {
				fmt.Fprintf(&b, "\n--- %s (attached file) ---\n%s\n--- end %s ---\n", f.Name, body, f.Name)
			} else if strings.HasPrefix(strings.ToLower(f.Mime), "image/") || isImageExt(f.Name) {
				fmt.Fprintf(&b, "\n- %s (image — view it with your vision tool) is at: %s\n", f.Name, path)
			} else {
				fmt.Fprintf(&b, "\n- %s (%s) is at: %s\n", f.Name, orDash(f.Mime), path)
			}
		}
	}
	return b.String()
}

// readTextAttachment returns an attachment's content if it's small, text-typed,
// and valid UTF-8 — the "inline it into the prompt" case.
func readTextAttachment(path string, f threads.FileRef) (string, bool) {
	if f.Size > hermesInlineMax || !isTextAttachment(f) {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) > hermesInlineMax || !utf8.Valid(data) {
		return "", false
	}
	return strings.TrimRight(string(data), "\n"), true
}

func isTextAttachment(f threads.FileRef) bool {
	m := strings.ToLower(f.Mime)
	if strings.HasPrefix(m, "text/") || m == "application/json" || m == "application/xml" ||
		strings.Contains(m, "javascript") || strings.Contains(m, "yaml") || strings.Contains(m, "csv") {
		return true
	}
	switch strings.ToLower(filepath.Ext(f.Name)) {
	case ".md", ".txt", ".css", ".html", ".htm", ".js", ".jsx", ".ts", ".tsx", ".json",
		".yaml", ".yml", ".csv", ".go", ".py", ".sh", ".toml", ".xml", ".svg", ".rs", ".c", ".cpp", ".sql":
		return true
	}
	return false
}

func isImageExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic":
		return true
	}
	return false
}

// --- turn durability (2026-09-04: an Ask at 17:45:25 vanished because manifest
// restarted at 17:45:55 — the accepted turn lived only in hermes.running) ------
//
// A do-bot turn is accepted in memory and run by a goroutine; a restart kills
// both, and nothing on disk said a reply was owed (relay-pending only covers a
// relay REFUSED while busy). So every accepted turn leaves a private turn-open
// marker (agent/phase/intent/the owner's text) and its completion — success
// or failure — leaves a turn-closed marker. The sweep re-dispatches an open
// marker with no close, no in-flight turn, and no agent reply after it.
const (
	actTurnOpen   = "turn-open"   // an accepted do-bot turn (owed)
	actTurnClosed = "turn-closed" // the turn finished — success or ⚠ failure
	// hermesTurnRetries caps re-dispatches of one owed turn (opens without a
	// close) — a turn that outlives every deploy window must not loop forever.
	hermesTurnRetries = 3
)

// hermesTurnMark writes a turn marker into the private store; a server with
// no thread store (feed digs, chat) has nothing durable to record and no-ops.
func (s *Server) hermesTurnMark(taskID, action string, extra map[string]any) {
	if s.threads == nil || s.threads.private == nil {
		return
	}
	s.markerAddMeta(taskID, action, "", extra)
}

// hermesTurnSweep re-drives owed do-bot turns the process died on. Runs inside
// agentLoopSweep (the one 60s scheduler — never a third). Idempotent: a turn
// that already answered (reply comment after the open) is closed in place
// rather than re-sent, so a crash between "reply posted" and "marker closed"
// cannot double-post.
func (s *Server) hermesTurnSweep() {
	if s.hermes == nil || s.threads == nil || s.threads.private == nil {
		return
	}
	priv := s.threads.private
	for _, id := range priv.TaskIDs() {
		var opens []threads.Comment
		var lastClosed time.Time
		for _, c := range priv.Thread(id) {
			switch c.Action {
			case actTurnOpen:
				opens = append(opens, c)
			case actTurnClosed:
				if c.At.After(lastClosed) {
					lastClosed = c.At
				}
			}
		}
		// the owed chain: every open since the last close (attempts so far)
		var owed []threads.Comment
		for _, c := range opens {
			if c.At.After(lastClosed) {
				owed = append(owed, c)
			}
		}
		if len(owed) == 0 {
			continue
		}
		s.hermes.mu.Lock()
		_, inFlight := s.hermes.running[id]
		s.hermes.mu.Unlock()
		if inFlight {
			continue // this process is still on it
		}
		open := owed[len(owed)-1]
		agent, _ := open.Meta["agent"].(string)
		phase, _ := open.Meta["phase"].(string)
		intent, _ := open.Meta["intent"].(string)
		text, _ := open.Meta["text"].(string)
		who := agentTokenIdentity(agent)
		// the turn finished but its close was lost with the process: the
		// reply is on the thread, so close the record — never re-send
		if s.hermesTurnAnswered(id, who.ID, open.At) {
			s.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": agent, "phase": phase, "repaired": true})
			continue
		}
		h := s.findHarness(s.agentHarness(agent))
		if len(owed) >= hermesTurnRetries || !s.hermesForked(h) {
			log.Printf("hermes turn %s (%s): giving up after %d attempt(s)", id, phase, len(owed))
			_, _ = s.addThreadEntry(who, id, threads.ActComment,
				"⚠ "+who.Name+" couldn't finish that — the turn was interrupted "+
					fmt.Sprintf("%d time(s); ask again to retry", len(owed)), nil, nil, map[string]any{"hermes": true})
			s.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": agent, "phase": phase, "abandoned": true})
			continue
		}
		log.Printf("hermes turn %s (%s): re-dispatching an interrupted turn (attempt %d)", id, phase, len(owed)+1)
		// re-compose from the durable inputs — the fresh order writes its
		// own turn-open marker, extending the owed chain
		if err := s.spoolTaskWorkOrderAs(h, agent, id, phase, text, intent); err != nil {
			log.Printf("hermes turn %s (%s): re-dispatch: %v", id, phase, err)
		}
	}
}

// hermesTurnAnswered reports whether the agent posted anything visible on the
// thread after the turn opened — the reply itself, or the ⚠ failure note.
func (s *Server) hermesTurnAnswered(taskID, agentID string, since time.Time) bool {
	for _, c := range s.listThread(taskID) {
		if c.Author == agentID && c.At.After(since) {
			return true
		}
	}
	return false
}

// overlayHermesRunning injects in-flight runner turns into the delegation index
// so the panel shows a live "Hermes is thinking" chip — those turns have no
// spool/run for delegationIndex to scan.
func (s *Server) overlayHermesRunning(out map[string]delegationView) {
	if s.hermes == nil {
		return
	}
	s.hermes.mu.Lock()
	defer s.hermes.mu.Unlock()
	for id, t := range s.hermes.running {
		state := "plan-running"
		if t.Phase == "go" {
			state = "go-queued"
		}
		out[id] = delegationView{State: state, Phase: t.Phase, Harness: "hermes", Agent: t.Agent, Started: t.Since}
	}
}
