package server

// Construction Intelligence (owner-approved plan, docs/construction-intelligence.md):
// private construction problems bound to an existing property or the shared
// Home, entered from those pages. This file composes the construction domain
// with the server: routes, subject/scope resolution against the read-only
// source records, and JSON views. The domain (manifest/construction) owns
// validation, commits and geometry; source records are only ever read here.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"manifest/construction"
	"manifest/ledger"
	"manifest/realestate"
	"manifest/tasks"
)

type constructionCfg struct {
	store        *construction.Store
	trustedHosts map[string]bool
	nonce        string
	principal    constructionPrincipal
	mu           sync.Mutex
	geometry     geometryCache
	runner       *construction.Runner
	runs         constructionRunTracker
	fixture      bool // the synthetic fixture source adapter is wired (tests only)
	opts         ConstructionOptions
}

// ConstructionOptions configure the feature at composition time.
type ConstructionOptions struct {
	// TrustedHosts are the owner's private entry hosts (e.g. the tailnet
	// name) accepted in the Host header besides loopback.
	TrustedHosts []string
	// Forbidden roots the store must not live under (vault, team/public dirs).
	Forbidden []string
	Now       func() time.Time
	Failpoint func(string) error
	// FixtureSources wires the synthetic fixture source adapter from that
	// directory (testdata/roof-wall). Tests only: production never sets it.
	FixtureSources string
	// MaxConcurrentRuns bounds in-process research workers (default 2).
	MaxConcurrentRuns int
}

// UseConstruction opens the private construction store at root (outside the
// vault) and enables the construction routes on the private handler.
func (s *Server) UseConstruction(root string, o ConstructionOptions) error {
	st, err := construction.Open(root, construction.Options{
		Forbidden: o.Forbidden,
		Now:       o.Now,
		Failpoint: o.Failpoint,
		Publish:   s.publishConstruction,
	})
	if err != nil {
		return err
	}
	hosts := map[string]bool{}
	for _, h := range o.TrustedHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts[h] = true
		}
	}
	runner := &construction.Runner{Store: st, Adapters: []construction.SourceAdapter{construction.ImportedAdapter{}}, Now: o.Now}
	fixture := false
	if o.FixtureSources != "" {
		fx, err := construction.LoadFixtureAdapter(o.FixtureSources)
		if err != nil {
			st.Close()
			return err
		}
		runner.Adapters = append(runner.Adapters, fx)
		fixture = true
	}
	n := o.MaxConcurrentRuns
	if n <= 0 {
		n = 2
	}
	s.construction = &constructionCfg{store: st, trustedHosts: hosts, nonce: newConstructionNonce(), runner: runner, fixture: fixture, opts: o,
		runs: constructionRunTracker{active: map[string]context.CancelFunc{}, sem: make(chan struct{}, n)}}
	if constructionUseHook != nil {
		constructionUseHook(s)
	}
	return nil
}

// constructionUseHook lets later phases (native delivery) attach to the
// runner once the store is open.
var constructionUseHook func(s *Server)

// publishConstruction projects a commit into the daily ledger. The ledger is
// never commit authority; with no ledger wired there is nothing to publish.
func (s *Server) publishConstruction(sub construction.SubjectRef, problemID string, gen int64, events []construction.LedgerEvent) error {
	if s.ledgerStore == nil {
		return nil
	}
	for _, e := range events {
		err := s.ledgerStore.Append(ledger.Entry{TS: time.Now(), Source: "construction", Kind: e.Kind, Actor: "owner",
			Object: ledger.Object{Kind: "construction-problem", ID: problemID}, Text: ledger.Snip(e.Text, 280),
			Meta: map[string]any{"subject": sub.Kind + ":" + sub.ID, "generation": gen}})
		if err != nil {
			return err
		}
	}
	return nil
}

