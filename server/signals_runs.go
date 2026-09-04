package server

// Run-failure FEED cards (owner ask 2026-08-11 + big-change Phase 7): a ritual
// that ran and FAILED pages the human in the feed. One signal per
// harness/spirit/ritual whose LATEST terminal run inside the window failed —
// auto-clears the moment a newer run of the same pair completes (the §5
// auto-clearing rule); a dismissal re-arms on the NEXT failure (hash = run id).

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"manifest/signals"
	"manifest/threads"
)

// runFailureWindow: failures older than this stop paging (the run list still
// holds them; decay is the medium's norm).
const runFailureWindow = 48 * time.Hour

// RunFailureEmitter adapts the harness federation to the signals contract.
// Lazy over s.eachHarness() — wiring order in main doesn't matter.
func (s *Server) RunFailureEmitter() signals.Emitter { return runFailEmitter{s} }

// DelegationDoneEmitter (owner ask 2026-08-11, re-routed 2026-08-12): delegated
// work whose run COMPLETED while the todo is still open — "your result is
// ready". Still a §5 SIGNAL (an app-derived condition that auto-clears when the
// todo gets checked — the closed set stays four), but the client renders it as
// a full FEED CARD in the main stream instead of a one-line strip chip: view
// the result, open the todo, Done ✓. The label navigates to the todo (#/tasks);
// the RESULT is a separate, explicit action, so a click on the work never
// lands in an unrelated run report.
func (s *Server) DelegationDoneEmitter() signals.Emitter { return delegDoneEmitter{s} }

// PlanReadyEmitter (todo-panel plan Phase 4): an assigned agent's PLAN came
// back — the emitter first MATERIALIZES the brief into the todo's record
// (## plan, `todo-plans-agent`, idempotent by run id — the §12 lane), then
// pages "review the plan →". Auto-clears on fire (the go-phase run outranks
// plan-ready) or when the todo closes.
func (s *Server) PlanReadyEmitter() signals.Emitter { return planReadyEmitter{s} }

// TaskAgentReplyEmitter projects the newest unanswered agent comment on each
// open task into FEED. It is deliberately computed on the existing signals
// read/sweep cadence: thread files stay canonical, and no third scheduler or
// second transcript writer is introduced.
func (s *Server) TaskAgentReplyEmitter() signals.Emitter { return taskAgentReplyEmitter{s} }

type taskAgentReplyEmitter struct{ s *Server }

func (e taskAgentReplyEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	if e.s.tasksStore == nil || e.s.threads == nil {
		return []signals.Signal{}, nil
	}
	doc, err := e.s.tasksStore.Load()
	if err != nil {
		return nil, err
	}
	out := []signals.Signal{}
	for _, row := range e.s.unifiedRows(doc, now) {
		thread := e.s.listThread(row.ID)
		if len(thread) == 0 {
			continue
		}
		last := thread[len(thread)-1]
		// A reply is actionable until the owner writes back. Promoted chat
		// history is context, not a new task reply, and stays out of FEED.
		if last.Action != threads.ActComment || !strings.HasPrefix(last.Author, "agent:") ||
			last.Text == "" || threadCommentFromChat(last) {
			continue
		}
		persona, _ := last.Meta["persona"].(string)
		out = append(out, signals.Signal{
			ID:   "task-agent-reply:" + row.ID,
			Kind: "task-agent-reply", Entity: row.Text,
			Label:   "agent replied · " + row.Text,
			ActHref: "#/tasks/" + url.PathEscape(row.ID),
			Hash:    last.ID, GoalID: row.ID,
			Reply: last.Text, ReplyAuthor: orStr(last.AuthorName, strings.TrimPrefix(last.Author, "agent:")),
			ReplyPersona: persona, ReplyAt: last.At.Format(time.RFC3339),
		})
	}
	return out, nil
}

func threadCommentFromChat(c threads.Comment) bool {
	from, _ := c.Meta["from"].(string)
	return from == "chat"
}

type planReadyEmitter struct{ s *Server }

func (e planReadyEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	s := e.s
	index := s.delegationIndex()
	s.agentLoopSweep(index)
	if s.threads == nil || s.threads.private == nil {
		return out, nil
	}
	// signal state derives from the PRIVATE structural trail: the newest of
	// {questions, plan, fire, result} decides what (if anything) pages —
	// answering/firing naturally clears the card because a newer marker lands.
	for _, id := range s.threads.private.TaskIDs() {
		var newest string
		var newestAt time.Time
		var run, harness string
		for _, c := range s.threads.private.Thread(id) {
			switch c.Action {
			case "questions", "plan", "fire", "result":
				if c.At.After(newestAt) {
					newest, newestAt = c.Action, c.At
					run, _ = c.Meta["run"].(string)
					if h, _ := c.Meta["harness"].(string); h != "" {
						harness = h
					}
				}
			}
		}
		if newest != "questions" && newest != "plan" {
			continue
		}
		if harness == "" {
			harness = s.agentHarness(s.readPlanRecord(id).Assignee)
		}
		text, open := s.openTaskText(id)
		if !open {
			continue
		}
		if newest == "questions" {
			// the owner already answered → the ball is back with the agent;
			// the card clears until hermes' next brief lands
			answered := false
			for _, c := range s.listThread(id) {
				if c.Action == "comment" && !strings.HasPrefix(c.Author, "agent:") && c.At.After(newestAt) {
					answered = true
					break
				}
			}
			if answered {
				continue
			}
			out = append(out, signals.Signal{
				ID:      "agent-questions:" + id,
				Kind:    "agent-questions",
				Entity:  text,
				Label:   "agent has questions · " + text + " · " + harness,
				ActHref: "#/tasks/" + id,
				Hash:    run,
				GoalID:  id,
				RunID:   run,
				Harness: harness,
			})
			continue
		}
		out = append(out, signals.Signal{
			ID:      "plan-ready:" + id,
			Kind:    "plan-ready",
			Entity:  text,
			Label:   "plan ready · " + text + " · " + harness,
			ActHref: "#/tasks/" + id,
			Hash:    run,
			GoalID:  id,
			RunID:   run,
			Harness: harness,
		})
	}
	return out, nil
}

