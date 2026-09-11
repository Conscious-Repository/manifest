package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"manifest/chatstate"
)

func (s *Server) UseChatState(root string) { s.chatState = chatstate.New(root) }

// These routes exist only on the private cockpit handler, even when a draft is
// intended for a team conversation. Unsent drafts do not become team content.
func (s *Server) handleChatState(w http.ResponseWriter, r *http.Request) {
	if s.chatProjectsPath != "" && r.PathValue("key") == "inbox" && r.PathValue("slot") == "workstreams" {
		s.handleChatProjects(w, r)
		return
	}
	if s.chatState == nil {
		http.Error(w, "conversation state unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	key, slot := r.PathValue("key"), r.PathValue("slot")
	var result chatstate.Snapshot
	var err error
	if r.Method == http.MethodGet {
		result, err = s.chatState.Read(key, slot)
	} else {
		var b struct {
			Revision *uint64         `json:"revision"`
			Value    json.RawMessage `json:"value"`
		}
		if err := decode(r, &b); err != nil || b.Revision == nil {
			http.Error(w, "expected revision is required", http.StatusBadRequest)
			return
		}
		result, err = s.chatState.Write(key, slot, *b.Revision, b.Value)
	}
	if errors.Is(err, chatstate.ErrConflict) {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, result)
		return
	}
	if errors.Is(err, chatstate.ErrInvalid) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, result)
}
