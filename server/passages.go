package server

import (
	"manifest/vaultwriter"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleWritingSources(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if s.writing == nil {
		http.Error(w, "writing unavailable", 503)
		return
	}
	if r.Method == http.MethodGet {
		sources, err := s.writing.Sources()
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		writeJSON(w, sources)
		return
	}
	var b struct {
		Folders  []string `json:"folders"`
		Revision string   `json:"revision"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if len(b.Folders) > 12 {
		http.Error(w, "choose at most 12 folders", 400)
		return
	}
	for i, p := range b.Folders {
		if p == "" {
			continue
		}
		full, err := vaultwriter.SafePath(s.vault.VaultRoot(), p)
		if err != nil {
			http.Error(w, "invalid source folder", 400)
			return
		}
		fi, err := os.Stat(full)
		if err != nil || !fi.IsDir() {
			http.Error(w, "source folder does not exist", 400)
			return
		}
		b.Folders[i] = filepath.ToSlash(filepath.Clean(p))
	}
	result, err := s.writing.SetSources(b.Folders, b.Revision)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, result)
}
func (s *Server) handleWritingPassages(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if s.writing == nil || s.index == nil {
		http.Error(w, "references unavailable", 503)
		return
	}
	var b struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if len(b.Text) > 12000 {
		http.Error(w, "passage too long", 400)
		return
	}
	// References come from the existing vault; no separate collection setup.
	passages, err := s.index.Passages(strings.TrimSpace(b.Text), b.Path, []string{""}, s.writing.Excluded)
	if err != nil {
		http.Error(w, "reference search failed", 500)
		return
	}
	writeJSON(w, map[string]any{"passages": passages})
}
