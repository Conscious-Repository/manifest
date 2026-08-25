package consume

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The dataDir side: poll caches and article bodies. Everything here is
// rebuildable by re-polling, which is exactly why it is allowed to live in
// tier 3 while the subscription list and the curated notes are not.
//
//	<dataDir>/consume/cache/<subID>.json    item metadata + cursors + status
//	<dataDir>/consume/snapshots/<hash>.html sanitized bodies, one file each
//
// Bodies are separate files on purpose. The cache is re-read from disk on
// every lane render (the portals/ habit), and a few hundred articles of inline
// HTML would turn each of those reads into megabytes of parsing for data the
// list view never displays.

// Retention rules. Unread reading is NOT a notice and must never silently
// expire — an essay the owner has not got to yet is still wanted. What ages
// out is what he already dealt with.
const (
	readRetention = 90 * 24 * time.Hour // read/dismissed items age out
	unreadCap     = 200                 // newest-N unread kept per subscription
)

// Store owns the dataDir tree.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore roots a store at <dataDir>/consume.
func NewStore(dataDir string) *Store { return &Store{root: filepath.Join(dataDir, "consume")} }

type cacheState struct {
	Cursors  map[string]string `json:"cursors"`
	Items    []Item            `json:"items"`
	LastPoll string            `json:"lastPoll"`
	LastOK   string            `json:"lastOK"`
	LastErr  string            `json:"lastErr"`
}

func (s *Store) cacheFile(subID string) string {
	return filepath.Join(s.root, "cache", safeName(subID)+".json")
}

func (s *Store) snapshotFile(itemID string) string {
	return filepath.Join(s.root, "snapshots", snapshotName(itemID))
}

// safeName keeps a subscription id from escaping its directory. Ids are slugs
// today, but the id can come from a hand-edited vault line.
func safeName(id string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, id)
	if clean == "" {
		return "unnamed"
	}
	return clean
}

func (s *Store) read(subID string) cacheState {
	st := cacheState{Cursors: map[string]string{}}
	if b, err := os.ReadFile(s.cacheFile(subID)); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Cursors == nil {
		st.Cursors = map[string]string{}
	}
	return st
}

