package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/daily"
	"manifest/goals"
	"manifest/realestate"
	"manifest/record"
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
	out := map[string]any{"domains": v.Domains, "areas": areas, "split": doc.SplitDone()}
	for k, uv := range s.unifiedView(doc) { // stage 4: the projection keys ride the same payload
		out[k] = uv
	}
	return out
}

// pinTodoID freezes a todo's identity BEFORE any panel artifact (description,
// plan, thread, assignment) keys on it (plan D1). Personal and property ids
// are text-derived — they change when the text is edited — so the first
// enrichment pins the current id as an explicit [todo:: id] in the source
// file. Aion ids are already stable. Idempotent; returns the id unchanged and
// whether it resolved to a real todo.
func (s *Server) pinTodoID(id string) (string, bool) {
	switch {
	case strings.HasPrefix(id, "aion:"), strings.HasPrefix(id, "re:"):
		store, bare, okb := s.backlogStoreFor(id)
		if !okb {
			return id, false
		}
		for _, it := range store.LoadBacklog().Items() {
			if it.ID == bare {
				return id, true
			}
		}
		return id, false
	case strings.HasPrefix(id, "prop:"):
		slug, lineID := splitPropID(id)
		if slug == "" {
			return id, false
		}
		return id, s.propTodoPin(slug, lineID)
	default:
		if s.todosStore == nil {
			return id, false
		}
		doc, err := s.todosStore.Load()
		if err != nil {
			return id, false
		}
		_, t := doc.Find(id)
		if t == nil {
			return id, false
		}
		if t.ExplicitID() != "" {
			return id, true // already pinned — no write
		}
		if !doc.Promote(id) {
			return id, false
		}
		return id, s.todosStore.Save(doc) == nil
	}
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
// Stage 4: a Container can target a property or the aion backlog directly —
// the line lands in THAT file (never a parallel copy here).
func (s *Server) handleTodoAdd(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct {
		Text, Domain, Rock, Issue, Bucket, Owner string
		Stage                                    string // milestone/stage placement (goals rocks + property stages)
		Container                                struct{ Kind, Slug string }
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Text) == "" {
		httpError(w, errBadRequest("text is required"))
		return
	}
	switch b.Container.Kind {
	case "property":
		if s.propTodoMutate(w, b.Container.Slug, func(list realestate.PropertyTodoList) (realestate.PropertyTodoList, bool, error) {
			return list.Append(&todos.Todo{
				Text:  strings.Join(strings.Fields(b.Text), " "),
				Owner: strings.TrimSpace(b.Owner),
				Stage: strings.TrimSpace(b.Stage),
				Added: time.Now().Format("2006-01-02"),
			}), true, nil
		}) {
			writeJSON(w, s.todosView())
		}
		return
	case "aion", "re":
		store := s.aion
		if b.Container.Kind == "re" {
			store = s.re
		}
		if store == nil {
			http.Error(w, b.Container.Kind+" backlog not available", http.StatusServiceUnavailable)
			return
		}
		if err := store.AddItem(&aion.BacklogItem{
			Kind:     aion.KindTask,
			Text:     strings.Join(strings.Fields(b.Text), " "),
			Owner:    strings.TrimSpace(b.Owner),
			Status:   aion.StatusOpen,
			Captured: time.Now().Format("2006-01-02"),
		}); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, s.todosView())
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
			Stage: strings.TrimSpace(b.Stage),
			Issue: strings.TrimSpace(b.Issue),
			Owner: strings.TrimSpace(b.Owner),
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

// handleBucketRename — edit a bucket's display name in place; its slug pins
// so links and board references survive.
func (s *Server) handleBucketRename(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ Domain, Slug, Name string }
	if err := decode(r, &b); err != nil || b.Slug == "" || strings.TrimSpace(b.Name) == "" {
		httpError(w, errBadRequest("domain, slug, and name are required"))
		return
	}
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom := d.Domain(b.Domain)
		if dom == nil {
			return false, nil
		}
		for _, bk := range dom.Buckets {
			if strings.EqualFold(bk.Slug, b.Slug) {
				bk.Rename(b.Name)
				return true, nil
			}
		}
		return false, nil
	})
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

