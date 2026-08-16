package signals

import (
	"strconv"
	"time"

	"manifest/tasks"
)

// Stale-todo signals (todos-surface-scope §4 "quiet pressure"): an OPEN todo
// past ~14 days gets exactly one feed signal asking for a decision (do · mark
// waiting · drop); a WAITING todo ages from its waiting-since date with a
// longer 30-day fuse. Conditions auto-clear the moment the todo changes state;
// the hash includes state + an age bucket so a dismissal re-arms on real change.

const (
	staleOpenDays    = 14
	staleWaitingDays = 30
)

// TaskLoader is the todos surface the emitter needs (tasks.Store).
type TaskLoader interface{ Load() (*tasks.Doc, error) }

// StaleTasks emits one card per aging open/waiting todo.
func StaleTasks(l TaskLoader) Emitter { return todoEmitter{l} }

type todoEmitter struct{ l TaskLoader }

func (e todoEmitter) Emit(now time.Time) ([]Signal, error) {
	doc, err := e.l.Load()
	if err != nil {
		return nil, err
	}
	var out []Signal
	for _, dom := range doc.Domains {
		dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) {
			// rock-tethered todos are exempt — the rock-stalled signal owns
			// that rhythm (no double nagging); issues/backlog never enter here
			if t.Rock != "" {
				return
			}
			age := t.AgeDays(now)
			bucket := strconv.Itoa(age / 7) // week bucket: dismissals re-arm as it keeps aging
			switch t.State() {
			case "open":
				if age < staleOpenDays {
					return
				}
				out = append(out, Signal{
					ID:      "todo-stale:" + t.ID,
					Kind:    "todo-stale",
					Entity:  t.Text,
					Label:   "stale todo · " + t.Text + " · " + strconv.Itoa(age) + "d",
					Age:     age,
					ActHref: "#/tasks",
					Hash:    "open|" + bucket,
					GoalID:  t.ID, // carries the todo id for the card's decision pills
				})
			case "waiting":
				if age < staleWaitingDays {
					return
				}
				out = append(out, Signal{
					ID:      "todo-waiting:" + t.ID,
					Kind:    "todo-waiting",
					Entity:  t.Text,
					Label:   "still waiting · " + t.Waiting + " · " + t.Text + " · " + strconv.Itoa(age) + "d",
					Age:     age,
					ActHref: "#/tasks",
					Hash:    "waiting|" + t.Since + "|" + bucket,
					GoalID:  t.ID,
				})
			}
		})
	}
	return out, nil
}