// registerConstructionRoutes mounts the private construction API. It is
// called from Handler only — never from a portal, share or public handler.
func (s *Server) registerConstructionRoutes(mux *http.ServeMux) {
	for _, p := range []string{"/api/properties/{slug}/construction", "/api/home/construction"} {
		mux.HandleFunc("GET "+p+"/session", s.handleConstructionSession)
		mux.HandleFunc("GET "+p+"/problems", s.handleConstructionProblems)
		mux.HandleFunc("POST "+p+"/problems", s.handleConstructionCreate)
		mux.HandleFunc("GET "+p+"/problems/{id}", s.handleConstructionProblem)
		mux.HandleFunc("POST "+p+"/problems/{id}/commands", s.handleConstructionCommands)
		mux.HandleFunc("GET "+p+"/problems/{id}/history", s.handleConstructionHistory)
		mux.HandleFunc("POST "+p+"/problems/{id}/inputs", s.handleConstructionInput)
		mux.HandleFunc("GET "+p+"/problems/{id}/artifacts/{artifact}", s.handleConstructionArtifact)
		s.registerConstructionAssemblyRoutes(mux, p)
		s.registerConstructionExportRoutes(mux, p)
		s.registerConstructionResearchRoutes(mux, p)
		s.registerConstructionSourceRoutes(mux, p)
		s.registerConstructionCatalogRoutes(mux, p)
		s.registerConstructionCommandRoutes(mux, p)
	}
}

// constructionSubject derives the typed subject from the URL. It never
// switches subject from a body field or header.
func constructionSubject(r *http.Request) (construction.SubjectRef, error) {
	if strings.HasPrefix(r.URL.Path, "/api/home/construction") {
		return construction.SubjectRef{Kind: construction.SubjectHome, ID: construction.HomeSubjectID}, nil
	}
	sub := construction.SubjectRef{Kind: construction.SubjectProperty, ID: r.PathValue("slug")}
	if err := construction.ValidSubject(sub); err != nil {
		return sub, construction.NotFound("no such property")
	}
	return sub, nil
}

// constructionBegin runs the guard and resolves subject (+ problem id).
func (s *Server) constructionBegin(w http.ResponseWriter, r *http.Request, mutation bool) (construction.SubjectRef, construction.Actor, bool) {
	actor, ok := s.constructionGuard(w, r, mutation)
	if !ok {
		return construction.SubjectRef{}, construction.Actor{}, false
	}
	sub, err := constructionSubject(r)
	if err != nil {
		constructionError(w, err)
		return sub, actor, false
	}
	return sub, actor, true
}

func readConstructionBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			constructionError(w, construction.ErrTooLarge)
		} else {
			constructionError(w, construction.Invalid("could not read the request body"))
		}
		return nil, false
	}
	return b, true
}

// ---- subject + scope resolution (read-only) ---------------------------------------

type constructionSubjectView struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Status  string `json:"status"` // resolved | missing | unavailable
	Title   string `json:"title"`
	Address string `json:"address,omitempty"`
}

// subjectRecord looks the subject up in its source (never writes it).
func (s *Server) constructionSubjectView(sub construction.SubjectRef) (constructionSubjectView, *realestate.Property) {
	v := constructionSubjectView{Kind: sub.Kind, ID: sub.ID, Status: "unavailable"}
	switch sub.Kind {
	case construction.SubjectProperty:
		if s.realestate == nil {
			return v, nil
		}
		p, ok := s.realestate.Get(sub.ID)
		if !ok {
			v.Status = "missing"
			return v, nil
		}
		v.Status, v.Title, v.Address = "resolved", firstNonEmptyStr(p.Short, p.Name, p.Slug), p.Address
		return v, &p
	case construction.SubjectHome:
		v.Title = "Home"
		if s.tasksStore != nil {
			v.Status = "resolved"
		}
	}
	return v, nil
}

// homeTasks are the shared Home tasks by id (text + completion), read-only.
func (s *Server) constructionHomeTasks() map[string]tasks.Task {
	out := map[string]tasks.Task{}
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
		dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) { out[t.ID] = *t })
	}
	return out
}

