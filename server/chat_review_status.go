package server

import (
	"errors"
	"net/http"
	"os"
	"path"

	"manifest/artifacts"
)

// Review counts are independent of execution and never imply that the thread,
// task, or all outputs have been accepted. Only exact canonical scope links are
// returned; no title matching or inferred project association is used.
func (s *Server) handleChatReviewStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	type counts struct {
		Ready      int `json:"ready"`
		Changes    int `json:"changes"`
		Accepted   int `json:"accepted"`
		Unreviewed int `json:"unreviewed"`
	}
	out := map[string]*counts{}
	byTask := map[string]*counts{}
	if s.artifactReg == nil || s.vault == nil || s.artifactReviewsRoot == "" {
		writeJSON(w, map[string]any{"by_scope": out, "by_task": byTask})
		return
	}
	for _, a := range s.artifactReg.List(artifacts.Filter{}) {
		scope := a.Provenance.Session
		target := out
		if scope == "" {
			scope = a.Provenance.Task
			target = byTask
		}
		if scope == "" {
			continue
		}
		raw, err := s.vault.ReadVaultFile(path.Join(s.artifactReviewsRoot, a.ID+".md"))
		if errors.Is(err, os.ErrNotExist) {
			raw = nil
			err = nil
		}
		if err != nil {
			httpError(w, err)
			return
		}
		review, err := parseArtifactReviews(raw, a.ID, a.Head)
		if err != nil {
			httpError(w, err)
			return
		}
		if target[scope] == nil {
			target[scope] = &counts{}
		}
		switch review.State {
		case "ready_for_review":
			target[scope].Ready++
		case "changes_requested":
			target[scope].Changes++
		case "accepted":
			target[scope].Accepted++
		default:
			target[scope].Unreviewed++
		}
	}
	writeJSON(w, map[string]any{"by_scope": out, "by_task": byTask})
}
