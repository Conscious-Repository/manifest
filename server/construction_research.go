package server

// Construction research runs (plan §6, P5): routes and the bounded
// in-process worker. A run advances only when the owner starts, retries or
// resumes it — never on a GET, a refresh or a timer; there is no cron and
// no job daemon. Progress is read from the run document's durable commits;
// the events route projects that sequence (a reconnecting client asks for
// events after the last sequence it saw). At startup every run that was
// running is reconciled to disconnected before anything can dispatch.

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"manifest/construction"
)

// constructionRunTracker is the worker's in-process state: which runs this
// process owns right now (with their cancel) and the concurrency bound.
type constructionRunTracker struct {
	mu     sync.Mutex
	active map[string]context.CancelFunc
	sem    chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

func (t *constructionRunTracker) owns(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.active[key]
	return ok
}

func (s *Server) registerConstructionResearchRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("POST "+p+"/problems/{id}/research-runs", s.handleConstructionRunCreate)
	mux.HandleFunc("GET "+p+"/problems/{id}/research-runs/{run}", s.handleConstructionRun)
	mux.HandleFunc("GET "+p+"/problems/{id}/research-runs/{run}/events", s.handleConstructionRunEvents)
	mux.HandleFunc("GET "+p+"/problems/{id}/research-runs/{run}/attempts/{attempt}/result", s.handleConstructionRunResult)
	for _, action := range []string{"start", "cancel", "retry", "resume", "questions"} {
		mux.HandleFunc("POST "+p+"/problems/{id}/research-runs/{run}/"+action, s.handleConstructionRunAction(action))
	}
}

// ReconcileConstruction marks runs a previous process left running as
// disconnected (consulting native receipts for agent steps) before any
// construction dispatch. main calls it before ResumeAgentChats; handlers
// also ensure it once, so ordering can never let stale work start.
func (s *Server) ReconcileConstruction() {
	if s.construction == nil {
		return
	}
	s.construction.runs.once.Do(func() {
		touched, err := s.construction.store.ReconcileRuns(s.constructionNativeState)
		if err != nil {
			log.Printf("construction: reconcile runs: %v", err)
		}
		if len(touched) > 0 {
			log.Printf("construction: reconciled %d interrupted research run(s); none resumed automatically", len(touched))
		}
	})
}

// constructionNativeState reports what the native chat store knows about an
// agent step's delivery (P8 seam); without a native store nothing is known.
var constructionNativeStateHook func(s *Server, n construction.NativeRef) string

func (s *Server) constructionNativeState(n construction.NativeRef) string {
	if constructionNativeStateHook != nil {
		return constructionNativeStateHook(s, n)
	}
	return ""
}

// constructionRunCapabilities observes what a run can do here right now.
func (s *Server) constructionRunCapabilities() construction.RunCapabilities {
	ready, notes := s.constructionNativeReady()
	return s.construction.runner.Capabilities(ready, notes)
}

// constructionNativeReady is the native preflight (P8 seam).
var constructionNativeReadyHook func(s *Server) (bool, []string)

func (s *Server) constructionNativeReady() (bool, []string) {
	if constructionNativeReadyHook != nil {
		return constructionNativeReadyHook(s)
	}
	return false, []string{"native agent research is unavailable: no native delivery seam is wired"}
}

