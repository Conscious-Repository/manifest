package contacts

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Touch is one dated interaction with a person and where the date came from.
// It is a projection: nothing here is ever written to a note.
type Touch struct {
	Date      string `json:"date"`
	Kind      string `json:"kind"` // met | email | transcript | note | upcoming | manual
	Person    string `json:"person,omitempty"`
	PersonKey string `json:"personKey,omitempty"`
	Title     string `json:"title,omitempty"`
	Ref       string `json:"ref,omitempty"` // vault-relative path when the source is a note or transcript
}

// touchRank orders kinds on an equal date: a calendar meeting is the firmest
// evidence, then mail, then a transcript, then a note that merely links the
// person, then something typed by hand.
var touchRank = map[string]int{"met": 0, "upcoming": 0, "email": 1, "transcript": 2, "note": 3, "manual": 4}

// Later reports whether a should replace b as the last touch: the later date
// wins, and on the same date the firmer kind wins.
func (a Touch) Later(b Touch) bool {
	if b.Date == "" {
		return a.Date != ""
	}
	if a.Date != b.Date {
		return a.Date > b.Date
	}
	return touchRank[a.Kind] < touchRank[b.Kind]
}

// Sooner reports whether a should replace b as the next touch: the earlier
// date wins, and on the same date the firmer kind wins.
func (a Touch) Sooner(b Touch) bool {
	if b.Date == "" {
		return a.Date != ""
	}
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	return touchRank[a.Kind] < touchRank[b.Kind]
}

// Touches computes one person's last and next touch from every source the
// people layer can see: calendar meetings matched by email, mail sent or
// received on the configured mailbox, transcripts, dated notes that link them,
// and the next confirmed-email calendar event. A note dated after today is a
// plan, not a touch. Either return is zero when nothing dated exists.
func (s *Service) Touches(rawKey string, now time.Time) (last, next Touch) {
	canon := s.canonical(rawKey)
	e, ok := s.ix.Entity(canon)
	display := canon
	if ok && e.Display != "" {
		display = e.Display
	}
	if !ok && s.directory != nil {
		if ext, extOK := s.directory.Person(canon); extOK && ext.Display != "" {
			display = ext.Display
		}
	}
	hasNote := ok && e.NotePath != ""
	emails := s.contactEmails(canon, hasNote)
	keys := s.keysFor(canon, e.NotePath)
	today := now.Format("2006-01-02")

	var candidates []Touch
	if ms := s.meetingsFor(emails, now); len(ms) > 0 {
		candidates = append(candidates, Touch{Date: ms[0].Date, Kind: "met", Title: ms[0].Title})
	}
	if t, ok := s.mailTouchFor(emails); ok {
		candidates = append(candidates, t)
	}
	tl := s.mergedTimeline(keys)
	for _, t := range s.transcripts(keys, tl) {
		if t.Date != "" {
			candidates = append(candidates, Touch{Date: t.Date, Kind: "transcript", Title: t.Title, Ref: t.Path})
		}
	}
	for _, en := range tl { // newest dated first; a note dated ahead is a plan, so keep looking
		if en.Date == "" || en.IsTranscript || en.Date > today {
			continue
		}
		candidates = append(candidates, Touch{Date: en.Date, Kind: "note", Title: en.Name, Ref: en.Path})
		break
	}
	for _, c := range candidates {
		if c.Date == "" || c.Date > today {
			continue
		}
		if c.Later(last) {
			last = c
		}
	}

	if len(emails) > 0 {
		want := map[string]bool{}
		for _, em := range emails {
			want[strings.ToLower(strings.TrimSpace(em))] = true
		}
		for _, ev := range s.upcomingEvents(now) {
			date := ev.Start.Format("2006-01-02")
			if date < today {
				continue
			}
			for _, a := range ev.Attendees {
				if want[strings.ToLower(strings.TrimSpace(a.Email))] {
					if c := (Touch{Date: date, Kind: "upcoming", Title: ev.Title}); c.Sooner(next) {
						next = c
					}
					break
				}
			}
		}
	}
	if last.Date != "" {
		last.Person, last.PersonKey = display, canon
	}
	if next.Date != "" {
		next.Person, next.PersonKey = display, canon
	}
	return last, next
}

// ---- mail as a touch (read-only, one mailbox, cached) ----

// MailTouch is the newest message exchanged with one address.
type MailTouch struct {
	Date    string `json:"date"`
	Subject string `json:"subject,omitempty"`
	Sent    bool   `json:"sent"` // true when the owner sent it
}

// MailReader is the minimal read-only mailbox surface the touch computation
// needs. Decoupled from Gmail so the service stays testable; main adapts the
// read-only Gmail client to it.
type MailReader interface {
	// LatestExchange returns the newest message sent to or received from the
	// address inside the reader's own lookback, or ok=false when there is none.
	LatestExchange(ctx context.Context, address string) (MailTouch, bool, error)
}

// mailCacheTTL bounds how often one address is asked about again. A cache is
// derived state, so it lives under dataDir and survives restarts.
const mailCacheTTL = 30 * time.Minute

type mailEntry struct {
	MailTouch
	Found     bool      `json:"found"`
	CheckedAt time.Time `json:"checkedAt"`
}

type mailCache struct {
	mu      sync.Mutex
	path    string
	reader  MailReader
	entries map[string]mailEntry // email-lower → newest exchange
}

