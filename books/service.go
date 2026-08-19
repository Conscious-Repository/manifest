// Package books provides the app's single, rate-limited Open Library client:
// the catalogue lookup behind "+ book". It is the reading surface's twin of
// package geocode — one shared limiter, a disk cache under dataDir, and an
// attribution line, so the shelf can name a book without the owner typing its
// author from memory.
//
// Open Library is the Internet Archive's open catalogue: no key, no account,
// metadata in the public domain. Results are DERIVED state (§2) — the record
// the owner keeps is the vault note the pick writes, never this cache.
package books

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Attribution is shown wherever results are: the catalogue is someone else's
// work and says so, the way the place picker credits OpenStreetMap.
const Attribution = "Book data © Open Library (Internet Archive)"

// Result is one catalogue candidate — the fields the shelf record wants, and
// nothing else. Authors is ordered as the catalogue returns it (first author
// first) because that is the one the note links.
type Result struct {
	Title       string   `json:"title"`
	Authors     []string `json:"authors"`
	Year        string   `json:"year,omitempty"`  // first publication, "" when unknown
	Pages       int      `json:"pages,omitempty"` // median across editions, 0 when unknown
	Key         string   `json:"key,omitempty"`   // Open Library work key, e.g. /works/OL45804W
	Attribution string   `json:"attribution"`
}

type diskCache struct {
	Queries map[string]cached `json:"queries"`
}

type cached struct {
	At      time.Time `json:"at"`
	Results []Result  `json:"results"`
}

// Service is the one client. Every lookup passes its limiter, so the reading
// surface cannot outrun a courteous request rate no matter how fast the owner
// types.
type Service struct {
	cachePath string
	base      string
	hc        *http.Client
	interval  time.Duration
	ttl       time.Duration

	mu    sync.Mutex
	cache diskCache

	requestMu sync.Mutex
	last      time.Time
}

// New loads the cache. A missing or corrupt cache is not an error — it is
// derived state; the next lookup refills it.
func New(dataDir string) *Service {
	s := &Service{
		cachePath: filepath.Join(dataDir, "books.json"),
		base:      "https://openlibrary.org",
		hc:        &http.Client{Timeout: 12 * time.Second},
		interval:  1100 * time.Millisecond,
		ttl:       30 * 24 * time.Hour, // a catalogue answer for a typed query is stable
		cache:     diskCache{Queries: map[string]cached{}},
	}
	if b, err := os.ReadFile(s.cachePath); err == nil {
		var c diskCache
		if json.Unmarshal(b, &c) == nil && c.Queries != nil {
			s.cache = c
		}
	}
	return s
}

// Search returns the catalogue's best candidates for a typed query. A cached
// query never leaves the machine; a live one waits its turn behind the
// limiter. An error is an error — a failed lookup must never read as "no such
// book" (§portals: failure ≠ empty).
func (s *Service) Search(ctx context.Context, query string) ([]Result, error) {
	q := strings.Join(strings.Fields(query), " ")
	if q == "" {
		return nil, errors.New("a title is required")
	}
	k := strings.ToLower(q)
	s.mu.Lock()
	if hit, ok := s.cache.Queries[k]; ok && time.Since(hit.At) < s.ttl {
		out := append([]Result(nil), hit.Results...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	if err := s.waitTurn(ctx); err != nil {
		return nil, err
	}
	params := url.Values{
		"q":      {q},
		"limit":  {"8"},
		"fields": {"key,title,author_name,first_publish_year,number_of_pages_median,edition_count"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/search.json?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// Open Library asks clients to identify themselves so they can reach a
	// human about traffic; this is that.
	req.Header.Set("User-Agent", "manifest-dashboard/1.0 (private personal reading shelf)")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("book lookup failed: " + resp.Status)
	}
	var raw struct {
		Docs []struct {
			Key          string   `json:"key"`
			Title        string   `json:"title"`
			AuthorName   []string `json:"author_name"`
			FirstPublish int      `json:"first_publish_year"`
			Pages        int      `json:"number_of_pages_median"`
			Editions     int      `json:"edition_count"`
		} `json:"docs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(raw.Docs))
	editions := map[string]int{}
	for _, d := range raw.Docs {
		title := strings.Join(strings.Fields(d.Title), " ")
		if title == "" {
			continue
		}
		r := Result{Title: title, Key: d.Key, Pages: d.Pages, Attribution: Attribution}
		for _, a := range d.AuthorName {
			if a = strings.Join(strings.Fields(a), " "); a != "" {
				r.Authors = append(r.Authors, a)
			}
		}
		if d.FirstPublish > 0 {
			r.Year = itoa(d.FirstPublish)
		}
		out = append(out, r)
		editions[title+"\x00"+strings.Join(r.Authors, ",")] = d.Editions
	}
	rank(out, q, editions)
	if len(out) > 6 {
		out = out[:6]
	}
	s.mu.Lock()
	s.cache.Queries[k] = cached{At: time.Now(), Results: append([]Result(nil), out...)}
	s.persistLocked()
	s.mu.Unlock()
	return out, nil
}

// rank orders candidates the way a person reading the list would: the title
// they typed first, then how canonical the record is (an edition count is the
// catalogue's own measure of that), and an author-less record last — it cannot
// fill in the thing the picker exists to fill in. Open Library's raw order is
// a keyword match, which puts a summary-of and an unrelated book above the
// book itself often enough to be worth correcting here.
func rank(out []Result, query string, editions map[string]int) {
	q := norm(query)
	qt := strings.Fields(q)
	score := func(r Result) int {
		t := norm(r.Title)
		n := 0
		// an exact hit and "the title, then its subtitle" are the SAME claim on
		// the query — a short exact match on an obscure record must not outrank
		// the canonical book, so the edition count decides inside this band
		if t == q || strings.HasPrefix(t, q) {
			n += 6
		} else if strings.Contains(t, q) {
			n += 3
		}
		// people type the author too ("power broker caro"), and a query rarely
		// matches a title verbatim — so count how much of it the record answers
		inTitle := " " + t + " "
		authors := " " + norm(strings.Join(r.Authors, " ")) + " "
		for _, w := range qt {
			if strings.Contains(inTitle, " "+w+" ") {
				n += 2
			}
			if strings.Contains(authors, " "+w+" ") {
				n += 2
			}
		}
		return n
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := score(out[i]), score(out[j])
		if si != sj {
			return si > sj
		}
		if hasAuthor(out[i]) != hasAuthor(out[j]) {
			return hasAuthor(out[i])
		}
		return editions[key(out[i])] > editions[key(out[j])]
	})
}

func hasAuthor(r Result) bool { return len(r.Authors) > 0 }
func key(r Result) string     { return r.Title + "\x00" + strings.Join(r.Authors, ",") }

// norm folds a title for comparison: lowercase, punctuation out, one space.
// "The Power Broker: Robert Moses…" and "the power broker" meet here.
func norm(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// waitTurn spaces requests, and yields to a cancelled request rather than
// holding the limiter while the owner types on.
func (s *Service) waitTurn(ctx context.Context) error {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if wait := s.interval - time.Since(s.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.last = time.Now()
	return nil
}

func (s *Service) persistLocked() {
	b, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(s.cachePath), 0o700) != nil {
		return
	}
	tmp := s.cachePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.cachePath)
	}
}

func itoa(n int) string {
	if n == 0 {
		return ""
	}
	digits := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}
