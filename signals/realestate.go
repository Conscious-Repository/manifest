package signals

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"manifest/realestate"
)

// Real-estate emitters (real-estate-management plan §4): cheap pure functions
// over the loaded property records, computed at read time like every other
// emitter. These fill the reserved "home-status" slot.

// PropertyLister is the narrow surface both emitters read.
type PropertyLister interface {
	Properties() ([]realestate.Property, error)
}

// OverBudgetProperties emits one signal per work STAGE whose committed exceeds
// its estimate (pass-5: the work list is the budget; a committed overrun warns
// earlier than a paid one). Hash = the overage, re-arming on growth.
func OverBudgetProperties(l PropertyLister) Emitter { return &overBudgetEmitter{l} }

type overBudgetEmitter struct{ l PropertyLister }

func (e *overBudgetEmitter) Emit(now time.Time) ([]Signal, error) {
	props, err := e.l.Properties()
	if err != nil {
		return nil, err // no data ≠ all-clear
	}
	var out []Signal
	for _, p := range props {
		for _, st := range p.Work {
			if st.EstTotal <= 0 || st.Committed <= st.EstTotal {
				continue
			}
			over := st.Committed - st.EstTotal
			out = append(out, Signal{
				ID:      "property-overbudget:" + p.Slug + ":" + st.ID,
				Kind:    "property-overbudget",
				Entity:  p.Address,
				Label:   fmt.Sprintf("%s — %s committed $%.0f over est", firstOr(p.Address, p.Slug), st.Text, over),
				Age:     0,
				ActHref: "#/properties/" + url.PathEscape(p.Slug),
				Hash:    fmt.Sprintf("%.0f", over),
			})
		}
	}
	return out, nil
}

// stalledAfter: an active-bucket property with no dated activity in this window
// is stalled (spec §4: no log line and no ledger row in 14d).
const stalledAfter = 14 * 24 * time.Hour

// StalledProperties emits one signal per active-bucket property
// (pre_development | construction) whose latest dated log line / ledger row is
// older than 14 days. Hash = that latest-activity date, so any new activity
// clears + re-arms naturally.
func StalledProperties(l PropertyLister) Emitter { return &stalledPropEmitter{l} }

type stalledPropEmitter struct{ l PropertyLister }

func (e *stalledPropEmitter) Emit(now time.Time) ([]Signal, error) {
	props, err := e.l.Properties()
	if err != nil {
		return nil, err
	}
	var out []Signal
	for _, p := range props {
		if p.Status != "pre_development" && p.Status != "construction" {
			continue
		}
		last := lastActivity(p)
		if !last.IsZero() && now.Sub(last) < stalledAfter {
			continue
		}
		days := 0
		lastLabel := "no dated activity"
		if !last.IsZero() {
			days = int(now.Sub(last).Hours() / 24)
			lastLabel = fmt.Sprintf("quiet %dd", days)
		}
		out = append(out, Signal{
			ID:      "property-stalled:" + p.Slug,
			Kind:    "property-stalled",
			Entity:  p.Address,
			Label:   fmt.Sprintf("%s — %s (%s)", firstOr(p.Address, p.Slug), lastLabel, p.Status),
			Age:     days,
			ActHref: "#/properties/" + url.PathEscape(p.Slug),
			Hash:    last.Format("2006-01-02"),
		})
	}
	return out, nil
}

// NoNextAction emits when an ACTIVE property has a work section whose current
// stage has zero open todos — the project has nothing queued next (pass-4
// plan). Hash = the current stage id, so it re-arms when the stage changes.
func NoNextAction(l PropertyLister) Emitter { return &noNextEmitter{l} }

type noNextEmitter struct{ l PropertyLister }

func (e *noNextEmitter) Emit(now time.Time) ([]Signal, error) {
	props, err := e.l.Properties()
	if err != nil {
		return nil, err
	}
	var out []Signal
	for _, p := range props {
		if p.Status != "pre_development" && p.Status != "construction" {
			continue
		}
		if len(p.Work) == 0 {
			continue // no work section yet — the stalled signal covers neglect
		}
		var cur *struct {
			id   string
			open int
		}
		for _, st := range p.Work {
			if st.Current {
				open := 0
				for _, t := range st.Tasks {
					if !t.Checked {
						open++
					}
				}
				cur = &struct {
					id   string
					open int
				}{st.ID, open}
				break
			}
		}
		if cur == nil || cur.open > 0 {
			continue
		}
		out = append(out, Signal{
			ID:      "property-no-next:" + p.Slug,
			Kind:    "property-no-next",
			Entity:  p.Address,
			Label:   firstOr(p.Address, p.Slug) + " — no next action queued (" + p.CurrentStage + ")",
			Age:     0,
			ActHref: "#/properties/" + url.PathEscape(p.Slug),
			Hash:    cur.id,
		})
	}
	return out, nil
}

// lastActivity is the latest date across `YYYY-MM-DD `-prefixed log lines and
// ledger rows. Undated log lines (seeded prose) don't count as activity.
func lastActivity(p realestate.Property) time.Time {
	var last time.Time
	consider := func(s string) {
		if len(s) < 10 {
			return
		}
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil && t.After(last) {
			last = t
		}
	}
	for _, l := range p.Log {
		consider(l)
	}
	for _, r := range p.Ledger {
		consider(r.Date)
	}
	return last
}

func firstOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
