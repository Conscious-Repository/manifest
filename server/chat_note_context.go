package server

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"manifest/artifacts"
)

// These routes belong only to the private owner handler. Browsing never
// registers context; explicit retention compares the exact preview bytes.
func (s *Server) handleChatNoteSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.index == nil {
		http.Error(w, "note index unavailable", 503)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 256 {
		http.Error(w, "query is too long", 400)
		return
	}
	notes, err := s.index.ContextNotes(q, 50)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"notes": notes, "limit": 50})
}

func (s *Server) contextNoteBytes(rel string) ([]byte, error) {
	if s.index == nil || !s.index.ContextNote(rel) {
		return nil, errBadRequest("indexed knowledge note unavailable")
	}
	full, ok := safeVaultPath(s.index.VaultRoot(), rel)
	if !ok {
		return nil, errBadRequest("invalid note path")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, errBadRequest("note unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, errBadRequest("note is not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 64001))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > 64000 || !utf8.Valid(b) || describeArtifactPreview(artifacts.Hash(b), b).Kind != "text" {
		return nil, errBadRequest("select a nonempty UTF-8 text note up to 64,000 bytes")
	}
	return b, nil
}

func (s *Server) handleChatNotePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	rel := r.URL.Query().Get("path")
	b, err := s.contextNoteBytes(rel)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"path": rel, "content": string(b), "revision": artifacts.Hash(b), "route": "#/note/" + url.PathEscape(rel)})
}

func (s *Server) handleChatNoteRetain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.artifactsOK(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var req struct {
		Path     string `json:"path"`
		Revision string `json:"revision"`
	}
	if err := decode(r, &req); err != nil || req.Revision == "" {
		http.Error(w, "path and reviewed revision required", 400)
		return
	}
	b, err := s.contextNoteBytes(req.Path)
	if err != nil {
		httpError(w, err)
		return
	}
	if artifacts.Hash(b) != req.Revision {
		http.Error(w, "note changed; review its current text before selecting it", 409)
		return
	}
	// The hash in Ref makes each observation independently immutable, while the
	// source path remains navigable. Repeating retention is a deduplicated Put.
	result, err := s.artifactReg.Put(artifacts.Put{Kind: artifacts.KindDocument, Title: req.Path, Harness: "vault", Ref: req.Path + "#context-" + req.Revision, Content: b, Actor: "owner", Provenance: artifacts.Provenance{Source: "knowledge-context"}})
	if err != nil {
		httpError(w, err)
		return
	}
	s.artifactEvent(result, "owner")
	rev, ok := result.Artifact.Revision(req.Revision)
	if !ok {
		http.Error(w, "reviewed revision unavailable", 409)
		return
	}
	writeJSON(w, map[string]any{"id": result.Artifact.ID, "revision": req.Revision, "title": result.Artifact.Title, "version": rev.N, "explicitArtifacts": true})
}

func knowledgeContextPath(a artifacts.Artifact) string {
	if a.Provenance.Source != "knowledge-context" || a.Harness != "vault" {
		return ""
	}
	i := strings.LastIndex(a.Ref, "#context-")
	if i < 0 || !artifacts.ValidHash(a.Ref[i+9:]) {
		return ""
	}
	return a.Ref[:i]
}
