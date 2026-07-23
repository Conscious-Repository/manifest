package portals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---- credential store (secrets, 0600, outside the vault) ----

// Store holds api-key portal credentials under <dataDir>/portals/<id>.json,
// mode 0600 — mirroring calendar/oauth.go's owner-only secret write. Credentials
// never enter config.json, the vault, any tracked file, or a log line. An
// environment override (MANIFEST_PORTAL_<ID>_<FIELD>) wins over the file, so a
// key can be injected without ever touching disk.
type Store struct {
	dir string
	mu  sync.Mutex
}

// credFile is the on-disk shape: the credential fields plus an optional
// per-portal poll interval override ("15m", "1h").
type credFile struct {
	Fields   map[string]string `json:"fields"`
	Interval string            `json:"interval,omitempty"`
}

func NewStore(dataDir string) *Store {
	return &Store{dir: filepath.Join(dataDir, "portals")}
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

var envUnsafe = regexp.MustCompile(`[^A-Z0-9]`)

// envKey builds MANIFEST_PORTAL_<ID>_<FIELD> (upper, non-alnum → _).
func envKey(id, field string) string {
	up := func(s string) string { return envUnsafe.ReplaceAllString(strings.ToUpper(s), "_") }
	return "MANIFEST_PORTAL_" + up(id) + "_" + up(field)
}

func (s *Store) load(id string) credFile {
	cf := credFile{Fields: map[string]string{}}
	if b, err := os.ReadFile(s.path(id)); err == nil {
		_ = json.Unmarshal(b, &cf)
		if cf.Fields == nil {
			cf.Fields = map[string]string{}
		}
	}
	return cf
}

// Creds returns the effective credentials for a portal: the file values with any
// per-field env override applied on top. Callers must not log the result.
func (s *Store) Creds(id string, def Def) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	cf := s.load(id)
	for _, f := range def.Fields {
		v := strings.TrimSpace(cf.Fields[f.Key])
		if ev := strings.TrimSpace(os.Getenv(envKey(id, f.Key))); ev != "" {
			v = ev
		}
		if v != "" {
			out[f.Key] = v
		}
	}
	return out
}

// SetCreds merges the provided fields into the file (empty value deletes a
// field) and writes 0600. Returns the effective creds after the write.
func (s *Store) SetCreds(id string, def Def, fields map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cf := s.load(id)
	for _, f := range def.Fields {
		v, ok := fields[f.Key]
		if !ok {
			continue
		}
		if v = strings.TrimSpace(v); v == "" {
			delete(cf.Fields, f.Key)
		} else {
			cf.Fields[f.Key] = v
		}
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(id), b, 0o600) // owner-only
}

