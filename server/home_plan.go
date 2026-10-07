package server

// The shared Home plan (homeplan): one revision-checked document beside the
// shared Home tasks, read and patched by both planners and by the planning
// chat through the same two calls. docs/home-plan.md is the contract.
import (
	"encoding/json"
	"errors"
	"io"
	"manifest/homeplan"
	"manifest/tasks"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // the household's calendar date, even on a host without zoneinfo
)

func (s *Server) UseHomePlan(sharedRoot string, write func(string, []byte) error) {
	if sharedRoot == "" {
		return
	}
	s.homePlan = &homeplan.Store{Path: filepath.Join(sharedRoot, "plan.json"), Write: write}
}

// homePlanTasks: every shared Home task by ID (text + completion), the join
// the plan's foreign keys resolve against.
func (s *Server) homePlanTasks() map[string]homeplan.TaskRef {
	out := map[string]homeplan.TaskRef{}
	if s.tasksStore == nil {
		return out
	}
	d, err := s.tasksStore.Load()
	if err != nil {
		return out
	}
	for _, dom := range d.Domains {
		if !strings.EqualFold(dom.Name, "Home") {
			continue
		}
		dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) {
			out[t.ID] = homeplan.TaskRef{Text: t.Text, Done: t.Checked}
		})
	}
	return out
}

func homePlanToday(p *homeplan.Plan) string {
	tz := "America/Chicago"
	if p != nil && p.Timezone != "" {
		tz = p.Timezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

type homePlanView struct {
	Revision string                      `json:"revision"`
	Plan     *homeplan.Plan              `json:"plan"`
	Derived  *homeplan.Derived           `json:"derived,omitempty"`
	Tasks    map[string]homeplan.TaskRef `json:"tasks"` // every shared Home task
}

func (s *Server) homePlanRespond(w http.ResponseWriter, asOf string) {
	p, _, rev, err := s.homePlan.Read()
	all := s.homePlanTasks()
	if errors.Is(err, homeplan.ErrNoPlan) {
		writeJSON(w, homePlanView{Tasks: all})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	refs := map[string]homeplan.TaskRef{}
	for id := range p.Tasks {
		ref, ok := all[id]
		if !ok {
			ref = homeplan.TaskRef{Text: id, Missing: true}
		}
		refs[id] = ref
	}
	if asOf == "" {
		asOf = homePlanToday(p)
	}
	d := homeplan.Derive(p, refs, asOf)
	writeJSON(w, homePlanView{Revision: rev, Plan: p, Derived: &d, Tasks: all})
}

func homePlanError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	var conflict *homeplan.ConflictError
	var invalid *homeplan.ValidationError
	switch {
	case errors.As(err, &conflict):
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"error": conflict.Error(), "revision": conflict.Current})
	case errors.As(err, &invalid):
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": invalid.Error(), "problems": invalid.Problems})
	default:
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
	}
}

// GET  /api/home/plan[?asOf=YYYY-MM-DD]  → {revision, plan, derived, tasks}
// POST /api/home/plan {revision, patch}  → the same, after the merge patch
func (s *Server) handleHomePlan(w http.ResponseWriter, r *http.Request) {
	if s.homePlan == nil {
		http.Error(w, "the shared Home plan is not configured", http.StatusServiceUnavailable)
		return
	}
	asOf := r.URL.Query().Get("asOf")
	if asOf != "" {
		if _, err := time.Parse("2006-01-02", asOf); err != nil {
			http.Error(w, "asOf must be YYYY-MM-DD", 400)
			return
		}
	}
	if r.Method == http.MethodPost {
		var b struct {
			Revision string          `json:"revision"`
			Patch    json.RawMessage `json:"patch"`
		}
		body, err := io.ReadAll(r.Body)
		if err == nil {
			err = json.Unmarshal(body, &b)
		}
		if err != nil || len(b.Patch) == 0 || b.Patch[0] != '{' {
			homePlanError(w, &homeplan.ValidationError{Problems: []string{"send {\"revision\": \"…\", \"patch\": {…}}"}})
			return
		}
		all := s.homePlanTasks()
		known := func(id string) bool { _, ok := all[id]; return ok }
		if _, _, err := s.homePlan.Apply(b.Revision, b.Patch, known, time.Now()); err != nil {
			homePlanError(w, err)
			return
		}
	}
	s.homePlanRespond(w, asOf)
}

// POST /api/home/plan/preview {patch} → {plan, derived} for the saved plan
// with the patch merged in — a draft or scenario's numbers. Writes nothing.
func (s *Server) handleHomePlanPreview(w http.ResponseWriter, r *http.Request) {
	if s.homePlan == nil {
		http.Error(w, "the shared Home plan is not configured", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Patch json.RawMessage `json:"patch"`
	}
	if err := decode(r, &b); err != nil || len(b.Patch) == 0 || b.Patch[0] != '{' {
		homePlanError(w, &homeplan.ValidationError{Problems: []string{"send {\"patch\": {…}}"}})
		return
	}
	p, raw, rev, err := s.homePlan.Read()
	if err != nil {
		homePlanError(w, err)
		return
	}
	merged, err := homeplan.MergePatch(raw, b.Patch)
	var draft *homeplan.Plan
	if err == nil {
		draft, err = homeplan.Decode(merged)
	}
	all := s.homePlanTasks()
	if err == nil {
		var added []string
		for id := range draft.Tasks {
			if _, ok := p.Tasks[id]; !ok {
				added = append(added, id)
			}
		}
		err = homeplan.Validate(draft, func(id string) bool { _, ok := all[id]; return ok }, added)
	}
	if err != nil {
		var invalid *homeplan.ValidationError
		if !errors.As(err, &invalid) {
			err = &homeplan.ValidationError{Problems: []string{err.Error()}}
		}
		homePlanError(w, err)
		return
	}
	refs := map[string]homeplan.TaskRef{}
	for id := range draft.Tasks {
		ref, ok := all[id]
		if !ok {
			ref = homeplan.TaskRef{Text: id, Missing: true}
		}
		refs[id] = ref
	}
	d := homeplan.Derive(draft, refs, homePlanToday(draft))
	writeJSON(w, homePlanView{Revision: rev, Plan: draft, Derived: &d, Tasks: all})
}

// GET /api/home/plan/history → {revision, history: [names, newest first]}
// POST /api/home/plan/restore {revision, name} → the plan, restored
func (s *Server) handleHomePlanHistory(w http.ResponseWriter, r *http.Request) {
	if s.homePlan == nil {
		http.Error(w, "the shared Home plan is not configured", http.StatusServiceUnavailable)
		return
	}
	_, _, rev, _ := s.homePlan.Read()
	writeJSON(w, map[string]any{"revision": rev, "history": s.homePlan.History()})
}

func (s *Server) handleHomePlanRestore(w http.ResponseWriter, r *http.Request) {
	if s.homePlan == nil {
		http.Error(w, "the shared Home plan is not configured", http.StatusServiceUnavailable)
		return
	}
	var b struct{ Revision, Name string }
	if err := decode(r, &b); err != nil {
		http.Error(w, "send {\"revision\", \"name\"}", 400)
		return
	}
	if _, err := s.homePlan.Restore(b.Revision, b.Name, nil, time.Now()); err != nil {
		homePlanError(w, err)
		return
	}
	s.homePlanRespond(w, "")
}
