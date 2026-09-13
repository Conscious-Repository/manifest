// Package signals computes the app-derived cards for the FEED — conditions the
// dashboard already knows (a going-cold contact, a stalled Rock), rendered as
// feed signals (plans/feed-central.md §2). They are conditions, NOT items: never
// markdown files, never in the engine's feed dir, never kept/discarded — a nudge
// must not pollute the quality signal the tune ritual reads. Computed fresh at
// read time; the only persisted state is the user's dismissals + snoozes.
package signals

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/contacts"
	"manifest/goals"
)

// Signal is one virtual feed card. A dismissal re-arms only when Hash changes
// (the underlying state actually moved), so it is flap- and failure-proof.
type Signal struct {
	ID      string `json:"id"`      // "contact-cold:<key>" | "rock-stalled:<goalID>"
	Kind    string `json:"kind"`    // "contact-cold" | "rock-stalled"
	Entity  string `json:"entity"`  // display name
	Label   string `json:"label"`   // "cold · fred lee · 31d"
	Age     int    `json:"age"`     // days, for sorting (most overdue first)
	ActHref string `json:"actHref"` // deep link, ready to assign to location.hash
	Hash    string `json:"hash"`    // dismissal re-arm key (client echoes it back)
	GoalID  string `json:"goalId,omitempty"`
	RunID   string `json:"runId,omitempty"` // delegation-done: the report to open in place
	// delegation-done also carries the RESULT, so its feed card can show the
	// deliverable rather than the narration (exactly one is ever set).
	ArtifactRef  string `json:"artifactRef,omitempty"`  // harness-relative → /api/spirits/file
	ArtifactPath string `json:"artifactPath,omitempty"` // vault-relative → the note view
	Harness      string `json:"harness,omitempty"`      // federation source tag
	// Agent reply signals carry the thread comment itself. The comment remains
	// the source of truth; this is only the computed FEED projection that lets
	// the owner see and open an unanswered reply without hunting for its task.
	Reply        string `json:"reply,omitempty"`
	ReplyAuthor  string `json:"replyAuthor,omitempty"`
	ReplyPersona string `json:"replyPersona,omitempty"`
	ReplyAt      string `json:"replyAt,omitempty"`
}

// Emitter computes the currently-active conditions of one kind. An emitter that
// cannot read its source returns (nil, err) — NEVER an empty slice as if all
// conditions were absent, which would wipe suppression state.
type Emitter interface {
	Emit(now time.Time) ([]Signal, error)
}

// Service composes the emitters with the dismissal/snooze store.
type Service struct {
	store    *Store
	emitters []Emitter
	// WithCache: one computed pass served for ttl (zero = compute every call)
	ttl      time.Duration
	cacheMu  sync.Mutex
	cachedAt time.Time
	cached   []Signal
	cachedOK bool
}

// WithCache makes Active serve one computed pass for ttl before recomputing.
// The FEED list, its badge and the AGENTS status all ask within a second of
// each other and the phone polls every 3 s, while a pass costs ~0.7 s (the
// delegation index, the contacts list). Dismiss and Snooze drop the cached
// pass so a verdict shows on the very next read; anything else moves within
// ttl. Tests construct without it and see every emitter call.
func (s *Service) WithCache(ttl time.Duration) *Service { s.ttl = ttl; return s }

// Invalidate drops the cached pass (a verdict, or a caller that just changed
// what an emitter reads).
func (s *Service) Invalidate() {
	s.cacheMu.Lock()
	s.cached, s.cachedOK, s.cachedAt = nil, false, time.Time{}
	s.cacheMu.Unlock()
}

func New(store *Store, emitters ...Emitter) *Service {
	return &Service{store: store, emitters: emitters}
}

// Active returns the signals to render: every emitter's output minus the ones
// the user dismissed (while the hash still matches) or snoozed (until lapsed),
// most-overdue first. An emitter error drops only that emitter's signals.
func (s *Service) Active(now time.Time) []Signal { return s.ActiveTraced(now, nil) }

// Timing is one emitter's cost in a traced pass (FEED ?trace=1 diagnostics).
type Timing struct {
	Emitter string        `json:"emitter"`
	Took    time.Duration `json:"took"`
	Count   int           `json:"count"`
}

