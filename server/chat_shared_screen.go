package server

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) AionChatTerminalScreen(w http.ResponseWriter, r *http.Request) {
	s.sharedTerminalScreen(s.kairosAgent(), w, r)
}
func (s *Server) OodaChatTerminalScreen(w http.ResponseWriter, r *http.Request) {
	s.sharedTerminalScreen(s.zeckAgent(), w, r)
}
func (s *Server) sharedTerminalScreen(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	thread, id := r.PathValue("thread"), r.PathValue("terminal")
	se, err := s.sharedTerminal(ag, thread, id)
	if err != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	state, err := s.terminalScreen(ctx, se)
	if _, accessErr := s.sharedTerminal(ag, thread, id); accessErr != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "Terminal screen is unavailable. Reconnect and try again.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, state)
}
