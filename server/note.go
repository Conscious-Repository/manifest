package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	writeJSON(w, map[string]any{
		"path": filepath.ToSlash(rel), "name": name, "raw": string(raw),
		"backlinks": backlinks, "isPerson": isPerson,
		"zone":  s.index.NoteZone(filepath.ToSlash(rel)), // "system" → quiet SYSTEM badge
		"vault": filepath.Base(s.index.VaultRoot()),      // for the obsidian:// URI
	})
}

func (s *Server) handleNotePut(w http.ResponseWriter, r *http.Request) {
	if s.index == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if err := decode(r, &b); err != nil || b.Path == "" {
		httpError(w, errBadRequest("path is required"))
		return
	}
	if err := s.vault.WriteNote(b.Path, b.Body); err != nil {
		httpError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{b.Path})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleNoteTask(w http.ResponseWriter, r *http.Request) {
	if s.index == nil || s.vault == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Want bool   `json:"want"`
	}
	if err := decode(r, &b); err != nil || b.Path == "" {
		httpError(w, errBadRequest("path and line are required"))
		return
	}
	if err := s.vault.ToggleTask(b.Path, b.Line, b.Want); err != nil {
		httpError(w, err)
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
	full := filepath.Join(root, clean)
	relCheck, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", false
	}
	return full, true
}