type delegDoneEmitter struct{ s *Server }

func (e delegDoneEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	for id, d := range e.s.delegationIndex() {
		if d.State != "done" || d.RunID == "" {
			continue
		}
		text, open := e.s.openTaskText(id)
		if !open {
			continue // human already closed it — nothing to page
		}
		out = append(out, signals.Signal{
			ID:           "delegated-done:" + id,
			Kind:         "delegation-done",
			Entity:       text,
			Label:        "delegated work ready · " + text + " · " + d.Harness,
			ActHref:      "#/tasks",
			Hash:         d.RunID,
			GoalID:       id, // Done ✓ checks the todo through the unified endpoint
			RunID:        d.RunID,
			ArtifactRef:  d.ArtifactRef, // the deliverable, when the run wrote one
			ArtifactPath: d.ArtifactPath,
			Harness:      d.Harness,
		})
	}
	return out, nil
}

// openTaskText resolves a unified composite id to (text, still-open).
func (s *Server) openTaskText(id string) (string, bool) {
	switch {
	case strings.HasPrefix(id, "aion:"), strings.HasPrefix(id, "re:"):
		if store, bare, ok := s.backlogStoreFor(id); ok {
			if it := store.LoadBacklog().Find(bare); it != nil {
				return it.Text, !it.Checked
			}
		}
		return id, false
	case strings.HasPrefix(id, "prop:"):
		if s.realestate != nil {
			slug, lineID := splitPropID(id)
			if list, _, ok := s.realestate.LoadTasks(slug); ok {
				if n := list.Find(lineID); n != nil {
					return n.Task.Text, !n.Task.Checked
				}
			}
		}
		return id, false
	default:
		if s.tasksStore != nil {
			if doc, err := s.tasksStore.Load(); err == nil {
				if _, t := doc.Find(id); t != nil {
					return t.Text, !t.Checked
				}
			}
		}
		return id, false
	}
}

// EngineDownEmitter (Phase 7): a harness whose heartbeat is stale WHILE work
// is queued in its spool is a down engine with a backlog — page the feed.
// Auto-clears when the heartbeat freshens or the queue drains (a laptop dev
// dashboard with an empty spool stays quiet by construction).
func (s *Server) EngineDownEmitter() signals.Emitter { return engineDownEmitter{s} }

type engineDownEmitter struct{ s *Server }

func (e engineDownEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	for _, h := range e.s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		alive, at := h.Spirits.EngineAlive()
		if alive {
			continue
		}
		queued := len(h.Spirits.Queued())
		if queued == 0 {
			continue // nothing waiting — silence is fine
		}
		age := 0
		if !at.IsZero() {
			age = int(now.Sub(at).Hours() / 24)
		}
		out = append(out, signals.Signal{
			ID:      "engine-down:" + h.Name,
			Kind:    "engine-down",
			Entity:  h.Name,
			Label:   "engine down · " + h.Name + " · " + strconv.Itoa(queued) + " queued",
			Age:     age,
			ActHref: "#/agents",
			Hash:    at.Format(time.RFC3339) + ":" + strconv.Itoa(queued),
		})
	}
	return out, nil
}

type runFailEmitter struct{ s *Server }

func (e runFailEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	for _, h := range e.s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		// newest terminal run per spirit/ritual (Runs() is newest-first per store)
		latest := map[string]bool{} // spirit/ritual → seen a terminal run
		for _, r := range h.Spirits.Runs() {
			if r.Outcome == "running" || r.Outcome == "" {
				continue
			}
			key := r.Spirit + "/" + r.Ritual
			if latest[key] {
				continue // an older run — the newest terminal one already decided
			}
			latest[key] = true
			if r.Outcome == "completed" {
				continue
			}
			started, err := time.Parse(time.RFC3339, r.Started)
			if err != nil || now.Sub(started) > runFailureWindow {
				continue
			}
			label := "run failed · " + key + " · " + r.Outcome
			// the WHY, not just the kind — "error (protocol)" alone sent the
			// owner into the artifacts; the report's outcome line says it all
			if d := r.OutcomeDetail; d != "" {
				if cut := strings.Index(d, " — "); cut >= 0 {
					d = d[cut+len(" — "):] // the fm outcome already leads the label
				}
				if len(d) > 120 {
					d = d[:120] + "…"
				}
				label += " — " + d
			}
			if tag := e.s.harnessTag(h.Name); tag != "" {
				label += " · " + tag
			}
			out = append(out, signals.Signal{
				ID:      "run-failed:" + h.Name + "/" + key,
				Kind:    "run-failed",
				Entity:  key,
				Label:   label,
				Age:     int(now.Sub(started).Hours() / 24),
				ActHref: "#/agents",
				Hash:    r.ID, // a NEW failure re-arms a dismissal
			})
		}
	}
	return out, nil
}
