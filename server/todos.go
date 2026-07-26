package server

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"manifest/approvals"
	"manifest/daily"
	"manifest/goals"
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
	return map[string]any{"domains": v.Domains, "areas": areas, "split": doc.SplitDone()}
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

// handleTodoAdd — quick capture: one line + a domain (blank → Inbox), with
// optional tethers/placement: [rock::]/[issue::] and/or a bucket. ≤2s.
func (s *Server) handleTodoAdd(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ Text, Domain, Rock, Issue, Bucket string }
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
		t := &todos.Todo{
			Text:  strings.Join(strings.Fields(b.Text), " "),
			Rock:  strings.TrimSpace(b.Rock),
			Issue: strings.TrimSpace(b.Issue),
			Added: time.Now().Format("2006-01-02"),
		}
		if bk := strings.TrimSpace(b.Bucket); bk != "" {
			bucket := dom.EnsureBucket(bk)
			bucket.Todos = append(bucket.Todos, t)
		} else {
			dom.Todos = append(dom.Todos, t)
		}
		return true, nil
	})
	if b.Rock != "" {
		s.stampRockMoved(strings.TrimSpace(b.Rock)) // new tethered work = movement
	}
}

// handleIssueAdd — a decision/blocker under the domain's ### issues.
func (s *Server) handleIssueAdd(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ Text, Domain string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Text) == "" || strings.TrimSpace(b.Domain) == "" {
		httpError(w, errBadRequest("text and domain are required"))
		return
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom := d.EnsureDomain(strings.TrimSpace(b.Domain))
		dom.Issues = append(dom.Issues, &todos.Issue{
			Text:  strings.Join(strings.Fields(b.Text), " "),
			Added: time.Now().Format("2006-01-02"),
		})
		return true, nil
	})
}

// handleIssueResolve — resolving = checking with a one-line resolution note
// (the sweep archives it later; nothing is deleted).
func (s *Server) handleIssueResolve(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ ID, Resolution string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if strings.TrimSpace(b.Resolution) == "" {
		httpError(w, errBadRequest("a one-line resolution is required"))
		return
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		_, is := d.FindIssue(b.ID)
		if is == nil {
			return false, nil
		}
		is.Resolution = strings.Join(strings.Fields(b.Resolution), " ")
		is.Checked = true
		is.Done = time.Now().Format("2006-01-02")
		return true, nil
	})
}

// handleTodoToIssue — the stale card's `→ issue` conversion: the line moves
// under ### issues with an auto id (explicit user action, never automatic).
func (s *Server) handleTodoToIssue(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ ID string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom, t := d.Find(b.ID)
		if t == nil {
			return false, nil
		}
		removeTodo(dom, t)
		dom.Issues = append(dom.Issues, &todos.Issue{Text: t.Text, Added: t.Added})
		return true, nil
	})
}

// removeTodo pulls a todo out of its domain (loose or bucket).
func removeTodo(dom *todos.Domain, t *todos.Todo) {
	filter := func(list []*todos.Todo) []*todos.Todo {
		var keep []*todos.Todo
		for _, o := range list {
			if o != t {
				keep = append(keep, o)
			}
		}
		return keep
	}
	dom.Todos = filter(dom.Todos)
	for _, b := range dom.Buckets {
		b.Todos = filter(b.Todos)
	}
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
	var rockID string
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		_, t := d.Find(b.ID)
		if t == nil {
			return false, nil
		}
		t.Checked = b.Checked
		if b.Checked {
			t.Done = time.Now().Format("2006-01-02")
			// rock-tethered completion: stamp the then-current stage for
			// history + count as Rock movement
			if t.Rock != "" {
				rockID = t.Rock
				if t.Stage == "" {
					t.Stage = s.currentStageName(t.Rock)
				}
			}
		} else {
			t.Done = ""
		}
		return true, nil
	})
	if rockID != "" {
		s.stampRockMoved(rockID)
	}
}

