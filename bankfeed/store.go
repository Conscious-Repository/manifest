package bankfeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store — the portals secrets/cache split:
//   - <dataDir>/bankfeeds/feed.json (0600): the access URL + account links.
//     The same class as portal creds — never the vault, never synced.
//   - <dataDir>/bankfeed-cache/state.json: cursors + seen-txn ids + digest
//     cards. Disposable; deleting it re-pulls the backfill window and the
//     workbench dedupe absorbs the repeats.
type Store struct {
	mu       sync.Mutex
	dir      string // <dataDir>/bankfeeds
	cacheDir string // <dataDir>/bankfeed-cache

	feed  feedFile
	cache cacheFile
}

type feedFile struct {
	AccessURL string `json:"accessUrl"`
	Links     []Link `json:"links"`
}

type cacheFile struct {
	Cursors map[string]string          `json:"cursors"` // simplefinID → RFC3339 of newest posted
	Seen    map[string]map[string]bool `json:"seen"`    // simplefinID → txn id set
	Digests []Digest                   `json:"digests"`
}

func NewStore(dataDir string) *Store {
	s := &Store{
		dir:      filepath.Join(dataDir, "bankfeeds"),
		cacheDir: filepath.Join(dataDir, "bankfeed-cache"),
	}
	s.load()
	return s
}

func (s *Store) load() {
	if raw, err := os.ReadFile(filepath.Join(s.dir, "feed.json")); err == nil {
		_ = json.Unmarshal(raw, &s.feed)
	}
	if raw, err := os.ReadFile(filepath.Join(s.cacheDir, "state.json")); err == nil {
		_ = json.Unmarshal(raw, &s.cache)
	}
	if s.cache.Cursors == nil {
		s.cache.Cursors = map[string]string{}
	}
	if s.cache.Seen == nil {
		s.cache.Seen = map[string]map[string]bool{}
	}
}

func (s *Store) saveFeed() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	out, err := json.MarshalIndent(s.feed, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "feed.json"), out, 0o600)
}

func (s *Store) saveCache() {
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return
	}
	out, _ := json.Marshal(s.cache)
	_ = os.WriteFile(filepath.Join(s.cacheDir, "state.json"), out, 0o644)
}

func (s *Store) AccessURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feed.AccessURL
}

func (s *Store) SetAccessURL(url string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feed.AccessURL = url
	return s.saveFeed()
}

func (s *Store) Links() []Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Link, len(s.feed.Links))
	copy(out, s.feed.Links)
	return out
}

// LinkFor returns the link bound to a bridge account id.
func (s *Store) LinkFor(simplefinID string) (Link, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.feed.Links {
		if l.SimplefinID == simplefinID {
			return l, true
		}
	}
	return Link{}, false
}

// Upsert links a bridge account (or updates its binding). Empty entitySlug
// removes the link.
func (s *Store) Upsert(link Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.feed.Links {
		if l.SimplefinID == link.SimplefinID {
			if link.EntitySlug == "" {
				s.feed.Links = append(s.feed.Links[:i], s.feed.Links[i+1:]...)
			} else {
				link.LastSync, link.LastError = l.LastSync, l.LastError
				s.feed.Links[i] = link
			}
			return s.saveFeed()
		}
	}
	if link.EntitySlug == "" {
		return nil
	}
	s.feed.Links = append(s.feed.Links, link)
	return s.saveFeed()
}

// SetLinkHealth stamps a sync outcome ("" error = healthy).
func (s *Store) SetLinkHealth(simplefinID, lastSync, lastError string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.feed.Links {
		if s.feed.Links[i].SimplefinID == simplefinID {
			if lastSync != "" {
				s.feed.Links[i].LastSync = lastSync
			}
			s.feed.Links[i].LastError = lastError
			_ = s.saveFeed()
			return
		}
	}
}

func (s *Store) Cursor(simplefinID string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := time.Parse(time.RFC3339, s.cache.Cursors[simplefinID])
	if err != nil {
		return time.Time{}
	}
	return t
}

func (s *Store) Seen(simplefinID, txnID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache.Seen[simplefinID][txnID]
}

// MarkSeen records txn ids + advances the cursor to the newest posted time.
func (s *Store) MarkSeen(simplefinID string, txnIDs []string, newest time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.cache.Seen[simplefinID]
	if set == nil {
		set = map[string]bool{}
		s.cache.Seen[simplefinID] = set
	}
	for _, id := range txnIDs {
		set[id] = true
	}
	if !newest.IsZero() {
		cur, _ := time.Parse(time.RFC3339, s.cache.Cursors[simplefinID])
		if newest.After(cur) {
			s.cache.Cursors[simplefinID] = newest.Format(time.RFC3339)
		}
	}
	s.saveCache()
}

// AddDigest files one FEED card entry (auto-apply receipt / sync notice).
func (s *Store) AddDigest(d Digest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache.Digests = append(s.cache.Digests, d)
	if len(s.cache.Digests) > 200 {
		s.cache.Digests = s.cache.Digests[len(s.cache.Digests)-200:]
	}
	s.saveCache()
}

// Digests returns the undismissed cards, newest first.
func (s *Store) Digests() []Digest {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Digest
	for i := len(s.cache.Digests) - 1; i >= 0; i-- {
		if !s.cache.Digests[i].Dismissed {
			out = append(out, s.cache.Digests[i])
		}
	}
	return out
}

// Dismiss hides one digest card.
func (s *Store) Dismiss(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cache.Digests {
		if s.cache.Digests[i].ID == id {
			s.cache.Digests[i].Dismissed = true
		}
	}
	s.saveCache()
}
