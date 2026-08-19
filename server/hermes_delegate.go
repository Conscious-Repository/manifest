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
	readTools string            // -t scope for plan/comment (read-only)
	mu        sync.Mutex        // guards running
	running   map[string]string // taskID → phase currently executing
}

// UseHermes wires the do-bot runner. readTools is the read-only toolset scope
// applied to plan/comment turns (the approval-gate pre-stage).
func (s *Server) UseHermes(r *hermes.Runner, readTools string) {
	if r == nil || !r.Enabled() {
		return
	}
	s.hermes = &hermesCfg{runner: r, readTools: readTools, running: map[string]string{}}
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
func (s *Server) startHermesTurn(taskID, phase, intent, prompt string) error {
	s.hermes.mu.Lock()
	if _, busy := s.hermes.running[taskID]; busy {
		s.hermes.mu.Unlock()
		return errBadRequest("Hermes is already working on this — wait for it to finish")
	}
	s.hermes.running[taskID] = phase
	s.hermes.mu.Unlock()
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
	go s.runHermesTurn(taskID, phase, intent, prompt)
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
- {"type":"goals-item","mode":"add|edit|move","level":"rock|milestone","area":"<## heading in goals.md>",
   "title":"...","parentId":"<rock id, for milestone add/move>",
   "targetId":"<goal id, edit/move>","anchorText":"<the goal's current text, edit/move>"} — place/edit/move a goal line (owner's words only)

Each block is filed for the OWNER'S APPROVAL — nothing happens until he
confirms, so file the change and reference it in your brief rather than
claiming it is done. Everything else in your reply is the RESULT brief.
`

// runHermesTurn invokes the CLI and materializes the reply. Always clears the
// in-flight marker.
func (s *Server) runHermesTurn(taskID, phase, intent, prompt string) {
	defer func() {
		s.hermes.mu.Lock()
		delete(s.hermes.running, taskID)
		s.hermes.mu.Unlock()
	}()
	who := agentIdentity("hermes")
	res, err := s.hermes.runner.Run(context.Background(), hermes.Request{
		Prompt:   prompt,
		Session:  hermesSession(taskID), // per-thread continuity, shared with the CLI/Telegram store
		Toolsets: s.hermes.readTools,    // read-only scope until Phase 2's gated execution
	})
	if err != nil {
		log.Printf("hermes turn %s (%s): %v", taskID, phase, err)
		_, _ = s.addThreadEntry(who, taskID, threads.ActComment,
			"⚠ Hermes couldn't finish that — "+err.Error(), nil, nil, map[string]any{"hermes": true})
		return
	}
	persona := ""
	if p, ok := s.persona(intent); ok {
		persona = p.Intent
	}
	s.materializeHermesBrief(taskID, phase, persona, res.Reply)
}

// materializeHermesBrief turns the CLI's reply into the same surfaces the
// excalibur path produces (mirrors agentLoopSweep, but sourced directly from the
// reply rather than a harness run report): a plan brief → the plan record + a
// thread note; a questions brief → a thread question; a non-plan persona reply
// or a fired result → a thread comment.
func (s *Server) materializeHermesBrief(taskID, phase, persona, brief string) {
	brief = strings.TrimSpace(brief)
	if brief == "" || s.threads == nil || s.todoPlans == nil || s.vault == nil {
		return
	}
	who := agentIdentity("hermes")
	meta := map[string]any{"hermes": true}
	rec := s.readPlanRecord(taskID)
	if rec.Assignee == "" { // engagement implies assignment (mirrors the sweep)
		_ = s.setPlanAssignee(taskID, "agent:hermes")
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
		_, _ = s.addThreadEntry(who, taskID, threads.ActComment, text, nil, nil, meta)
		return
	}
	// non-plan persona (brief/info/…) → the whole reply IS the answer.
	if persona != "" && persona != "plan" {
		_, _ = s.addThreadEntry(who, taskID, threads.ActComment, brief, nil, nil, meta)
		return
	}
	// questions-only → post them as dialog, leave the plan untouched.
	questions, questionsOnly := briefQuestions(brief)
	if questionsOnly && questions != "" {
		_, _ = s.addThreadEntry(who, taskID, threads.ActComment, questions, nil, nil, meta)
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
	_, _ = s.addThreadEntry(who, taskID, threads.ActComment, verb, nil, nil, meta)
	if questions != "" { // drift guard: embedded questions still surface as dialog
		_, _ = s.addThreadEntry(who, taskID, threads.ActComment, questions, nil, nil, meta)
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

// overlayHermesRunning injects in-flight runner turns into the delegation index
// so the panel shows a live "Hermes is thinking" chip — those turns have no
// spool/run for delegationIndex to scan.
func (s *Server) overlayHermesRunning(out map[string]delegationView) {
	if s.hermes == nil {
		return
	}
	s.hermes.mu.Lock()
	defer s.hermes.mu.Unlock()
	for id, phase := range s.hermes.running {
		state := "plan-running"
		if phase == "go" {
			state = "go-queued"
		}
		out[id] = delegationView{State: state, Phase: phase, Harness: "hermes", Started: time.Now()}
	}
}

// hermesSession maps a todo to a stable Hermes session name (--resume target),
// so a plan refined across the thread is one continuous conversation. Slashes
// and spaces in composite ids (aion:…, team/…) are flattened.
func hermesSession(taskID string) string {
	r := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return "manifest-todo-" + strings.Trim(r.Replace(strings.TrimSpace(taskID)), "-")
}
