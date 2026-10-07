package homeplan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// TaskRef is what the task system says about a task the plan refers to.
type TaskRef struct {
	Text    string `json:"text"`
	Done    bool   `json:"done"`
	Missing bool   `json:"missing,omitempty"` // no longer in the shared Home list
}

// Derived is computed on every read from the saved plan; it is never stored.
type Derived struct {
	AsOf      string                     `json:"asOf"`
	Weeks     []Week                     `json:"weeks"`
	Capacity  Totals                     `json:"capacity"`
	Items     []ItemView                 `json:"items"`
	Conflicts []Conflict                 `json:"conflicts"`
	Budget    *BudgetTotals              `json:"budget,omitempty"`
	Scenarios map[string]ScenarioOutcome `json:"scenarios,omitempty"`
	Sequence  *Sequence                  `json:"sequence,omitempty"` // assistant what-if; never saved
}

// Week is Monday-first. Weekend hours are elapsed hours together.
type Week struct {
	Start        string           `json:"start"` // Monday
	Saturday     string           `json:"saturday"`
	Status       string           `json:"status"` // shared | partial | solo | away | past
	Present      []string         `json:"present"`
	SharedHours  float64          `json:"sharedHours"`
	SoloHours    float64          `json:"soloHours,omitempty"` // light solo only; not baseline
	EveningHours float64          `json:"eveningHours"`        // planning/ordering only
	Reserved     []string         `json:"reserved,omitempty"`
	ReservedHrs  float64          `json:"reservedHours"`
	Allocated    float64          `json:"allocatedHours"`
	Free         float64          `json:"freeHours"` // may be negative: overallocated
	Allocations  []WeekAllocation `json:"allocations,omitempty"`
	Milestones   []string         `json:"milestones,omitempty"`
	Events       []string         `json:"events,omitempty"` // hours taken out of the day
	Away         []string         `json:"away,omitempty"`
	Past         bool             `json:"past,omitempty"`
	NormalHigh   *float64         `json:"normalHigh,omitempty"` // °F, from plan.climate
	NormalLow    *float64         `json:"normalLow,omitempty"`
}

type WeekAllocation struct {
	Ref   string  `json:"ref"`
	Hours float64 `json:"hours"`
}

type Totals struct {
	SharedWeekends int      `json:"sharedWeekends"`
	SharedHours    float64  `json:"sharedHours"`      // upcoming shared weekend hours, together
	ReservedHours  float64  `json:"reservedHours"`    // held on those weekends
	PoolHours      float64  `json:"poolHours"`        // shared − reserved
	KnownDemand    float64  `json:"knownDemand"`      // open pool work with a known estimate
	RemainingHours float64  `json:"remainingHours"`   // pool − known demand; negative stays negative
	AllocatedHours float64  `json:"allocatedHours"`   // of the known demand, placed on weekends
	UnknownItems   []string `json:"unknownItems"`     // open pool work with no estimate
	UnknownLow     float64  `json:"unknownLow"`       // sum of allowance lows where given
	UnknownHigh    float64  `json:"unknownHigh"`      // sum of allowance highs where given
	EveningHours   float64  `json:"eveningHours"`     // planning evenings through the horizon
	EveningDemand  float64  `json:"eveningDemand"`    // known evening estimates
	EveningUnknown []string `json:"eveningUnknown"`   // evening work with no estimate
	SoloWeekendHrs float64  `json:"soloWeekendHours"` // optional, outside the baseline
	ContingencyHrs float64  `json:"contingencyHours"`
	HelpHours      float64  `json:"helpHours,omitempty"` // shortfall family help is expected to cover
	WaitUnknown    []string `json:"waitUnknown"`         // supplier/crew waits with no duration
	HardDeadline   string   `json:"hardDeadline,omitempty"`
}

