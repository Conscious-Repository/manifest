package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/todos"
)

// TODOS — the third surface (todos-surface-scope): everything that must
// happen but drives no vision. `to do.md` is truth; every handler is
// load → mutate → save → respond with the fresh view (goals `mutate` idiom).

func (s *Server) UseTodos(st *todos.Store) { s.todosStore = st }

func (s *Server) todosOK(w http.ResponseWriter) bool {
	if s.todosStore == nil {
		http.Error(w, "todos not available", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// todosView is the board payload: the doc + the shared area vocabulary
// (live goals areas — one list, never two).
func (s *Server) todosView() map[string]any {
	doc, err := s.todosStore.Load()
	if err != nil {
		return map[string]any{"domains": []any{}, "areas": []any{}}
	}
	var areas []string
	if s.goals != nil {
		if gd := s.goals.Load(); gd != nil {
			for _, a := range gd.Areas {
				areas = append(areas, a.Name)
			}
		}
	}
	v := doc.View(time.Now())
	return map[string]any{"domains": v.Domains, "areas": areas}
}

func (s *Server) todosMutate(w http.ResponseWriter, fn func(*todos.Doc) (bool, error)) {
	doc, err := s.todosStore.Load()
	if err != nil {
		httpError(w, err)
		return
	}
	ok, err := fn(doc)
	if err != nil {
		httpError(w, err)
		return
	}
	if !ok {
		http.Error(w, "todo not found", http.StatusNotFound)
		return
	}
	if err := s.todosStore.Save(doc); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.todosView())
}

func (s *Server) handleTodosGet(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	_, _ = s.todosStore.Sweep(time.Now()) // keep the live file lean on every read
	writeJSON(w, s.todosView())
}

// handleTodoAdd — quick capture: one line + a domain (blank → Inbox). ≤2s.
func (s *Server) handleTodoAdd(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ Text, Domain string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Text) == "" {
		httpError(w, errBadRequest("text is required"))
		return
	}
	domain := strings.TrimSpace(b.Domain)
	if domain == "" {
		domain = todos.InboxName
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom := d.EnsureDomain(domain)
		dom.Todos = append(dom.Todos, &todos.Todo{
			Text:  strings.Join(strings.Fields(b.Text), " "),
			Added: time.Now().Format("2006-01-02"),
		})
		return true, nil
	})
}

// handleTodoCheck — done/undone (stamps [done::], the sweep key).
func (s *Server) handleTodoCheck(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct {
		ID      string
		Checked bool
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		_, t := d.Find(b.ID)
		if t == nil {
			return false, nil
		}
		t.Checked = b.Checked
		if b.Checked {
			t.Done = time.Now().Format("2006-01-02")
		} else {
			t.Done = ""
		}
		return true, nil
	})
}

// handleTodoUpdate — edit text, move domain, set/clear waiting.
func (s *Server) handleTodoUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct {
		ID      string
		Text    *string
		Domain  *string
		Waiting *string // "" clears (back to open)
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom, t := d.Find(b.ID)
		if t == nil {
			return false, nil
		}
		if b.Text != nil && strings.TrimSpace(*b.Text) != "" {
			t.Text = strings.Join(strings.Fields(*b.Text), " ")
		}
		if b.Waiting != nil {
			t.Waiting = strings.TrimSpace(*b.Waiting)
			if t.Waiting == "" {
				t.Since = ""
			} else if t.Since == "" {
				t.Since = time.Now().Format("2006-01-02")
			}
		}
		if b.Domain != nil && strings.TrimSpace(*b.Domain) != "" && !strings.EqualFold(dom.Name, *b.Domain) {
			var keep []*todos.Todo
			for _, o := range dom.Todos {
				if o != t {
					keep = append(keep, o)
				}
			}
			dom.Todos = keep
			target := d.EnsureDomain(strings.TrimSpace(*b.Domain))
			target.Todos = append(target.Todos, t)
		}
		return true, nil
	})
}

// handleTodoDrop — the stale-nudge's third exit: out of the live file, into
// the archive with a [dropped::] stamp (never deleted).
func (s *Server) handleTodoDrop(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ ID string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if err := s.todosStore.Drop(b.ID, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.todosView())
}
