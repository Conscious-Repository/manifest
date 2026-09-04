package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/tasks"
)

// ---- task coordination state (manifest P1 Phase 1) ----
//
// `[depends:: id, id]` and `[priority:: high|med|low]` are FIELDS on the thin
// task line, in whichever file owns the line (tasks.md or a property's rock
// tree). `blocked`, "blocked by" and "what depends on this" are DERIVED here,
// on every read, across every source the board projects — so a personal
// line can wait on a property line or an aion backlog item, and the answer
// is always the files' current state, never a stored flag that could drift.

// coordinateRows derives State / BlockedBy / Unresolved / Dependents on the
// open rows. known maps every id any source can see → still open?
func coordinateRows(rows []unifiedRow, known map[string]bool) {
	resolve := func(id string) (open, ok bool) {
		open, ok = known[id]
		return open, ok
	}
	dependents := map[string][]string{}
	for i := range rows {
		r := &rows[i]
		r.BlockedBy, r.Unresolved = tasks.ResolveDepends(r.Depends, resolve)
		for _, dep := range r.Depends {
			dependents[dep] = append(dependents[dep], r.ID)
		}
	}
	for i := range rows {
		r := &rows[i]
		r.Dependents = dependents[r.ID]
		switch {
		case len(r.BlockedBy) > 0:
			r.State = "blocked"
		case strings.TrimSpace(r.Waiting) != "":
			r.State = "waiting"
		default:
			r.State = "open"
		}
	}
}

// taskCoord is one id's coordination projection off the live rows (the
// panel payload). Nil when the id is not an open row.
func (s *Server) taskCoord(id string) *tasks.Coord {
	var doc *tasks.Doc
	if s.tasksStore != nil {
		doc, _ = s.tasksStore.Load()
	}
	for _, r := range s.unifiedRows(doc, time.Now()) {
		if r.ID == id {
			return &tasks.Coord{State: r.State, BlockedBy: r.BlockedBy, Unresolved: r.Unresolved, Dependents: r.Dependents}
		}
	}
	return nil
}

// coordMutate routes a coordination write to the file owning the line:
// personal ids → tasks.md, prop: ids → the property's rock tree. Backlog
// items (aion:/re:) carry neither field yet — refused, never silently dropped.
func (s *Server) coordMutate(w http.ResponseWriter, id string, fn func(t *tasks.Task) error) {
	switch {
	case strings.HasPrefix(id, "aion:"), strings.HasPrefix(id, "re:"):
		httpError(w, errBadRequest("backlog items don't carry priority or depends yet — set them on the personal or property line"))
	case strings.HasPrefix(id, "prop:"):
		slug, lineID := splitPropID(id)
		if slug == "" {
			httpError(w, errBadRequest("malformed property todo id"))
			return
		}
		if s.propTaskMutate(w, slug, func(list *realestate.PropertyTaskList) (bool, error) {
			n := list.Find(lineID)
			if n == nil || n.Task == nil {
				return false, nil
			}
			return true, fn(n.Task)
		}) {
			writeJSON(w, s.tasksView())
		}
	default:
		if !s.tasksOK(w) {
			return
		}
		s.tasksMutate(w, func(d *tasks.Doc) (bool, error) {
			_, t := d.Find(id)
			if t == nil {
				return false, nil
			}
			return true, fn(t)
		})
	}
}

// handleTaskPriority — set or clear ("" clears) the closed-set priority.
func (s *Server) handleTaskPriority(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID, Priority string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	p, ok := tasks.NormalizePriority(b.Priority)
	if !ok {
		httpError(w, errBadRequest("priority must be one of "+strings.Join(tasks.Priorities, " · ")+" (or empty to clear)"))
		return
	}
	s.coordMutate(w, b.ID, func(t *tasks.Task) error {
		t.Priority = p
		return nil
	})
}

// handleTaskDepends — replace the list ({depends: [...]}) or edit one edge
// ({add: id} / {remove: id}). An id nobody knows is accepted (it surfaces as
// unresolved on the row); a known target is pinned first so the reference
// survives rewording (the plan-D1 identity contract).
func (s *Server) handleTaskDepends(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID      string
		Depends *[]string
		Add     string
		Remove  string
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if b.Depends == nil && strings.TrimSpace(b.Add) == "" && strings.TrimSpace(b.Remove) == "" {
		httpError(w, errBadRequest("depends, add, or remove is required"))
		return
	}
	// pin targets BEFORE the owning file loads for the mutate — pinTaskID
	// writes tasks.md itself, and a later stale save would drop the pin
	var targets []string
	if b.Depends != nil {
		targets = *b.Depends
	}
	if a := strings.TrimSpace(b.Add); a != "" {
		targets = append(targets, a)
	}
	for _, id := range targets {
		if id = strings.TrimSpace(id); id != "" && id != b.ID {
			s.pinTaskID(id) // false = unknown id; tolerated, surfaced as unresolved
		}
	}
	s.coordMutate(w, b.ID, func(t *tasks.Task) error {
		if b.Depends != nil {
			t.SetDepends(*b.Depends)
		}
		if a := strings.TrimSpace(b.Add); a != "" && a != b.ID {
			t.AddDepends(a)
		}
		if rm := strings.TrimSpace(b.Remove); rm != "" {
			t.RemoveDepends(rm)
		}
		return nil
	})
}
