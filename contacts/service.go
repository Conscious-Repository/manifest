package contacts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/vaultindex"
	"manifest/vaultwriter"
)

// pastMeetingWindowDays is how far back calendar-verified "last met" looks
// (24 months); meetingCacheTTL bounds how often that pull is repeated.
const (
	pastMeetingWindowDays = 730
	meetingCacheTTL       = 30 * time.Minute
)

// CalendarReader is the minimal calendar surface the contacts layer needs for
// upcoming-meeting matching (§6) and calendar-verified "last met". Decoupled
// from the calendar package so the service is testable without Google; main.go
// adapts calendar.Client to this.
type CalendarReader interface {
	Upcoming(now time.Time, days int) []Event
	// PastMeetings returns timed, non-declined events with ≥1 non-self attendee
	// within the last `days`, for email-matched "last met" (calendar).
	PastMeetings(now time.Time, days int) []Event
}

// Event is a future calendar event with its non-self attendees.
type Event struct {
	Start     time.Time
	Title     string
	Attendees []Attendee
}

// Attendee is one participant (name + email as the calendar has them).
type Attendee struct{ Name, Email string }

// TranscriptSource yields transcripts for an entity from a backend (vault today;
// Granola is stubbed behind this interface — funder §4).
type TranscriptSource interface {
	Transcripts(keys []string) []Transcript
}

// Transcript is one conversation record (vault note or, later, Granola).
type Transcript struct {
	Date   string `json:"date"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Service is the people layer: it reads the vault index graph, applies the
// triage store, and performs the three user-action vault writes.
type Service struct {
	ix      *vaultindex.Index
	store   *Store
	vw      *vaultwriter.Writer
	cal     CalendarReader   // nil → no upcoming / no calendar last-met
	granola TranscriptSource // nil → vault-only transcripts

	mu       sync.Mutex    // guards the meeting cache below
	meetings *meetingIndex // email→past-meetings projection, TTL-cached
}

// meetingIndex is a cached projection of the past-meeting window keyed by
// attendee email, so a 24-month calendar pull is not repeated per render.
type meetingIndex struct {
	byEmail map[string][]meetingRef // email-lower → its meetings (deduped by date)
	builtAt time.Time
}

// meetingRef is one past calendar meeting (date + title) for the Meetings section.
type meetingRef struct{ Date, Title string }

// New builds the contacts service. cal and granola may be nil.
func New(ix *vaultindex.Index, store *Store, vw *vaultwriter.Writer, cal CalendarReader, granola TranscriptSource) *Service {
	return &Service{ix: ix, store: store, vw: vw, cal: cal, granola: granola}
}

// ---- list ----

// Contact is one row in the contacts list. "Last met" (calendar-verified, matched
// by the contact's email) is kept DISTINCT from "last mentioned" (newest dated
// note that links them) — writing [[them]] in a planning note is not a meeting.
type Contact struct {
	Key           string `json:"key"`
	Display       string `json:"display"`
	NotePath      string `json:"notePath"`
	HasNote       bool   `json:"hasNote"`
	LastMet       string `json:"lastMet"`       // calendar-verified (email-matched); "" if no email / no meeting
	LastMentioned string `json:"lastMentioned"` // newest dated NOTE that links them; "" if none
	RefCount      int    `json:"refCount"`
	Upcoming      string `json:"upcoming"`  // ISO date of the next CONFIRMED-email match, else ""
	OpenLoops     int    `json:"openLoops"` // unchecked meeting-context loops naming this contact
	// Neglect lens (pure computation, no writes). Basis is "meetings" when the
	// contact has a linked email with past meetings, else "mentions" (note dates).
	NeglectBasis string `json:"neglectBasis"`
	Interactions int    `json:"interactions"` // count of DATED interactions in the basis
	MedianGap    int    `json:"medianGap"`    // median days between them
	DaysSince    int    `json:"daysSince"`    // days since the last, -1 if none
	Cold         bool   `json:"cold"`         // 3+ interactions AND daysSince > max(30, 2×median)
}

// List returns all contacts, sorted by most-recent dated interaction (dated
// first, newest; undated last), then name. Triage-only note-less targets are
// excluded (they surface in Triage, not here).
func (s *Service) List(now time.Time) ([]Contact, error) {
	people, err := s.ix.PeopleNotes()
	if err != nil {
		return nil, err
	}
	targets, err := s.ix.NoteLessTargets()
	if err != nil {
		return nil, err
	}
	byKey := map[string]*Contact{}
	add := func(c Contact) {
		canon := s.canonical(c.Key)
		if canon != c.Key {
			return // a bound variant is absorbed by its canonical
		}
		if _, ok := byKey[c.Key]; !ok {
			byKey[c.Key] = &c
		}
	}
	for _, p := range people {
		add(Contact{Key: p.Key, Display: p.Display, NotePath: p.NotePath, HasNote: true})
	}
	for _, t := range targets {
		if s.store.IsDismissed(t.Key) {
			continue
		}
		if !t.InMeetingContext && !s.store.IsConfirmed(t.Key) {
			continue // triage-only — not a contact yet
		}
		add(Contact{Key: t.Key, Display: t.Display, RefCount: t.RefCount})
	}

	// confirmed-email upcoming matches, open-loop counts, and interaction-date
	// histories — each resolved ONCE for the whole list.
	upcoming := s.confirmedUpcoming(now)
	loopCounts, _ := s.ix.OpenLoopCounts()
	dateMap, _ := s.ix.InteractionDatesByKey()

	out := make([]Contact, 0, len(byKey))
	for _, c := range byKey {
		keys := s.keysFor(c.Key, c.NotePath)
		c.LastMentioned = s.mergedLastMet(keys) // newest dated note (was LastMet)
		if u, ok := upcoming[c.Key]; ok {
			c.Upcoming = u
		}
		var mentionDates []string
		for _, k := range keys {
			c.OpenLoops += loopCounts[k]
			mentionDates = append(mentionDates, dateMap[k]...)
		}
		// calendar-verified last-met + hybrid neglect basis (meetings, else mentions)
		meetDates := s.meetingDatesFor(s.emailsFor(c.HasNote, c.Key), now)
		c.LastMet = firstDate(meetDates) // meetDates is newest-first
		if len(meetDates) > 0 {
			c.NeglectBasis = "meetings"
			c.Interactions, c.MedianGap, c.DaysSince, c.Cold = neglect(meetDates, now)
		} else {
			c.NeglectBasis = "mentions"
			c.Interactions, c.MedianGap, c.DaysSince, c.Cold = neglect(mentionDates, now)
		}
		out = append(out, *c)
	}
	// sort by the most recent signal (calendar last-met or note mention), newest first
	prim := func(c Contact) string {
		if c.LastMet > c.LastMentioned {
			return c.LastMet
		}
		return c.LastMentioned
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := prim(out[i]), prim(out[j])
		if (pi == "") != (pj == "") {
			return pi != "" // dated contacts first
		}
		if pi != pj {
			return pi > pj // newest first
		}
		return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display)
	})
	return out, nil
}

// ---- page ----

// Ref is a lightweight entity reference (firms, search results).
type Ref struct {
	Key      string `json:"key"`
	Display  string `json:"display"`
	HasNote  bool   `json:"hasNote"`
	IsPerson bool   `json:"isPerson"`
	RefCount int    `json:"refCount"`
}

// TimelineItem is one dated interaction on the contact page.
type TimelineItem struct {
	Date         string `json:"date"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	SourceType   string `json:"sourceType"`
	IsTranscript bool   `json:"isTranscript"`
}

