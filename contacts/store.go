// Package contacts is the people layer over the read-only vault index
// (plans/contacts-feature.md). It composes the index graph with a small triage
// store and the vault writer. The ONLY vault writes are explicit user actions
// (create a person note, bind an alias, confirm an email); everything else reads.
package contacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Store persists the user's triage decisions — which note-less link targets are
// confirmed people, which were dismissed (never re-asked), and alias bindings of
// one spelling to another entity. It lives under DataDir (outside the vault) and
// survives index rebuilds, since the index is a disposable projection.
type Store struct {
	path string
	mu   sync.Mutex
	st   state
}

type state struct {
	Confirmed map[string]bool   `json:"confirmed"` // note-less keys confirmed as people
	Dismissed map[string]bool   `json:"dismissed"` // keys dismissed from triage (remembered)
	Orgs      map[string]bool   `json:"orgs"`      // keys marked as org/firm (kept — seed firm pages later)
	Bindings  map[string]string `json:"bindings"`  // variant key -> canonical key
}

// NewStore loads (or initializes) the triage store at <dataDir>/contacts.json.
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		path: filepath.Join(dataDir, "contacts.json"),
		st:   state{Confirmed: map[string]bool{}, Dismissed: map[string]bool{}, Bindings: map[string]string{}},
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return nil, err
	}
	if s.st.Confirmed == nil {
		s.st.Confirmed = map[string]bool{}
	}
	if s.st.Dismissed == nil {
		s.st.Dismissed = map[string]bool{}
	}
	if s.st.Orgs == nil {
		s.st.Orgs = map[string]bool{}
	}
	if s.st.Bindings == nil {
		s.st.Bindings = map[string]string{}
	}
	return s, nil
}

func key(k string) string { return strings.ToLower(strings.TrimSpace(k)) }

// Confirm marks a note-less target as a real person (auto-adds to contacts, and
// clears any prior dismissal).
func (s *Store) Confirm(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Confirmed[key(k)] = true
	delete(s.st.Dismissed, key(k))
	return s.save()
}

// Dismiss remembers that a note-less target is NOT a person; it never returns to
// the triage queue.
func (s *Store) Dismiss(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Dismissed[key(k)] = true
	delete(s.st.Confirmed, key(k))
	return s.save()
}

// Bind records that variant is another spelling of canonical (identity merge via
// alias, never by rewriting notes).
func (s *Store) Bind(variant, canonical string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key(variant) != key(canonical) {
		s.st.Bindings[key(variant)] = key(canonical)
	}
	return s.save()
}

// MarkOrg records a note-less target as an org/firm — removed from the person
// triage queue but REMEMBERED (it will seed firm pages later), never discarded.
func (s *Store) MarkOrg(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Orgs[key(k)] = true
	delete(s.st.Dismissed, key(k))
	delete(s.st.Confirmed, key(k))
	return s.save()
}

// DismissAll bulk-dismisses the long tail in one write.
func (s *Store) DismissAll(keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		s.st.Dismissed[key(k)] = true
		delete(s.st.Confirmed, key(k))
	}
	return s.save()
}

func (s *Store) IsConfirmed(k string) bool {
	return s.read(func() bool { return s.st.Confirmed[key(k)] })
}
func (s *Store) IsDismissed(k string) bool {
	return s.read(func() bool { return s.st.Dismissed[key(k)] })
}
func (s *Store) IsOrg(k string) bool {
	return s.read(func() bool { return s.st.Orgs[key(k)] })
}

// VariantsOf returns every variant key bound to the given canonical key.
func (s *Store) VariantsOf(canonical string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for v, c := range s.st.Bindings {
		if c == key(canonical) {
			out = append(out, v)
		}
	}
	return out
}

// CanonicalOf returns the canonical key a variant is bound to, or "" if unbound.
func (s *Store) CanonicalOf(k string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Bindings[key(k)]
}

func (s *Store) read(fn func() bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}
