package server

import (
	"net/http"

	"manifest/hermes"
)

// Cron management + observability (Step 4) — thin proxies to Hermes /api/jobs and
// /api/sessions. Surfaces schedule + last-run health so nothing runs silently.

func (s *Server) handleJobsList(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	jobs, err := s.hermes.ListJobs(r.Context())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"data": jobs})
}

func (s *Server) handleJobCreate(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	var in hermes.JobInput
	if err := decode(r, &in); err != nil {
		httpError(w, err)
		return
	}
	job, err := s.hermes.CreateJob(r.Context(), in)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, job)
}

func (s *Server) handleJobUpdate(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	var in hermes.JobInput
	if err := decode(r, &in); err != nil {
		httpError(w, err)
		return
	}
	job, err := s.hermes.UpdateJob(r.Context(), r.PathValue("id"), in)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, job)
}

func (s *Server) handleJobDelete(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.hermes.DeleteJob(r.Context(), r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSessionsList surfaces recent runs (observability): source, model, tokens, cost.
func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	sessions, err := s.hermes.ListSessions(r.Context(), r.URL.Query().Get("source"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"data": sessions})
}
