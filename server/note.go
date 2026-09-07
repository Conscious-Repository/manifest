package server

import (
	"errors"
	"manifest/vaultwriter"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// UNIVERSAL NOTE VIEW (plans contacts power-pass §1). Read any vault note, save
// its raw markdown (user write), toggle a checkbox line, and resolve a
// [[wikilink]] target to where it should open. Reads go through the index; the
// two writes go through the vaultwriter and reindex the file.

// noteBacklink is a note linking the viewed note, for the backlinks strip.
type noteBacklink struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Date string `json:"date"`
}

func (s *Server) handleNoteGet(w http.ResponseWriter, r *http.Request) {
	if s.index == nil {
		http.Error(w, "index disabled", http.StatusServiceUnavailable)
		return
	}
	rel := r.URL.Query().Get("path")
	full, ok := safeVaultPath(s.index.VaultRoot(), rel)
	if !ok {
		httpError(w, errBadRequest("invalid note path"))
		return
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		http.Error(w, "note not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSuffix(filepath.Base(rel), ".md")
	// backlinks to THIS note's name (dated first), AI-authored excluded
	var backlinks []noteBacklink
	if bls, err := s.index.Backlinks(strings.ToLower(name)); err == nil {
		for _, b := range bls {
			if b.AIAuthored {
				continue
			}
			backlinks = append(backlinks, noteBacklink{Path: b.Path, Name: b.Name, Date: b.Date})
		}
	}
	isPerson := false
	if e, ok := s.index.Entity(strings.ToLower(name)); ok {
		isPerson = e.IsPerson
	}
	// engine-owned notes (system/excalibur, system/agents) are read-only — the
	// write guard refuses them, so the UI hides the edit affordance.
	readOnly := s.vault == nil || !s.vault.CanUserWrite(filepath.ToSlash(rel)) || !utf8.Valid(raw) || len(raw) > 4<<20
	writeJSON(w, map[string]any{
		"path": filepath.ToSlash(rel), "name": name, "raw": string(raw), "revision": vaultwriter.Revision(raw), "vaultID": vaultwriter.Revision([]byte(s.index.VaultRoot())),
		"backlinks": backlinks, "isPerson": isPerson,
		"zone":     s.index.NoteZone(filepath.ToSlash(rel)), // "system" → quiet SYSTEM badge
		"readOnly": readOnly,
		"vault":    filepath.Base(s.index.VaultRoot()), // for the obsidian:// URI
	})
}

func (s *Server) handleNotePut(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if s.index == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Path       string `json:"path"`
		Body       string `json:"body"`
		IfRevision string `json:"ifRevision"`
	}
	if err := decode(r, &b); err != nil || b.Path == "" {
		httpError(w, errBadRequest("path is required"))
		return
	}
	if b.IfRevision == "" {
		http.Error(w, "ifRevision is required", http.StatusPreconditionRequired)
		return
	}
	revision, err := s.vault.WriteNoteIfRevision(b.Path, b.Body, b.IfRevision)
	if err != nil {
		noteWriteError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{b.Path})
	writeJSON(w, map[string]any{"ok": true, "revision": revision})
}

func (s *Server) handleNoteTask(w http.ResponseWriter, r *http.Request) {
	if s.index == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Path       string `json:"path"`
		Line       int    `json:"line"`
		Want       bool   `json:"want"`
		IfRevision string `json:"ifRevision"`
	}
	if err := decode(r, &b); err != nil || b.Path == "" {
		httpError(w, errBadRequest("path and line are required"))
		return
	}
	if b.IfRevision == "" {
		http.Error(w, "ifRevision is required", http.StatusPreconditionRequired)
		return
	}
	if _, err := s.vault.ToggleTaskIfRevision(b.Path, b.Line, b.Want, b.IfRevision); err != nil {
		noteWriteError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{b.Path})
	writeJSON(w, map[string]bool{"ok": true})
}

// handleNoteResolve resolves a [[wikilink]] target to where it opens: a person →
// their contact page, another note → the note view, a bare target with links →
// its contact page (which shows backlinks), else missing.
func (s *Server) handleNoteResolve(w http.ResponseWriter, r *http.Request) {
	if s.index == nil {
		http.Error(w, "index disabled", http.StatusServiceUnavailable)
		return
	}
	target := r.URL.Query().Get("target")
	if strings.TrimSpace(target) == "" {
		httpError(w, errBadRequest("target is required"))
		return
	}
	e, ok := s.index.Resolve(target)
	if !ok {
		writeJSON(w, map[string]any{"kind": "missing", "target": target})
		return
	}
	switch {
	case e.HasNote && e.IsPerson:
		writeJSON(w, map[string]any{"kind": "contact", "key": e.Key})
	case e.HasNote:
		writeJSON(w, map[string]any{"kind": "note", "path": e.NotePath})
	default: // note-less but a real link target → its contact page shows backlinks
		writeJSON(w, map[string]any{"kind": "contact", "key": e.Key})
	}
}

// safeVaultPath resolves a vault-relative markdown path, refusing traversal and
// non-markdown files.
func safeVaultPath(root, rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" || !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full, pathErr := vaultwriter.SafePath(root, clean)
	if pathErr != nil {
		return "", false
	}
	relCheck, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", false
	}
	return full, true
}

func noteWriteError(w http.ResponseWriter, err error) {
	var conflict *vaultwriter.Conflict
	if errors.As(err, &conflict) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, conflict)
		return
	}
	if os.IsExist(err) {
		http.Error(w, "a file already exists at that path", http.StatusConflict)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}
