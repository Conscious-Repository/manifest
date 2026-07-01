package server

import (
	"net/http"

	"manifest/profiles"
)

// Profiles (Step 2) — CRUD over local markdown presets. The tool picker is populated
// from Hermes /v1/toolsets so you can only grant tools that exist.

func (s *Server) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		writeJSON(w, map[string]any{"data": []any{}})
		return
	}
	writeJSON(w, map[string]any{"data": s.profiles.List()})
}

func (s *Server) handleProfileSave(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		http.Error(w, "profiles disabled", http.StatusServiceUnavailable)
		return
	}
	var p profiles.Profile
	if err := decode(r, &p); err != nil {
		httpError(w, err)
		return
	}
	saved, err := s.profiles.Save(p)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		http.Error(w, "profiles disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.profiles.Delete(r.PathValue("name")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleHermesToolsets proxies /v1/toolsets — the profile tool-picker source.
func (s *Server) handleHermesToolsets(w http.ResponseWriter, r *http.Request) {
	if s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "hermes not configured", http.StatusServiceUnavailable)
		return
	}
	sets, err := s.hermes.ListToolsets(r.Context())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"data": sets})
}