// Mention is one undated reference — never produces a date claim.
type Mention struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// MeetingItem is one past calendar meeting (email-matched) on the contact page —
// distinct from the note Timeline.
type MeetingItem struct {
	Date  string `json:"date"`
	Title string `json:"title"`
}

// UpcomingItem is a matched or candidate future event (§6).
type UpcomingItem struct {
	Date      string `json:"date"`
	Title     string `json:"title"`
	Confirmed bool   `json:"confirmed"`       // exact email match
	Email     string `json:"email,omitempty"` // attendee email to confirm (candidates)
}

// Page is the full contact page (spec §3).
type Page struct {
	Key           string          `json:"key"`
	Display       string          `json:"display"`
	NotePath      string          `json:"notePath"`
	HasNote       bool            `json:"hasNote"`
	Aliases       []string        `json:"aliases"`
	Firms         []Ref           `json:"firms"`
	LastMet       string          `json:"lastMet"`       // calendar-verified (email-matched)
	LastMentioned string          `json:"lastMentioned"` // newest dated note that links them
	Emails        []string        `json:"emails"`        // the contact's linked emails (manageable)
	Meetings      []MeetingItem   `json:"meetings"`      // past calendar meetings, newest-first
	Timeline      []TimelineItem  `json:"timeline"`
	Mentions      []Mention       `json:"mentions"`
	Transcripts   []Transcript    `json:"transcripts"`
	Upcoming      []UpcomingItem  `json:"upcoming"`
	Loops         []OpenLoopGroup `json:"loops"`
	NoteBody      string          `json:"noteBody"`
	// Neglect lens ("meetings" basis when email-linked, else "mentions")
	NeglectBasis string `json:"neglectBasis"`
	Interactions int    `json:"interactions"`
	MedianGap    int    `json:"medianGap"`
	DaysSince    int    `json:"daysSince"`
	Cold         bool   `json:"cold"`
}