// startConstructionRun hands a queued run to the bounded worker. A run this
// process already owns is left alone.
func (s *Server) startConstructionRun(sub construction.SubjectRef, pid, runID string) {
	c := s.construction
	key := pid + "/" + runID
	c.runs.mu.Lock()
	if c.runs.active == nil {
		c.runs.active = map[string]context.CancelFunc{}
	}
	if _, busy := c.runs.active[key]; busy {
		c.runs.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.runs.active[key] = cancel
	c.runs.mu.Unlock()
	c.runs.wg.Add(1)
	go func() {
		defer c.runs.wg.Done()
		defer func() {
			c.runs.mu.Lock()
			delete(c.runs.active, key)
			c.runs.mu.Unlock()
			cancel()
		}()
		select {
		case c.runs.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-c.runs.sem }()
		if err := c.runner.Advance(ctx, sub, pid, runID); err != nil {
			log.Printf("construction: research run %s: %v", runID, err)
		}
	}()
}

// stopConstructionRun cancels this process's work on a run (after the stop
// request is durable).
func (s *Server) stopConstructionRun(pid, runID string) {
	c := s.construction
	c.runs.mu.Lock()
	cancel := c.runs.active[pid+"/"+runID]
	c.runs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// stopAllConstructionRuns cancels every in-process run (tests, shutdown).
// The runs' durable state is whatever their last commit says; a later
// start reconciles anything left running.
func (s *Server) stopAllConstructionRuns() {
	if s.construction == nil {
		return
	}
	c := s.construction
	c.runs.mu.Lock()
	for _, cancel := range c.runs.active {
		cancel()
	}
	c.runs.mu.Unlock()
}

// WaitConstructionRuns waits for in-process runs (tests, shutdown).
func (s *Server) WaitConstructionRuns() {
	if s.construction != nil {
		s.construction.runs.wg.Wait()
	}
}

func (s *Server) handleConstructionRunCreate(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	s.ReconcileConstruction()
	body, ok := readConstructionBody(w, r, construction.MaxCommandBytes)
	if !ok {
		return
	}
	req, hash, err := construction.ParseRunRequest(body)
	if err != nil {
		constructionError(w, err)
		return
	}
	pid := r.PathValue("id")
	st, run, rc, err := s.construction.store.CreateRun(sub, pid, req, hash, actor, s.constructionRunCapabilities())
	if err != nil {
		constructionError(w, err)
		return
	}
	if run.State == construction.RunQueued || run.State == construction.RunRunning {
		s.startConstructionRun(sub, pid, run.ID)
	}
	constructionJSON(w, map[string]any{"run": run, "receipt": rc, "view": s.constructionView(sub, st)})
}

// runResults decodes each stage's latest retained result (bounded) for the
// research view; the bytes are read through the problem's membership.
func (s *Server) runResults(sub construction.SubjectRef, pid string, run *construction.ResearchRun) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, sg := range run.Stages {
		at := sg.Completed()
		if at == nil {
			at = sg.LastAttempt()
		}
		if at == nil || at.Result == nil {
			continue
		}
		b, err := s.construction.store.Content(sub, pid, at.Result.ID, at.Result.Revision)
		if err != nil || len(b) > 2<<20 {
			continue
		}
		out[sg.Name] = b
	}
	return out
}

func (s *Server) handleConstructionRun(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid := r.PathValue("id")
	st, err := s.construction.store.Load(sub, pid)
	if err != nil {
		constructionError(w, err)
		return
	}
	run := st.Runs[r.PathValue("run")]
	if run == nil {
		constructionError(w, construction.NotFound("no such run"))
		return
	}
	w.Header().Set("ETag", `"`+st.Revision("run:"+run.ID)+`"`)
	constructionJSON(w, map[string]any{"run": run, "revision": st.Revision("run:" + run.ID), "results": s.runResults(sub, pid, run),
		"owned": s.construction.runs.owns(pid + "/" + run.ID), "notice": construction.NonApprovalNotice})
}

// handleConstructionRunEvents projects the durable event sequence after a
// client's last seen sequence. wait=1 holds the request (bounded) until the
// sequence moves; it never starts or advances work.
func (s *Server) handleConstructionRunEvents(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid, runID := r.PathValue("id"), r.PathValue("run")
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	deadline := time.Now()
	if r.URL.Query().Get("wait") == "1" {
		deadline = deadline.Add(8 * time.Second)
	}
	for {
		st, err := s.construction.store.Load(sub, pid)
		if err != nil {
			constructionError(w, err)
			return
		}
		run := st.Runs[runID]
		if run == nil {
			constructionError(w, construction.NotFound("no such run"))
			return
		}
		if run.Sequence > after || time.Now().After(deadline) || r.Context().Err() != nil {
			events := []construction.RunEvent{}
			for _, e := range run.Events {
				if e.Seq > after {
					events = append(events, e)
				}
			}
			gap := len(run.Events) > 0 && run.Events[0].Seq > after+1
			constructionJSON(w, map[string]any{"runId": run.ID, "state": run.State, "epoch": run.Epoch, "sequence": run.Sequence,
				"events": events, "gap": gap, "generation": st.Head.Generation})
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (s *Server) handleConstructionRunResult(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	pid := r.PathValue("id")
	st, err := s.construction.store.Load(sub, pid)
	if err != nil {
		constructionError(w, err)
		return
	}
	run := st.Runs[r.PathValue("run")]
	if run == nil {
		constructionError(w, construction.NotFound("no such run"))
		return
	}
	for _, sg := range run.Stages {
		for _, at := range sg.Attempts {
			if at.ID != r.PathValue("attempt") || at.Result == nil {
				continue
			}
			b, err := s.construction.store.Content(sub, pid, at.Result.ID, at.Result.Revision)
			if err != nil {
				constructionError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(b)
			return
		}
	}
	constructionError(w, construction.NotFound("no such attempt result"))
}

type constructionRunAction struct {
	SchemaVersion int                     `json:"schemaVersion"`
	RequestID     string                  `json:"requestId"`
	AcceptRuntime bool                    `json:"acceptRuntime,omitempty"`
	Questions     []construction.Question `json:"questions,omitempty"`
}

func (s *Server) handleConstructionRunAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub, actor, ok := s.constructionBegin(w, r, true)
		if !ok {
			return
		}
		s.ReconcileConstruction()
		body, ok := readConstructionBody(w, r, construction.MaxCommandBytes)
		if !ok {
			return
		}
		var in constructionRunAction
		if err := construction.DecodeRequest(body, &in); err != nil {
			constructionError(w, err)
			return
		}
		if in.SchemaVersion != 1 || !construction.ValidRequestID(in.RequestID) {
			constructionError(w, construction.Invalid("schemaVersion 1 and a requestId are required"))
			return
		}
		canon, err := construction.CanonicalizeJSON(body)
		if err != nil {
			constructionError(w, construction.Invalid(err.Error()))
			return
		}
		hash := construction.Token(append([]byte(action+"\x00"), canon...))
		pid, runID := r.PathValue("id"), r.PathValue("run")
		store := s.construction.store
		var run *construction.ResearchRun
		var st *construction.State
		switch action {
		case "start":
			st, run, err = store.StartRun(sub, pid, runID, in.RequestID, actor)
		case "cancel":
			st, run, err = store.StopRun(sub, pid, runID, in.RequestID, hash, actor)
			if err == nil {
				s.stopConstructionRun(pid, runID) // the stop is durable first
			}
		case "retry", "resume":
			st, run, err = store.RestartRun(sub, pid, runID, action, in.AcceptRuntime, in.RequestID, hash, actor)
		case "questions":
			st, run, err = store.SetRunQuestions(sub, pid, runID, in.Questions, in.RequestID, hash, actor)
		}
		if err != nil {
			constructionError(w, err)
			return
		}
		if run.State == construction.RunQueued {
			s.startConstructionRun(sub, pid, runID)
		}
		constructionJSON(w, map[string]any{"run": run, "view": s.constructionView(sub, st)})
	}
}
