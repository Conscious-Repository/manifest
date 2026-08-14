package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Sticky (cmd-ctr import P4): the ⌘I floating post-it. One markdown file in
// dataDir — deliberately NOT the vault (scratch, not knowledge; the owner
// promotes anything worth keeping by hand).

// UseSticky wires the sticky-note path (<dataDir>/sticky.md).
func (s *Server) UseSticky(path string) { s.stickyPath = path }

func (s *Server) handleStickyGet(w http.ResponseWriter, r *http.Request) {
	if s.stickyPath == "" {
		writeJSON(w, map[string]string{"body": ""})
		return
	}
	b, err := os.ReadFile(s.stickyPath)
	if err != nil {
		writeJSON(w, map[string]string{"body": ""})
		return
	}
	writeJSON(w, map[string]string{"body": string(b)})
}

func (s *Server) handleStickyPut(w http.ResponseWriter, r *http.Request) {
	if s.stickyPath == "" {
		http.Error(w, "sticky disabled", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	if err != nil {
		httpError(w, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.stickyPath), 0o755); err != nil {
		httpError(w, err)
		return
	}
	if err := os.WriteFile(s.stickyPath, body, 0o644); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
