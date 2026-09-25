package server

import (
	"errors"
	"manifest/artifacts"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Edits version the registered artifact; they never overwrite arbitrary runtime
// files. Task plans retain their canonical vault writer and sharing rules.
func (s *Server) handleArtifactText(w http.ResponseWriter, r *http.Request) {
	if !s.artifactsOK(w) {
		return
	}
	var b struct{ ID, Content, ExpectedRevision, RequestID string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	a, ok := s.artifactReg.Get(b.ID)
	if !ok {
		http.Error(w, "Artifact not found", 404)
		return
	}
	ext := strings.ToLower(filepath.Ext(a.Ref))
	if (a.Provenance.Source == "task-plan" || a.Provenance.Source == "knowledge-context" || a.Provenance.Source == "task-context" || a.Provenance.Source == "goal-context" || a.Provenance.Source == "person-context" || a.Provenance.Source == "project-context" || a.Provenance.Source == "candidate-context" || a.Provenance.Source == "organization-context" || a.Provenance.Source == "schedule-context" || a.Provenance.Source == "calendar-context") || !strings.Contains("|.md|.txt|.json|.csv|.tsv|.yaml|.yml|.toml|.js|.jsx|.ts|.tsx|.py|.go|.html|.css|.sql|.sh|.xml|.svg|", "|"+ext+"|") || ext == "" {
		http.Error(w, "This file is preview-only", 400)
		return
	}
	if b.RequestID != "" && !artifacts.ValidRequestID(b.RequestID) {
		http.Error(w, "Invalid save request identity", 400)
		return
	}
	if !artifacts.ValidHash(b.ExpectedRevision) || len(b.Content) == 0 || len(b.Content) > 1024*1024 || !utf8.ValidString(b.Content) || strings.ContainsRune(b.Content, 0) {
		http.Error(w, "A starting revision and nonempty UTF-8 text up to 1 MB are required", 400)
		return
	}
	prior, err := s.artifactReg.Content(b.ExpectedRevision)
	if err != nil || !utf8.Valid(prior) || strings.ContainsRune(string(prior), 0) {
		http.Error(w, "This file is preview-only", 400)
		return
	}
	result, err := s.artifactReg.Put(artifacts.Put{ID: a.ID, ExpectedHead: b.ExpectedRevision, RequestID: b.RequestID, Content: []byte(b.Content), Actor: "owner", Note: "Edited in chat"})
	if errors.Is(err, artifacts.ErrRevisionConflict) {
		http.Error(w, err.Error(), 409)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	s.artifactEvent(result, "owner")
	writeJSON(w, struct {
		artifacts.Artifact
		SavedRevision string `json:"savedRevision"`
		SavedVersion  int    `json:"savedVersion"`
		SaveRequestID string `json:"saveRequestID,omitempty"`
	}{result.Artifact, result.Revision.Hash, result.Revision.N, b.RequestID})
}
