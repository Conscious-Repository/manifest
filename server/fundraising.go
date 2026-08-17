package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/contacts"
	"manifest/fundraising"
)

// fundraisingDirectoryAdapter is the explicit business→people bridge. Keeping
// it here prevents the personal contacts package from importing a CRM domain.
type fundraisingDirectoryAdapter struct{ store *fundraising.Store }

func (a fundraisingDirectoryAdapter) People() []contacts.CRMContact {
	out := []contacts.CRMContact{}
	for _, p := range a.store.People() {
		out = append(out, contacts.CRMContact{Key: p.Key, Display: p.Display, NotePath: p.NotePath, Emails: p.Emails})
	}
	return out
}
func (a fundraisingDirectoryAdapter) Person(key string) (contacts.CRMContact, bool) {
	p, ok := a.store.Person(key)
	return contacts.CRMContact{Key: p.Key, Display: p.Display, NotePath: p.NotePath, Emails: p.Emails}, ok
}
func (a fundraisingDirectoryAdapter) AddEmail(key, email string) error {
	return a.store.AddEmail(key, email)
}
func (a fundraisingDirectoryAdapter) AttachNote(key, notePath string) error {
	return a.store.AttachNote(key, notePath)
}
func (a fundraisingDirectoryAdapter) Fundraising(key string) []contacts.FundraisingSummary {
	out := []contacts.FundraisingSummary{}
	for _, op := range a.store.OpportunitiesFor(key) {
		if op.Archived {
			continue
		}
		out = append(out, contacts.FundraisingSummary{ID: op.ID, Firm: op.Firm, Status: op.Status, Interest: op.Interest, Amount: op.Amount, NextStep: op.NextStep})
	}
	return out
}

func (s *Server) wireFundraisingContacts() {
	if s.contacts != nil && s.fundraising != nil {
		s.contacts.UseCRMDirectory(fundraisingDirectoryAdapter{s.fundraising})
	}
}

func (s *Server) fundraisingView() map[string]any {
	ops := []fundraising.Opportunity{}
	if s.fundraising != nil {
		ops, _ = s.fundraising.List()
	}
	if s.contacts != nil {
		now := time.Now()
		for i := range ops {
			for _, p := range ops[i].People {
				if d := s.contacts.LatestInteraction(p.Key, now); d > ops[i].ComputedLastTouchpoint {
					ops[i].ComputedLastTouchpoint = d
				}
			}
		}
	}
	resources := []fundraising.Resource{}
	if s.fundraising != nil {
		resources = s.fundraising.Resources()
	}
	return map[string]any{"opportunities": ops, "statuses": fundraising.Statuses, "interests": fundraising.Interests, "resources": resources}
}

func (s *Server) handleFundraisingList(w http.ResponseWriter, _ *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, s.fundraisingView())
}

func (s *Server) handleFundraisingCreate(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Firm string `json:"firm"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Firm) == "" {
		httpError(w, errBadRequest("firm is required"))
		return
	}
	if _, err := s.fundraising.Create(b.Firm); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingView())
}

func (s *Server) handleFundraisingUpdate(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	var b map[string]any
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.fundraising.Update(r.PathValue("id"), b); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingView())
}

func (s *Server) handleFundraisingArchive(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Archived bool `json:"archived"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.fundraising.Archive(r.PathValue("id"), b.Archived); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingView())
}

func (s *Server) handleFundraisingPersonAdd(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	var p fundraising.PersonRef
	if err := decode(r, &p); err != nil {
		httpError(w, err)
		return
	}
	if _, err := s.fundraising.AddPerson(r.PathValue("id"), p); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingView())
}

func (s *Server) handleFundraisingPersonRemove(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key string `json:"key"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Key) == "" {
		httpError(w, errBadRequest("key is required"))
		return
	}
	if _, err := s.fundraising.RemovePerson(r.PathValue("id"), b.Key); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.fundraisingView())
}
