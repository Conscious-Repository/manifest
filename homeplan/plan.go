// Package homeplan is the shared Home project's scheduling model: availability,
// reservations, milestones, estimates, dependencies and allocations for the
// shared Home tasks, kept beside them in system/home/plan.json.
//
// Task identity, titles, completion, descriptions and comments stay in the
// task system (system/home/tasks.md and notes/); this file only refers to
// tasks by ID. Unknown is null, never zero. Every write is a JSON merge patch
// (RFC 7396) against a revision, validated as a whole before it lands.
package homeplan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const Schema = "manifest.home-plan/1"

// Plan is the canonical document. Maps are keyed by stable IDs so a merge
// patch can change one entry without restating the others.
type Plan struct {
	Schema       string                 `json:"schema"`
	Project      string                 `json:"project"`  // the goals rock the plan serves
	Timezone     string                 `json:"timezone"` // display only; all arithmetic is date-only
	Horizon      Span                   `json:"horizon"`
	People       map[string]Person      `json:"people"`
	Capacity     Capacity               `json:"capacity"`
	Away         map[string]Away        `json:"away,omitempty"`
	Events       map[string]Event       `json:"events,omitempty"`
	Reservations map[string]Reservation `json:"reservations,omitempty"`
	Milestones   map[string]Milestone   `json:"milestones,omitempty"`
	Tasks        map[string]Task        `json:"tasks,omitempty"`
	Decisions    map[string]Decision    `json:"decisions,omitempty"` // project-wide
	Budget       *Budget                `json:"budget,omitempty"`
	Scenarios    map[string]Scenario    `json:"scenarios,omitempty"`
	Climate      *Climate               `json:"climate,omitempty"`
	Source       string                 `json:"source,omitempty"`
}

// Climate is typical weather per weekend (normals, not a forecast), so
// temperature-limited work can be checked against when it is placed.
type Climate struct {
	Source  string            `json:"source"`
	Normals map[string]Normal `json:"normals"` // Saturday → normal high/low °F
}

type Normal struct {
	High float64 `json:"high"`
	Low  float64 `json:"low"`
}

type Span struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Person struct {
	Name string `json:"name"`
}

// Capacity is household time. Weekend hours are ELAPSED hours the people
// spend together: two people working eight hours is eight, not sixteen.
// Evenings are planning/ordering time only and never carry physical work.
type Capacity struct {
	WeekendDayHours float64 `json:"weekendDayHours"`
	EveningsPerWeek int     `json:"eveningsPerWeek"`
	EveningHours    float64 `json:"eveningHours"`
	Note            string  `json:"note,omitempty"`
	Solo            string  `json:"solo,omitempty"` // what a one-person weekend may hold; never baseline
	// FamilyHelp, when set, says helpers on some weekends cover work beyond
	// the household's own hours: a shortfall is then a ballpark for them,
	// not a conflict. Free text: who helps and how it is estimated.
	FamilyHelp string   `json:"familyHelp,omitempty"`
	Days       []string `json:"days,omitempty"` // reserved for later; weekends are Sat+Sun
}

// Away removes people from the calendar for an inclusive date range.
type Away struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Who  []string `json:"who"`
	Note string   `json:"note,omitempty"`
}

// Event takes hours out of one day's work time while everyone is home (a
// neighborhood planting morning). It may point at the Home task it is.
type Event struct {
	Date  string  `json:"date"`
	Hours float64 `json:"hours"`
	Title string  `json:"title"`
	Task  string  `json:"task,omitempty"`
	Note  string  `json:"note,omitempty"`
}

// Reservation holds shared weekend hours for a purpose. It is a capacity
// hold, not measured effort; covered tasks draw from it, not from the pool.
type Reservation struct {
	Weekend string  `json:"weekend"` // the Saturday
	Hours   float64 `json:"hours"`
	Purpose string  `json:"purpose"`
	Kind    string  `json:"kind"`   // work | checks | contingency
	Status  string  `json:"status"` // provisional | confirmed
	Note    string  `json:"note,omitempty"`
}

