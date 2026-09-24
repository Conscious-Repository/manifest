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
type fundraisingDirectoryAdapter struct {
	store *fundraising.Store
	srv   *Server
}

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
	key = strings.ToLower(strings.TrimSpace(key))
	for _, op := range a.srv.FundraisingSnapshot() {
		if op.Archived || !opportunityNames(op, key) {
			continue
		}
		out = append(out, contacts.FundraisingSummary{
			ID: op.ID, Firm: op.Firm, Status: op.Status, Amount: op.Amount, NextStep: op.NextStep,
			LastTouch: contactTouch(op.LastTouch), NextTouch: contactTouch(op.NextTouch),
		})
	}
	return out
}

// opportunityNames reports whether an opportunity links the person, as one of
// its people or as its source.
func opportunityNames(op fundraising.Opportunity, key string) bool {
	if op.Source != nil && op.Source.Contact != nil && strings.ToLower(op.Source.Contact.Key) == key {
		return true
	}
	for _, p := range op.People {
		if strings.ToLower(p.Key) == key {
			return true
		}
	}
	return false
}

func contactTouch(t *fundraising.Touch) *contacts.Touch {
	if t == nil {
		return nil
	}
	return &contacts.Touch{Date: t.Date, Kind: t.Kind, Person: t.Person, PersonKey: t.PersonKey, Title: t.Title, Ref: t.Ref}
}

func fundraisingTouch(t contacts.Touch) fundraising.Touch {
	return fundraising.Touch{Date: t.Date, Kind: t.Kind, Person: t.Person, PersonKey: t.PersonKey, Title: t.Title, Ref: t.Ref}
}

func (s *Server) wireFundraisingContacts() {
	if s.contacts != nil && s.fundraising != nil {
		s.contacts.UseCRMDirectory(fundraisingDirectoryAdapter{s.fundraising, s})
	}
}

func (s *Server) fundraisingView() map[string]any {
	ops := s.FundraisingSnapshot()
	resources := []fundraising.Resource{}
	if s.fundraising != nil {
		resources = s.fundraising.Resources()
	}
	return map[string]any{"opportunities": ops, "statuses": fundraising.Statuses, "resources": resources}
}

// FundraisingSnapshot is the private complete projection shared by the owner
// cockpit and the Sheet sync. It is never mounted on the team portal. Each
// opportunity carries its winning last and next touch: the people layer's
// automatic touches across every linked person, merged with the hand-typed
// dates under "latest wins". Computed here per request, never stored.
func (s *Server) FundraisingSnapshot() []fundraising.Opportunity {
	ops := []fundraising.Opportunity{}
	if s.fundraising != nil {
		ops, _ = s.fundraising.List()
	}
	now := time.Now()
	today := now.Format("2006-01-02")
	for i := range ops {
		var lasts, nexts []fundraising.Touch
		if s.contacts != nil {
			for _, p := range ops[i].People {
				last, next := s.contacts.Touches(p.Key, now)
				if last.Date != "" {
					lasts = append(lasts, fundraisingTouch(last))
				}
				if next.Date != "" {
					nexts = append(nexts, fundraisingTouch(next))
				}
			}
		}
		ops[i].LastTouch = fundraising.MergeLast(fundraising.PickLast(lasts), ops[i].LastTouchpointDate)
		ops[i].NextTouch = fundraising.MergeNext(fundraising.PickNext(nexts), ops[i].NextStepDue, today)
	}
	return ops
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

func (s *Server) handleFundraisingDelete(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil {
		http.Error(w, "fundraising unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.fundraising.Delete(r.PathValue("id")); err != nil {
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
	// A person added from the tracker is real at once: the owner's action
	// creates the vault note when none exists (no note-less CRM contacts).
	if p.NotePath == "" && s.contacts != nil {
		key, rel, err := s.contacts.EnsureNote(p.Key, p.Display)
		if err != nil {
			httpError(w, err)
			return
		}
		p.Key, p.NotePath = key, rel
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
