package realestate

import (
	"strings"
	"time"
)

// The schedule IS the rock list with [done-by::] dates (overhaul decision 3
// — no separate schedule object). A rock's span ends at its done-by date; a
// checked rock's [done:: date] pins the real end instead. Rocks without a
// done-by fall back to the legacy sequential-weeks derivation ([weeks:: N],
// default 1) from the previous rock's end — so un-migrated and half-dated
// records still render.

// StageSpan is one derived bar: [Start, End) ISO dates.
type StageSpan struct {
	ID     string  `json:"id"`
	Text   string  `json:"text"`
	Start  string  `json:"start"`
	End    string  `json:"end"`
	Weeks  float64 `json:"weeks"` // legacy knob (used only when done-by is absent)
	Done   bool    `json:"done"`
	Pinned bool    `json:"pinned"` // end came from a [done::] stamp or [done-by::] date
}

// DeriveSchedule computes rock spans. The cursor starts at work-start (empty
// → the first dated rock's anchor); each span ends at [done::] (checked),
// else [done-by::], else cursor + weeks.
func DeriveSchedule(workStart string, stages []WorkStage) []StageSpan {
	parse := func(v string) (time.Time, bool) {
		d, err := time.Parse("2006-01-02", strings.TrimSpace(v))
		return d, err == nil
	}
	cursor, haveCursor := parse(workStart)
	if !haveCursor {
		// no work-start: anchor on the first dated rock, if any
		for _, st := range stages {
			if d, ok := parse(st.DoneBy); ok {
				anchor := d.Add(-time.Duration(weeksOr1(st.Weeks) * 7 * 24 * float64(time.Hour)))
				cursor, haveCursor = anchor, true
				break
			}
			if st.Checked {
				if d, ok := parse(st.Done); ok {
					cursor, haveCursor = d, true
					break
				}
			}
		}
	}
	if !haveCursor {
		return nil // nothing dated — no schedule until one date exists
	}
	var out []StageSpan
	for _, st := range stages {
		weeks := weeksOr1(st.Weeks)
		span := StageSpan{ID: st.ID, Text: st.Text, Weeks: weeks, Done: st.Checked}
		spanStart := cursor
		var spanEnd time.Time
		if st.Checked {
			if d, ok := parse(st.Done); ok {
				spanEnd = d
				span.Pinned = true
			}
		}
		if spanEnd.IsZero() {
			if d, ok := parse(st.DoneBy); ok {
				spanEnd = d
				span.Pinned = true
			}
		}
		if spanEnd.IsZero() {
			spanEnd = spanStart.Add(time.Duration(weeks * 7 * 24 * float64(time.Hour)))
		}
		if spanEnd.Before(spanStart) { // an end date earlier than the derived start
			spanStart = spanEnd
		}
		span.Start = spanStart.Format("2006-01-02")
		span.End = spanEnd.Format("2006-01-02")
		out = append(out, span)
		cursor = spanEnd
	}
	return out
}

func weeksOr1(w float64) float64 {
	if w <= 0 {
		return 1
	}
	return w
}