type constructionScopeView struct {
	WorkID          string  `json:"workId,omitempty"`
	TaskID          string  `json:"taskId,omitempty"`
	Text            string  `json:"text"`
	Checked         bool    `json:"checked"`
	Est             float64 `json:"est,omitempty"`
	Status          string  `json:"status"` // resolved | unresolved
	SourceRevision  string  `json:"sourceRevision,omitempty"`
	CurrentRevision string  `json:"currentRevision,omitempty"`
	Changed         bool    `json:"changed"` // the source moved since it was linked
}

type constructionTaskView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}

// resolvePropertyScope finds an exact work id (rock or node) on a property.
func resolvePropertyScope(p *realestate.Property, sc construction.ScopeRef) (constructionScopeView, []constructionTaskView, bool) {
	var view constructionScopeView
	var children []constructionTaskView
	found := false
	for i := range p.Work {
		st := &p.Work[i]
		if st.ID == sc.WorkID && (sc.TaskID == "" || sc.TaskID == st.ID) {
			view = constructionScopeView{WorkID: st.ID, TaskID: sc.TaskID, Text: st.Text, Checked: st.Checked, Est: st.EstTotal, Status: "resolved"}
			for _, n := range st.Tasks {
				children = append(children, constructionTaskView{ID: n.TaskID(), Text: n.Task.Text, Checked: n.Task.Checked})
			}
			found = true
		}
	}
	if !found {
		realestate.WalkNodes(p.Work, func(_ *realestate.WorkStage, n *realestate.WorkNode) {
			if found || n.ID != sc.WorkID || (sc.TaskID != "" && sc.TaskID != n.TaskID()) {
				return
			}
			view = constructionScopeView{WorkID: n.ID, TaskID: n.TaskID(), Text: n.Task.Text, Checked: n.Task.Checked, Est: n.EstTotal, Status: "resolved"}
			for _, c := range n.Children {
				children = append(children, constructionTaskView{ID: c.TaskID(), Text: c.Task.Text, Checked: c.Task.Checked})
			}
			found = true
		})
	}
	if found {
		_, view.CurrentRevision, _ = construction.TokenOf(map[string]any{"kind": "property-work", "property": p.Slug, "workId": view.WorkID, "text": view.Text, "checked": view.Checked})
	}
	return view, children, found
}

// constructionScopeResolver validates a requested scope against the source
// record by exact id and stamps the source revision it saw.
func (s *Server) constructionScopeResolver(sub construction.SubjectRef) func(construction.ScopeRef) (construction.ScopeRef, error) {
	return func(sc construction.ScopeRef) (construction.ScopeRef, error) {
		switch sub.Kind {
		case construction.SubjectProperty:
			_, p := s.constructionSubjectView(sub)
			if p == nil {
				return sc, construction.Invalid("the property record is not available to link a scope")
			}
			if sc.WorkID == "" {
				return sc, construction.Invalid("a property scope names an exact work id")
			}
			view, _, ok := resolvePropertyScope(p, sc)
			if !ok {
				return sc, construction.Invalid("no work item with that exact id on this property (ids are never fuzzy-matched)")
			}
			return construction.ScopeRef{WorkID: view.WorkID, TaskID: sc.TaskID, SourceRevision: view.CurrentRevision}, nil
		case construction.SubjectHome:
			if sc.WorkID != "" || sc.TaskID == "" {
				return sc, construction.Invalid("a Home scope names one shared Home task id")
			}
			t, ok := s.constructionHomeTasks()[sc.TaskID]
			if !ok {
				return sc, construction.Invalid("no shared Home task with that exact id")
			}
			_, rev, _ := construction.TokenOf(map[string]any{"kind": "home-task", "id": t.ID, "text": t.Text, "done": t.Checked})
			return construction.ScopeRef{TaskID: t.ID, SourceRevision: rev}, nil
		}
		return sc, construction.Invalid("unknown subject")
	}
}

