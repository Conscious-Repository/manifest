package server

import (
	"net/http"

	"manifest/agents"
)

func (s *Server) handleAgentsStatus(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, map[string]any{
		"enabled":   true,
		"counts":    s.agents.Counts(),
		"outbox":    s.agents.Outbox(10),
		"approvals": s.agents.Approvals("pending"),
	})
}

// handleAgentsPost enqueues a task from the dashboard (hand-post work to agents).
func (s *Server) handleAgentsPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.agents == nil {
		http.Error(w, "agents disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Type string `json:"type"`
		Body string `json:"body"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	t, err := s.agents.Post(agents.Task{Type: b.Type, Body: b.Body})
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": t.ID})
}

func (s *Server) handleApprovalConfirm(w http.ResponseWriter, r *http.Request) {
	s.approvalAction(w, r, func(id, _ string) error { return s.agents.Confirm(id) })
}

func (s *Server) handleApprovalReject(w http.ResponseWriter, r *http.Request) {
	s.approvalAction(w, r, func(id, reason string) error { return s.agents.Reject(id, reason) })
}

func (s *Server) approvalAction(w http.ResponseWriter, r *http.Request, fn func(id, reason string) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.agents == nil {
		http.Error(w, "agents disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := fn(b.ID, b.Reason); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
