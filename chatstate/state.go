// Package chatstate stores owner-only conversation UI state. It never sends
// messages, changes source transcripts, or writes into a team's portal store.
package chatstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

var ErrConflict = errors.New("conversation state changed on another device")
var ErrInvalid = errors.New("invalid conversation state")
var keyRE = regexp.MustCompile(`^conversation-[0-9a-f]{32}$`)
var landingKeyRE = regexp.MustCompile(`^landing-[0-9a-f]{32}$`)
var artifactKeyRE = regexp.MustCompile(`^artifact-[0-9a-f]{16}$`)

type Snapshot struct {
	Key      string          `json:"key"`
	Slot     string          `json:"slot"`
	Revision uint64          `json:"revision"`
	Value    json.RawMessage `json:"value"`
	Updated  string          `json:"updated,omitempty"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) *Store { return &Store{root: root} }
func valid(key, slot string) bool {
	return (key == "inbox" && slot == "pins") || (keyRE.MatchString(key) && (slot == "draft" || slot == "view" || slot == "deliveries")) || (landingKeyRE.MatchString(key) && (slot == "draft" || slot == "deliveries")) || (artifactKeyRE.MatchString(key) && slot == "edit")
}
func (s *Store) path(key, slot string) string { return filepath.Join(s.root, key+"-"+slot+".json") }

func canonical(value json.RawMessage) (json.RawMessage, error) {
	if len(value) > 96000 {
		return nil, ErrInvalid
	}
	var obj map[string]any
	d := json.NewDecoder(bytes.NewReader(value))
	d.UseNumber()
	if err := d.Decode(&obj); err != nil {
		return nil, ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, ErrInvalid
	}
	b, err := json.Marshal(obj)
	return b, err
}

func (s *Store) read(key, slot string) (Snapshot, error) {
	if !valid(key, slot) {
		return Snapshot{}, ErrInvalid
	}
	b, err := os.ReadFile(s.path(key, slot))
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{Key: key, Slot: slot, Value: json.RawMessage(`null`)}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if json.Unmarshal(b, &snap) != nil || snap.Key != key || snap.Slot != slot || snap.Revision == 0 {
		return Snapshot{}, ErrInvalid
	}
	value, err := canonical(snap.Value)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Value = value
	return snap, nil
}

func (s *Store) Read(key, slot string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(key, slot)
}

// Write compares before replacing; a stale device cannot resurrect a cleared
// draft. Identical retries are idempotent, including a lost successful response.
func (s *Store) Write(key, slot string, expected uint64, value json.RawMessage) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.read(key, slot)
	if err != nil {
		return Snapshot{}, err
	}
	value, err = canonical(value)
	if err != nil {
		return Snapshot{}, err
	}
	if bytes.Equal(value, current.Value) {
		return current, nil
	}
	if expected != current.Revision {
		return current, ErrConflict
	}
	if current.Revision == ^uint64(0) {
		return current, ErrInvalid
	}
	next := Snapshot{Key: key, Slot: slot, Revision: current.Revision + 1, Value: value, Updated: time.Now().UTC().Format(time.RFC3339Nano)}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return current, err
	}
	if err = os.MkdirAll(s.root, 0700); err != nil {
		return current, err
	}
	f, err := os.CreateTemp(s.root, ".chat-state-*")
	if err != nil {
		return current, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return current, err
	}
	if err = os.Rename(f.Name(), s.path(key, slot)); err != nil {
		return current, err
	}
	d, err := os.Open(s.root)
	if err != nil {
		return next, err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return next, err
	}
	return next, nil
}
