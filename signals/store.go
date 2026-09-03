package signals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store persists the user's signal decisions under DataDir (outside both trees,
// per the derived-state invariant) — dismissals keyed by the condition hash so a
// dismissal re-arms only when the world actually changed, and snoozes by lapse
// time. Mirrors contacts.Store (mutex + JSON save()).
type Store struct {
	path string
	mu   sync.Mutex
	st   sigState
}

type sigState struct {
	Dismissed map[string]string `json:"dismissed"` // signal id -> condition hash it was dismissed at
	Snoozed   map[string]string `json:"snoozed"`   // signal id -> RFC3339 until
}

// NewStore loads (or initializes) the store at <dataDir>/feed-signals.json.
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		path: filepath.Join(dataDir, "feed-signals.json"),
		st:   sigState{Dismissed: map[string]string{}, Snoozed: map[string]string{}},
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
	if s.st.Dismissed == nil {
		s.st.Dismissed = map[string]string{}
	}
	if s.st.Snoozed == nil {
		s.st.Snoozed = map[string]string{}
	}
	return s, nil
}

// Suppressed reports whether a signal should be hidden: dismissed at the SAME
// hash (re-arms when the hash changes), or snoozed with time still to run.
//
// This is also where lapsed snoozes are garbage-collected: the read path is the
// only place that knows the caller's notion of "now", so purging here (rather
// than against the wall clock in save) keeps the GC consistent with the answer
// it just gave. Persisting the prune is best-effort — the in-memory state is
// already pruned, and the next write will land it regardless.
func (s *Store) Suppressed(id, hash string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prune(now) {
		_ = s.save()
	}
	if h, ok := s.st.Dismissed[id]; ok && h == hash {
		return true
	}
	if until, ok := s.st.Snoozed[id]; ok {
		if t, err := time.Parse(time.RFC3339, until); err == nil && now.Before(t) {
			return true
		}
	}
	return false
}

// prune drops snoozes that have lapsed relative to now (or that fail to parse)
// so the file doesn't grow unbounded. Reports whether anything was removed.
// Caller holds s.mu.
func (s *Store) prune(now time.Time) bool {
	changed := false
	for id, until := range s.st.Snoozed {
		if t, err := time.Parse(time.RFC3339, until); err != nil || !now.Before(t) {
			delete(s.st.Snoozed, id)
			changed = true
		}
	}
	return changed
}

// Dismiss records that a signal was dismissed at its current condition hash.
func (s *Store) Dismiss(id, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Dismissed[id] = hash
	delete(s.st.Snoozed, id)
	return s.save()
}

// Snooze suppresses a signal until the given time (clears any dismissal).
func (s *Store) Snooze(id string, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Snoozed[id] = until.UTC().Format(time.RFC3339)
	delete(s.st.Dismissed, id)
	return s.save()
}

// save writes the current state. It deliberately does not consult the wall
// clock: lapsed-snooze GC is driven by the now the reader supplies (see
// Suppressed), never by time.Now.
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
