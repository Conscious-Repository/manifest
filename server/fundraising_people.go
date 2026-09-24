package server

import (
	"net/http"
	"sort"
	"strings"

	"manifest/fundraising"
)

// The person note is the one identity behind the pipeline. This file holds
// the owner-side mechanics that keep it so: the startup sweep that adopts
// notes for names that already have one, the review list for the rest, and
// the address list the mail touch refresher works from.

// SweepFundraisingPeople runs the identity sweep once contacts and the
// pipeline are both attached: registry rows and opportunity people that
// resolve to a person note adopt it (registry emails land on the note), and
// an opportunity named exactly like a person with nobody linked gets that
// person.
func (s *Server) SweepFundraisingPeople() (fundraising.SweepResult, error) {
	if s.fundraising == nil || s.contacts == nil {
		return fundraising.SweepResult{}, nil
	}
	return s.fundraising.Sweep(s.contacts.NoteFor, s.contacts.AddEmails)
}

// FundraisingLinkedEmails is every address on a person linked to an open
// opportunity — the set the mailbox is asked about. Bounded on purpose: the
// mail signal is for the pipeline, not for every contact in the vault.
func (s *Server) FundraisingLinkedEmails() []string {
	if s.fundraising == nil || s.contacts == nil {
		return nil
	}
	ops, _ := s.fundraising.List()
	seen := map[string]bool{}
	out := []string{}
	for _, op := range ops {
		if op.Archived {
			continue
		}
		for _, p := range op.People {
			for _, em := range s.contacts.EmailsOf(p.Key) {
				em = strings.ToLower(strings.TrimSpace(em))
				if em != "" && !seen[em] {
					seen[em] = true
					out = append(out, em)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// handlePeopleReview lists the names waiting to become person notes.
func (s *Server) handlePeopleReview(w http.ResponseWriter, _ *http.Request) {
	if s.fundraising == nil {
		writeJSON(w, map[string]any{"pending": []any{}})
		return
	}
	writeJSON(w, map[string]any{"pending": s.fundraising.Pending()})
}

// handlePeopleReviewCreate settles a pending name by creating its note (full
// name, optional email and location) and rewriting the pipeline to it. An
// owner action: this is the one place a Sheet-typed name becomes a note.
func (s *Server) handlePeopleReviewCreate(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil || s.contacts == nil {
		http.Error(w, "contacts unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key           string `json:"key"`
		Origin        string `json:"origin"`
		OpportunityID string `json:"opportunityId"`
		Name          string `json:"name"`
		Email         string `json:"email"`
		Location      string `json:"location"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Key) == "" || strings.TrimSpace(b.Name) == "" {
		httpError(w, errBadRequest("key and full name are required"))
		return
	}
	name := strings.TrimSpace(b.Name)
	key, rel, err := s.contacts.EnsureNote(strings.ToLower(name), name)
	if err != nil {
		httpError(w, err)
		return
	}
	if em := strings.TrimSpace(b.Email); em != "" {
		if err := s.contacts.AddEmails(rel, []string{em}); err != nil {
			httpError(w, err)
			return
		}
	}
	if loc := strings.TrimSpace(b.Location); loc != "" {
		if s.geocoder != nil {
			if _, ok := s.geocoder.CachedPlace(loc); !ok {
				s.geocoder.EnqueuePlace(loc)
			}
		}
		if _, err := s.contacts.SaveLocation(key, name, loc, ""); err != nil {
			httpError(w, err)
			return
		}
	}
	if err := s.fundraising.ResolvePending(b.Key, b.Origin, b.OpportunityID, fundraising.PersonRef{Key: key, Display: name, NotePath: rel}); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "key": key, "pending": s.fundraising.Pending()})
}

// handlePeopleReviewLink settles a pending name as an existing contact.
func (s *Server) handlePeopleReviewLink(w http.ResponseWriter, r *http.Request) {
	if s.fundraising == nil || s.contacts == nil {
		http.Error(w, "contacts unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Key           string `json:"key"`
		Origin        string `json:"origin"`
		OpportunityID string `json:"opportunityId"`
		ContactKey    string `json:"contactKey"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Key) == "" || strings.TrimSpace(b.ContactKey) == "" {
		httpError(w, errBadRequest("key and contactKey are required"))
		return
	}
	rel, ok := s.contacts.NoteFor(b.ContactKey)
	if !ok {
		httpError(w, errBadRequest("that contact has no person note yet"))
		return
	}
	to := fundraising.PersonRef{Key: strings.ToLower(strings.TrimSpace(b.ContactKey)), Display: s.contacts.DisplayOf(b.ContactKey), NotePath: rel}
	if err := s.fundraising.ResolvePending(b.Key, b.Origin, b.OpportunityID, to); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "key": to.Key, "pending": s.fundraising.Pending()})
}
