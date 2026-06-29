package goals

import (
	"sort"
	"strings"
)

// Horizon is the time window a goal sits under inside an area.
type Horizon string

const (
	H90   Horizon = "90-day"
	H30   Horizon = "30-day"
	HNone Horizon = ""
)

// Goal is a single task line under an area, with parsed inline fields.
type Goal struct {
	ID      string
	Text    string
	Checked bool
	Owner   string // "", "me", "team", or a name; "" resolves to "me"
	Due     string // "" or YYYY-MM-DD
	Horizon Horizon
	Fields  []Field // all inline fields in source order (owner/due/goal + unknown)
}

// ResolvedOwner returns the effective owner ("me" when unset).
func (g *Goal) ResolvedOwner() string {
	if g.Owner == "" {
		return "me"
	}
	return g.Owner
}

func (g *Goal) ownerIsMe() bool { return strings.EqualFold(g.ResolvedOwner(), "me") }

// Area is a "## " section: a North Star plus 90-day / 30-day / loose goals.
type Area struct {
	Name      string
	NorthStar string // text after "> North Star:"; "" when absent
	Goals90   []*Goal
	Goals30   []*Goal
	Loose     []*Goal // goals directly under the area (no horizon), e.g. Sidequests

	has90      bool     // a "### 90-day" section exists (even if empty)
	has30      bool     // a "### 30-day" section exists (even if empty)
	headingRaw string   // original "## ..." line (unused once normalized)
	nsRaw      string   // original "> ..." line
	extra      []string // unrecognized lines within the area, preserved verbatim
}

func (a *Area) allGoals() []*Goal {
	out := make([]*Goal, 0, len(a.Goals90)+len(a.Goals30)+len(a.Loose))
	out = append(out, a.Goals90...)
	out = append(out, a.Goals30...)
	out = append(out, a.Loose...)
	return out
}

func (a *Area) bucket(h Horizon) *[]*Goal {
	switch h {
	case H90:
		return &a.Goals90
	case H30:
		return &a.Goals30
	default:
		return &a.Loose
	}
}

func (a *Area) removeGoal(g *Goal) {
	for _, bp := range []*[]*Goal{&a.Goals90, &a.Goals30, &a.Loose} {
		b := *bp
		for i, x := range b {
			if x == g {
				*bp = append(b[:i], b[i+1:]...)
				return
			}
		}
	}
}

// Doc is the parsed goals.md: a verbatim preamble (through "# Goals") + areas.
type Doc struct {
	preamble string
	Areas    []*Area
}

func (d *Doc) FindArea(name string) *Area {
	for _, a := range d.Areas {
		if a.Name == name {
			return a
		}
	}
	return nil
}

func (d *Doc) FindGoal(id string) (*Area, *Goal) {
	for _, a := range d.Areas {
		for _, g := range a.allGoals() {
			if g.ID == id {
				return a, g
			}
		}
	}
	return nil, nil
}

// ----- mutations (all leave the doc in a serializable state) -----

func (d *Doc) AddArea(name string) *Area {
	name = strings.TrimSpace(name)
	if a := d.FindArea(name); a != nil {
		return a
	}
	a := &Area{Name: name, has90: true, has30: true}
	d.Areas = append(d.Areas, a)
	return a
}

func (d *Doc) RenameArea(old, neu string) bool {
	a := d.FindArea(old)
	if a == nil {
		return false
	}
	a.Name = strings.TrimSpace(neu)
	return true
}

func (d *Doc) SetNorthStar(area, text string) bool {
	a := d.FindArea(area)
	if a == nil {
		return false
	}
	a.NorthStar = strings.TrimSpace(text)
	return true
}

func (d *Doc) DeleteArea(name string) bool {
	for i, a := range d.Areas {
		if a.Name == name {
			d.Areas = append(d.Areas[:i], d.Areas[i+1:]...)
			return true
		}
	}
	return false
}

func (d *Doc) ReorderAreas(order []string) {
	byName := map[string]*Area{}
	for _, a := range d.Areas {
		byName[a.Name] = a
	}
	var out []*Area
	seen := map[string]bool{}
	for _, n := range order {
		if a := byName[n]; a != nil && !seen[n] {
			out = append(out, a)
			seen[n] = true
		}
	}
	for _, a := range d.Areas {
		if !seen[a.Name] {
			out = append(out, a)
		}
	}
	d.Areas = out
}

func (d *Doc) AddGoal(area string, h Horizon, text, owner, due string) (*Goal, bool) {
	a := d.FindArea(area)
	if a == nil {
		return nil, false
	}
	g := &Goal{Text: strings.TrimSpace(text), Owner: strings.TrimSpace(owner), Due: normalizeDue(due), Horizon: h}
	switch h {
	case H90:
		a.has90 = true
	case H30:
		a.has30 = true
	}
	b := a.bucket(h)
	*b = append(*b, g)
	return g, true
}

// GoalEdit carries optional field updates; nil fields are left unchanged.
type GoalEdit struct {
	Text    *string
	Owner   *string
	Due     *string
	Horizon *Horizon
}

