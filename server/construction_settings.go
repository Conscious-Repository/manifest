package server

// Construction workspace settings (2026-10-09): the model Alfred uses for
// construction problem chats — research, approaches, decision points — chosen
// once for the whole workspace instead of following whatever Alfred's
// profile default happens to be. Owner: "astra on high from my codex
// subscription right now, with a picker that includes all models available
// from all my subscriptions". The list is the same catalog the chat composer
// offers (every provider Hermes has listed models for). The 3D models are
// not drawn by this model: geometry is compiled deterministically from the
// approach's typed parameters; the model decides what those are.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"manifest/construction"
)

type constructionSettings struct {
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

// the starting choice when nothing is saved: Astra on high through the Codex
// subscription, if this machine lists it
var constructionDefaultModel = constructionSettings{Model: "gpt-6-astra", Provider: "openai-codex", Effort: "high"}

var constructionSettingsMu sync.Mutex

func (s *Server) constructionSettingsPath() string {
	return filepath.Join(s.construction.store.Root(), "workspace-settings.json")
}

// constructionSettingsNow is the saved choice, else the default when this
// machine lists it, else none (the profile default).
func (s *Server) constructionSettingsNow() (constructionSettings, bool) {
	constructionSettingsMu.Lock()
	defer constructionSettingsMu.Unlock()
	var cur constructionSettings
	if b, err := os.ReadFile(s.constructionSettingsPath()); err == nil && json.Unmarshal(b, &cur) == nil && cur.Model != "" {
		return cur, true
	}
	d := constructionDefaultModel
	if s.hermesChoiceError(d.Model, d.Provider, d.Effort) == nil {
		return d, false
	}
	return constructionSettings{}, false
}

func (s *Server) registerConstructionSettingsRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("GET "+p+"/settings", s.handleConstructionSettingsGet)
	mux.HandleFunc("PUT "+p+"/settings", s.handleConstructionSettingsPut)
}

func (s *Server) constructionSettingsView() map[string]any {
	cur, saved := s.constructionSettingsNow()
	agent := alfredAgent
	cat := s.hermesModelCatalog("")
	return map[string]any{"settings": cur, "saved": saved, "agent": agent, "models": cat.Models, "efforts": cat.Efforts,
		"default": constructionDefaultModel}
}

func (s *Server) handleConstructionSettingsGet(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.constructionBegin(w, r, false); !ok {
		return
	}
	constructionJSON(w, s.constructionSettingsView())
}

func (s *Server) handleConstructionSettingsPut(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.constructionBegin(w, r, true); !ok {
		return
	}
	raw, ok := readConstructionBody(w, r, 8<<10)
	if !ok {
		return
	}
	var in constructionSettings
	if err := json.Unmarshal(raw, &in); err != nil {
		constructionError(w, construction.Invalid("send {model, provider, effort}"))
		return
	}
	in.Model, in.Provider, in.Effort = strings.TrimSpace(in.Model), strings.TrimSpace(in.Provider), strings.TrimSpace(in.Effort)
	if in.Model == "" || in.Provider == "" {
		constructionError(w, construction.Invalid("choose a model and its provider"))
		return
	}
	if err := s.hermesChoiceError(in.Model, in.Provider, in.Effort); err != nil {
		constructionError(w, construction.Invalid(err.Error()))
		return
	}
	b, _ := json.MarshalIndent(in, "", "  ")
	constructionSettingsMu.Lock()
	err := writeFileAtomic(s.constructionSettingsPath(), append(b, '\n'), 0o600)
	constructionSettingsMu.Unlock()
	if err != nil {
		constructionError(w, construction.Unavailable(err.Error()))
		return
	}
	constructionJSON(w, s.constructionSettingsView())
}
