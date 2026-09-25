package server

import (
	"context"
	"net/http"
)

type agentChatInvocation struct {
	requestID string
	cancel    context.CancelFunc
}

func (s *Server) handleAgentChatInterrupt(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	var input struct {
		RequestID string `json:"requestId"`
	}
	if err := decode(r, &input); err != nil {
		httpError(w, err)
		return
	}
	c := s.agentChat
	agent, id := r.PathValue("agent"), r.PathValue("id")
	c.runMu.Lock()
	receipt, err := c.store.RequestStop(agent, id, input.RequestID)
	if err == nil {
		if running, ok := c.running[agent+"/"+id]; ok && running.requestID == input.RequestID {
			running.cancel()
		}
	}
	c.runMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]any{"delivery": receipt})
}
