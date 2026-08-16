package server

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

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
	running   map[string]string // todoID → phase currently executing
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

// startHermesTurn kicks off one agent turn in the background (a turn is slow —
// the tool loop). It coalesces: a second call while a turn is in flight for the
// same todo is refused, mirroring the harness double-spool guard.
func (s *Server) startHermesTurn(todoID, phase, intent, prompt string) error {
	s.hermes.mu.Lock()
	if _, busy := s.hermes.running[todoID]; busy {
		s.hermes.mu.Unlock()
		return errBadRequest("Hermes is already working on this — wait for it to finish")
	}
	s.hermes.running[todoID] = phase
	s.hermes.mu.Unlock()
	go s.runHermesTurn(todoID, phase, intent, prompt)
	return nil
}

// runHermesTurn invokes the CLI and materializes the reply. Always clears the
// in-flight marker.
func (s *Server) runHermesTurn(todoID, phase, intent, prompt string) {
	defer func() {
		s.hermes.mu.Lock()
		delete(s.hermes.running, todoID)
		s.hermes.mu.Unlock()
	}()
	who := agentIdentity("hermes")
	res, err := s.hermes.runner.Run(context.Background(), hermes.Request{
		Prompt:   prompt,
		Session:  hermesSession(todoID), // per-thread continuity, shared with the CLI/Telegram store
		Toolsets: s.hermes.readTools,    // read-only scope until Phase 2's gated execution
	})
	if err != nil {
		log.Printf("hermes turn %s (%s): %v", todoID, phase, err)
		_, _ = s.addThreadEntry(who, todoID, threads.ActComment,
			"⚠ Hermes couldn't finish that — "+err.Error(), nil, nil, map[string]any{"hermes": true})
		return
	}
	persona := ""
	if p, ok := s.persona(intent); ok {
		persona = p.Intent
	}
	s.materializeHermesBrief(todoID, phase, persona, res.Reply)
}

// materializeHermesBrief turns the CLI's reply into the same surfaces the
// excalibur path produces (mirrors agentLoopSweep, but sourced directly from the
// reply rather than a harness run report): a plan brief → the plan record + a
// thread note; a questions brief → a thread question; a non-plan persona reply
// or a fired result → a thread comment.
func (s *Server) materializeHermesBrief(todoID, phase, persona, brief string) {
	brief = strings.TrimSpace(brief)
	if brief == "" || s.threads == nil || s.todoPlans == nil || s.vault == nil {
		return
	}
	who := agentIdentity("hermes")
	meta := map[string]any{"hermes": true}
	rec := s.readPlanRecord(todoID)
	if rec.Assignee == "" { // engagement implies assignment (mirrors the sweep)
		_ = s.setPlanAssignee(todoID, "agent:hermes")
	}

	// go phase → the deliverable itself lands in the thread.
	if phase == "go" {
		text := ledger.Snip(brief, 3600) +
			"\n\n— result delivered; review it, then close the item or send it back with a comment"
		_, _ = s.addThreadEntry(who, todoID, threads.ActComment, text, nil, nil, meta)
		return
	}
	// non-plan persona (brief/info/…) → the whole reply IS the answer.
	if persona != "" && persona != "plan" {
		_, _ = s.addThreadEntry(who, todoID, threads.ActComment, brief, nil, nil, meta)
		return
	}
	// questions-only → post them as dialog, leave the plan untouched.
	questions, questionsOnly := briefQuestions(brief)
	if questionsOnly && questions != "" {
		_, _ = s.addThreadEntry(who, todoID, threads.ActComment, questions, nil, nil, meta)
		return
	}
	// plan brief → attach/update the canon plan + a thread note.
	hadPlan := strings.TrimSpace(rec.Plan) != ""
	if err := s.writePlanSection("todo-plans-agent", todoID, "plan", brief); err != nil {
		log.Printf("hermes plan write %s: %v", todoID, err)
		return
	}
	verb := "plan attached to this task — answer in the thread to refine it, edit it directly, or fire to execute"
	if hadPlan {
		verb = "plan updated on this task — answer in the thread to refine it, edit it directly, or fire to execute"
	}
	s.ledger(ledger.Entry{Source: "plan", Kind: "plan.materialized",
		Actor: who.ID, Todo: todoID, Harness: "hermes", Ref: s.readPlanRecord(todoID).Rel})
	_, _ = s.addThreadEntry(who, todoID, threads.ActComment, verb, nil, nil, meta)
	if questions != "" { // drift guard: embedded questions still surface as dialog
		_, _ = s.addThreadEntry(who, todoID, threads.ActComment, questions, nil, nil, meta)
	}
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
func hermesSession(todoID string) string {
	r := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return "manifest-todo-" + strings.Trim(r.Replace(strings.TrimSpace(todoID)), "-")
}