// Page assembles the contact page for an entity key (or a bound variant).
func (s *Service) Page(rawKey string, now time.Time) (Page, bool) {
	canon := s.canonical(rawKey)
	e, ok := s.ix.Entity(canon)
	if !ok {
		// note-less but real (confirmed / has links): synthesize from links
		if disp, refs := s.displayAndRefs(canon); refs > 0 || s.store.IsConfirmed(canon) {
			e = vaultindex.Entity{Key: canon, Display: disp}
		} else {
			return Page{}, false
		}
	}
	p := Page{Key: e.Key, Display: e.Display, NotePath: e.NotePath, HasNote: e.NotePath != ""}
	keys := s.keysFor(e.Key, e.NotePath)

	if al, err := s.ix.EntityAliases(e.Key); err == nil {
		p.Aliases = al
	}
	if fm, err := s.ix.LinkedFirms(e.Key); err == nil {
		for _, f := range fm {
			p.Firms = append(p.Firms, Ref{Key: f.Key, Display: f.Display, HasNote: f.HasNote})
		}
	}

	tl := s.mergedTimeline(keys)
	var mentionDates []string
	for _, e := range tl {
		if e.Date != "" {
			if p.LastMentioned == "" {
				p.LastMentioned = e.Date // list is newest-first, so the first dated entry is last-mentioned
			}
			mentionDates = append(mentionDates, e.Date)
			p.Timeline = append(p.Timeline, TimelineItem{
				Date: e.Date, Name: e.Name, Path: e.Path, SourceType: e.SourceType, IsTranscript: e.IsTranscript,
			})
		} else {
			p.Mentions = append(p.Mentions, Mention{Name: e.Name, Path: e.Path})
		}
	}

	// calendar-verified meetings (email-matched) — the true "last met", distinct
	// from the note timeline above.
	if p.HasNote {
		if es, err := s.ix.Emails(p.Key); err == nil {
			p.Emails = es
		}
	}
	meetings := s.meetingsFor(p.Emails, now)
	var meetDates []string
	for _, m := range meetings {
		p.Meetings = append(p.Meetings, MeetingItem{Date: m.Date, Title: m.Title})
		meetDates = append(meetDates, m.Date)
	}
	p.LastMet = firstDate(meetDates) // meetings are newest-first

	p.Transcripts = s.transcripts(keys, tl)
	p.Upcoming = s.upcomingFor(p, now)
	p.Loops = s.OpenLoops(e.Key)
	// neglect basis: meeting cadence when email-linked, else note mentions
	if len(meetDates) > 0 {
		p.NeglectBasis = "meetings"
		p.Interactions, p.MedianGap, p.DaysSince, p.Cold = neglect(meetDates, now)
	} else {
		p.NeglectBasis = "mentions"
		p.Interactions, p.MedianGap, p.DaysSince, p.Cold = neglect(mentionDates, now)
	}
	if p.HasNote {
		p.NoteBody = s.noteBody(p.NotePath)
	}
	return p, true
}

// ---- quick-lookup card ----

// Card is the compact contact summary the command bar shows instantly.
type Card struct {
	Key              string      `json:"key"`
	Display          string      `json:"display"`
	HasNote          bool        `json:"hasNote"`
	LastMet          string      `json:"lastMet"`       // calendar-verified (email-matched)
	LastMentioned    string      `json:"lastMentioned"` // newest dated note that links them
	NextUpcoming     string      `json:"nextUpcoming"`  // "date · title", or ""
	LatestTranscript *Transcript `json:"latestTranscript,omitempty"`
	RefCount         int         `json:"refCount"`
}

// Card assembles the quick-lookup card (last met, next upcoming, latest
// transcript) without building the full page.
func (s *Service) Card(rawKey string, now time.Time) (Card, bool) {
	canon := s.canonical(rawKey)
	e, ok := s.ix.Entity(canon)
	if !ok {
		disp, refs := s.displayAndRefs(canon)
		if refs == 0 && !s.store.IsConfirmed(canon) {
			return Card{}, false
		}
		e = vaultindex.Entity{Key: canon, Display: disp}
	}
	c := Card{Key: e.Key, Display: e.Display, HasNote: e.NotePath != ""}
	keys := s.keysFor(e.Key, e.NotePath)
	c.LastMentioned = s.mergedLastMet(keys) // notes
	c.LastMet = firstDate(s.meetingDatesFor(s.emailsFor(c.HasNote, e.Key), now))
	tl := s.mergedTimeline(keys)
	c.RefCount = len(tl)
	if ts := s.transcripts(keys, tl); len(ts) > 0 {
		c.LatestTranscript = &ts[0] // timeline is newest-first
	}
	// next upcoming: the soonest confirmed-or-candidate event
	up := s.upcomingFor(Page{Key: e.Key, Display: e.Display, HasNote: e.NotePath != "", Aliases: aliasesOf(s, e.Key)}, now)
	if len(up) > 0 {
		c.NextUpcoming = up[0].Date + " · " + up[0].Title
	}
	return c, true
}

