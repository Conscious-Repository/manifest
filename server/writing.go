package server

import (
	"manifest/vaultwriter"
	"manifest/writing"
	"net/http"
	"os"
	"strings"
)

// The writing browser is deliberately confined to the configured vault. The
// listing comes from the text index, whose walk keeps the browser's filters.
func (s *Server) handleWritingFiles(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	type entry struct {
		Path     string   `json:"path"`
		Name     string   `json:"name"`
		Modified int64    `json:"modified"`
		ReadOnly bool     `json:"readOnly"`
		Excerpt  string   `json:"excerpt"`
		Tags     []string `json:"tags"`
	}
	view, err := s.writingTextIndex().refresh(s)
	if err != nil {
		http.Error(w, "could not read the vault file list", 500)
		return
	}
	files := make([]entry, 0, len(view.files))
	for _, d := range view.files {
		tags := make([]string, 0, len(d.tagKeys))
		for _, k := range d.tagKeys {
			tags = append(tags, view.tagName[k])
		}
		files = append(files, entry{d.Path, d.Name, d.Modified, d.ReadOnly, d.excerpt, tags})
	}
	writeJSON(w, map[string]any{"files": files, "folders": view.folders, "vaultID": vaultwriter.Revision([]byte(s.vault.VaultRoot()))})
}
func (s *Server) handleWritingCreate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if s.vault == nil {
		http.Error(w, "vault unavailable", 503)
		return
	}
	var b struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.writingNoteFolderExists(b.Path); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	rev, err := s.vault.CreateNote(b.Path, b.Body)
	if err != nil {
		noteWriteError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{b.Path})
	}
	writeJSON(w, map[string]any{"path": b.Path, "revision": rev})
}
func (s *Server) handleWritingMove(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if s.vault == nil {
		http.Error(w, "vault unavailable", 503)
		return
	}
	var b struct {
		Path       string `json:"path"`
		To         string `json:"to"`
		IfRevision string `json:"ifRevision"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	s.writingAskMu.Lock()
	defer s.writingAskMu.Unlock()
	for key := range s.writingAsks {
		if strings.HasPrefix(key, b.Path+"\x00") {
			http.Error(w, "Wait for the answer before moving this note.", 409)
			return
		}
	}
	var moveErr error
	if s.writing != nil {
		before, after, err := s.writing.RelocatedRecord(b.Path, b.To)
		if err != nil {
			noteWriteError(w, err)
			return
		}
		moveErr = s.vault.MoveNoteWithRecord(b.Path, b.To, b.IfRevision, s.writing.RecordPath(b.Path), s.writing.RecordPath(b.To), before, after)
	} else {
		moveErr = s.vault.MoveNote(b.Path, b.To, b.IfRevision)
	}
	if err := moveErr; err != nil {
		noteWriteError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{b.Path, b.To})
	}
	s.carryWritingAuthorship(b.Path, b.To)
	warning := ""
	if err := s.relinkWritingTasks(b.Path, b.To); err != nil {
		warning = "File moved; a task link could not be updated. Rebind it from the task panel."
	}
	writeJSON(w, map[string]any{"path": b.To, "revision": b.IfRevision, "warning": warning})
}

func (s *Server) UseWriting(root string, excluded ...string) {
	if s.vault != nil {
		s.writing = &writing.Store{Writer: s.vault, Root: root, Excluded: excluded}
	}
}
func (s *Server) handleWritingComments(w http.ResponseWriter, r *http.Request) {
	if s.writing == nil {
		http.Error(w, "writing conversations unavailable", 503)
		return
	}
	p := r.URL.Query().Get("path")
	if _, err := vaultwriter.SafePath(s.vault.VaultRoot(), p); err != nil || !strings.HasSuffix(p, ".md") {
		http.Error(w, "invalid note path", 400)
		return
	}
	d, err := s.writingReadRecover(p)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]any{"document": d, "agentAvailable": s.writingComplete != nil})
}
func (s *Server) handleWritingComment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if s.writing == nil {
		http.Error(w, "writing conversations unavailable", 503)
		return
	}
	var b struct {
		Path     string         `json:"path"`
		Revision string         `json:"revision"`
		ID       string         `json:"id"`
		Thread   string         `json:"thread"`
		Body     string         `json:"body"`
		State    string         `json:"state"`
		Anchor   writing.Anchor `json:"anchor"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if len(b.ID) < 8 || len(b.ID) > 80 || len(b.Body) > 24000 {
		http.Error(w, "invalid comment", 400)
		return
	}
	if _, err := vaultwriter.SafePath(s.vault.VaultRoot(), b.Path); err != nil || !strings.HasSuffix(b.Path, ".md") {
		http.Error(w, "invalid note path", 400)
		return
	}
	e := writing.Event{ID: b.ID, Thread: b.Thread}
	if b.State != "" {
		e.Type = "state"
		e.State = b.State
	} else {
		if strings.TrimSpace(b.Body) == "" {
			http.Error(w, "comment text is required", 400)
			return
		}
		reply := writing.NewReply(b.ID, "owner", b.Body)
		e.Reply = &reply
		if b.Thread == "" {
			full, ok := safeVaultPath(s.vault.VaultRoot(), b.Path)
			if !ok {
				http.Error(w, "invalid note path", 400)
				return
			}
			raw, err := os.ReadFile(full)
			if err != nil {
				http.Error(w, "note unavailable", 409)
				return
			}
			if err = writing.ValidateAnchor(raw, b.Anchor); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			a := writing.StampAnchor(raw, b.Anchor)
			e.Type = "thread"
			e.Anchor = &a
		} else {
			e.Type = "reply"
		}
	}
	d, err := s.writing.Append(b.Path, b.Revision, e, false)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, d)
}
