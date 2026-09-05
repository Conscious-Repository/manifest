package recruiting

import (
	"strings"

	"manifest/record"
)

// EDIT AND DELETE — the gestures these documents never had.
//
// seeds.md, network/people.md and network/edges.md were append-only from the
// UI: a mistyped seed name or a duplicated person was permanent unless the
// owner opened the markdown by hand. That was an omission, not a policy — the
// board's "archive is reversible, there is no delete" is a deliberate rule
// about CANDIDATES, who carry judgment and history, and it silently read as a
// promise about the whole surface.
//
// The rule the owner set (2026-09-05): **delete the cheap things, archive the
// people.** A place and an edge are re-addable in seconds, so they cut; a
// person — candidate or connector — archives, because the record is the
// history of a decision.
//
// Every mutator here works the record way: find the row, change or drop THAT
// row, leave every other byte alone. A hand-edited row this package does not
// recognize round-trips untouched, and an unknown id is an error rather than a
// silent no-op — a delete that reports success without deleting is how a UI
// starts lying.

// Remove drops one seed by id. Reports whether a row went.
func (d *SeedsDoc) Remove(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	kept := d.Lines[:0]
	gone := false
	for _, ln := range d.Lines {
		if ln.Row != nil && strings.TrimSpace(ln.Row.Get("id")) == id {
			gone = true
			continue
		}
		kept = append(kept, ln)
	}
	d.Lines = kept
	return gone
}

// Update edits one seed in place. Only the fields a person can see are
// writable; id, added and source stay as they were, because they are the
// row's identity and its provenance, not its content.
func (d *SeedsDoc) Update(id string, set map[string]string) (Seed, error) {
	row := findRow(d.Lines, "id", strings.TrimSpace(id))
	if row == nil {
		return Seed{}, errf("no such place %q", id)
	}
	for key, val := range set {
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			if val == "" {
				return Seed{}, errf("a place needs a name")
			}
			row.Set("name", val)
		case "class":
			if !ValidSeedClass(val) {
				return Seed{}, errf("class must be one of %s", strings.Join(SeedClasses, ", "))
			}
			row.Set("class", val)
		case "org", "url", "feed":
			// an emptied field is REMOVED, not written blank: a row carrying
			// `[url:: ]` reads as "has a url" to everything downstream
			if val == "" {
				row.Drop(key)
			} else {
				row.Set(key, val)
			}
		default:
			return Seed{}, errf("a place has no %q to edit", key)
		}
	}
	return seedOf(row), nil
}

// Remove drops one network person by id.
func (d *PeopleDoc) Remove(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	kept := d.Lines[:0]
	gone := false
	for _, ln := range d.Lines {
		if ln.Row != nil && strings.TrimSpace(ln.Row.Get("id")) == id {
			gone = true
			continue
		}
		kept = append(kept, ln)
	}
	d.Lines = kept
	return gone
}

// Update edits one network person. `email` is writable HERE and only here:
// D15 forbids an ADAPTER from ever setting one, and this path is the owner
// typing it (network.go's own rule, said again where it is enforced).
func (d *PeopleDoc) Update(id string, set map[string]string) (NetworkPerson, error) {
	row := findRow(d.Lines, "id", strings.TrimSpace(id))
	if row == nil {
		return NetworkPerson{}, errf("no such person %q", id)
	}
	for key, val := range set {
		val = strings.TrimSpace(val)
		k := strings.ToLower(strings.TrimSpace(key))
		switch k {
		case "name":
			if val == "" {
				return NetworkPerson{}, errf("a person needs a name")
			}
			row.Set("name", val)
		case "type":
			if val != "" && !ValidPersonType(val) {
				return NetworkPerson{}, errf("type must be one of %s", strings.Join(PersonTypes, ", "))
			}
			setOrRemove(row, "type", val)
		case "consent":
			if val != "" && !ValidConsent(val) {
				return NetworkPerson{}, errf("consent must be one of %s", strings.Join(ConsentKinds, ", "))
			}
			setOrRemove(row, "consent", val)
		case "email", "linkedin", "github", "org", "title", "archived", "ref":
			setOrRemove(row, k, val)
		default:
			return NetworkPerson{}, errf("a person has no %q to edit", key)
		}
	}
	return personOf(row), nil
}

// Remove drops one edge, addressed the way the graph addresses it: the two
// endpoints and the kind. An id would be a fourth spelling of a fact that is
// already identified by its ends (edges_identity.go's edgeKey).
func (d *EdgesDoc) Remove(from, to, kind string) bool {
	want := edgeKey(from, to, kind)
	kept := d.Lines[:0]
	gone := false
	for _, ln := range d.Lines {
		if ln.Row != nil && edgeKey(ln.Row.Get("from"), ln.Row.Get("to"), ln.Row.Get("kind")) == want {
			gone = true
			continue
		}
		kept = append(kept, ln)
	}
	d.Lines = kept
	return gone
}

// ---- store-level wrappers: load, mutate, save, under the lock ----

// RemoveSeed cuts one place.
func (s *Store) RemoveSeed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadSeeds()
	if !doc.Remove(id) {
		return errf("no such place %q", id)
	}
	return s.SaveSeeds(doc)
}

// UpdateSeed edits one place.
func (s *Store) UpdateSeed(id string, set map[string]string) (Seed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadSeeds()
	out, err := doc.Update(id, set)
	if err != nil {
		return Seed{}, err
	}
	if err := s.SaveSeeds(doc); err != nil {
		return Seed{}, err
	}
	return out, nil
}

// RemoveNetworkPerson cuts one connector. The edges naming them are left
// alone on purpose: an edge is a claim about a relationship, and deleting the
// node does not make the claim untrue — the NETWORK view shows an edge whose
// endpoint is no longer a record as exactly that.
func (s *Store) RemoveNetworkPerson(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadNetworkPeople()
	if !doc.Remove(id) {
		return errf("no such person %q", id)
	}
	return s.SaveNetworkPeople(doc)
}

// UpdateNetworkPerson edits one connector.
func (s *Store) UpdateNetworkPerson(id string, set map[string]string) (NetworkPerson, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadNetworkPeople()
	out, err := doc.Update(id, set)
	if err != nil {
		return NetworkPerson{}, err
	}
	if err := s.SaveNetworkPeople(doc); err != nil {
		return NetworkPerson{}, err
	}
	return out, nil
}

// RemoveEdge cuts one relationship claim.
func (s *Store) RemoveEdge(from, to, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadEdges()
	if !doc.Remove(from, to, kind) {
		return errf("no such edge %s → %s (%s)", from, to, kind)
	}
	return s.SaveEdges(doc)
}

// ---- small shared helpers ----

// findRow returns the first row whose `key` equals want, or nil.
func findRow(lines []Line, key, want string) *Row {
	if want == "" {
		return nil
	}
	for _, ln := range lines {
		if ln.Row != nil && strings.TrimSpace(ln.Row.Get(key)) == want {
			return ln.Row
		}
	}
	return nil
}

// setOrRemove writes a field, or removes it when the value is empty — an
// emptied optional field must leave no `[key:: ]` husk behind.
func setOrRemove(r *record.Row, key, val string) {
	if val == "" {
		r.Drop(key)
		return
	}
	r.Set(key, val)
}