// Clear deletes the credential file (disconnect).
func (s *Store) Clear(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Interval returns the per-portal override, or the def default.
func (s *Store) Interval(id string, def Def) time.Duration {
	s.mu.Lock()
	cf := s.load(id)
	s.mu.Unlock()
	if d, err := time.ParseDuration(cf.Interval); err == nil && d >= time.Minute {
		return d
	}
	return def.Interval
}

// HasCreds reports whether every secret field is present (via file or env).
func (s *Store) HasCreds(id string, def Def) bool {
	creds := s.Creds(id, def)
	for _, f := range def.Fields {
		if f.Secret && creds[f.Key] == "" {
			return false
		}
	}
	// A portal with no secret fields (none in v1) is never "connected".
	return def.primarySecret() != ""
}

// Masked renders the key column: last 4 of the primary secret, or "".
func (s *Store) Masked(id string, def Def) string {
	if k := def.primarySecret(); k != "" {
		return maskLast4(s.Creds(id, def)[k])
	}
	return ""
}

// HaveKeys lists which credential fields are currently set (names only).
func (s *Store) HaveKeys(id string, def Def) []string {
	creds := s.Creds(id, def)
	var out []string
	for _, f := range def.Fields {
		if creds[f.Key] != "" {
			out = append(out, f.Key)
		}
	}
	sort.Strings(out)
	return out
}

// ---- per-portal cache (derived, disposable, rebuildable — never in the vault) ----

// Cache is one polled portal's on-disk state under
// <dataDir>/portal-cache/<id>/. It holds the poll cursors, the accumulated event
// log, and the user's dismissals — exactly the calendar-cache discipline:
// derived data outside both trees, written only on change.
type Cache struct {
	dir string
	mu  sync.Mutex
}

type cacheState struct {
	Cursors   map[string]string `json:"cursors"`   // per-object-type: kind → RFC3339 high-water
	Events    []Event           `json:"events"`    // accumulated, aged out at retention
	Dismissed map[string]string `json:"dismissed"` // card id → RFC3339 dismissed-at
	LastPoll  string            `json:"lastPoll"`  // RFC3339 of the last poll attempt
	LastOK    string            `json:"lastOK"`    // RFC3339 of the last successful poll
	LastErr   string            `json:"lastErr"`   // degraded reason from the last failure
}

func newCache(dataDir, id string) *Cache {
	return &Cache{dir: filepath.Join(dataDir, "portal-cache", id)}
}

func (c *Cache) file() string { return filepath.Join(c.dir, "cache.json") }

func (c *Cache) read() cacheState {
	st := cacheState{Cursors: map[string]string{}, Dismissed: map[string]string{}}
	if b, err := os.ReadFile(c.file()); err == nil {
		_ = json.Unmarshal(b, &st)
		if st.Cursors == nil {
			st.Cursors = map[string]string{}
		}
		if st.Dismissed == nil {
			st.Dismissed = map[string]string{}
		}
	}
	return st
}

func (c *Cache) write(st cacheState) {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}
	if existing, err := os.ReadFile(c.file()); err == nil && string(existing) == string(b) {
		return // unchanged — no churn
	}
	_ = os.WriteFile(c.file(), b, 0o644)
}

// Cursor returns the high-water mark for one object-kind (zero if never polled).
func (c *Cache) Cursor(kind string) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v := c.read().Cursors[kind]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

const retention = 14 * 24 * time.Hour // portal items are notices, not obligations

// Commit merges a successful poll's events + advanced cursors into the cache,
// ages out anything past retention, and records the crossing time. Dedupe is by
// Event.ID (idempotent re-polls). Passing ok=false records the failure and keeps
// the old cache intact — no data ≠ all-clear (same rule as signal emitters).
func (c *Cache) Commit(now time.Time, ok bool, events []Event, cursors map[string]string, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.read()
	st.LastPoll = now.UTC().Format(time.RFC3339)
	if !ok {
		st.LastErr = errMsg
		c.write(st)
		return
	}
	st.LastErr = ""
	st.LastOK = st.LastPoll
	for k, v := range cursors {
		st.Cursors[k] = v
	}
	seen := map[string]int{}
	for i, e := range st.Events {
		seen[e.ID] = i
	}
	for _, e := range events {
		if i, ok := seen[e.ID]; ok {
			st.Events[i] = e // an edited object re-surfaces with its new payload
		} else {
			st.Events = append(st.Events, e)
			seen[e.ID] = len(st.Events) - 1
		}
	}
	// Age out old events and their dismissals.
	cut := now.Add(-retention)
	kept := st.Events[:0]
	for _, e := range st.Events {
		if e.At.After(cut) {
			kept = append(kept, e)
		}
	}
	st.Events = kept
	for id, at := range st.Dismissed {
		if t, err := time.Parse(time.RFC3339, at); err == nil && t.Before(cut) {
			delete(st.Dismissed, id)
		}
	}
	c.write(st)
}

// Events returns the cached events newest-first (for card building).
func (c *Cache) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.read()
	out := append([]Event(nil), st.Events...)
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// Dismiss records a card id as dismissed (survives reload; GC'd at retention).
func (c *Cache) Dismiss(cardID string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.read()
	st.Dismissed[cardID] = now.UTC().Format(time.RFC3339)
	c.write(st)
}

// Dismissed reports whether a card id has been dismissed.
func (c *Cache) Dismissed(cardID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.read().Dismissed[cardID]
	return ok
}

// Status returns (lastOK, lastErr) for the panel row.
func (c *Cache) Status() (lastOK time.Time, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.read()
	if st.LastOK != "" {
		lastOK, _ = time.Parse(time.RFC3339, st.LastOK)
	}
	return lastOK, st.LastErr
}