func (s *Store) write(subID string, st cacheState) {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	path := s.cacheFile(subID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(b) {
		return // unchanged — no churn
	}
	_ = os.WriteFile(path, b, 0o644)
}

// Cursors returns the stored poll cursors for a subscription.
func (s *Store) Cursors(subID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.read(subID).Cursors {
		out[k] = v
	}
	return out
}

// Commit merges one poll's result.
//
// ok=false records the failure and keeps the previous cache intact — the
// failure ≠ empty discipline (portals/store.go). A feed that 500s for a day
// must not empty the lane and read as "you are all caught up".
func (s *Store) Commit(subID string, now time.Time, ok bool, items []Item, cursors map[string]string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.read(subID)
	st.LastPoll = now.UTC().Format(time.RFC3339)
	if !ok {
		st.LastErr = errMsg
		s.write(subID, st)
		return
	}
	st.LastErr = ""
	st.LastOK = st.LastPoll
	for k, v := range cursors {
		st.Cursors[k] = v
	}

	at := map[string]int{}
	for i, it := range st.Items {
		at[it.ID] = i
	}
	for _, in := range items {
		body := in.Body
		in.Body = "" // bodies live in their own files, never in the cache
		if i, ok := at[in.ID]; ok {
			// ⚠ An upsert must NOT clobber lifecycle state. portals/ replaces
			// the whole event because a notice has none; here, re-polling a
			// feed re-delivers every item in it, and a wholesale replace would
			// mark everything the owner had already read as unread again on
			// the very next poll.
			prior := st.Items[i]
			in.ReadAt = prior.ReadAt
			in.DismissedAt = prior.DismissedAt
			if in.FetchedAt.IsZero() {
				in.FetchedAt = prior.FetchedAt
			}
			st.Items[i] = in
		} else {
			st.Items = append(st.Items, in)
			at[in.ID] = len(st.Items) - 1
		}
		if body != "" {
			s.putBody(in.ID, body)
		}
	}
	st.Items = s.prune(st.Items, now)
	s.write(subID, st)
}

// prune drops what the owner has finished with and caps the unread backlog.
// The two rules are deliberately asymmetric: read/dismissed items are history
// and expire; unread items are a promise and only ever fall off the far end of
// a very long queue.
func (s *Store) prune(items []Item, now time.Time) []Item {
	cut := now.Add(-readRetention)
	kept := make([]Item, 0, len(items))
	unread := 0
	sort.SliceStable(items, func(i, j int) bool { return itemTime(items[i]).After(itemTime(items[j])) })
	for _, it := range items {
		if !it.Unread() {
			if stamp(it).Before(cut) {
				s.dropBody(it.ID)
				continue
			}
			kept = append(kept, it)
			continue
		}
		if unread >= unreadCap {
			s.dropBody(it.ID)
			continue
		}
		unread++
		kept = append(kept, it)
	}
	return kept
}

// itemTime is the sort key: when it was published, falling back to when we saw
// it, so a feed with broken dates still orders sensibly.
func itemTime(it Item) time.Time {
	if !it.PublishedAt.IsZero() {
		return it.PublishedAt
	}
	return it.FetchedAt
}

// stamp is when the owner finished with the item — the clock retention runs on.
func stamp(it Item) time.Time {
	for _, s := range []string{it.DismissedAt, it.ReadAt} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return itemTime(it)
}

// Items returns one subscription's cached items, newest first.
func (s *Store) Items(subID string) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]Item(nil), s.read(subID).Items...)
	sort.SliceStable(items, func(i, j int) bool { return itemTime(items[i]).After(itemTime(items[j])) })
	return items
}

// Status returns (lastOK, lastErr) for the manage panel's dot.
func (s *Store) Status(subID string) (time.Time, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read(subID)
	var lastOK time.Time
	if st.LastOK != "" {
		lastOK, _ = time.Parse(time.RFC3339, st.LastOK)
	}
	return lastOK, st.LastErr
}

// Mark sets read/dismissed state on one item. Returns false when the id is not
// in this subscription's cache.
func (s *Store) Mark(subID, itemID string, read, dismissed bool, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read(subID)
	ts := now.UTC().Format(time.RFC3339)
	for i, it := range st.Items {
		if it.ID != itemID {
			continue
		}
		if read && st.Items[i].ReadAt == "" {
			st.Items[i].ReadAt = ts
		}
		if dismissed {
			st.Items[i].DismissedAt = ts
			if st.Items[i].ReadAt == "" {
				st.Items[i].ReadAt = ts
			}
		}
		s.write(subID, st)
		return true
	}
	return false
}

// Get returns one item with its body loaded — the reader's fetch.
func (s *Store) Get(subID, itemID string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.read(subID).Items {
		if it.ID == itemID {
			it.Body = s.body(it.ID)
			return it, true
		}
	}
	return Item{}, false
}

// Body returns one item's stored HTML ("" when the snapshot is gone).
func (s *Store) Body(itemID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body(itemID)
}

func (s *Store) body(itemID string) string {
	b, err := os.ReadFile(s.snapshotFile(itemID))
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *Store) putBody(itemID, html string) {
	path := s.snapshotFile(itemID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == html {
		return
	}
	_ = os.WriteFile(path, []byte(html), 0o644)
}

func (s *Store) dropBody(itemID string) { _ = os.Remove(s.snapshotFile(itemID)) }

// Forget deletes a subscription's cache and every body it owned —
// unsubscribing should not leave a subscription's worth of disk behind.
// Curated notes are in the vault and are untouched by design.
func (s *Store) Forget(subID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.read(subID).Items {
		s.dropBody(it.ID)
	}
	_ = os.Remove(s.cacheFile(subID))
}
