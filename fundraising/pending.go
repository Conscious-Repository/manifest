package fundraising

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The person note is the one identity. system/crm/contacts.md — once a
// registry of note-less "CRM contacts" — is now only the holding list for
// names that have not become notes yet; Sweep drains it as notes appear and
// the review list on the Contacts page lets the owner finish the rest.

// SweepResult counts what one sweep changed.
type SweepResult struct {
	Adopted   int // registry rows that matched an existing person note
	Relinked  int // opportunity people that gained their note path
	AutoLinks int // opportunities named after a person who is now linked
	Pending   int // names still waiting for the owner
}

// Sweep walks every registry row and opportunity person: a key that resolves
// to a people note adopts it (registry emails land on the note through
// adopt, the row leaves the registry, every opportunity link learns the
// path). An opportunity named exactly like a person note with nobody linked
// gets that person — "adam gries" is adam gries. resolve answers with the
// note path of a people note, and only that.
func (s *Store) Sweep(resolve func(key string) (string, bool), adopt func(notePath string, emails []string) error) (SweepResult, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var res SweepResult
	people, raw, err := s.registry()
	if err != nil {
		return res, err
	}
	notes := map[string]string{} // key → note path, for keys the sweep resolved
	lookup := func(key string) (string, bool) {
		key = normalizeKey(key)
		if rel, ok := notes[key]; ok {
			return rel, rel != ""
		}
		rel, ok := resolve(key)
		if !ok {
			rel = ""
		}
		notes[key] = rel
		return rel, ok
	}
	keep := people[:0]
	for _, p := range people {
		if p.NotePath != "" {
			continue // already a note: the row is redundant and goes
		}
		rel, ok := lookup(p.Key)
		if !ok {
			keep = append(keep, p)
			continue
		}
		if len(p.Emails) > 0 && adopt != nil {
			if err := adopt(rel, p.Emails); err != nil {
				return res, err
			}
		}
		res.Adopted++
	}
	res.Pending = len(keep)
	if len(keep) != len(people) {
		if err := s.saveRegistry(keep, raw); err != nil {
			return res, err
		}
	}
	ops, err := s.List()
	if err != nil {
		return res, err
	}
	for _, op := range ops {
		changed := false
		for i := range op.People {
			if op.People[i].NotePath != "" {
				continue
			}
			if rel, ok := lookup(op.People[i].Key); ok {
				op.People[i].NotePath = rel
				changed = true
				res.Relinked++
			}
		}
		if op.Source != nil && op.Source.Contact != nil && op.Source.Contact.NotePath == "" {
			if rel, ok := lookup(op.Source.Contact.Key); ok {
				op.Source.Contact.NotePath = rel
				changed = true
			}
		}
		if len(op.People) == 0 && len(op.UnlinkedPeople) == 0 {
			if rel, ok := lookup(op.Firm); ok {
				op.People = append(op.People, PersonRef{Key: normalizeKey(op.Firm), Display: strings.TrimSpace(op.Firm), NotePath: rel})
				changed = true
				res.AutoLinks++
			}
		}
		if changed {
			if err := s.replaceKnown(op); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// Pending lists every name on the pipeline that is not a person note yet,
// with the opportunities it appears on: registry rows without a note, and
// names collaborators typed into the Sheet (kept as plain text until the
// owner decides).
func (s *Store) Pending() []PendingPerson {
	ops, _ := s.List()
	byKey := map[string]*PendingPerson{}
	order := []string{}
	add := func(key, display, origin string, emails []string, op *Opportunity) {
		key = normalizeKey(key)
		if key == "" {
			return
		}
		id := origin + ":" + key
		p, ok := byKey[id]
		if !ok {
			p = &PendingPerson{Key: key, Display: strings.TrimSpace(display), Origin: origin, Opportunities: []PendingRef{}}
			byKey[id] = p
			order = append(order, id)
		}
		if p.Display == "" {
			p.Display = strings.TrimSpace(display)
		}
		p.Emails = dedupeEmails(append(p.Emails, emails...))
		if op != nil {
			for _, ref := range p.Opportunities {
				if ref.ID == op.ID {
					return
				}
			}
			p.Opportunities = append(p.Opportunities, PendingRef{ID: op.ID, Firm: op.Firm})
		}
	}
	for _, p := range s.RegistryPeople() {
		if p.NotePath == "" {
			add(p.Key, p.Display, "registry", p.Emails, nil)
		}
	}
	for i := range ops {
		op := ops[i]
		for _, p := range op.People {
			if p.NotePath == "" {
				add(p.Key, p.Display, "registry", p.Emails, &op)
			}
		}
		for _, name := range op.UnlinkedPeople {
			add(name, name, "sheet", nil, &op)
		}
	}
	out := make([]PendingPerson, 0, len(order))
	for _, id := range order {
		out = append(out, *byKey[id])
	}
	sortPending(out)
	return out
}

// ResolvePending settles one pending name with the owner's answer: `to` is
// the person it becomes (an existing contact, or the note just created).
// A registry name is rewritten on every opportunity that links it and leaves
// the registry; a Sheet name leaves that opportunity's plain-text list and
// joins its linked people. The registry file itself goes once it is empty.
func (s *Store) ResolvePending(key, origin, opportunityID string, to PersonRef) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	key = normalizeKey(key)
	to.Key = normalizeKey(to.Key)
	to.Display = strings.TrimSpace(to.Display)
	to.NotePath = filepath.ToSlash(strings.TrimSpace(to.NotePath))
	if key == "" || to.Key == "" || to.Display == "" || to.NotePath == "" {
		return errors.New("a pending person resolves to a person note")
	}
	switch origin {
	case "registry":
		ops, err := s.List()
		if err != nil {
			return err
		}
		for _, op := range ops {
			changed := false
			for i := range op.People {
				if normalizeKey(op.People[i].Key) == key {
					op.People[i] = to
					changed = true
				}
			}
			if op.Source != nil && op.Source.Contact != nil && normalizeKey(op.Source.Contact.Key) == key {
				c := to
				op.Source.Contact = &c
				changed = true
			}
			if changed {
				op.People = mergePeople(nil, op.People)
				if err := s.replaceKnown(op); err != nil {
					return err
				}
			}
		}
		people, raw, err := s.registry()
		if err != nil {
			return err
		}
		keep := people[:0]
		for _, p := range people {
			if p.Key != key {
				keep = append(keep, p)
			}
		}
		if len(keep) == 0 {
			return s.dropRegistry()
		}
		return s.saveRegistry(keep, raw)
	case "sheet":
		op, ok := s.Get(opportunityID)
		if !ok {
			return fmt.Errorf("opportunity %q not found", opportunityID)
		}
		plain := op.UnlinkedPeople[:0]
		found := false
		for _, name := range op.UnlinkedPeople {
			if normalizeKey(name) == key {
				found = true
				continue
			}
			plain = append(plain, name)
		}
		if !found {
			return fmt.Errorf("%q is not pending on %s", key, op.Firm)
		}
		op.UnlinkedPeople = plain
		op.People = mergePeople(nil, append(op.People, to))
		return s.replaceKnown(op)
	}
	return fmt.Errorf("unknown pending origin %q", origin)
}

// dropRegistry removes the registry file once nothing is pending. Removal
// goes through the injected remover so this package still never touches the
// vault on its own; without one the file is left holding only its heading.
func (s *Store) dropRegistry() error {
	if s.removePeople != nil {
		return s.removePeople(s.abs(s.registryRel))
	}
	return s.saveRegistry(nil, []string{strings.TrimRight(registrySeed, "\n")})
}

// UseRegistryRemover injects the capability that deletes the registry file
// when it empties.
func (s *Store) UseRegistryRemover(remove func(abs string) error) { s.removePeople = remove }
