package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/studio"
)

// Content Studio — Inspiration tab writes (iteration-2 §8). The dashboard's
// corpus contract is widened NARROWLY: it may write only x_accounts.commentary,
// x_accounts.is_self, and post_annotations (machine-local derived data). Adding
// an account writes a spool job the engine's scheduler claims (an x-backfill).

func (s *Server) corpusRW() (*studio.Corpus, error) {
	return studio.OpenCorpusRW(s.corpusPath)
}

// handleStudioCommentary writes an account's commentary.
func (s *Server) handleStudioCommentary(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Text string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	c, err := s.corpusRW()
	if err != nil || c == nil {
		http.Error(w, "corpus unavailable", http.StatusServiceUnavailable)
		return
	}
	defer c.Close()
	if err := c.SetCommentary(r.PathValue("handle"), b.Text); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStudioSelf marks/unmarks the owner's own account.
func (s *Server) handleStudioSelf(w http.ResponseWriter, r *http.Request) {
	var b struct {
		On bool `json:"on"`
	}
	_ = decode(r, &b)
	c, err := s.corpusRW()
	if err != nil || c == nil {
		http.Error(w, "corpus unavailable", http.StatusServiceUnavailable)
		return
	}
	defer c.Close()
	if err := c.SetSelf(r.PathValue("handle"), b.On); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStudioAnnotate upserts an owner note + tags on a post.
func (s *Server) handleStudioAnnotate(w http.ResponseWriter, r *http.Request) {
	var b struct {
		PostID string `json:"postId"`
		Note   string `json:"note"`
		Tags   string `json:"tags"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.PostID) == "" {
		httpError(w, errBadRequest("postId is required"))
		return
	}
	c, err := s.corpusRW()
	if err != nil || c == nil {
		http.Error(w, "corpus unavailable", http.StatusServiceUnavailable)
		return
	}
	defer c.Close()
	if err := c.Annotate(b.PostID, b.Note, b.Tags); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStudioAddAccount writes an x-backfill spool job the engine picks up.
func (s *Server) handleStudioAddAccount(w http.ResponseWriter, r *http.Request) {
	if s.spirits == nil {
		http.Error(w, "engine spool unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Handle string `json:"handle"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	handle := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(b.Handle), "@"))
	if handle == "" {
		httpError(w, errBadRequest("handle is required"))
		return
	}
	spoolDir := filepath.Join(s.spirits.Root(), "vessel", "spool")
	if err := os.MkdirAll(spoolDir, 0o755); err != nil {
		httpError(w, err)
		return
	}
	job := map[string]string{
		"kind": "x-backfill", "handle": handle, "source": "studio",
		"requested_at": time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(job)
	name := time.Now().UTC().Format("20060102-150405") + "-x-backfill-" + handle + ".json"
	if err := os.WriteFile(filepath.Join(spoolDir, name), data, 0o644); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "queued": handle})
}