// handleIssueToTodo — the reverse conversion: an issue becomes a plain task
// line (optionally tethered to a rock/stage in the same motion — the goals
// outline's "→ rock…" on an unanchored issue). Explicit user action.
func (s *Server) handleIssueToTodo(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ ID, Rock, Stage string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	var movedRock string
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom, is := d.FindIssue(b.ID)
		if is == nil {
			return false, nil
		}
		var keep []*todos.Issue
		for _, o := range dom.Issues {
			if o != is {
				keep = append(keep, o)
			}
		}
		dom.Issues = keep
		t := &todos.Todo{
			Text:  is.Text,
			Added: is.Added,
			Rock:  strings.TrimSpace(b.Rock),
			Stage: strings.TrimSpace(b.Stage),
		}
		if t.Added == "" {
			t.Added = time.Now().Format("2006-01-02")
		}
		movedRock = t.Rock
		dom.Todos = append(dom.Todos, t)
		return true, nil
	})
	if movedRock != "" {
		s.stampRockMoved(movedRock)
	}
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
	// composite ids route to the owning file (stage 4): completed anywhere =
	// completed everywhere, because there is only one line in one file.
	if strings.HasPrefix(b.ID, "prop:") {
		s.propTodoCheck(w, b.ID, b.Checked)
		return
	}
	if strings.HasPrefix(b.ID, "aion:") || strings.HasPrefix(b.ID, "re:") {
		s.backlogTodoCheck(w, b.ID, b.Checked)
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
		Owner   *string // "" clears (back to mine) — stage 4 assignment
		Rock    *string // "" untethers — the rock this line advances
		Stage   *string // optional stage placement within the rock's trail
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if strings.HasPrefix(b.ID, "prop:") {
		slug, lineID := splitPropID(b.ID)
		if slug == "" {
			httpError(w, errBadRequest("malformed property todo id"))
			return
		}
		if s.propTodoMutate(w, slug, func(list realestate.PropertyTodoList) (realestate.PropertyTodoList, bool, error) {
			t := list.Find(lineID)
			if t == nil {
				return list, false, nil
			}
			if b.Text != nil && strings.TrimSpace(*b.Text) != "" {
				t.Text = strings.Join(strings.Fields(*b.Text), " ")
			}
			if b.Owner != nil {
				t.Owner = strings.TrimSpace(*b.Owner)
			}
			if b.Stage != nil {
				// stage vocabulary = THIS property's own `## stages` names
				// ("" clears — the task rides the current stage again)
				want := strings.TrimSpace(*b.Stage)
				if want != "" {
					matched := ""
					if p, found := s.realestate.Get(slug); found {
						for _, st := range p.Work {
							if strings.EqualFold(st.Text, want) {
								matched = st.Text
								break
							}
						}
					}
					if matched == "" {
						return list, false, errBadRequest("no stage named " + want + " on this property")
					}
					want = matched
				}
				t.Stage = want
			}
			return list, true, nil
		}) {
			writeJSON(w, s.todosView())
		}
		return
	}
	if strings.HasPrefix(b.ID, "aion:") || strings.HasPrefix(b.ID, "re:") {
		store, bare, okb := s.backlogStoreFor(b.ID)
		if !okb {
			http.Error(w, "backlog not available", http.StatusServiceUnavailable)
			return
		}
		set := map[string]string{}
		if b.Text != nil && strings.TrimSpace(*b.Text) != "" {
			set["title"] = strings.Join(strings.Fields(*b.Text), " ")
		}
		if b.Owner != nil {
			set["owner"] = strings.TrimSpace(*b.Owner)
		}
		if b.Rock != nil {
			set["rock"] = strings.TrimSpace(*b.Rock)
		}
		if len(set) == 0 {
			httpError(w, errBadRequest("nothing to update"))
			return
		}
		if err := store.UpdateItem(bare, set, time.Now()); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, s.todosView())
		return
	}
	var movedRock string
	s.todosMutate(w, func(d *todos.Doc) (bool, error) {
		dom, t := d.Find(b.ID)
		if t == nil {
			return false, nil
		}
		if b.Text != nil && strings.TrimSpace(*b.Text) != "" {
			t.Text = strings.Join(strings.Fields(*b.Text), " ")
		}
		if b.Owner != nil {
			t.Owner = strings.TrimSpace(*b.Owner)
		}
		if b.Rock != nil {
			t.Rock = strings.TrimSpace(*b.Rock)
			if t.Rock == "" {
				t.Stage = "" // untethering clears the stage placement too
			} else {
				movedRock = t.Rock // new tethered work = movement
			}
		}
		if b.Stage != nil {
			t.Stage = strings.TrimSpace(*b.Stage)
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
	if movedRock != "" {
		s.stampRockMoved(movedRock) // same contract as capture/check
	}
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
		// aion-backed ticks route through syncAionTasks, not the to-do.md store —
		// leaving them here would flag every one as a "missed" approval nudge
		if t.TodoID != "" && !strings.HasPrefix(t.TodoID, "aion:") {
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
				if m[2] == " " {
					open = append(open, cleanFrozenText(m[3]))
				} else {
					checked++
				}
			}
		}
	}
	return open, checked
}

