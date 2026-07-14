package server

import (
	"net/http"
	"strings"

	"manifest/studio"
)

// CONTENT STUDIO (content-studio §8). The dashboard renders the draft board +
// the inspiration watchlist and captures the owner's edits + feedback. It never
// invokes the engine; approving/posting are user actions over files the engine
// produced. Gated on excaliburPath (studio == nil disables the tab).

// UseStudio wires the Content Studio surfaces: the draft store (excalibur
// artifacts/studio/drafts), the read-only X corpus, and the vault X-posts file.
func (s *Server) UseStudio(st *studio.Store, corpusPath, xPostsFile string) {
	s.studio = st
	s.corpusPath = corpusPath
	s.xPostsFile = xPostsFile
}

// handleStudio serves the board (drafts) + inspiration (watchlist w/ top posts).
// The runs strip is derived client-side from /api/spirits/runs (scribe/critic).
func (s *Server) handleStudio(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil {
		http.Error(w, "studio disabled", http.StatusServiceUnavailable)
		return
	}
	inspiration := []studio.Account{}
	if c, err := studio.OpenCorpus(s.corpusPath); err == nil && c != nil {
		defer c.Close()
		if accts, err := c.Watchlist(5); err == nil {
			inspiration = accts
		}
	}
	writeJSON(w, map[string]any{
		"board":       s.studio.List(),
		"inspiration": inspiration,
		"xPostsFile":  s.xPostsFile,
	})
}

// handleStudioFeedback records the owner's feedback text + quick chips onto a draft.
func (s *Server) handleStudioFeedback(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil {
		http.Error(w, "studio disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Text string   `json:"text"`
		Tags []string `json:"tags"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	d, err := s.studio.SetFeedback(r.PathValue("id"), b.Text, b.Tags)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, d)
}

// handleStudioEdit records the owner's edited draft text. When an approvalId is
// given (from the feed card), the linked pending append-x-queue proposal's bullet
// is rewritten too, so approving lands the edited text.
func (s *Server) handleStudioEdit(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil {
		http.Error(w, "studio disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Text       string `json:"text"`
		ApprovalID string `json:"approvalId"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if strings.TrimSpace(b.Text) == "" {
		httpError(w, errBadRequest("edited text is required"))
		return
	}
	d, err := s.studio.SetEdited(r.PathValue("id"), b.Text)
	if err != nil {
		httpError(w, err)
		return
	}
	if b.ApprovalID != "" && s.approvals != nil {
		// the queue bullet is the post text as one bullet line
		if err := s.approvals.SetProposed(b.ApprovalID, "- "+strings.TrimSpace(b.Text)); err != nil {
			httpError(w, err)
			return
		}
	}
	writeJSON(w, d)
}

// handleStudioMarkPosted moves the draft's bullet from `# queue` to `# posted`
// in the vault X-posts file and records the (optional) tweet URL on the draft.
func (s *Server) handleStudioMarkPosted(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "studio/vault unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		URL string `json:"url"`
	}
	_ = decode(r, &b) // url optional but encouraged
	id := r.PathValue("id")
	d, ok := s.studio.Get(id)
	if !ok {
		http.Error(w, "draft not found", http.StatusNotFound)
		return
	}
	if err := s.vault.MoveBulletToPosted(s.xPostsFile, "- "+d.Effective()); err != nil {
		httpError(w, err)
		return
	}
	updated, err := s.studio.MarkPosted(id, b.URL)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, updated)
}
