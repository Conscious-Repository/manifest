package server

import (
	"net/http"
	"strings"
)

func (s *Server) handleFundraisingSyncStatus(w http.ResponseWriter, _ *http.Request) {
	if s.fundraisingSync == nil {
		writeJSON(w, map[string]any{"enabled": false, "initialized": false, "conflicts": []any{}})
		return
	}
	writeJSON(w, s.fundraisingSync.Status())
}

func (s *Server) handleFundraisingSyncNow(w http.ResponseWriter, r *http.Request) {
	if s.fundraisingSync == nil {
		http.Error(w, "fundraising sheet sync is disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.fundraisingSync.Sync(r.Context()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingSync.Status())
}

func (s *Server) handleFundraisingSyncResolve(w http.ResponseWriter, r *http.Request) {
	if s.fundraisingSync == nil {
		http.Error(w, "fundraising sheet sync is disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		ID     string `json:"id"`
		Field  string `json:"field"`
		Choice string `json:"choice"`
	}
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.ID) == "" || strings.TrimSpace(body.Field) == "" {
		httpError(w, errBadRequest("id and field are required"))
		return
	}
	if err := s.fundraisingSync.Resolve(r.Context(), body.ID, body.Field, body.Choice); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingSync.Status())
}