// ActiveTraced is Active with a per-emitter cost record appended to *trace
// when it is non-nil — the diagnostic behind the FEED's ?trace=1.
func (s *Service) ActiveTraced(now time.Time, trace *[]Timing) []Signal {
	if s.ttl <= 0 || trace != nil {
		return s.compute(now, trace)
	}
	// one pass at a time: a second reader arriving mid-compute waits for this
	// result instead of starting its own
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cachedOK && time.Since(s.cachedAt) < s.ttl {
		return append([]Signal(nil), s.cached...)
	}
	all := s.compute(now, nil)
	s.cached, s.cachedOK, s.cachedAt = all, true, time.Now()
	return append([]Signal(nil), all...)
}

func (s *Service) compute(now time.Time, trace *[]Timing) []Signal {
	var all []Signal
	for _, e := range s.emitters {
		start := time.Now()
		sigs, err := e.Emit(now)
		if trace != nil {
			*trace = append(*trace, Timing{Emitter: fmt.Sprintf("%T", e), Took: time.Since(start), Count: len(sigs)})
		}
		if err != nil {
			continue // no data ≠ all-clear; just contribute nothing this pass
		}
		for _, sig := range sigs {
			if s.store.Suppressed(sig.ID, sig.Hash, now) {
				continue
			}
			all = append(all, sig)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Age != all[j].Age {
			return all[i].Age > all[j].Age // most overdue first
		}
		return all[i].Label < all[j].Label
	})
	return all
}

// Count is the badge contribution (active, unsuppressed).
func (s *Service) Count(now time.Time) int { return len(s.Active(now)) }

// Dismiss suppresses a signal while its condition hash is unchanged.
func (s *Service) Dismiss(id, hash string) error {
	defer s.Invalidate()
	return s.store.Dismiss(id, hash)
}

// Snooze suppresses a signal until the given time.
func (s *Service) Snooze(id string, until time.Time) error {
	defer s.Invalidate()
	return s.store.Snooze(id, until)
}

// ---- emitters ----

// ContactLister is the contacts surface the cold emitter needs (contacts.Service).
type ContactLister interface {
	List(now time.Time) ([]contacts.Contact, error)
}

// ColdContacts emits one card per going-cold contact (the existing neglect lens).
func ColdContacts(l ContactLister) Emitter { return coldEmitter{l} }

// ColdContactsCached is ColdContacts over a list held for ttl. The contacts
// list is six index queries plus calendar joins (~0.3 s) and only moves when
// notes or meetings do, so a minute-old read serves the cold signal as well
// as a fresh one; the Contacts tab keeps its own live read.
func ColdContactsCached(l ContactLister, ttl time.Duration) Emitter {
	return coldEmitter{&cachedLister{l: l, ttl: ttl}}
}

type cachedLister struct {
	l    ContactLister
	ttl  time.Duration
	mu   sync.Mutex
	at   time.Time
	rows []contacts.Contact
}

func (c *cachedLister) List(now time.Time) ([]contacts.Contact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rows != nil && time.Since(c.at) < c.ttl {
		return c.rows, nil
	}
	rows, err := c.l.List(now)
	if err != nil {
		return nil, err // no data ≠ all-clear, and never cached
	}
	c.rows, c.at = rows, time.Now()
	return rows, nil
}

type coldEmitter struct{ l ContactLister }

func (e coldEmitter) Emit(now time.Time) ([]Signal, error) {
	list, err := e.l.List(now)
	if err != nil {
		return nil, err
	}
	var out []Signal
	for _, c := range list {
		if !c.Cold {
			continue
		}
		// the interaction date that drives Cold — meetings basis uses the
		// calendar date, mentions basis the note date. NOT DaysSince (daily drift).
		last := c.LastMentioned
		if c.NeglectBasis == "meetings" {
			last = c.LastMet
		}
		out = append(out, Signal{
			ID:      "contact-cold:" + c.Key,
			Kind:    "contact-cold",
			Entity:  c.Display,
			Label:   "cold · " + c.Display + " · " + strconv.Itoa(c.DaysSince) + "d",
			Age:     c.DaysSince,
			ActHref: "#/contacts/" + url.PathEscape(c.Key),
			Hash:    c.NeglectBasis + "|" + last,
		})
	}
	return out, nil
}

// GoalLoader is the goals surface the stalled emitter needs (goals.Store).
type GoalLoader interface{ Load() *goals.Doc }

// StalledRocks emits one card per Rock in the current quarter, past its midpoint,
// not yet complete, with no movement in 14 days.
func StalledRocks(l GoalLoader) Emitter { return rockEmitter{l} }

type rockEmitter struct{ l GoalLoader }