func aliasesOf(s *Service, key string) []string {
	al, _ := s.ix.EntityAliases(key)
	return al
}

// ---- triage ----

// TriageItem is a note-less name awaiting an "is this a person?" decision, with
// deterministic signals so the queue ranks likely-people first and flags likely-orgs.
type TriageItem struct {
	Key       string `json:"key"`
	Display   string `json:"display"`
	RefCount  int    `json:"refCount"`
	LikelyOrg bool   `json:"likelyOrg"` // linked FROM people notes → probably a firm they link to
	score     int
}

// Triage returns note-less targets seen ONLY outside meeting context, not yet
// confirmed/dismissed/marked-org (§4). Ranked by person-likelihood using
// DETERMINISTIC signals only (no guessing, never auto-confirmed):
//   - a 2+-capitalized-word display looks like a person's name (+)
//   - being linked FROM people notes looks like a firm those people link to (−)
func (s *Service) Triage() ([]TriageItem, error) {
	targets, err := s.ix.NoteLessTargets()
	if err != nil {
		return nil, err
	}
	var out []TriageItem
	for _, t := range targets {
		if t.InMeetingContext || s.store.IsConfirmed(t.Key) || s.store.IsDismissed(t.Key) || s.store.IsOrg(t.Key) {
			continue
		}
		if s.canonical(t.Key) != t.Key {
			continue // bound to another contact already
		}
		it := TriageItem{Key: t.Key, Display: t.Display, RefCount: t.RefCount, LikelyOrg: t.LinkedFromPeople > 0}
		if capitalizedWords(t.Display) >= 2 {
			it.score += 2 // looks like a person's name
		}
		if t.LinkedFromPeople > 0 {
			it.score -= 3 // people link their firms → org signal dominates
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score // most person-like first
		}
		return out[i].RefCount > out[j].RefCount
	})
	return out, nil
}

// Confirm promotes a note-less target to a contact AND materializes its note in
// the vault in the user's format: a LOWERCASE <name>.md with `categories: [people]`
// (write-once — an existing note is never touched). Hitting "Person" therefore
// creates the file, matching the existing lowercase person notes. Explicit user write.
func (s *Service) Confirm(key, display string) error {
	if err := s.store.Confirm(key); err != nil {
		return err
	}
	canon := s.canonical(key)
	if e, ok := s.ix.Entity(canon); ok && e.NotePath != "" {
		return nil // already note-backed — nothing to create
	}
	name := strings.TrimSpace(display)
	if name == "" {
		name = canon
	}
	rel, err := s.vw.CreatePersonNote(name, s.aliasesForNew(canon, name), "")
	if err != nil {
		return err
	}
	return s.ix.ReindexPaths([]string{rel})
}

// Dismiss removes a name for good; MarkOrg files it as a firm (remembered
// separately to seed firm pages later).
func (s *Service) Dismiss(key string) error        { return s.store.Dismiss(key) }
func (s *Service) MarkOrg(key string) error        { return s.store.MarkOrg(key) }
func (s *Service) BulkDismiss(keys []string) error { return s.store.DismissAll(keys) }

// capitalizedWords counts whitespace-separated tokens that start uppercase — a
// deterministic "looks like a proper name" signal.
func capitalizedWords(display string) int {
	n := 0
	for _, w := range strings.Fields(display) {
		r := []rune(w)
		if len(r) > 0 && r[0] >= 'A' && r[0] <= 'Z' {
			n++
		}
	}
	return n
}

// ---- create / bind (§5) ----

// Search surfaces existing entities a typed name might bind to, before creating.
func (s *Service) Search(query string) ([]Ref, error) {
	refs, err := s.ix.Search(query)
	if err != nil {
		return nil, err
	}
	out := make([]Ref, 0, len(refs))
	for _, r := range refs {
		out = append(out, Ref{Key: r.Key, Display: r.Display, HasNote: r.HasNote, IsPerson: r.IsPerson, RefCount: r.RefCount})
	}
	return out, nil
}

// Bind records that variant is another spelling of canonical (§5): it stores the
// binding and, when the canonical has a note, adds the variant as an alias:
// (an explicit user write). It never rewrites old notes.
func (s *Service) Bind(variant, canonical, variantDisplay string) error {
	if err := s.store.Bind(variant, canonical); err != nil {
		return err
	}
	if e, ok := s.ix.Entity(strings.ToLower(canonical)); ok && e.NotePath != "" {
		disp := strings.TrimSpace(variantDisplay)
		if disp == "" {
			disp = variant
		}
		if err := s.vw.AddFrontmatterValue(e.NotePath, "alias", disp); err != nil {
			return err
		}
		_ = s.ix.ReindexPaths([]string{e.NotePath})
	}
	return nil
}