type ItemView struct {
	Ref        string    `json:"ref"`
	Task       string    `json:"task"`
	Sub        string    `json:"sub,omitempty"`
	Title      string    `json:"title"`
	Phase      string    `json:"phase"`
	Draws      string    `json:"draws"`
	IncludedIn string    `json:"includedIn,omitempty"`
	Done       bool      `json:"done"`
	Missing    bool      `json:"missing,omitempty"`
	Hours      *float64  `json:"hours"`
	Low        *float64  `json:"low,omitempty"`
	High       *float64  `json:"high,omitempty"`
	Crew       int       `json:"crew,omitempty"`
	PersonHrs  *float64  `json:"personHours,omitempty"`
	Allocated  float64   `json:"allocated"`
	First      string    `json:"first,omitempty"` // earliest allocated Saturday
	Last       string    `json:"last,omitempty"`
	Window     *Span     `json:"window,omitempty"`
	DependsOn  []string  `json:"dependsOn,omitempty"`
	Order      int       `json:"order"`
	Unknown    bool      `json:"unknown"`
	Estimate   *Estimate `json:"estimate,omitempty"`
	MinTempF   *float64  `json:"minTempF,omitempty"`
}

type Conflict struct {
	Kind    string `json:"kind"` // overallocated | unavailable | order | deadline | missing-task | overdemand | reservation
	Ref     string `json:"ref"`
	Message string `json:"message"`
}

type BudgetTotals struct {
	Total     *float64 `json:"total"`
	Known     float64  `json:"known"`
	Unknown   []string `json:"unknown"`
	Remaining *float64 `json:"remaining"` // total − known; unknown lines still open
}

type ScenarioOutcome struct {
	Label     string     `json:"label"`
	Basis     string     `json:"basis"`
	Capacity  Totals     `json:"capacity"`
	Conflicts []Conflict `json:"conflicts"`
	Sequence  *Sequence  `json:"sequence,omitempty"`
	Error     string     `json:"error,omitempty"`
}

func day(t time.Time) string { return t.Format("2006-01-02") }

// Derive computes the calendar, capacity and conflicts. asOf is the
// household's local date (America/Chicago); everything here is date-only, so
// a daylight-saving change can never add or lose an hour.
func Derive(p *Plan, tasks map[string]TaskRef, asOf string) Derived {
	d := derive(p, tasks, asOf)
	if len(p.Scenarios) > 0 {
		d.Scenarios = map[string]ScenarioOutcome{}
		base, _ := json.Marshal(p)
		for id, s := range p.Scenarios {
			out := ScenarioOutcome{Label: s.Label, Basis: s.Basis}
			merged, err := MergePatch(base, s.Patch)
			var sp *Plan
			if err == nil {
				sp, err = Decode(merged)
			}
			if err == nil {
				sp.Scenarios = nil
				err = Validate(sp, nil, nil)
			}
			if err != nil {
				out.Error = err.Error()
			} else {
				sd := derive(sp, tasks, asOf)
				out.Capacity, out.Conflicts, out.Sequence = sd.Capacity, sd.Conflicts, sd.Sequence
			}
			d.Scenarios[id] = out
		}
	}
	return d
}

