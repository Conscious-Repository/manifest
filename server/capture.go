package server

import (
	"net/http"

	"manifest/capture"
)

// Capture tray (cmd-ctr import P5): quick notes / shared links / images →
// editable cards → triaged into todos, day tasks, or chat by the OWNER.
// /api/capture/share is the PWA share_target action — the one route a phone
// share-sheet posts into.

// UseCapture wires the tray store.
func (s *Server) UseCapture(c *capture.Store) { s.capture = c }

func (s *Server) handleCaptureList(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		writeJSON(w, map[string]any{"items": []any{}})
		return
	}
	writeJSON(w, map[string]any{"items": s.capture.List()})
}

func (s *Server) handleCaptureBadge(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		writeJSON(w, map[string]int{"open": 0})
		return
	}
	writeJSON(w, map[string]int{"open": s.capture.OpenCount()})
}

func (s *Server) handleCaptureAdd(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		http.Error(w, "capture disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Title  string `json:"title"`
		Text   string `json:"text"`
		URL    string `json:"url"`
		Source string `json:"source"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Source == "" {
		b.Source = "manual"
	}
	it, err := s.capture.Add(b.Title, b.Text, b.URL, b.Source)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, it)
}

// handleCaptureUpload takes multipart files (+ optional text) from paste/drop.
func (s *Server) handleCaptureUpload(w http.ResponseWriter, r *http.Request) {
	s.captureMultipart(w, r, "paste", "")
}

// handleCaptureShare is the PWA share_target action. The 303 sends the share
// flow's navigation to the tray.
func (s *Server) handleCaptureShare(w http.ResponseWriter, r *http.Request) {
	s.captureMultipart(w, r, "share", "/#/capture")
}

func (s *Server) captureMultipart(w http.ResponseWriter, r *http.Request, source, redirect string) {
	if s.capture == nil {
		http.Error(w, "capture disabled", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 30<<20)
	if err := r.ParseMultipartForm(30 << 20); err != nil {
		httpError(w, errBadRequest("upload too large or malformed"))
		return
	}
	it := s.capture.NewForFiles(r.FormValue("title"), r.FormValue("text"), r.FormValue("url"), source)
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["files"] {
			if err := s.capture.AddFile(&it, fh); err != nil {
				httpError(w, errBadRequest(err.Error()))
				return
			}
		}
	}
	if it.Title == "" && it.Text == "" && it.URL == "" && len(it.Media) == 0 {
		httpError(w, errBadRequest("empty share"))
		return
	}
	if err := s.capture.Save(it); err != nil {
		httpError(w, err)
		return
	}
	if redirect != "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	writeJSON(w, it)
}

func (s *Server) handleCaptureUpdate(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		http.Error(w, "capture disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.capture.Update(r.PathValue("id"), b.Title, b.Text); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleCaptureStatus(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		http.Error(w, "capture disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Status string `json:"status"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.capture.SetStatus(r.PathValue("id"), b.Status); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleCaptureDismiss(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		http.Error(w, "capture disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.capture.Trash(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleCaptureMedia(w http.ResponseWriter, r *http.Request) {
	if s.capture == nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.capture.MediaPath(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p)
}