func (e rockEmitter) Emit(now time.Time) ([]Signal, error) {
	doc := e.l.Load()
	if doc == nil {
		return nil, nil
	}
	cq := goals.CurrentQuarter(now)
	if !pastMidpoint(cq, now) {
		return nil, nil // early in the quarter — nothing is "stalled" yet
	}
	var out []Signal
	for _, area := range doc.Areas {
		for _, rock := range area.Rocks {
			if rock.Quarter != cq {
				continue
			}
			checked, total := rockProgress(rock)
			if total > 0 && checked == total {
				continue // done
			}
			idle := daysSinceOrHuge(rock.Moved, now)
			if idle < 14 {
				continue // moved recently
			}
			out = append(out, Signal{
				ID:      "rock-stalled:" + rock.ID,
				Kind:    "rock-stalled",
				Entity:  rock.Text,
				Label:   "stalled · " + rock.Text + " · " + idleLabel(rock.Moved, idle),
				Age:     idle,
				ActHref: "#/goals/" + url.PathEscape(rock.ID),
				Hash:    rock.Moved + "|" + rock.Quarter,
				GoalID:  rock.ID,
			})
		}
	}
	return out, nil
}

// rockProgress counts checked vs total leaf tasks across a Rock's stages.
func rockProgress(rock *goals.Goal) (checked, total int) {
	for _, stage := range rock.Children {
		for _, task := range stage.Children {
			total++
			if task.Checked {
				checked++
			}
		}
	}
	return
}

// pastMidpoint reports whether now is past the midpoint of quarter slug q
// ("2026-Q3"). A malformed slug is treated as past (fail toward surfacing).
func pastMidpoint(q string, now time.Time) bool {
	y, m, ok := quarterStart(q)
	if !ok {
		return true
	}
	start := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 3, 0)
	mid := start.Add(end.Sub(start) / 2)
	return now.After(mid)
}

func quarterStart(q string) (year, month int, ok bool) {
	parts := strings.Split(q, "-Q")
	if len(parts) != 2 {
		return 0, 0, false
	}
	y, e1 := strconv.Atoi(parts[0])
	qn, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || qn < 1 || qn > 4 {
		return 0, 0, false
	}
	return y, (qn-1)*3 + 1, true
}

// daysSinceOrHuge is whole days since an ISO date, or a large number when the
// date is empty/unparseable (a never-moved Rock reads as very stale).
func daysSinceOrHuge(iso string, now time.Time) int {
	if iso == "" {
		return 9999
	}
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return 9999
	}
	d := int(now.Sub(t).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return d
}

func idleLabel(moved string, idle int) string {
	if moved == "" {
		return "no movement yet"
	}
	return "idle " + strconv.Itoa(idle) + "d"
}

// ---- gmail reconnect ----

// GmailAuthChecker is the auth surface the reconnect emitter reads
// (implemented by gmailauth.Client). NeedsReauth is true only when a token
// exists but its refresh token is dead — i.e. a genuine failure, not the
// never-connected setup state. `email` is best-effort for the label; `detail`
// is a short human reason. An error means "couldn't determine" — the emitter
// then contributes nothing rather than nagging on a transient blip.
type GmailAuthChecker interface {
	AuthState(now time.Time) (needsReauth bool, email, detail string, err error)
}

// GmailReauth raises ONE signal when the engine's Gmail sign-in has expired, so
// the waiting-on digest breaking becomes a visible nudge instead of silent
// failure. Acting on it deep-links to the Portals reconnect.
func GmailReauth(c GmailAuthChecker) Emitter { return gmailReauthEmitter{c} }

type gmailReauthEmitter struct{ c GmailAuthChecker }

func (e gmailReauthEmitter) Emit(now time.Time) ([]Signal, error) {
	needs, email, detail, err := e.c.AuthState(now)
	if err != nil {
		return nil, err // undetermined ≠ all-clear; suppression state preserved
	}
	if !needs {
		return nil, nil
	}
	who := email
	if who == "" {
		who = "Gmail"
	}
	if detail == "" {
		detail = "sign-in expired"
	}
	return []Signal{{
		ID:      "gmail-reauth",
		Kind:    "gmail-reauth",
		Entity:  who,
		Label:   "reconnect Gmail · " + who + " · " + detail,
		Age:     9000, // pins near the top — a broken integration is urgent
		ActHref: "#/settings/connections",
		Hash:    "needs-reauth", // stable while the condition holds; re-arms on fix
	}}, nil
}