type Milestone struct {
	Date      string   `json:"date,omitempty"` // "" = no firm date
	Title     string   `json:"title"`
	Kind      string   `json:"kind"` // deadline | target | external | later
	Confirmed bool     `json:"confirmed"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Note      string   `json:"note,omitempty"`
	Source    string   `json:"source,omitempty"`
}

// Estimate is elapsed household hours (crew working together). Hours null
// means unknown; Low/High bound an allowance without pretending to know.
type Estimate struct {
	Hours      *float64 `json:"hours"`
	Low        *float64 `json:"low,omitempty"`
	High       *float64 `json:"high,omitempty"`
	Crew       int      `json:"crew,omitempty"`       // people working at once; person-hours = hours × crew
	Basis      string   `json:"basis"`                // user | assistant | supplier | crew | unknown
	Confidence string   `json:"confidence,omitempty"` // confirmed | allowance | guess | unknown
	Excludes   string   `json:"excludes,omitempty"`
	Source     string   `json:"source,omitempty"`
}

// Wait is elapsed calendar time nobody in the household works (supplier
// lead time, cure, outside crew). It never counts against capacity.
type Wait struct {
	Days  *float64 `json:"days"`
	Label string   `json:"label"`
	Note  string   `json:"note,omitempty"`
}

// Item is what tasks and subtasks share: the schedulable unit.
type Item struct {
	Phase       string             `json:"phase"`           // planning | procurement | execution | later
	Draws       string             `json:"draws,omitempty"` // pool | evening | outside | reservation:<id> | none
	IncludedIn  string             `json:"includedIn,omitempty"`
	Owner       string             `json:"owner,omitempty"`
	NextAction  string             `json:"nextAction,omitempty"`
	Estimate    *Estimate          `json:"estimate,omitempty"`
	Wait        *Wait              `json:"wait,omitempty"`
	Window      *Span              `json:"window,omitempty"` // planned dates for outside/crew work; never proof of completion
	DependsOn   []string           `json:"dependsOn,omitempty"`
	Allocations map[string]float64 `json:"allocations,omitempty"` // Saturday → elapsed hours
	MinTempF    *float64           `json:"minTempF,omitempty"`    // lowest application/cure temperature of its materials
	Order       int                `json:"order,omitempty"`
	Note        string             `json:"note,omitempty"`
	Source      string             `json:"source,omitempty"`
}

type Task struct {
	Item
	Subtasks  map[string]Subtask  `json:"subtasks,omitempty"`
	Decisions map[string]Decision `json:"decisions,omitempty"`
	Links     map[string]Link     `json:"links,omitempty"`
}

// Subtask lives only in the plan: a piece of a task's scope with its own
// estimate. Its done flag is the plan's; the parent's is the task system's.
type Subtask struct {
	Item
	Title string `json:"title"`
	Done  bool   `json:"done,omitempty"`
}

type Decision struct {
	Question string   `json:"question"`
	Status   string   `json:"status"` // open | decided | deferred
	Options  []string `json:"options,omitempty"`
	Answer   string   `json:"answer,omitempty"`
	Blocks   []string `json:"blocks,omitempty"`
	Note     string   `json:"note,omitempty"`
	Source   string   `json:"source,omitempty"`
}

type Link struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

type Budget struct {
	Total    *float64              `json:"total"`
	Currency string                `json:"currency"`
	Note     string                `json:"note,omitempty"`
	Lines    map[string]BudgetLine `json:"lines,omitempty"`
}

type BudgetLine struct {
	Label  string   `json:"label"`
	Amount *float64 `json:"amount"` // null = unknown price
	Status string   `json:"status"` // estimate | quote | committed | paid | unknown
	Note   string   `json:"note,omitempty"`
}

// Scenario is a what-if: a merge patch over the saved plan, derived on
// read and never applied to the baseline by itself.
type Scenario struct {
	Label string          `json:"label"`
	Basis string          `json:"basis"` // assistant | user
	Note  string          `json:"note,omitempty"`
	Patch json.RawMessage `json:"patch"`
	Order int             `json:"order,omitempty"`
}

// Decode reads a plan strictly: an unknown field is an error, so a typo in a
// patch is refused instead of silently ignored.
func Decode(raw []byte) (*Plan, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var p Plan
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func Encode(p *Plan) ([]byte, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ValidationError lists every problem at once.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "plan rejected: " + strings.Join(e.Problems, "; ")
}

var (
	phases     = set("planning", "procurement", "execution", "later")
	msKinds    = set("deadline", "target", "external", "later")
	resKinds   = set("work", "checks", "contingency")
	resStatus  = set("provisional", "confirmed")
	decStatus  = set("open", "decided", "deferred")
	estBasis   = set("user", "assistant", "supplier", "crew", "unknown")
	estConf    = set("", "confirmed", "allowance", "guess", "unknown")
	lineStatus = set("estimate", "quote", "committed", "paid", "unknown")
	scenBasis  = set("assistant", "user")
)

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// Validate checks the whole document. known reports whether a task ID exists
// in the shared Home task list; newTasks are the task keys this write adds
// (a task deleted from the list later is flagged, not a reason to refuse
// every unrelated edit).
func Validate(p *Plan, known func(string) bool, newTasks []string) error {
	var bad []string
	add := func(f string, a ...any) { bad = append(bad, fmt.Sprintf(f, a...)) }
	if p.Schema != Schema {
		add("schema must be %q", Schema)
	}
	start, ok1 := parseDate(p.Horizon.Start)
	end, ok2 := parseDate(p.Horizon.End)
	if !ok1 || !ok2 || end.Before(start) {
		add("horizon needs start ≤ end as YYYY-MM-DD")
	}
	if p.Capacity.WeekendDayHours < 0 || p.Capacity.WeekendDayHours > 24 || p.Capacity.EveningHours < 0 || p.Capacity.EveningHours > 24 || p.Capacity.EveningsPerWeek < 0 || p.Capacity.EveningsPerWeek > 7 {
		add("capacity hours out of range")
	}
	if len(p.People) == 0 {
		add("people must name who shares the work")
	}
	for id, a := range p.Away {
		f, okf := parseDate(a.From)
		t, okt := parseDate(a.To)
		if !okf || !okt || t.Before(f) {
			add("away %s needs from ≤ to", id)
		}
		if len(a.Who) == 0 {
			add("away %s names nobody", id)
		}
		for _, w := range a.Who {
			if _, ok := p.People[w]; !ok {
				add("away %s: unknown person %q", id, w)
			}
		}
	}
	for id, e := range p.Events {
		if _, ok := parseDate(e.Date); !ok || e.Hours < 0 || e.Hours > p.Capacity.WeekendDayHours || strings.TrimSpace(e.Title) == "" {
			add("event %s needs a date, a title and 0 ≤ hours ≤ a day's hours", id)
		}
		if e.Task != "" && known != nil && !known(e.Task) {
			add("event %s: task %s is not a shared Home task", id, e.Task)
		}
	}
	for id, r := range p.Reservations {
		if d, ok := parseDate(r.Weekend); !ok || d.Weekday() != time.Saturday {
			add("reservation %s: weekend must be a Saturday date", id)
		}
		if r.Hours < 0 || !resKinds[r.Kind] || !resStatus[r.Status] {
			add("reservation %s: hours ≥ 0, kind work|checks|contingency, status provisional|confirmed", id)
		}
	}
	for id, m := range p.Milestones {
		if m.Date != "" {
			if _, ok := parseDate(m.Date); !ok {
				add("milestone %s: bad date", id)
			}
		}
		if !msKinds[m.Kind] || strings.TrimSpace(m.Title) == "" {
			add("milestone %s: needs a title and kind deadline|target|external|later", id)
		}
	}
	for _, id := range newTasks {
		if known != nil && !known(id) {
			add("task %s is not a shared Home task (create it in Home first)", id)
		}
	}
	checkItem := func(where string, it Item) {
		if !phases[it.Phase] {
			add("%s: phase must be planning|procurement|execution|later", where)
		}
		switch {
		case it.Draws == "", it.Draws == "pool", it.Draws == "evening", it.Draws == "outside", it.Draws == "none":
		case strings.HasPrefix(it.Draws, "reservation:"):
			if _, ok := p.Reservations[strings.TrimPrefix(it.Draws, "reservation:")]; !ok {
				add("%s: draws from unknown %s", where, it.Draws)
			}
		default:
			add("%s: draws must be pool|evening|outside|none|reservation:<id>", where)
		}
		if it.Draws == "evening" && it.Phase == "execution" {
			add("%s: evenings are for planning and ordering, not physical work", where)
		}
		if it.Draws == "evening" && len(it.Allocations) > 0 {
			add("%s: evening work cannot be allocated to weekends", where)
		}
		if it.IncludedIn != "" && !refExists(p, it.IncludedIn) {
			add("%s: includedIn %q does not exist", where, it.IncludedIn)
		}
		if e := it.Estimate; e != nil {
			for _, v := range []*float64{e.Hours, e.Low, e.High} {
				if v != nil && *v < 0 {
					add("%s: estimate hours cannot be negative", where)
				}
			}
			if e.Low != nil && e.High != nil && *e.High < *e.Low {
				add("%s: estimate high < low", where)
			}
			if !estBasis[e.Basis] || !estConf[e.Confidence] || e.Crew < 0 {
				add("%s: estimate basis user|assistant|supplier|crew|unknown", where)
			}
		}
		if w := it.Wait; w != nil && w.Days != nil && *w.Days < 0 {
			add("%s: wait days cannot be negative", where)
		}
		if w := it.Window; w != nil {
			if _, ok := parseDate(w.Start); w.Start != "" && !ok {
				add("%s: window start bad date", where)
			}
			if _, ok := parseDate(w.End); w.End != "" && !ok {
				add("%s: window end bad date", where)
			}
		}
		for sat, h := range it.Allocations {
			d, ok := parseDate(sat)
			if !ok || d.Weekday() != time.Saturday {
				add("%s: allocation %q must be a Saturday date", where, sat)
			} else if d.Before(start.AddDate(0, 0, -6)) || d.After(end) {
				add("%s: allocation %s is outside the horizon", where, sat)
			}
			if h <= 0 {
				add("%s: allocation %s needs hours > 0 (remove it with null)", where, sat)
			}
		}
		for _, d := range it.DependsOn {
			if !refExists(p, d) {
				add("%s: depends on unknown %q", where, d)
			}
		}
	}
	for id, t := range p.Tasks {
		checkItem("task "+id, t.Item)
		for sid, st := range t.Subtasks {
			if strings.TrimSpace(st.Title) == "" {
				add("subtask %s#%s needs a title", id, sid)
			}
			checkItem("subtask "+id+"#"+sid, st.Item)
		}
		for did, d := range t.Decisions {
			if !decStatus[d.Status] || strings.TrimSpace(d.Question) == "" {
				add("decision %s#%s needs a question and status open|decided|deferred", id, did)
			}
		}
		for lid, l := range t.Links {
			if !(strings.HasPrefix(l.Href, "https://") || strings.HasPrefix(l.Href, "http://")) {
				add("link %s#%s must be an http(s) URL", id, lid)
			}
		}
	}
	for id, d := range p.Decisions {
		if !decStatus[d.Status] || strings.TrimSpace(d.Question) == "" {
			add("decision %s needs a question and status open|decided|deferred", id)
		}
	}
	if b := p.Budget; b != nil {
		for id, l := range b.Lines {
			if !lineStatus[l.Status] {
				add("budget line %s: status estimate|quote|committed|paid|unknown", id)
			}
		}
	}
	for id, m := range p.Milestones {
		for _, d := range m.DependsOn {
			if !refExists(p, d) {
				add("milestone %s: depends on unknown %q", id, d)
			}
		}
	}
	if cyc := findCycle(p); cyc != nil {
		add("dependencies form a cycle: %s", strings.Join(cyc, " → "))
	}
	if c := p.Climate; c != nil {
		for sat, n := range c.Normals {
			if d, ok := parseDate(sat); !ok || d.Weekday() != time.Saturday || n.High < n.Low {
				add("climate normal %s: a Saturday with high ≥ low", sat)
			}
		}
	}
	for id, s := range p.Scenarios {
		if !scenBasis[s.Basis] || strings.TrimSpace(s.Label) == "" {
			add("scenario %s needs a label and basis assistant|user", id)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return &ValidationError{bad}
	}
	return nil
}

// refExists resolves a dependency reference: "<task>", "<task>#<subtask>"
// or "milestone:<id>".
func refExists(p *Plan, ref string) bool {
	if id, ok := strings.CutPrefix(ref, "milestone:"); ok {
		_, ok := p.Milestones[id]
		return ok
	}
	task, sub, hasSub := strings.Cut(ref, "#")
	t, ok := p.Tasks[task]
	if !ok {
		return false
	}
	if hasSub {
		_, ok = t.Subtasks[sub]
	}
	return ok
}

func edges(p *Plan) map[string][]string {
	g := map[string][]string{}
	for id, t := range p.Tasks {
		g[id] = append(g[id], t.DependsOn...)
		for sid, st := range t.Subtasks {
			g[id+"#"+sid] = append(g[id+"#"+sid], st.DependsOn...)
		}
	}
	for id, m := range p.Milestones {
		g["milestone:"+id] = append(g["milestone:"+id], m.DependsOn...)
	}
	return g
}

func findCycle(p *Plan) []string {
	g := edges(p)
	keys := make([]string, 0, len(g))
	for k := range g {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	state := map[string]int{} // 1 visiting, 2 done
	var stack []string
	var cycle []string
	var visit func(string) bool
	visit = func(n string) bool {
		state[n] = 1
		stack = append(stack, n)
		for _, m := range g[n] {
			if state[m] == 1 {
				for i, s := range stack {
					if s == m {
						cycle = append(append([]string{}, stack[i:]...), m)
					}
				}
				return true
			}
			if state[m] == 0 && visit(m) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = 2
		return false
	}
	for _, k := range keys {
		if state[k] == 0 && visit(k) {
			return cycle
		}
	}
	return nil
}

// MergePatch applies an RFC 7396 merge patch: objects merge key by key,
// null deletes, anything else replaces.
func MergePatch(doc, patch []byte) ([]byte, error) {
	var d, p any
	if len(doc) > 0 {
		if err := json.Unmarshal(doc, &d); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(patch, &p); err != nil {
		return nil, fmt.Errorf("patch is not JSON: %w", err)
	}
	return json.Marshal(merge(d, p))
}

func merge(doc, patch any) any {
	pm, ok := patch.(map[string]any)
	if !ok {
		return patch
	}
	dm, ok := doc.(map[string]any)
	if !ok {
		dm = map[string]any{}
	}
	for k, v := range pm {
		if v == nil {
			delete(dm, k)
			continue
		}
		dm[k] = merge(dm[k], v)
	}
	return dm
}