// constructionContextView is the read-only projection of the linked source:
// budget, scope and its tasks. Nothing here is written back.
type constructionContextView struct {
	Subject constructionSubjectView `json:"subject"`
	Budget  any                     `json:"budget"`
	Scope   *constructionScopeView  `json:"scope"`
	Tasks   []constructionTaskView  `json:"tasks"`
}

func (s *Server) constructionContext(sub construction.SubjectRef, p *construction.Problem) constructionContextView {
	sv, prop := s.constructionSubjectView(sub)
	ctx := constructionContextView{Subject: sv, Tasks: []constructionTaskView{}}
	if prop != nil && prop.Project != nil {
		ctx.Budget = prop.Project
	}
	if p == nil || p.ScopeRef == nil {
		return ctx
	}
	sc := *p.ScopeRef
	unresolved := &constructionScopeView{WorkID: sc.WorkID, TaskID: sc.TaskID, Status: "unresolved", SourceRevision: sc.SourceRevision}
	switch sub.Kind {
	case construction.SubjectProperty:
		if prop == nil {
			ctx.Scope = unresolved
			return ctx
		}
		view, children, ok := resolvePropertyScope(prop, sc)
		if !ok {
			ctx.Scope = unresolved
			return ctx
		}
		view.SourceRevision = sc.SourceRevision
		view.Changed = sc.SourceRevision != "" && sc.SourceRevision != view.CurrentRevision
		ctx.Scope, ctx.Tasks = &view, nonNilTasks(children)
	case construction.SubjectHome:
		t, ok := s.constructionHomeTasks()[sc.TaskID]
		if !ok {
			ctx.Scope = unresolved
			return ctx
		}
		_, rev, _ := construction.TokenOf(map[string]any{"kind": "home-task", "id": t.ID, "text": t.Text, "done": t.Checked})
		ctx.Scope = &constructionScopeView{TaskID: t.ID, Text: t.Text, Checked: t.Checked, Status: "resolved",
			SourceRevision: sc.SourceRevision, CurrentRevision: rev, Changed: sc.SourceRevision != "" && sc.SourceRevision != rev}
	}
	return ctx
}

func nonNilTasks(x []constructionTaskView) []constructionTaskView {
	if x == nil {
		return []constructionTaskView{}
	}
	return x
}

// ---- handlers -------------------------------------------------------------------------

func (s *Server) handleConstructionSession(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	sv, _ := s.constructionSubjectView(sub)
	constructionJSON(w, map[string]any{
		"nonce":     s.construction.nonce,
		"principal": "owner",
		"subject":   sv,
		"versions": map[string]any{"schema": construction.SchemaVersion, "convention": construction.CoordinateConvention,
			"compiler": construction.CompilerVersion},
		"limits": map[string]any{"inputBytes": construction.MaxInputBytes, "opsPerRequest": construction.MaxOpsPerRequest,
			"components": construction.MaxComponents, "triangles": construction.MaxTriangles},
		"operations":   construction.OperationNames(),
		"templates":    construction.TemplateNames(),
		"capabilities": s.constructionCapabilities(),
		"boundary":     "trusted-local-host: loopback or configured private hosts, same-origin, per-process mutation nonce",
	})
}

func (s *Server) handleConstructionProblems(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	list, err := s.construction.store.List(sub)
	if err != nil {
		constructionError(w, err)
		return
	}
	sv, _ := s.constructionSubjectView(sub)
	constructionJSON(w, map[string]any{"subject": sv, "problems": list})
}

