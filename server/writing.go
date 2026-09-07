package server

import (
	"io/fs"
	"manifest/vaultwriter"
	"manifest/writing"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The writing browser is deliberately confined to the configured vault.
func (s *Server) handleWritingFiles(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	type entry struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		Modified int64  `json:"modified"`
		ReadOnly bool   `json:"readOnly"`
	}
	files := []entry{}
	folders := []string{""}
	root := s.vault.VaultRoot()
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if full == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if s.writing != nil && (rel == s.writing.Root || strings.HasPrefix(rel, s.writing.Root+"/")) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !s.vault.CanUserWrite(rel) {
				return filepath.SkipDir
			}
			folders = append(folders, rel)
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(rel), ".md") {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, entry{rel, strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), fi.ModTime().Unix(), !s.vault.CanUserWrite(rel)})
		return nil
	})
	if err != nil {
		http.Error(w, "could not read the vault file list", 500)
		return
	}
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path) })
	sort.Strings(folders)
	writeJSON(w, map[string]any{"files": files, "folders": folders, "vaultID": vaultwriter.Revision([]byte(root))})
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
	d, err := s.writing.Read(p)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, map[string]any{"document": d, "agentAvailable": false})
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
