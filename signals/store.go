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
func (s *Store) Suppressed(id, hash string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *Store) save() error {
	// GC lapsed snoozes so the file doesn't grow unbounded.
	now := time.Now()
	for id, until := range s.st.Snoozed {
		if t, err := time.Parse(time.RFC3339, until); err != nil || !now.Before(t) {
			delete(s.st.Snoozed, id)
		}
	}
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}
