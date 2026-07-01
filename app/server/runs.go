package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Async agent runs. On-demand agent work (a feed scan, an on-demand scout, an
// ea-coordinator draft) can take minutes and dozens of tool calls — far longer than a
// browser is willing to hold a request open. So the run handlers kick the work onto a
// goroutine, return a runId immediately (202), and the frontend polls /api/agents/runs/{id}.
// The goroutine's context is decoupled from the HTTP request (a generous standalone cap),
// so the run survives the response and materializes into the local store when it finishes.

type runState struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`   // "feed" | "approvals"
	Status  string    `json:"status"` // "running" | "done" | "error"
	New     int       `json:"new"`    // items/proposals materialized (when done)
	Err     string    `json:"error"`  // failure reason (when error)
	Started time.Time `json:"-"`
}

// runMax bounds a single background run — a safety net, not the expected duration.
const runMax = 15 * time.Minute

// runRetention is how long a finished run stays queryable for the poller.
const runRetention = 30 * time.Minute

// startRun registers a run, launches fn on a goroutine bound to a fresh (request-independent)
// context, and returns the run id. fn returns the count of newly materialized items.
func (s *Server) startRun(kind string, fn func(ctx context.Context) (int, error)) string {
	s.runsMu.Lock()
	if s.runs == nil {
		s.runs = map[string]*runState{}
	}
	s.runSeq++
	id := fmt.Sprintf("run-%d", s.runSeq)
	st := &runState{ID: id, Kind: kind, Status: "running", Started: time.Now()}
	s.runs[id] = st
	s.pruneRunsLocked()
	s.runsMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runMax)
		defer cancel()
		n, err := fn(ctx)
		s.runsMu.Lock()
		if err != nil {
			st.Status = "error"
			st.Err = err.Error()
		} else {
			st.Status = "done"
			st.New = n
		}
		s.runsMu.Unlock()
	}()
	return id
}

// runStatus returns a snapshot of a run's state.
func (s *Server) runStatus(id string) (runState, bool) {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	st, ok := s.runs[id]
	if !ok {
		return runState{}, false
	}
	return *st, true
}

// pruneRunsLocked drops finished runs past the retention window. Caller holds runsMu.
func (s *Server) pruneRunsLocked() {
	cutoff := time.Now().Add(-runRetention)
	for id, st := range s.runs {
		if st.Status != "running" && st.Started.Before(cutoff) {
			delete(s.runs, id)
		}
	}
}

// handleRunStatus is the poll endpoint: GET /api/agents/runs/{id}.
func (s *Server) handleRunStatus(w http.ResponseWriter, r *http.Request) {
	st, ok := s.runStatus(r.PathValue("id"))
	if !ok {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, st)
}

// writeRunAccepted responds 202 with the run id the frontend polls. Content-Type is set
// before WriteHeader (headers are frozen once the status is written).
func writeRunAccepted(w http.ResponseWriter, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"runId": id, "status": "running"})
}