// UseMail attaches the read-only mailbox and the cache file its answers
// persist to. Nothing is read until RefreshMail runs.
func (s *Service) UseMail(r MailReader, cachePath string) {
	c := &mailCache{path: cachePath, reader: r, entries: map[string]mailEntry{}}
	if b, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(b, &c.entries)
		if c.entries == nil {
			c.entries = map[string]mailEntry{}
		}
	}
	s.mu.Lock()
	s.mail = c
	s.mu.Unlock()
}

// RefreshMail asks the mailbox about every address whose cached answer is
// older than the TTL, one address at a time, and persists the cache. It is
// meant for a background loop: it never runs on a request path.
func (s *Service) RefreshMail(ctx context.Context, emails []string) (int, error) {
	s.mu.Lock()
	c := s.mail
	s.mu.Unlock()
	if c == nil || c.reader == nil {
		return 0, errors.New("mailbox not configured")
	}
	now := time.Now()
	todo := []string{}
	seen := map[string]bool{}
	c.mu.Lock()
	for _, em := range emails {
		em = strings.ToLower(strings.TrimSpace(em))
		if em == "" || seen[em] {
			continue
		}
		seen[em] = true
		if ent, ok := c.entries[em]; ok && now.Sub(ent.CheckedAt) < mailCacheTTL {
			continue
		}
		todo = append(todo, em)
	}
	c.mu.Unlock()
	sort.Strings(todo)
	refreshed := 0
	var firstErr error
	for _, em := range todo {
		if ctx.Err() != nil {
			break
		}
		t, found, err := c.reader.LatestExchange(ctx, em)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // a failed lookup keeps whatever was cached
		}
		c.mu.Lock()
		c.entries[em] = mailEntry{MailTouch: t, Found: found, CheckedAt: time.Now()}
		c.mu.Unlock()
		refreshed++
	}
	if refreshed > 0 {
		if err := c.save(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return refreshed, firstErr
}

func (c *mailCache) save() error {
	c.mu.Lock()
	b, err := json.MarshalIndent(c.entries, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// mailTouchFor is the newest cached exchange across a person's addresses.
func (s *Service) mailTouchFor(emails []string) (Touch, bool) {
	s.mu.Lock()
	c := s.mail
	s.mu.Unlock()
	if c == nil {
		return Touch{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var best Touch
	for _, em := range emails {
		ent, ok := c.entries[strings.ToLower(strings.TrimSpace(em))]
		if !ok || !ent.Found || ent.Date == "" {
			continue
		}
		t := Touch{Date: ent.Date, Kind: "email", Title: ent.Subject}
		if t.Later(best) {
			best = t
		}
	}
	return best, best.Date != ""
}

// MailStatus reports whether a mailbox is attached and how many addresses the
// cache knows about — for the settings row, never for a decision.
func (s *Service) MailStatus() (attached bool, cached int) {
	s.mu.Lock()
	c := s.mail
	s.mu.Unlock()
	if c == nil {
		return false, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reader != nil, len(c.entries)
}

// ---- the person note as the one identity ----

// NoteFor returns the vault-relative person note for a key, or ok=false when
// the key has no people note (a bare link target is not a contact).
func (s *Service) NoteFor(rawKey string) (string, bool) {
	e, ok := s.ix.Entity(s.canonical(rawKey))
	if !ok || e.NotePath == "" || !e.IsPerson {
		return "", false
	}
	return e.NotePath, true
}

// EnsureNote makes a person real in the vault: an existing people note is
// returned as is, otherwise one is created from the display name (lowercase
// file, categories: [people]). Owner action only — the tracker's "add person".
func (s *Service) EnsureNote(rawKey, display string) (key, notePath string, err error) {
	canon := s.canonical(rawKey)
	if rel, ok := s.NoteFor(canon); ok {
		return canon, rel, nil
	}
	name := strings.TrimSpace(display)
	if name == "" {
		name = canon
	}
	rel, err := s.vw.CreatePersonNote(name, s.aliasesForNew(canon, name), "")
	if err != nil {
		return "", "", err
	}
	_ = s.store.Confirm(canon)
	if err := s.ix.ReindexPaths([]string{rel}); err != nil {
		return "", "", err
	}
	key = strings.ToLower(strings.TrimSuffix(filepath.Base(rel), ".md"))
	return key, rel, nil
}

// AddEmails records addresses on a person note (idempotent) and drops the
// calendar caches so a fresh email matches immediately.
func (s *Service) AddEmails(notePath string, emails []string) error {
	changed := false
	for _, em := range emails {
		em = strings.ToLower(strings.TrimSpace(em))
		if em == "" {
			continue
		}
		if err := s.vw.AddFrontmatterValue(notePath, "email", em); err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if err := s.ix.ReindexPaths([]string{notePath}); err != nil {
		return err
	}
	s.invalidateMeetings()
	return nil
}

// EmailsOf returns the addresses on a person's note (none for a note-less key).
func (s *Service) EmailsOf(rawKey string) []string {
	canon := s.canonical(rawKey)
	e, ok := s.ix.Entity(canon)
	return s.contactEmails(canon, ok && e.NotePath != "")
}

// DisplayOf is a person's display name as the index knows it (the key when
// unknown).
func (s *Service) DisplayOf(rawKey string) string {
	canon := s.canonical(rawKey)
	if e, ok := s.ix.Entity(canon); ok && e.Display != "" {
		return e.Display
	}
	return canon
}