// currentStageName is the Rock's first unchecked stage ("" when none).
func (s *Server) currentStageName(rockID string) string {
	if s.goals == nil {
		return ""
	}
	doc := s.goals.Load()
	if _, rock := doc.FindGoal(rockID); rock != nil {
		for _, st := range rock.Children {
			if !st.Checked {
				return st.Text
			}
		}
	}
	return ""
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

// syncTodoTasks mirrors todo-linked daily-note ticks back into `to do.md`
// (the syncGoalTasks contract: on a miss, no write + an approvals note —
// never a guess).
func (s *Server) syncTodoTasks(tasks []daily.Task) {
	if s.todosStore == nil {
		return
	}
	updates := map[string]bool{}
	for _, t := range tasks {
		if t.TodoID != "" {
			updates[t.TodoID] = t.Done
		}
	}
	if len(updates) == 0 {
		return
	}
	missed, err := s.todosStore.SyncChecks(updates, time.Now())
	// rock-tethered completions from the daily note stamp the then-current
	// stage + the Rock's moved:: (same contract as a board check)
	if err == nil {
		if doc, e2 := s.todosStore.Load(); e2 == nil {
			changed := false
			for id, done := range updates {
				if !done {
					continue
				}
				if _, t := doc.Find(id); t != nil && t.Rock != "" {
					if t.Stage == "" {
						t.Stage = s.currentStageName(t.Rock)
						changed = true
					}
					s.stampRockMoved(t.Rock)
				}
			}
			if changed {
				_ = s.todosStore.Save(doc)
			}
		}
	}
	if err != nil || s.approvals == nil || len(missed) == 0 {
		return
	}
	missedSet := map[string]bool{}
	for _, id := range missed {
		missedSet[id] = true
	}
	for _, t := range tasks {
		if t.TodoID != "" && t.Done && missedSet[t.TodoID] {
			_, _ = s.approvals.Propose(approvals.Proposal{
				Agent:  "manifest",
				Action: "Couldn't sync a ticked task to todos",
				Body: "You ticked \"" + t.Text + "\" ([todo:: " + t.TodoID + "]) in the daily manifest, but no matching " +
					"item is in to do.md — it may have been reworded, moved, or dropped. Check the TODOS board if it's still open.",
			})
		}
	}
}

// ---- the ONE TASK SUBSTRATE split (task-substrate spec §7): per active
// Rock, its open frozen task lines move to to do.md with a [rock::] tether
// (keep) or into a new bucket with the Rock closed as a Learn (demote).
// Previewed, user-committed, both files backed up first. ----

type splitRock struct {
	ID           string   `json:"id"`
	Area         string   `json:"area"`
	Rock         string   `json:"rock"`
	OpenTasks    []string `json:"openTasks"` // cleaned text of open frozen lines
	CheckedCount int      `json:"checkedCount"`
}

// splitFrozen partitions a rock's frozen lines: open task texts + whether each
// raw line is open (for removal on apply).
func splitFrozen(rock *goals.Goal) (open []string, checked int) {
	for _, st := range rock.Children {
		for _, ln := range st.Frozen {
			if m := todoLineMatch(ln); m != nil {
				if m[1] == " " {
					open = append(open, cleanFrozenText(m[2]))
				} else {
					checked++
				}
			}
		}
	}
	return open, checked
}

var frozenFieldRe = regexp.MustCompile(`\[([A-Za-z][\w-]*)\s*::\s*([^\]]*)\]`)

// cleanFrozenText strips the legacy [goal:: …] identity off a frozen task line
// (the todo gets a fresh [rock::] tether instead); other fields ride along.
func cleanFrozenText(rest string) string {
	out := frozenFieldRe.ReplaceAllStringFunc(rest, func(m string) string {
		sm := frozenFieldRe.FindStringSubmatch(m)
		if strings.EqualFold(strings.TrimSpace(sm[1]), "goal") {
			return ""
		}
		return m
	})
	return strings.Join(strings.Fields(out), " ")
}

func (s *Server) handleTodosSplit(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) || s.goals == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		gdoc := s.goals.Load()
		rocks := []splitRock{}
		for _, a := range gdoc.Areas {
			for _, rock := range a.Rocks {
				if rock.Checked {
					continue
				}
				open, checked := splitFrozen(rock)
				if len(open) == 0 && checked == 0 {
					continue
				}
				rocks = append(rocks, splitRock{ID: rock.ID, Area: a.Name, Rock: rock.Text,
					OpenTasks: open, CheckedCount: checked})
			}
		}
		tdoc, _ := s.todosStore.Load()
		done := tdoc != nil && tdoc.SplitDone()
		writeJSON(w, map[string]any{"rocks": rocks, "done": done})
	case http.MethodPost:
		var b struct {
			Rocks []struct{ ID, Action, Bucket string } `json:"rocks"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		gdoc := s.goals.Load()
		tdoc, err := s.todosStore.Load()
		if err != nil {
			httpError(w, err)
			return
		}
		today := time.Now().Format("2006-01-02")
		type moveRec struct{ Line, To string }
		var report []moveRec
		var demotes []struct{ ID, Bucket string }
		for _, choice := range b.Rocks {
			area, rock := gdoc.FindGoal(choice.ID)
			if rock == nil {
				httpError(w, errBadRequest("rock not found: "+choice.ID+" — nothing was written"))
				return
			}
			var target func(text string) // where open tasks land
			switch strings.ToLower(choice.Action) {
			case "demote":
				name := strings.TrimSpace(choice.Bucket)
				if name == "" {
					name = rock.Text
				}
				bucket := tdoc.EnsureDomain(area.Name).EnsureBucket(name)
				target = func(text string) {
					bucket.Todos = append(bucket.Todos, &todos.Todo{Text: text, Added: today})
					report = append(report, moveRec{text, area.Name + " / " + name})
				}
				demotes = append(demotes, struct{ ID, Bucket string }{rock.ID, name})
			default: // keep as Rock
				dom := tdoc.EnsureDomain(area.Name)
				rockID := rock.ID
				target = func(text string) {
					dom.Todos = append(dom.Todos, &todos.Todo{Text: text, Rock: rockID, Added: today})
					report = append(report, moveRec{text, area.Name + " · [rock:: " + rockID + "]"})
				}
			}
			// move open frozen lines; checked ones stay frozen in place
			for _, st := range rock.Children {
				var keep []string
				for _, ln := range st.Frozen {
					if m := todoLineMatch(ln); m != nil && m[1] == " " {
						target(cleanFrozenText(m[2]))
						continue
					}
					keep = append(keep, ln)
				}
				st.Frozen = keep
			}
		}
		tdoc.MarkSplitDone(today)
		// backups, then todos, then goals; demote closes run last (they reload)
		if err := backupOnce(s.todosStore.Path(), s.todosStore.Path()+".pre-split"); err != nil {
			httpError(w, err)
			return
		}
		if err := backupOnce(s.goals.Path(), s.goals.Path()+".pre-split"); err != nil {
			httpError(w, err)
			return
		}
		if err := s.todosStore.Save(tdoc); err != nil {
			httpError(w, err)
			return
		}
		if err := s.goals.Save(gdoc); err != nil {
			httpError(w, err)
			return
		}
		for _, d := range demotes {
			if err := s.goals.CloseGoal(d.ID, "learn", "reclassified as bucket", "", time.Now()); err != nil {
				httpError(w, err)
				return
			}
		}
		writeJSON(w, map[string]any{"moved": report, "demoted": len(demotes), "view": s.todosView()})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

var todoLineRe2 = regexp.MustCompile(`^[ \t]*[-*]\s*\[([ xX])\]\s?(.*)$`)

func todoLineMatch(ln string) []string { return todoLineRe2.FindStringSubmatch(ln) }

// backupOnce copies src to dst if dst doesn't exist (the .pre-migration pattern).
func backupOnce(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
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
