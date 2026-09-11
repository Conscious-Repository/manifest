package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

type chatCodingModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type chatCodingCatalog struct {
	Default string            `json:"default"`
	Models  []chatCodingModel `json:"models"`
}

// The installed CLI cache is metadata only. Hidden/internal models are omitted;
// deployment policy supplies defaults and the fallback when no cache exists.
func chatCodingModels(cache []byte) map[string]chatCodingCatalog {
	out := map[string]chatCodingCatalog{}
	for kind, policy := range codingModels {
		c := chatCodingCatalog{Default: policy.best}
		for _, id := range policy.allowed {
			c.Models = append(c.Models, chatCodingModel{id, id})
		}
		out[kind] = c
	}
	var cached struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(cache, &cached) == nil {
		c := out["codex"]
		seen := map[string]bool{}
		var models []chatCodingModel
		for _, m := range cached.Models {
			if m.Visibility != "list" || m.Slug == "" || seen[m.Slug] {
				continue
			}
			seen[m.Slug] = true
			label := m.DisplayName
			if label == "" {
				label = m.Slug
			}
			models = append(models, chatCodingModel{m.Slug, label})
		}
		if len(models) > 0 {
			for _, m := range c.Models {
				if !seen[m.ID] {
					models = append(models, m)
				}
			}
			c.Models = models
			out["codex"] = c
		}
	}
	return out
}
func (s *Server) handleChatCodingModels(w http.ResponseWriter, r *http.Request) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".codex")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "models_cache.json"))
	writeJSON(w, chatCodingModels(data))
}
