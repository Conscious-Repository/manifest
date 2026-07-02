// Package calendar reads Google Calendar (read-only) and maps timed events onto
// the daily half-hour schedule. The filter/map core (EventsToSlots) is pure and
// dependency-free so it can be unit-tested without Google or a network.
package calendar

import (
	"fmt"
	"sort"
	"time"
)

// Event is a normalized calendar event (Google types are converted to this).
type Event struct {
	ID        string
	Title     string
	Start     time.Time
	End       time.Time
	AllDay    bool
	Declined  bool       // self-attendee responseStatus == "declined"
	Attendees []Attendee // non-self attendees (for contact matching)
}

// Attendee is one non-self participant on an event (name + email as Google has them).
type Attendee struct {
	Name  string
	Email string
}

// Slot is a half-hour schedule slot derived from a timed event.
type Slot struct {
	Token   string // canonical half-hour token, e.g. "9:30A"
	Title   string
	EventID string
}

// EventsToSlots filters all-day and multi-day events, then maps each remaining
// timed event onto the half-hour slots it covers within `day` (interpreted at
// local midnight in loc). It is pure: no I/O, no Google types.
//
// Rules (matching the spec):
//   - skip all-day events (birthdays, reminders),
//   - skip multi-day events (>= 24h, or spanning a midnight boundary as a block),
//   - floor each start down to the 30-minute grid,
//   - emit one slot per covered half-hour; first event by start time wins a slot.
func EventsToSlots(events []Event, day time.Time, loc *time.Location) []Slot {
	if loc == nil {
		loc = time.Local
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)

	sorted := append([]Event(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })

	var slots []Slot
	seen := map[string]bool{}
	for _, e := range sorted {
		if e.AllDay {
			continue
		}
		if e.Declined {
			continue // events I declined don't belong on my schedule
		}
		if e.End.Sub(e.Start) >= 24*time.Hour {
			continue
		}
		// A block that crosses local midnight (e.g. 10pm–2am) is multi-day too.
		// End-1s keeps an event ending exactly at midnight on its own day.
		if !sameLocalDate(e.Start.In(loc), e.End.Add(-time.Second).In(loc)) {
			continue
		}

		startMin := int(e.Start.Sub(dayStart).Minutes())
		endMin := int(e.End.Sub(dayStart).Minutes())
		if startMin < 0 {
			startMin = 0
		}
		if endMin > 1440 {
			endMin = 1440
		}
		if endMin <= 0 || startMin >= 1440 || endMin <= startMin {
			continue // no overlap with this day
		}
		startMin -= startMin % 30 // floor to the 30-minute grid
		// One event is one item: the title goes on its first (lead) slot only;
		// the slots it spans get an empty title so the UI shows it once, start to
		// finish (the calendar left-bar still marks the whole span).
		lead := true
		for m := startMin; m < endMin; m += 30 {
			tok := slotToken(m)
			if seen[tok] {
				continue
			}
			seen[tok] = true
			title := ""
			if lead {
				title = e.Title
				lead = false
			}
			slots = append(slots, Slot{Token: tok, Title: title, EventID: e.ID})
		}
	}
	return slots
}

func sameLocalDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// slotToken renders minutes-from-midnight as a half-hour label. It mirrors
// daily.slotToken so the Go schedule and JS UI agree on tokens.
func slotToken(min int) string {
	min = ((min % 1440) + 1440) % 1440
	h24, m := min/60, min%60
	suffix := "A"
	if h24 >= 12 {
		suffix = "P"
	}
	h12 := h24 % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d%s", h12, m, suffix)
}