func (s *Server) handleConstructionCreate(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	body, ok := readConstructionBody(w, r, construction.MaxCommandBytes)
	if !ok {
		return
	}
	req, hash, err := construction.ParseCreate(body)
	if err != nil {
		constructionError(w, err)
		return
	}
	sv, _ := s.constructionSubjectView(sub)
	if sv.Status != "resolved" {
		// a new problem binds only to a subject that exists; existing
		// problems of a vanished subject stay readable as orphans
		constructionError(w, construction.NotFound("the "+sub.Kind+" is not available to bind a new problem"))
		return
	}
	var scope *construction.ScopeRef
	if req.Scope != nil {
		resolved, err := s.constructionScopeResolver(sub)(*req.Scope)
		if err != nil {
			constructionError(w, err)
			return
		}
		scope = &resolved
	}
	st, rc, err := s.construction.store.CreateProblem(sub, req, hash, actor, scope)
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"receipt": rc, "view": s.constructionView(sub, st)})
}

func (s *Server) handleConstructionProblem(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	st, err := s.construction.store.Load(sub, r.PathValue("id"))
	if err != nil {
		constructionError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+st.Head.Commit.Revision+`"`)
	constructionJSON(w, s.constructionView(sub, st))
}

func (s *Server) handleConstructionCommands(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	body, ok := readConstructionBody(w, r, construction.MaxCommandBytes)
	if !ok {
		return
	}
	pc, err := construction.ParseCommand(body)
	if err != nil {
		constructionError(w, err)
		return
	}
	if pc.Command.ProblemID != r.PathValue("id") {
		constructionError(w, construction.Invalid("problemId does not match the URL"))
		return
	}
	s.executeConstructionCommand(w, sub, pc, actor)
}

func (s *Server) executeConstructionCommand(w http.ResponseWriter, sub construction.SubjectRef, pc *construction.ParsedCommand, actor construction.Actor) {
	st, rc, err := s.construction.store.ExecuteCommand(sub, pc, actor, &construction.ApplyContext{ResolveScope: s.constructionScopeResolver(sub)})
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"receipt": rc, "view": s.constructionView(sub, st)})
}

func (s *Server) handleConstructionHistory(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	h, err := s.construction.store.History(sub, r.PathValue("id"), 500)
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"history": h})
}

// constructionView is a problem's full JSON view: documents, their exact
// revision tokens, and the read-only source projection.
func (s *Server) constructionView(sub construction.SubjectRef, st *construction.State) map[string]any {
	revs := map[string]string{}
	for k, d := range st.Head.Docs {
		revs[k] = d.Revision
	}
	v := map[string]any{
		"problem":        st.Problem,
		"revisions":      revs,
		"generation":     st.Head.Generation,
		"updatedAt":      st.Head.UpdatedAt,
		"readOnly":       st.ReadOnly,
		"context":        s.constructionContext(sub, st.Problem),
		"assemblies":     st.Assemblies,
		"validation":     st.Validation,
		"decisions":      st.Decisions,
		"decisionStates": construction.DecisionStates(st),
		"runs":           st.Runs,
		"views":          st.Views,
		"catalog":        st.Catalog,
		"evidence":       st.Evidence,
		"derived":        st.Derived,
		"notice":         construction.NonApprovalNotice,
	}
	if st.ReadOnly {
		raw := map[string]json.RawMessage{}
		for k, b := range st.Raw {
			raw[k] = b
		}
		v["rawDocuments"] = raw
	}
	return v
}

// constructionCapabilities reports what this server can actually do for
// construction right now — observed, never assumed from configuration.
func (s *Server) constructionCapabilities() map[string]any {
	caps := s.constructionRunCapabilities()
	return map[string]any{
		"autonomousAcquisition": caps.AutonomousAcquisition,
		"nativeAgent":           caps.NativeAgent,
		"pdfExtraction":         caps.PDFExtraction,
		"notes":                 caps.Notes,
		"fixtureSources":        s.construction.fixture,
		"agentMutationTool":     s.constructionAgentToolState(),
		"blender":               "absent",
	}
}

// constructionAgentToolState reports the agent command capability (P7 seam).
var constructionAgentToolHook func(s *Server) string

func (s *Server) constructionAgentToolState() string {
	if constructionAgentToolHook != nil {
		return constructionAgentToolHook(s)
	}
	return "unavailable"
}