func derive(p *Plan, tasks map[string]TaskRef, asOf string) Derived {
	d := Derived{AsOf: asOf, Conflicts: []Conflict{}}
	today, ok := parseDate(asOf)
	if !ok {
		today = time.Now().UTC().Truncate(24 * time.Hour)
	}
	start, _ := parseDate(p.Horizon.Start)
	end, _ := parseDate(p.Horizon.End)
	monday := start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
	people := make([]string, 0, len(p.People))
	for id := range p.People {
		people = append(people, id)
	}
	sort.Strings(people)
	awayOn := func(date time.Time) ([]string, []string) {
		var gone, why []string
		for id, a := range p.Away {
			f, _ := parseDate(a.From)
			t, _ := parseDate(a.To)
			if !date.Before(f) && !date.After(t) {
				gone = append(gone, a.Who...)
				why = append(why, id)
			}
		}
		return gone, why
	}
	present := func(gone []string) []string {
		var out []string
		for _, id := range people {
			if !contains(gone, id) {
				out = append(out, id)
			}
		}
		return out
	}
	var hardDeadline string
	for _, m := range p.Milestones {
		if m.Kind == "deadline" && m.Date != "" && (hardDeadline == "" || m.Date < hardDeadline) {
			hardDeadline = m.Date
		}
	}
	d.Capacity.HardDeadline = hardDeadline

	// items: tasks and their subtasks, flattened
	itemOf := map[string]Item{}
	for id, t := range p.Tasks {
		ref := tasks[id]
		iv := view(id, "", t.Item, ref.Text, ref.Done)
		iv.Missing = ref.Missing
		if ref.Missing {
			d.Conflicts = append(d.Conflicts, Conflict{"missing-task", id, "the plan refers to a task no longer in the shared Home list"})
		}
		d.Items = append(d.Items, iv)
		itemOf[id] = t.Item
		for sid, st := range t.Subtasks {
			sv := view(id, sid, st.Item, st.Title, st.Done || ref.Done)
			d.Items = append(d.Items, sv)
			itemOf[id+"#"+sid] = st.Item
		}
	}
	sort.Slice(d.Items, func(i, j int) bool {
		a, b := d.Items[i], d.Items[j]
		if a.Phase != b.Phase {
			return phaseRank(a.Phase) < phaseRank(b.Phase)
		}
		if a.Task != b.Task {
			ta, tb := p.Tasks[a.Task].Order, p.Tasks[b.Task].Order
			if ta != tb {
				return ta < tb
			}
			return a.Task < b.Task
		}
		if (a.Sub == "") != (b.Sub == "") {
			return a.Sub == ""
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Sub < b.Sub
	})
	doneOf := map[string]bool{}
	for _, iv := range d.Items {
		doneOf[iv.Ref] = iv.Done
	}

	// weeks
	weekIdx := map[string]int{}
	for w := monday; !w.After(end); w = w.AddDate(0, 0, 7) {
		sat, sun := w.AddDate(0, 0, 5), w.AddDate(0, 0, 6)
		wk := Week{Start: day(w), Saturday: day(sat)}
		goneSat, whySat := awayOn(sat)
		goneSun, whySun := awayOn(sun)
		pSat, pSun := present(goneSat), present(goneSun)
		wk.Away = uniq(append(whySat, whySun...))
		wk.Present = uniq(append(append([]string{}, pSat...), pSun...))
		together := 0
		for i, pr := range [][]string{pSat, pSun} {
			date := day(sat.AddDate(0, 0, i))
			hours := p.Capacity.WeekendDayHours
			for _, id := range sortedKeys(p.Events) {
				if e := p.Events[id]; e.Date == date {
					hours -= e.Hours
					wk.Events = append(wk.Events, id)
				}
			}
			hours = max(hours, 0)
			switch {
			case len(pr) == len(people) && len(people) > 0:
				wk.SharedHours += hours
				together++
			case len(pr) > 0:
				wk.SoloHours += hours
			}
		}
		switch {
		case together == 2:
			wk.Status = "shared"
		case together == 1:
			wk.Status = "partial"
		case wk.SoloHours > 0:
			wk.Status = "solo"
		default:
			wk.Status = "away"
		}
		wk.EveningHours = float64(p.Capacity.EveningsPerWeek) * p.Capacity.EveningHours
		wk.Past = sun.Before(today)
		if sat.After(end) {
			wk.SharedHours, wk.SoloHours = 0, 0 // the deadline week: no weekend left
			wk.Status = "after"
		}
		if p.Climate != nil {
			if n, ok := p.Climate.Normals[wk.Saturday]; ok {
				hi, lo := n.High, n.Low
				wk.NormalHigh, wk.NormalLow = &hi, &lo
			}
		}
		weekIdx[wk.Saturday] = len(d.Weeks)
		d.Weeks = append(d.Weeks, wk)
	}
	for id, r := range p.Reservations {
		i, ok := weekIdx[r.Weekend]
		if !ok {
			d.Conflicts = append(d.Conflicts, Conflict{"reservation", id, "reserved weekend " + r.Weekend + " is outside the horizon"})
			continue
		}
		w := &d.Weeks[i]
		w.Reserved = append(w.Reserved, id)
		w.ReservedHrs += r.Hours
		if w.Status != "shared" {
			d.Conflicts = append(d.Conflicts, Conflict{"unavailable", id, fmt.Sprintf("%s is reserved but the weekend of %s is not shared (%s)", r.Purpose, r.Weekend, w.Status)})
		}
	}
	for _, m := range sortedKeys(p.Milestones) {
		ms := p.Milestones[m]
		if ms.Date == "" {
			continue
		}
		md, _ := parseDate(ms.Date)
		for i := range d.Weeks {
			ws, _ := parseDate(d.Weeks[i].Start)
			if !md.Before(ws) && md.Before(ws.AddDate(0, 0, 7)) {
				d.Weeks[i].Milestones = append(d.Weeks[i].Milestones, m)
			}
		}
	}
	// allocations land on weeks
	for i := range d.Items {
		iv := &d.Items[i]
		it := itemOf[iv.Ref]
		for _, sat := range sortedKeys(it.Allocations) {
			h := it.Allocations[sat]
			iv.Allocated += h
			if iv.First == "" || sat < iv.First {
				iv.First = sat
			}
			if sat > iv.Last {
				iv.Last = sat
			}
			wi, ok := weekIdx[sat]
			if !ok {
				continue
			}
			w := &d.Weeks[wi]
			w.Allocations = append(w.Allocations, WeekAllocation{iv.Ref, h})
			if strings.HasPrefix(iv.Draws, "reservation:") && contains(w.Reserved, strings.TrimPrefix(iv.Draws, "reservation:")) {
				continue // inside its own reservation: already held
			}
			w.Allocated += h
			need := 2
			if it.Estimate != nil && it.Estimate.Crew == 1 {
				need = 1
			}
			if w.Status != "shared" && !(need == 1 && w.SoloHours > 0) {
				d.Conflicts = append(d.Conflicts, Conflict{"unavailable", iv.Ref, fmt.Sprintf("%s is placed on %s, which is not a shared weekend (%s)", iv.Title, sat, w.Status)})
			}
			if tooCold(it.MinTempF, w) {
				d.Conflicts = append(d.Conflicts, Conflict{"cold", iv.Ref, fmt.Sprintf("%s needs %.0f°F; the weekend of %s normally peaks at %.0f°F", iv.Title, *it.MinTempF, sat, *w.NormalHigh)})
			}
			if hardDeadline != "" && sat > hardDeadline {
				d.Conflicts = append(d.Conflicts, Conflict{"deadline", iv.Ref, iv.Title + " is placed after the deadline " + hardDeadline})
			}
		}
	}
	for i := range d.Weeks {
		w := &d.Weeks[i]
		w.Free = w.SharedHours - w.ReservedHrs - w.Allocated
		if w.Free < 0 {
			d.Conflicts = append(d.Conflicts, Conflict{"overallocated", w.Saturday, fmt.Sprintf("weekend of %s holds %.0f h on %.0f shared hours", w.Saturday, w.ReservedHrs+w.Allocated, w.SharedHours)})
		}
		if w.Past || w.Status == "after" {
			continue
		}
		if w.Status == "shared" || w.Status == "partial" {
			d.Capacity.SharedHours += w.SharedHours
			d.Capacity.ReservedHours += w.ReservedHrs
			if w.Status == "shared" {
				d.Capacity.SharedWeekends++
			}
		}
		d.Capacity.SoloWeekendHrs += w.SoloHours
		d.Capacity.EveningHours += w.EveningHours
		for _, r := range w.Reserved {
			if p.Reservations[r].Kind == "contingency" {
				d.Capacity.ContingencyHrs += p.Reservations[r].Hours
			}
		}
	}
	d.Capacity.PoolHours = d.Capacity.SharedHours - d.Capacity.ReservedHours

	// demand
	d.Capacity.UnknownItems, d.Capacity.EveningUnknown, d.Capacity.WaitUnknown = []string{}, []string{}, []string{}
	for _, iv := range d.Items {
		it := itemOf[iv.Ref]
		if w := it.Wait; w != nil && w.Days == nil && !iv.Done {
			d.Capacity.WaitUnknown = append(d.Capacity.WaitUnknown, iv.Ref)
		}
		if iv.Done || iv.IncludedIn != "" {
			continue
		}
		switch iv.Draws {
		case "pool":
			if iv.Hours != nil {
				d.Capacity.KnownDemand += *iv.Hours
				d.Capacity.AllocatedHours += iv.Allocated
			} else {
				d.Capacity.UnknownItems = append(d.Capacity.UnknownItems, iv.Ref)
				if iv.Low != nil {
					d.Capacity.UnknownLow += *iv.Low
				}
				if iv.High != nil {
					d.Capacity.UnknownHigh += *iv.High
				}
			}
		case "evening":
			if iv.Hours != nil {
				d.Capacity.EveningDemand += *iv.Hours
			} else {
				d.Capacity.EveningUnknown = append(d.Capacity.EveningUnknown, iv.Ref)
			}
		}
		if iv.Hours != nil && iv.Allocated > *iv.Hours {
			d.Conflicts = append(d.Conflicts, Conflict{"overallocated", iv.Ref, fmt.Sprintf("%s has %.0f h placed for a %.0f h estimate", iv.Title, iv.Allocated, *iv.Hours)})
		}
	}
	d.Capacity.RemainingHours = d.Capacity.PoolHours - d.Capacity.KnownDemand
	if d.Capacity.RemainingHours < 0 {
		if p.Capacity.FamilyHelp != "" {
			d.Capacity.HelpHours = -d.Capacity.RemainingHours
		} else {
			d.Conflicts = append(d.Conflicts, Conflict{"overdemand", "pool", fmt.Sprintf("known work needs %.0f h but the open weekends hold %.0f h", d.Capacity.KnownDemand, d.Capacity.PoolHours)})
		}
	}

	// dependency order: nothing placed before what it waits on
	last := func(ref string) (string, string) {
		if id, ok := strings.CutPrefix(ref, "milestone:"); ok {
			return p.Milestones[id].Date, p.Milestones[id].Title
		}
		for _, iv := range d.Items {
			if iv.Ref == ref {
				l := iv.Last
				if iv.Window != nil && iv.Window.End > l {
					l = iv.Window.End
				}
				return l, iv.Title
			}
		}
		return "", ref
	}
	for _, iv := range d.Items {
		if iv.First == "" || iv.Done {
			continue
		}
		for _, dep := range iv.DependsOn {
			if doneOf[dep] {
				continue
			}
			if l, title := last(dep); l != "" && l > iv.First {
				d.Conflicts = append(d.Conflicts, Conflict{"order", iv.Ref, fmt.Sprintf("%s starts %s, before %s (%s)", iv.Title, iv.First, title, l)})
			}
		}
	}
	for id, m := range p.Milestones {
		if m.Date == "" {
			continue
		}
		for _, dep := range m.DependsOn {
			if doneOf[dep] {
				continue
			}
			if l, title := last(dep); l != "" && l > m.Date {
				d.Conflicts = append(d.Conflicts, Conflict{"order", "milestone:" + id, fmt.Sprintf("%s (%s) comes before %s (%s)", m.Title, m.Date, title, l)})
			}
		}
	}
	if b := p.Budget; b != nil {
		bt := &BudgetTotals{Total: b.Total, Unknown: []string{}}
		for _, id := range sortedKeys(b.Lines) {
			l := b.Lines[id]
			if l.Amount == nil {
				bt.Unknown = append(bt.Unknown, id)
			} else {
				bt.Known += *l.Amount
			}
		}
		if b.Total != nil {
			r := *b.Total - bt.Known
			bt.Remaining = &r
		}
		d.Budget = bt
	}
	sort.SliceStable(d.Conflicts, func(i, j int) bool { return d.Conflicts[i].Ref < d.Conflicts[j].Ref })
	d.Sequence = sequence(p, &d, itemOf)
	return d
}

// ColdMarginF: a normal high only this far above a material's minimum is
// treated as too cold — the high lasts a few hours and mornings are colder.
const ColdMarginF = 5

func tooCold(min *float64, w *Week) bool {
	return min != nil && w.NormalHigh != nil && *w.NormalHigh < *min+ColdMarginF
}

func view(task, sub string, it Item, title string, done bool) ItemView {
	ref := task
	if sub != "" {
		ref = task + "#" + sub
	}
	iv := ItemView{Ref: ref, Task: task, Sub: sub, Title: title, Phase: it.Phase, Draws: it.Draws, IncludedIn: it.IncludedIn,
		Done: done, Window: it.Window, DependsOn: it.DependsOn, Order: it.Order, Estimate: it.Estimate, MinTempF: it.MinTempF}
	if iv.Draws == "" {
		iv.Draws = "pool"
		if it.Phase != "execution" {
			iv.Draws = "none"
		}
	}
	if e := it.Estimate; e != nil {
		iv.Hours, iv.Low, iv.High, iv.Crew = e.Hours, e.Low, e.High, e.Crew
		if e.Hours != nil && e.Crew > 0 {
			ph := *e.Hours * float64(e.Crew)
			iv.PersonHrs = &ph
		}
	}
	iv.Unknown = iv.Hours == nil && iv.IncludedIn == "" && iv.Draws != "outside" && iv.Draws != "none"
	return iv
}

func phaseRank(p string) int {
	return map[string]int{"planning": 0, "procurement": 1, "execution": 2, "later": 3}[p]
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func uniq(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