// SaveNote is the note-pane save (§3.5): for a contact with no note it CREATES
// <Display>.md with categories: [people] (+alias if the display differs) and the
// typed body; for an existing note it replaces the body, preserving frontmatter.
// Returns the (possibly new) canonical key so the caller can re-render.
func (s *Service) SaveNote(rawKey, display, body string) (string, error) {
	canon := s.canonical(rawKey)
	e, _ := s.ix.Entity(canon)
	if e.NotePath != "" {
		if err := s.vw.ReplaceBody(e.NotePath, body); err != nil {
			return "", err
		}
		_ = s.ix.ReindexPaths([]string{e.NotePath})
		return canon, nil
	}
	name := strings.TrimSpace(display)
	if name == "" {
		name = e.Display
	}
	if name == "" {
		name = canon
	}
	rel, err := s.vw.CreatePersonNote(name, s.aliasesForNew(canon, name), body)
	if err != nil {
		return "", err
	}
	_ = s.store.Confirm(canon) // now a real contact
	_ = s.ix.ReindexPaths([]string{rel})
	return strings.ToLower(strings.TrimSuffix(filepath.Base(rel), ".md")), nil
}

// ConfirmEmail records an email on a contact's note (§6), creating the note if
// none exists. After this, calendar matching for the contact is exact by email.
func (s *Service) ConfirmEmail(rawKey, display, email string) error {
	canon := s.canonical(rawKey)
	e, _ := s.ix.Entity(canon)
	notePath := e.NotePath
	if notePath == "" {
		name := strings.TrimSpace(display)
		if name == "" {
			name = e.Display
		}
		if name == "" {
			name = canon
		}
		rel, err := s.vw.CreatePersonNote(name, s.aliasesForNew(canon, name), "")
		if err != nil {
			return err
		}
		_ = s.store.Confirm(canon)
		_ = s.ix.ReindexPaths([]string{rel})
		notePath = rel
	}
	if err := s.vw.AddFrontmatterValue(notePath, "email", email); err != nil {
		return err
	}
	if err := s.ix.ReindexPaths([]string{notePath}); err != nil {
		return err
	}
	s.invalidateMeetings() // last-met must reflect the new email immediately
	return nil
}

// ---- email-linking review queue (§4) ----

// EmailCandidate is one proposed attendee→contact email link in the review queue.
type EmailCandidate struct {
	Email          string `json:"email"`        // as the calendar has it
	AttendeeName   string `json:"attendeeName"` // display name on the invite, if any
	ContactKey     string `json:"contactKey"`   // the existing contact to link it to
	ContactDisplay string `json:"contactDisplay"`
	MetOn          string `json:"metOn"` // most recent meeting date with this attendee
	Via            string `json:"via"`   // "name" | "email" (how it matched)
}

