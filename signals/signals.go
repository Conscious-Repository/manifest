// Package signals computes the app-derived cards for the FEED — conditions the
// dashboard already knows (a going-cold contact, a stalled Rock), rendered as
// feed signals (plans/feed-central.md §2). They are conditions, NOT items: never
// markdown files, never in the engine's feed dir, never kept/discarded — a nudge
// must not pollute the quality signal the tune ritual reads. Computed fresh at
// read time; the only persisted state is the user's dismissals + snoozes.
package signals

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
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
}

func New(store *Store, emitters ...Emitter) *Service {
	return &Service{store: store, emitters: emitters}
}

// Active returns the signals to render: every emitter's output minus the ones
// the user dismissed (while the hash still matches) or snoozed (until lapsed),
// most-overdue first. An emitter error drops only that emitter's signals.
func (s *Service) Active(now time.Time) []Signal {
	var all []Signal
	for _, e := range s.emitters {
		sigs, err := e.Emit(now)
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
func (s *Service) Dismiss(id, hash string) error { return s.store.Dismiss(id, hash) }

// Snooze suppresses a signal until the given time.
func (s *Service) Snooze(id string, until time.Time) error { return s.store.Snooze(id, until) }

// ---- emitters ----

// ContactLister is the contacts surface the cold emitter needs (contacts.Service).
type ContactLister interface {
	List(now time.Time) ([]contacts.Contact, error)
}

// ColdContacts emits one card per going-cold contact (the existing neglect lens).
func ColdContacts(l ContactLister) Emitter { return coldEmitter{l} }

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