func (d *Doc) EditGoal(id string, e GoalEdit) bool {
	a, g := d.FindGoal(id)
	if g == nil {
		return false
	}
	if e.Text != nil {
		g.Text = strings.TrimSpace(*e.Text)
	}
	if e.Owner != nil {
		g.Owner = strings.TrimSpace(*e.Owner)
	}
	if e.Due != nil {
		g.Due = normalizeDue(*e.Due)
	}
	if e.Horizon != nil && *e.Horizon != g.Horizon {
		a.removeGoal(g)
		g.Horizon = *e.Horizon
		switch *e.Horizon {
		case H90:
			a.has90 = true
		case H30:
			a.has30 = true
		}
		b := a.bucket(*e.Horizon)
		*b = append(*b, g)
	}
	return true
}

func (d *Doc) CheckGoal(id string, checked bool) bool {
	_, g := d.FindGoal(id)
	if g == nil {
		return false
	}
	g.Checked = checked
	return true
}

func (d *Doc) DeleteGoal(id string) bool {
	a, g := d.FindGoal(id)
	if g == nil {
		return false
	}
	a.removeGoal(g)
	return true
}

func (d *Doc) ReorderGoals(area string, h Horizon, ids []string) bool {
	a := d.FindArea(area)
	if a == nil {
		return false
	}
	b := a.bucket(h)
	byID := map[string]*Goal{}
	for _, g := range *b {
		byID[g.ID] = g
	}
	var out []*Goal
	seen := map[string]bool{}
	for _, id := range ids {
		if g := byID[id]; g != nil && !seen[id] {
			out = append(out, g)
			seen[id] = true
		}
	}
	for _, g := range *b {
		if !seen[g.ID] {
			out = append(out, g)
		}
	}
	*b = out
	return true
}

// ----- projections (JSON for the API / daily panels) -----

type DocView struct {
	Areas []AreaView `json:"areas"`
}

type AreaView struct {
	Name      string     `json:"name"`
	NorthStar string     `json:"northStar"`
	Goals90   []GoalView `json:"goals90"`
	Goals30   []GoalView `json:"goals30"`
	Loose     []GoalView `json:"loose"`
}

type GoalView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Owner   string `json:"owner"`
	Due     string `json:"due"`
	Horizon string `json:"horizon"`
}

func (d *Doc) View() DocView {
	d.assignIDs()
	areas := make([]AreaView, 0, len(d.Areas))
	for _, a := range d.Areas {
		areas = append(areas, AreaView{
			Name:      a.Name,
			NorthStar: a.NorthStar,
			Goals90:   goalViews(a.Goals90),
			Goals30:   goalViews(a.Goals30),
			Loose:     goalViews(a.Loose),
		})
	}
	return DocView{Areas: areas}
}

func goalViews(gs []*Goal) []GoalView {
	out := make([]GoalView, 0, len(gs))
	for _, g := range gs {
		out = append(out, GoalView{
			ID: g.ID, Text: g.Text, Checked: g.Checked,
			Owner: g.ResolvedOwner(), Due: g.Due, Horizon: string(g.Horizon),
		})
	}
	return out
}

// ----- My Plate -----

type PlateItem struct {
	Source  string `json:"source"`
	Area    string `json:"area"`
	Horizon string `json:"horizon"`
	GoalID  string `json:"goalId,omitempty"`
	Text    string `json:"text"`
	Due     string `json:"due,omitempty"`
}

type PlateGroup struct {
	Area  string      `json:"area"`
	Items []PlateItem `json:"items"`
}

// MyPlate returns all open, owner==me goals grouped by area, sorted by due date
// (undated items last).
func (d *Doc) MyPlate() []PlateGroup {
	d.assignIDs()
	var groups []PlateGroup
	for _, a := range d.Areas {
		var items []PlateItem
		for _, g := range a.allGoals() {
			if g.Checked || !g.ownerIsMe() {
				continue
			}
			items = append(items, PlateItem{
				Source: "goal", Area: a.Name, Horizon: string(g.Horizon),
				GoalID: g.ID, Text: g.Text, Due: g.Due,
			})
		}
		sort.SliceStable(items, func(i, j int) bool { return dueLess(items[i].Due, items[j].Due) })
		if len(items) > 0 {
			groups = append(groups, PlateGroup{Area: a.Name, Items: items})
		}
	}
	return groups
}

func dueLess(a, b string) bool {
	switch {
	case a == "":
		return false
	case b == "":
		return true
	default:
		return a < b
	}
}

// HorizonTextsForMe returns the text of open, owner==me goals in a horizon,
// across all areas — used to fill the read-only daily Goals/Milestones panels.
func (d *Doc) HorizonTextsForMe(h Horizon) []string {
	var out []string
	for _, a := range d.Areas {
		var gs []*Goal
		switch h {
		case H90:
			gs = a.Goals90
		case H30:
			gs = a.Goals30
		}
		for _, g := range gs {
			if !g.Checked && g.ownerIsMe() {
				out = append(out, g.Text)
			}
		}
	}
	return out
}
