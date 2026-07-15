package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/feed"
	"manifest/studio"
	"manifest/vaultwriter"
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
// Board drafts are enriched with their real lifecycle status (draft → passed →
// queued → posted, computed from x posts.md) and the linked approval id (§4).
func (s *Server) handleStudio(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil {
		http.Error(w, "studio disabled", http.StatusServiceUnavailable)
		return
	}
	board := s.studio.List()

	// lifecycle status from the actual file: a draft whose text sits in # queue is
	// "queued"; in # posted is "posted" — reflecting reality without extra writes.
	queueSet, postedSet := map[string]bool{}, map[string]bool{}
	if s.vault != nil && s.vault.Enabled() {
		if b, err := s.vault.ReadVaultFile(s.xPostsFile); err == nil {
			doc := vaultwriter.ParseXPosts(string(b))
			for _, x := range doc.Queue {
				queueSet[normText(x.Lead)] = true
			}
			for _, x := range doc.Posted {
				postedSet[normText(x.Lead)] = true
			}
		}
	}
	// link each draft to its append-x-queue approval (via the feed draft cards)
	approvalByDraft := map[string]string{}
	if s.spirits != nil {
		for _, it := range s.spirits.Feed.List(feed.Filter{Type: "draft"}, time.Now()) {
			if it.DraftID != "" && it.ApprovalID != "" {
				approvalByDraft[it.DraftID] = it.ApprovalID
			}
		}
	}
	for i := range board {
		t := normText(board[i].Effective())
		if postedSet[t] {
			board[i].Status = "posted"
		} else if queueSet[t] {
			board[i].Status = "queued"
		}
		if id, ok := approvalByDraft[board[i].ID]; ok {
			board[i].ApprovalID = id
		}
	}

	inspiration := []studio.Account{}
	if c, err := studio.OpenCorpus(s.corpusPath); err == nil && c != nil {
		defer c.Close()
		if accts, err := c.Watchlist(5); err == nil {
			inspiration = accts
		}
	}
	writeJSON(w, map[string]any{
		"board":       board,
		"inspiration": inspiration,
		"xPostsFile":  s.xPostsFile,
	})
}

func normText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "- "))), " ")
}

// handleStudioOverrule queues a critic-killed draft anyway (§4): appends its text
// to # queue and records the overrule on the draft (first-class tune evidence).
func (s *Server) handleStudioOverrule(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "studio/vault unavailable", http.StatusServiceUnavailable)
		return
	}
	d, ok := s.studio.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "draft not found", http.StatusNotFound)
		return
	}
	if err := s.vault.AppendQueueBullet(s.xPostsFile, "- "+d.Effective()); err != nil {
		httpError(w, err)
		return
	}
	updated, err := s.studio.Overrule(d.ID)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, updated)
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
