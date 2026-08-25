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
	// tombstoneCap bounds the dismissed-forever set. Ids are 12 hex chars, so
	// 2,000 of them is ~50KB — cheap enough to keep long after the item record
	// itself has been pruned.
	tombstoneCap = 2000
)

// Store owns the dataDir tree.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore roots a store at <dataDir>/consume.
func NewStore(dataDir string) *Store { return &Store{root: filepath.Join(dataDir, "consume")} }

type cacheState struct {
	Cursors map[string]string `json:"cursors"`
	Items   []Item            `json:"items"`

	// Tombstones are the ids of items dismissed forever. They MUST outlive the
	// item records they refer to: prune drops a dismissed item after 90 days,
	// but plenty of feeds still list a post a year later, and without a
	// tombstone that post would arrive again as brand new. Dismiss means gone.
	Tombstones []string `json:"tombstones,omitempty"`

	LastPoll string `json:"lastPoll"`
	LastOK   string `json:"lastOK"`
	LastErr  string `json:"lastErr"`

	// Fails counts consecutive failed polls, and drives the backoff that stops
	// us retrying a dead feed hourly forever.
	Fails int `json:"fails,omitempty"`
	// RetryAfter is a publisher's explicit "not before" (429/503 Retry-After).
	RetryAfter string `json:"retryAfter,omitempty"`
	// TTLMinutes is the feed's own <ttl> / sy:updatePeriod hint, if it gave one.
	TTLMinutes int `json:"ttlMinutes,omitempty"`
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
	s.commit(subID, now, ok, items, cursors, errMsg, PollMeta{})
}

// PollMeta is what a fetcher learned about SCHEDULING during a poll, as opposed
// to about content: the feed's own refresh hint and any explicit back-off the
// publisher asked for.
type PollMeta struct {
	TTLMinutes int       // <ttl> / sy:updatePeriod, 0 = none offered
	RetryAfter time.Time // from a 429/503 Retry-After header; zero = none
}

func (s *Store) commit(subID string, now time.Time, ok bool, items []Item, cursors map[string]string, errMsg string, meta PollMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.read(subID)
	st.LastPoll = now.UTC().Format(time.RFC3339)
	if meta.TTLMinutes > 0 {
		st.TTLMinutes = meta.TTLMinutes
	}
	st.RetryAfter = ""
	if !meta.RetryAfter.IsZero() {
		st.RetryAfter = meta.RetryAfter.UTC().Format(time.RFC3339)
	}
	if !ok {
		st.LastErr = errMsg
		// Each consecutive failure pushes the next attempt further out. A feed
		// that has been dead for a week should not still be polled hourly.
		st.Fails++
		s.write(subID, st)
		return
	}
	st.LastErr = ""
	st.Fails = 0
	st.LastOK = st.LastPoll
	for k, v := range cursors {
		st.Cursors[k] = v
	}

	tomb := map[string]bool{}
	for _, id := range st.Tombstones {
		tomb[id] = true
	}
	at := map[string]int{}
	byLink := map[string]int{}
	for i, it := range st.Items {
		at[it.ID] = i
		if k := curateKey(it.URL); k != "" {
			byLink[k] = i
		}
	}
	for _, in := range items {
		if tomb[in.ID] {
			continue // dismissed forever — the feed re-listing it changes nothing
		}
		body := in.Body
		in.Body = "" // bodies live in their own files, never in the cache
		// ⚠ Second dedupe axis. Item identity prefers the canonical link, but
		// ~41% of feeds regenerate their guid on every fetch, and a feed that
		// also rewrites its links would otherwise produce a fresh "new" item
		// every hour. Matching on the NORMALIZED url (tracking params stripped)
		// collapses those before they reach the lane.
		if _, known := at[in.ID]; !known {
			if k := curateKey(in.URL); k != "" {
				if i, dup := byLink[k]; dup {
					in.ID = st.Items[i].ID // adopt the identity we already hold
				}
			}
		}
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
			if k := curateKey(in.URL); k != "" {
				byLink[k] = len(st.Items) - 1
			}
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
			st.Tombstones = addTombstone(st.Tombstones, itemID)
		}
		s.write(subID, st)
		return true
	}
	return false
}

// Undismiss reverses a dismissal — both the item's own state and the tombstone,
// or the next poll would silently re-suppress it.
func (s *Store) Undismiss(subID, itemID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read(subID)
	found := false
	for i, it := range st.Items {
		if it.ID == itemID {
			st.Items[i].DismissedAt = ""
			st.Items[i].ReadAt = "" // undo puts it back where it was: unread
			found = true
			break
		}
	}
	kept := st.Tombstones[:0]
	for _, id := range st.Tombstones {
		if id != itemID {
			kept = append(kept, id)
		}
	}
	st.Tombstones = kept
	s.write(subID, st)
	return found
}

// MarkAllRead marks every unread item read. Returns how many it touched.
func (s *Store) MarkAllRead(subID string, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read(subID)
	ts := now.UTC().Format(time.RFC3339)
	n := 0
	for i, it := range st.Items {
		if it.Unread() {
			st.Items[i].ReadAt = ts
			n++
		}
	}
	if n > 0 {
		s.write(subID, st)
	}
	return n
}

// addTombstone appends an id, keeping the set bounded and duplicate-free.
func addTombstone(ids []string, id string) []string {
	for _, x := range ids {
		if x == id {
			return ids
		}
	}
	ids = append(ids, id)
	if len(ids) > tombstoneCap {
		ids = ids[len(ids)-tombstoneCap:]
	}
	return ids
}

// Schedule reports what the poll loop needs to decide whether a subscription is
// due: when it last ran, how many consecutive failures it has, the feed's own
// refresh hint, and any publisher-requested back-off.
func (s *Store) Schedule(subID string) (lastPoll time.Time, fails int, ttl time.Duration, retryAfter time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.read(subID)
	if st.LastPoll != "" {
		lastPoll, _ = time.Parse(time.RFC3339, st.LastPoll)
	}
	if st.RetryAfter != "" {
		retryAfter, _ = time.Parse(time.RFC3339, st.RetryAfter)
	}
	return lastPoll, st.Fails, time.Duration(st.TTLMinutes) * time.Minute, retryAfter
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
