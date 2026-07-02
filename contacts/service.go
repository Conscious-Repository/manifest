package contacts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/vaultindex"
	"manifest/vaultwriter"
)

// CalendarReader is the minimal calendar surface the contacts layer needs for
// upcoming-meeting matching (§6). Decoupled from the calendar package so the
// service is testable without Google; main.go adapts calendar.Client to this.
type CalendarReader interface {
	Upcoming(now time.Time, days int) []Event
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
	cal     CalendarReader   // nil → no upcoming
	granola TranscriptSource // nil → vault-only transcripts
}

// New builds the contacts service. cal and granola may be nil.
func New(ix *vaultindex.Index, store *Store, vw *vaultwriter.Writer, cal CalendarReader, granola TranscriptSource) *Service {
	return &Service{ix: ix, store: store, vw: vw, cal: cal, granola: granola}
}

// ---- list ----

// Contact is one row in the contacts list.
type Contact struct {
	Key      string `json:"key"`
	Display  string `json:"display"`
	NotePath string `json:"notePath"`
	HasNote  bool   `json:"hasNote"`
	LastMet  string `json:"lastMet"` // "" when there is no dated evidence — never a guess
	RefCount int    `json:"refCount"`
	Upcoming string `json:"upcoming"` // ISO date of the next CONFIRMED-email match, else ""
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

	// confirmed-email upcoming matches, resolved once
	upcoming := s.confirmedUpcoming(now)

	out := make([]Contact, 0, len(byKey))
	for _, c := range byKey {
		keys := s.keysFor(c.Key, c.NotePath)
		c.LastMet = s.mergedLastMet(keys)
		if u, ok := upcoming[c.Key]; ok {
			c.Upcoming = u
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].LastMet == "") != (out[j].LastMet == "") {
			return out[i].LastMet != "" // dated contacts first
		}
		if out[i].LastMet != out[j].LastMet {
			return out[i].LastMet > out[j].LastMet // newest first
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

// UpcomingItem is a matched or candidate future event (§6).
type UpcomingItem struct {
	Date      string `json:"date"`
	Title     string `json:"title"`
	Confirmed bool   `json:"confirmed"`       // exact email match
	Email     string `json:"email,omitempty"` // attendee email to confirm (candidates)
}

// Page is the full contact page (spec §3).
type Page struct {
	Key         string         `json:"key"`
	Display     string         `json:"display"`
	NotePath    string         `json:"notePath"`
	HasNote     bool           `json:"hasNote"`
	Aliases     []string       `json:"aliases"`
	Firms       []Ref          `json:"firms"`
	LastMet     string         `json:"lastMet"`
	Timeline    []TimelineItem `json:"timeline"`
	Mentions    []Mention      `json:"mentions"`
	Transcripts []Transcript   `json:"transcripts"`
	Upcoming    []UpcomingItem `json:"upcoming"`
	NoteBody    string         `json:"noteBody"`
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
	for _, e := range tl {
		if e.Date != "" {
			if p.LastMet == "" {
				p.LastMet = e.Date // list is newest-first, so the first dated entry is last-met
			}
			p.Timeline = append(p.Timeline, TimelineItem{
				Date: e.Date, Name: e.Name, Path: e.Path, SourceType: e.SourceType, IsTranscript: e.IsTranscript,
			})
		} else {
			p.Mentions = append(p.Mentions, Mention{Name: e.Name, Path: e.Path})
		}
	}

	p.Transcripts = s.transcripts(keys, tl)
	p.Upcoming = s.upcomingFor(p, now)
	if p.HasNote {
		p.NoteBody = s.noteBody(p.NotePath)
	}
	return p, true
}

// ---- triage ----

// TriageItem is a note-less name awaiting an "is this a person?" decision.
type TriageItem struct {
	Key      string `json:"key"`
	Display  string `json:"display"`
	RefCount int    `json:"refCount"`
}

// Triage returns note-less targets seen ONLY outside meeting context, not yet
// confirmed or dismissed — the quiet "is this a person?" queue (§4).
func (s *Service) Triage() ([]TriageItem, error) {
	targets, err := s.ix.NoteLessTargets()
	if err != nil {
		return nil, err
	}
	var out []TriageItem
	for _, t := range targets {
		if t.InMeetingContext || s.store.IsConfirmed(t.Key) || s.store.IsDismissed(t.Key) {
			continue
		}
		if s.canonical(t.Key) != t.Key {
			continue // bound to another contact already
		}
		out = append(out, TriageItem{Key: t.Key, Display: t.Display, RefCount: t.RefCount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RefCount > out[j].RefCount })
	return out, nil
}

// Confirm promotes a note-less target to a contact; Dismiss removes it for good.
func (s *Service) Confirm(key string) error { return s.store.Confirm(key) }
func (s *Service) Dismiss(key string) error { return s.store.Dismiss(key) }

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
	return s.ix.ReindexPaths([]string{notePath})
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