// EmailReview proposes attendee→contact email links: every distinct calendar
// attendee (past + upcoming) whose email is NOT already linked and that matches
// an EXISTING contact by name or email local-part. Suggestions only — each is
// confirmed or dismissed by the user, never auto-written. This is how the contact
// DB of linked emails gets built up, one confirm at a time.
func (s *Service) EmailReview(now time.Time) ([]EmailCandidate, error) {
	if s.cal == nil {
		return nil, nil
	}
	type att struct{ name, email, date string }
	latest := map[string]att{} // email-lower → most-recent occurrence
	consider := func(evs []Event) {
		for _, ev := range evs {
			date := ev.Start.Format("2006-01-02")
			for _, a := range ev.Attendees {
				em := strings.ToLower(strings.TrimSpace(a.Email))
				if em == "" {
					continue
				}
				if cur, ok := latest[em]; !ok || date > cur.date {
					latest[em] = att{name: a.Name, email: a.Email, date: date}
				}
			}
		}
	}
	consider(s.cal.PastMeetings(now, pastMeetingWindowDays))
	consider(s.cal.Upcoming(now, 30))

	list, err := s.List(now)
	if err != nil {
		return nil, err
	}
	var out []EmailCandidate
	for em, a := range latest {
		if s.ix.ResolveEmail(em) != "" {
			continue // already linked to some contact
		}
		ck, cd, via := s.matchContact(a.name, em, list)
		if ck == "" || s.store.IsEmailDismissed(a.email, ck) {
			continue
		}
		out = append(out, EmailCandidate{
			Email: a.email, AttendeeName: a.name,
			ContactKey: ck, ContactDisplay: cd, MetOn: a.date, Via: via,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MetOn != out[j].MetOn {
			return out[i].MetOn > out[j].MetOn // most-recent attendee first
		}
		return out[i].Email < out[j].Email
	})
	return out, nil
}

// DismissEmailCandidate remembers that an email should not link to a contact; the
// pair never re-appears in the review queue.
func (s *Service) DismissEmailCandidate(email, contactKey string) error {
	return s.store.DismissEmail(email, contactKey)
}

// matchContact finds an existing contact for an unlinked attendee, deterministically:
// (a) the attendee's display name contains a contact's full name/alias, or
// (b) the email local-part (dots/digits/+tag stripped) equals a contact's
// normalized name/alias (so michaeltrinh19 → "michael trinh"). Suggestion only.
func (s *Service) matchContact(attendeeName, emailLower string, list []Contact) (key, display, via string) {
	if strings.TrimSpace(attendeeName) != "" {
		for _, c := range list {
			for _, n := range s.namesOf(c) {
				if nameTokenMatch(n, attendeeName) {
					return c.Key, c.Display, "name"
				}
			}
		}
	}
	if local := normalizeLocal(emailLower); local != "" {
		for _, c := range list {
			for _, n := range s.namesOf(c) {
				// only match a FULL name encoded in the local-part (michaeltrinh19 →
				// "michael trinh"); a lone first name ("rob", "sean") is too ambiguous.
				if len(significantTokens(n)) >= 2 && normalizeLocal(n) == local {
					return c.Key, c.Display, "email"
				}
			}
		}
	}
	return "", "", ""
}

// namesOf is a contact's display plus its aliases (for matching).
func (s *Service) namesOf(c Contact) []string {
	names := []string{c.Display}
	if al, err := s.ix.EntityAliases(c.Key); err == nil {
		names = append(names, al...)
	}
	return names
}

// nameTokenMatch reports whether every significant token of a contact's name
// appears as a WHOLE token in the attendee name (order-independent). Token-based,
// not substring, so "eli" no longer matches "corn·eli·us" and "rico meinl" no
// longer matches "Federico Villani" — a precise, low-false-positive suggestion.
func nameTokenMatch(contactName, attendeeName string) bool {
	req := significantTokens(contactName)
	if len(req) < 2 {
		return false // a lone first name ("Sean", "Jim") is too ambiguous to suggest
	}
	have := map[string]bool{}
	for _, t := range significantTokens(attendeeName) {
		have[t] = true
	}
	for _, t := range req {
		if !have[t] {
			return false
		}
	}
	return true
}

// significantTokens lowercases and splits on any non-alphanumeric run, keeping
// tokens of length ≥2 (drops initials/apostrophe fragments like the "o" in o'neill).
func significantTokens(s string) []string {
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	out := parts[:0]
	for _, p := range parts {
		if len(p) >= 2 {
			out = append(out, p)
		}
	}
	return out
}

// normalizeLocal reduces an email local-part (or a name) to comparable letters:
// lowercased, with the domain, a +tag, and any non-letters removed — so
// "michaeltrinh19@gmail.com" and "Michael Trinh" both collapse to "michaeltrinh".
func normalizeLocal(s string) string {
	if at := strings.IndexByte(s, '@'); at >= 0 {
		s = s[:at]
	}
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' {
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}

// ---- open loops (§2) ----

// OpenLoopGroup is the unchecked loops from one source note.
type OpenLoopGroup struct {
	Path  string         `json:"path"`
	Name  string         `json:"name"`
	Date  string         `json:"date"`
	Loops []OpenLoopItem `json:"loops"`
}

// OpenLoopItem is one unchecked task (toggleable) or next-step line.
type OpenLoopItem struct {
	Line int    `json:"line"`
	Text string `json:"text"`
	Kind string `json:"kind"` // "checkbox" (toggleable) | "nextstep"
}

// OpenLoops returns the contact's unchecked meeting-context loops, grouped by
// source note (newest note first).
func (s *Service) OpenLoops(rawKey string) []OpenLoopGroup {
	canon := s.canonical(rawKey)
	e, _ := s.ix.Entity(canon)
	loops, err := s.ix.OpenLoops(s.keysFor(canon, e.NotePath))
	if err != nil {
		return nil
	}
	var groups []OpenLoopGroup
	idx := map[string]int{}
	for _, l := range loops {
		i, ok := idx[l.Path]
		if !ok {
			i = len(groups)
			idx[l.Path] = i
			groups = append(groups, OpenLoopGroup{Path: l.Path, Name: l.Name, Date: l.Date})
		}
		groups[i].Loops = append(groups[i].Loops, OpenLoopItem{Line: l.Line, Text: l.Text, Kind: l.Kind})
	}
	return groups
}

// ---- neglect lens (§3) ----

// neglect computes the neglect signals from a contact's dated-interaction dates:
// the count, the median gap between them, days since the last, and whether they
// are "going cold" (3+ interactions AND days-since > max(30, 2×median gap)).
// Pure computation — no writes, no AI. (Seam: a future funder lens can weight
// the threshold by pipeline stage:: here.)
func neglect(dates []string, now time.Time) (interactions, medianGap, daysSince int, cold bool) {
	seen := map[string]bool{}
	var days []int
	for _, d := range dates {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		if t, err := time.Parse("2006-01-02", d); err == nil {
			days = append(days, int(t.Unix()/86400))
		}
	}
	sort.Ints(days)
	interactions = len(days)
	daysSince = -1
	if interactions == 0 {
		return
	}
	daysSince = int(now.UTC().Unix()/86400) - days[len(days)-1]
	if daysSince < 0 {
		daysSince = 0
	}
	if interactions >= 2 {
		gaps := make([]int, 0, interactions-1)
		for i := 1; i < len(days); i++ {
			gaps = append(gaps, days[i]-days[i-1])
		}
		sort.Ints(gaps)
		medianGap = medianInt(gaps)
	}
	threshold := 30
	if 2*medianGap > threshold {
		threshold = 2 * medianGap
	}
	cold = interactions >= 3 && daysSince > threshold
	return
}

func medianInt(xs []int) int {
	n := len(xs)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}

// ---- internals ----

// canonical resolves a key through any binding to its canonical entity key.
func (s *Service) canonical(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	if c := s.store.CanonicalOf(k); c != "" {
		return c
	}
	return k
}

// keysFor is the set of link-target keys that mean this same person: the
// canonical key, its bound variants, and any alias that is also a link target.
func (s *Service) keysFor(canonKey, notePath string) []string {
	set := map[string]bool{canonKey: true}
	for _, v := range s.store.VariantsOf(canonKey) {
		set[v] = true
	}
	if al, err := s.ix.EntityAliases(canonKey); err == nil {
		for _, a := range al {
			set[strings.ToLower(a)] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func (s *Service) mergedTimeline(keys []string) []vaultindex.TimelineEntry {
	seen := map[string]bool{}
	var all []vaultindex.TimelineEntry
	for _, k := range keys {
		tl, err := s.ix.Timeline(k)
		if err != nil {
			continue
		}
		for _, e := range tl {
			if seen[e.Path] {
				continue
			}
			seen[e.Path] = true
			all = append(all, e)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if (all[i].Date == "") != (all[j].Date == "") {
			return all[i].Date != ""
		}
		if all[i].Date != all[j].Date {
			return all[i].Date > all[j].Date
		}
		return all[i].Name < all[j].Name
	})
	return all
}

func (s *Service) mergedLastMet(keys []string) string {
	best := ""
	for _, k := range keys {
		if d, _, ok := s.ix.LastMet(k); ok && d > best {
			best = d
		}
	}
	return best
}

// ---- calendar-verified "last met" (email-matched) ----

// meetingsByEmail returns the cached email→past-meetings index, rebuilding it
// when empty or older than the TTL. `now` drives both the calendar window and
// the TTL check (tests pass a fixed now; a future now forces a rebuild).
func (s *Service) meetingsByEmail(now time.Time) map[string][]meetingRef {
	if s.cal == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.meetings; m != nil && !now.Before(m.builtAt) && now.Sub(m.builtAt) < meetingCacheTTL {
		return m.byEmail
	}
	byEmail := map[string][]meetingRef{}
	seen := map[string]map[string]bool{} // email → date → already recorded
	for _, ev := range s.cal.PastMeetings(now, pastMeetingWindowDays) {
		date := ev.Start.Format("2006-01-02")
		for _, a := range ev.Attendees {
			em := strings.ToLower(strings.TrimSpace(a.Email))
			if em == "" {
				continue
			}
			if seen[em] == nil {
				seen[em] = map[string]bool{}
			}
			if seen[em][date] {
				continue // one meeting per email per day
			}
			seen[em][date] = true
			byEmail[em] = append(byEmail[em], meetingRef{Date: date, Title: ev.Title})
		}
	}
	s.meetings = &meetingIndex{byEmail: byEmail, builtAt: now}
	return byEmail
}

// invalidateMeetings forces the next meeting lookup to rebuild (after an email
// is linked, so calendar last-met updates instantly).
func (s *Service) invalidateMeetings() {
	s.mu.Lock()
	s.meetings = nil
	s.mu.Unlock()
}

// meetingsFor returns the union of past meetings across a contact's emails,
// deduped by date, newest-first.
func (s *Service) meetingsFor(emails []string, now time.Time) []meetingRef {
	if len(emails) == 0 {
		return nil
	}
	byEmail := s.meetingsByEmail(now)
	if len(byEmail) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []meetingRef
	for _, e := range emails {
		for _, m := range byEmail[strings.ToLower(strings.TrimSpace(e))] {
			if seen[m.Date] {
				continue // dedup across a person's several emails, by date
			}
			seen[m.Date] = true
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date }) // newest first
	return out
}

// meetingDatesFor is meetingsFor reduced to its dates (newest-first).
func (s *Service) meetingDatesFor(emails []string, now time.Time) []string {
	ms := s.meetingsFor(emails, now)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Date)
	}
	return out
}

// emailsFor returns a note-backed contact's linked emails (none for note-less).
func (s *Service) emailsFor(hasNote bool, key string) []string {
	if !hasNote {
		return nil
	}
	es, _ := s.ix.Emails(key)
	return es
}

// firstDate returns the first element of a newest-first date slice, or "".
func firstDate(dates []string) string {
	if len(dates) == 0 {
		return ""
	}
	return dates[0]
}

// transcripts merges vault transcript notes (from the timeline) with any Granola
// source, deduped by date (a meeting you exported counts once — funder §4).
func (s *Service) transcripts(keys []string, tl []vaultindex.TimelineEntry) []Transcript {
	var out []Transcript
	byDate := map[string]bool{}
	for _, e := range tl {
		if e.IsTranscript && e.Date != "" {
			out = append(out, Transcript{Date: e.Date, Title: e.Name, Path: e.Path, Source: "vault"})
			byDate[e.Date] = true
		}
	}
	if s.granola != nil {
		for _, g := range s.granola.Transcripts(keys) {
			if !byDate[g.Date] { // dedupe against exported vault notes
				out = append(out, g)
			}
		}
	}
	return out
}

// upcomingFor returns confirmed (exact-email) upcoming events plus, when the
// contact has no confirmed email, name/alias candidates to confirm. "Suggest,
// never assume": candidates carry the attendee email for one-click confirm.
func (s *Service) upcomingFor(p Page, now time.Time) []UpcomingItem {
	if s.cal == nil {
		return nil
	}
	emails := map[string]bool{}
	if p.HasNote {
		if es, err := s.ix.Emails(p.Key); err == nil {
			for _, e := range es {
				emails[strings.ToLower(e)] = true
			}
		}
	}
	names := append([]string{p.Display}, p.Aliases...)
	var out []UpcomingItem
	for _, ev := range s.cal.Upcoming(now, 30) {
		date := ev.Start.Format("2006-01-02")
		matchedConfirmed := false
		var candidateEmail string
		for _, a := range ev.Attendees {
			if len(emails) > 0 && emails[strings.ToLower(a.Email)] {
				matchedConfirmed = true
				break
			}
		}
		if !matchedConfirmed && len(emails) == 0 {
			if nameMatches(names, ev.Title) || attendeeNameMatches(names, ev.Attendees) {
				candidateEmail = firstAttendeeEmail(ev.Attendees)
			}
		}
		if matchedConfirmed {
			out = append(out, UpcomingItem{Date: date, Title: ev.Title, Confirmed: true})
		} else if candidateEmail != "" || (len(emails) == 0 && nameMatches(names, ev.Title)) {
			out = append(out, UpcomingItem{Date: date, Title: ev.Title, Confirmed: false, Email: candidateEmail})
		}
	}
	return out
}

// confirmedUpcoming maps each contact key to its next CONFIRMED upcoming date
// (for the list view — unconfirmed shows nothing there).
func (s *Service) confirmedUpcoming(now time.Time) map[string]string {
	out := map[string]string{}
	if s.cal == nil {
		return out
	}
	for _, ev := range s.cal.Upcoming(now, 30) {
		date := ev.Start.Format("2006-01-02")
		for _, a := range ev.Attendees {
			if k := s.ix.ResolveEmail(a.Email); k != "" {
				if cur, ok := out[k]; !ok || date < cur {
					out[k] = date
				}
			}
		}
	}
	return out
}

func (s *Service) noteBody(rel string) string {
	b, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	_, body := splitFront(string(b))
	return body
}

func (s *Service) displayAndRefs(key string) (string, int) {
	refs, _ := s.ix.Search(key)
	for _, r := range refs {
		if r.Key == key {
			return r.Display, r.RefCount
		}
	}
	return key, 0
}

// aliasesForNew returns the alias list for a newly created note: display variants
// that differ from the filename (§3.5 "+alias if variant differs").
func (s *Service) aliasesForNew(key, name string) []string {
	var out []string
	if al, err := s.ix.EntityAliases(key); err == nil {
		for _, a := range al {
			if !strings.EqualFold(a, name) {
				out = append(out, a)
			}
		}
	}
	return out
}

func splitFront(raw string) (string, string) {
	if !strings.HasPrefix(raw, "---\n") {
		return "", strings.TrimSpace(raw)
	}
	i := strings.Index(raw, "\n---")
	if i < 0 {
		return "", strings.TrimSpace(raw)
	}
	rest := raw[i+4:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	return raw[4:i], strings.TrimSpace(rest)
}

func nameMatches(names []string, text string) bool {
	lt := strings.ToLower(text)
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && strings.Contains(lt, n) {
			return true
		}
	}
	return false
}

func attendeeNameMatches(names []string, as []Attendee) bool {
	for _, a := range as {
		if nameMatches(names, a.Name) {
			return true
		}
	}
	return false
}

func firstAttendeeEmail(as []Attendee) string {
	for _, a := range as {
		if a.Email != "" {
			return a.Email
		}
	}
	return ""
}