var frozenFieldRe = record.FieldRe // the kernel grammar

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
					if m := todoLineMatch(ln); m != nil && m[2] == " " {
						target(cleanFrozenText(m[3]))
						continue
					}
					keep = append(keep, ln)
				}
				st.Frozen = keep
			}
		}
		tdoc.MarkSplitDone(today)
		// backups, then todos, then goals; demote closes run last (they reload)
		if err := s.todosStore.BackupOnce(".pre-split"); err != nil {
			httpError(w, err)
			return
		}
		if err := s.goals.BackupOnce(".pre-split"); err != nil {
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

// todoLineMatch applies the kernel checkbox grammar: indent m[1], state m[2],
// rest m[3].
func todoLineMatch(ln string) []string { return record.CheckboxRe.FindStringSubmatch(ln) }

// handleTodoDrop — the stale-nudge's third exit: out of the live file, into
// the archive with a [dropped::] stamp (never deleted). A property todo
// (prop:<slug>/<line>) is simply removed from its `## todos` section — the
// property record is its own history; deletion is the owner's explicit call.
func (s *Server) handleTodoDrop(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct{ ID string }
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if strings.HasPrefix(b.ID, "prop:") {
		slug, lineID := splitPropID(b.ID)
		if slug == "" {
			httpError(w, errBadRequest("malformed property todo id"))
			return
		}
		if s.propTodoMutate(w, slug, func(list realestate.PropertyTodoList) (realestate.PropertyTodoList, bool, error) {
			found := false
			var out realestate.PropertyTodoList
			for _, ln := range list {
				if ln.Todo != nil && ln.Todo.ID == lineID {
					found = true
					continue
				}
				out = append(out, ln)
			}
			return out, found, nil
		}) {
			writeJSON(w, s.todosView())
		}
		return
	}
	// an aion-backed row: hard-remove the backlog item (drop = delete for aion,
	// not archive — there is no to-do.md line to archive)
	if strings.HasPrefix(b.ID, "aion:") || strings.HasPrefix(b.ID, "re:") {
		store, bare, okb := s.backlogStoreFor(b.ID)
		if !okb {
			http.Error(w, "backlog not available", http.StatusServiceUnavailable)
			return
		}
		if err := store.DeleteItem(bare); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, s.todosView())
		return
	}
	if err := s.todosStore.Drop(b.ID, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.todosView())
}
