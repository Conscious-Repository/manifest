package server

import (
	"net/http"
	"time"
)

// CONTACTS — the people layer over the vault index (plans/contacts-feature.md).
// All reads are graph queries; the only writes are explicit user actions (create
// a person note, bind an alias, confirm an email) routed through the service.

func (s *Server) handleContactsList(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		writeJSON(w, map[string]any{"contacts": []any{}})
		return
	}
	list, err := s.contacts.List(time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"contacts": list})
}

func (s *Server) handleContactsTriage(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		writeJSON(w, map[string]any{"triage": []any{}})
		return
	}
	tri, err := s.contacts.Triage()
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"triage": tri})
}

func (s *Server) handleContactPage(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	p, ok := s.contacts.Page(key, time.Now())
	if !ok {
		http.Error(w, "no such contact", http.StatusNotFound)
		return
	}
	writeJSON(w, p)
}

func (s *Server) handleContactsSearch(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		writeJSON(w, map[string]any{"results": []any{}})
		return
	}
	refs, err := s.contacts.Search(r.URL.Query().Get("q"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"results": refs})
}

func (s *Server) handleContactCard(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	c, ok := s.contacts.Card(key, time.Now())
	if !ok {
		http.Error(w, "no such contact", http.StatusNotFound)
		return
	}
	writeJSON(w, c)
}

// Confirm materializes the person note (lowercase <name>.md, categories: [people]),
// so it needs the display for the filename.
func (s *Server) handleContactsConfirm(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key     string `json:"key"`
		Display string `json:"display"`
	}
	if err := decode(r, &b); err != nil || b.Key == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	if err := s.contacts.Confirm(b.Key, b.Display); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleContactsDismiss(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key string `json:"key"`
	}
	if err := decode(r, &b); err != nil || b.Key == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	if err := s.contacts.Dismiss(b.Key); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleContactsOrg(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key string `json:"key"`
	}
	if err := decode(r, &b); err != nil || b.Key == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	if err := s.contacts.MarkOrg(b.Key); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleContactsDismissBulk(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Keys []string `json:"keys"`
	}
	if err := decode(r, &b); err != nil || len(b.Keys) == 0 {
		httpError(w, errBadRequest("keys is required"))
		return
	}
	if err := s.contacts.BulkDismiss(b.Keys); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleContactsBind(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Variant   string `json:"variant"`
		Canonical string `json:"canonical"`
		Display   string `json:"display"`
	}
	if err := decode(r, &b); err != nil || b.Variant == "" || b.Canonical == "" {
		httpError(w, errBadRequest("variant and canonical are required"))
		return
	}
	if err := s.contacts.Bind(b.Variant, b.Canonical, b.Display); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleContactsNote(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key     string `json:"key"`
		Display string `json:"display"`
		Body    string `json:"body"`
	}
	if err := decode(r, &b); err != nil || b.Key == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	key, err := s.contacts.SaveNote(b.Key, b.Display, b.Body)
	if err != nil {
		httpError(w, err)
		return
	}
	// return the freshly re-rendered page (the note now exists)
	p, _ := s.contacts.Page(key, time.Now())
	writeJSON(w, p)
}

func (s *Server) handleContactsEmail(w http.ResponseWriter, r *http.Request) {
	if s.contacts == nil {
		http.Error(w, "contacts disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key     string `json:"key"`
		Display string `json:"display"`
		Email   string `json:"email"`
	}
	if err := decode(r, &b); err != nil || b.Key == "" || b.Email == "" {
		httpError(w, errBadRequest("key and email are required"))
		return
	}
	if err := s.contacts.ConfirmEmail(b.Key, b.Display, b.Email); err != nil {
		httpError(w, err)
		return
	}
	p, _ := s.contacts.Page(b.Key, time.Now())
	writeJSON(w, p)
}

type badRequest string

func (e badRequest) Error() string      { return string(e) }
func errBadRequest(s string) badRequest { return badRequest(s) }
