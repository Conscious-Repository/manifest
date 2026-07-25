package realestate

import (
	"strings"
	"time"
)

// The schedule (pass-5 §3) is DERIVED — the stage list is the schedule.
// Inputs: the property's `work-start:` date and each stage's [weeks:: N];
// a checked stage's [done:: date] pins its end (and the next stage's start).
// Editing is one number per stage — change weeks and everything downstream
// shifts. No month-by-month cascade, ever.

// StageSpan is one derived bar: [Start, End) ISO dates.
type StageSpan struct {
	ID     string  `json:"id"`
	Text   string  `json:"text"`
	Start  string  `json:"start"`
	End    string  `json:"end"`
	Weeks  float64 `json:"weeks"` // the editable knob (default 1 when unset)
	Done   bool    `json:"done"`
	Pinned bool    `json:"pinned"` // end came from a [done::] stamp
}

// DeriveSchedule computes sequential spans. Empty workStart → nil (no
// schedule until the one date is set).
func DeriveSchedule(workStart string, stages []WorkStage) []StageSpan {
	start, err := time.Parse("2006-01-02", strings.TrimSpace(workStart))
	if err != nil {
		return nil
	}
	cursor := start
	var out []StageSpan
	for _, st := range stages {
		weeks := st.Weeks
		if weeks <= 0 {
			weeks = 1
		}
		span := StageSpan{ID: st.ID, Text: st.Text, Weeks: weeks, Done: st.Checked}
		spanStart := cursor
		var spanEnd time.Time
		if st.Checked && st.Done != "" {
			if d, err := time.Parse("2006-01-02", st.Done); err == nil {
				spanEnd = d
				span.Pinned = true
			}
		}
		if spanEnd.IsZero() {
			spanEnd = spanStart.Add(time.Duration(weeks * 7 * 24 * float64(time.Hour)))
		}
		if spanEnd.Before(spanStart) { // a done stamp earlier than the derived start
			spanStart = spanEnd
		}
		span.Start = spanStart.Format("2006-01-02")
		span.End = spanEnd.Format("2006-01-02")
		out = append(out, span)
		cursor = spanEnd
	}
	return out
}
