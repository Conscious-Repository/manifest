package goals

import (
	"sort"
	"strings"
)

// Goal is one checkbox line in goals.md. Its role comes from where it sits:
// an area's Annuals are 1-year goals; its Rocks are 90-day priorities; a Rock's
// Children are stages (the growing trail) and a stage's Children are tasks. Depth
// under a Rock carries the role — one level under a Rock is a stage, two is a task
// (§1 literal depth rule).
type Goal struct {
	ID      string
	Text    string
	Checked bool
	Owner   string // "", "me", "team", or a name; "" resolves to "me"

	// Rock-only metadata (empty on annuals, stages, tasks).
	Quarter    string // "2026-Q3"; set at creation, updated on carry
	Serves     string // annual slug this Rock serves; "" = needs setup
	Status     string // "" (active) | "blocked" | "at-risk"
	RolledFrom string // "2026-Q2" when carried across a quarter
	Moved      string // last-movement date (YYYY-MM-DD); stamped when work lands beneath it

	Fields   []Field
	Children []*Goal
}

// ResolvedOwner returns the effective owner ("me" when unset).
func (g *Goal) ResolvedOwner() string {
	if g.Owner == "" {
		return "me"
	}
	return g.Owner
}

func (g *Goal) ownerIsMe() bool { return strings.EqualFold(g.ResolvedOwner(), "me") }

// currentStage returns a Rock's first unchecked stage (the trail's live tip), or
// nil when every stage is done or there are none.
func (g *Goal) currentStage() *Goal {
	for _, st := range g.Children {
		if !st.Checked {
			return st
		}
	}
	return nil
}

// Area is a "## " section: a North Star, a 1-year (annual) section, and the Rocks
// (90-day) section that ladders up to it.
type Area struct {
	Name      string
	NorthStar string // text after "> North Star:"; "" when absent

	Annuals []*Goal // under "### 1-year" — annual objectives
	Rocks   []*Goal // under "### Rocks (90-day)" — each owns stages, which own tasks

	yearLabel string   // the "— 2026" suffix on the 1-year heading; "" when absent
	hasAnnual bool     // a "### 1-year" section exists (even if empty)
	hasRocks  bool     // a "### Rocks (90-day)" section exists (even if empty)
	nsRaw     string   // original "> ..." line
	extra     []string // unrecognized lines within the area, preserved verbatim
}

// allGoals returns every goal in the area depth-first across both sections.
func (a *Area) allGoals() []*Goal {
	var out []*Goal
	var rec func(gs []*Goal)
	rec = func(gs []*Goal) {
		for _, g := range gs {
			out = append(out, g)
			rec(g.Children)
		}
	}
	rec(a.Annuals)
	rec(a.Rocks)
	return out
}

// roots returns pointers to the area's two top-level lists, so tree walks can
// cover both without duplicating logic.
func (a *Area) roots() []*[]*Goal { return []*[]*Goal{&a.Annuals, &a.Rocks} }

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

// FindGoal locates a goal anywhere (annual or rock subtree) by id, with its area.
func (d *Doc) FindGoal(id string) (*Area, *Goal) {
	for _, a := range d.Areas {
		for _, root := range a.roots() {
			if _, g := findIn(nil, *root, id); g != nil {
				return a, g
			}
		}
	}
	return nil, nil
}

// RockOf returns the top-level Rock whose subtree contains id (or the Rock itself),
// or nil. Used to stamp last-movement on a Rock when a check/add lands beneath it.
func (d *Doc) RockOf(id string) *Goal {
	for _, a := range d.Areas {
		for _, rock := range a.Rocks {
			if rock.ID == id || subtreeContains(rock, id) {
				return rock
			}
		}
	}
	return nil
}

func subtreeContains(g *Goal, id string) bool {
	for _, c := range g.Children {
		if c.ID == id || subtreeContains(c, id) {
			return true
		}
	}
	return false
}

func findIn(parent *Goal, gs []*Goal, id string) (*Goal, *Goal) {
	for _, g := range gs {
		if g.ID == id {
			return parent, g
		}
		if p, found := findIn(g, g.Children, id); found != nil {
			return p, found
		}
	}
	return nil, nil
}

// container returns the slice that directly holds the goal with id (and its area),
// so callers can append/remove/reorder siblings.
func (d *Doc) container(id string) (*Area, *[]*Goal, *Goal) {
	for _, a := range d.Areas {
		for _, root := range a.roots() {
			if cp, g := containerIn(root, id); g != nil {
				return a, cp, g
			}
		}
	}
	return nil, nil, nil
}

func containerIn(cp *[]*Goal, id string) (*[]*Goal, *Goal) {
	for i := range *cp {
		g := (*cp)[i]
		if g.ID == id {
			return cp, g
		}
		if c, found := containerIn(&g.Children, id); found != nil {
			return c, found
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
	a := &Area{Name: name, hasAnnual: true, hasRocks: true}
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

// AddGoal adds a goal. With parentID == "", section decides the root list:
// "annual" appends a 1-year goal, anything else appends a Rock. With parentID set,
// the goal is appended as a child (a stage under a Rock, or a task under a stage).
func (d *Doc) AddGoal(area, parentID, section, text, owner string) (*Goal, bool) {
	g := &Goal{Text: strings.TrimSpace(text), Owner: strings.TrimSpace(owner)}
	if parentID == "" {
		a := d.FindArea(area)
		if a == nil {
			return nil, false
		}
		if strings.EqualFold(section, "annual") {
			a.hasAnnual = true
			a.Annuals = append(a.Annuals, g)
		} else {
			a.hasRocks = true
			a.Rocks = append(a.Rocks, g)
		}
		d.assignIDs()
		return g, true
	}
	_, parent := d.FindGoal(parentID)
	if parent == nil {
		return nil, false
	}
	parent.Children = append(parent.Children, g)
	d.assignIDs()
	return g, true
}

// GoalEdit carries optional field updates; nil fields are left unchanged.
type GoalEdit struct {
	Text    *string
	Owner   *string
	Quarter *string
	Serves  *string
	Status  *string
}

func (d *Doc) EditGoal(id string, e GoalEdit) bool {
	_, g := d.FindGoal(id)
	if g == nil {
		return false
	}
	if e.Text != nil {
		g.Text = strings.TrimSpace(*e.Text)
	}
	if e.Owner != nil {
		g.Owner = strings.TrimSpace(*e.Owner)
	}
	if e.Quarter != nil {
		g.Quarter = strings.TrimSpace(*e.Quarter)
	}
	if e.Serves != nil {
		g.Serves = strings.TrimSpace(*e.Serves)
	}
	if e.Status != nil {
		g.Status = strings.TrimSpace(*e.Status)
	}
	d.assignIDs()
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
	_, cp, g := d.container(id)
	if g == nil {
		return false
	}
	for i, x := range *cp {
		if x == g {
			*cp = append((*cp)[:i], (*cp)[i+1:]...)
			return true
		}
	}
	return false
}

// ReorderGoals reorders siblings: with parentID == "", the area's Rocks (or its
// Annuals when section == "annual"); otherwise the children of parentID.
func (d *Doc) ReorderGoals(area, parentID, section string, ids []string) bool {
	var cp *[]*Goal
	if parentID == "" {
		a := d.FindArea(area)
		if a == nil {
			return false
		}
		if strings.EqualFold(section, "annual") {
			cp = &a.Annuals
		} else {
			cp = &a.Rocks
		}
	} else {
		_, parent := d.FindGoal(parentID)
		if parent == nil {
			return false
		}
		cp = &parent.Children
	}
	byID := map[string]*Goal{}
	for _, g := range *cp {
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
	for _, g := range *cp {
		if !seen[g.ID] {
			out = append(out, g)
		}
	}
	*cp = out
	return true
}

// ----- projections (JSON for the API) -----

type DocView struct {
	Areas []AreaView `json:"areas"`
}

type AreaView struct {
	Name      string     `json:"name"`
	NorthStar string     `json:"northStar"`
	Year      string     `json:"year"`
	Annuals   []GoalView `json:"annuals"`
	Rocks     []GoalView `json:"rocks"`
}

// GoalView backs both annuals and Rocks. Rock-only fields (quarter/serves/status)
// are empty for annuals, stages and tasks and omitted from the JSON.
type GoalView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Owner   string `json:"owner"`
	Quarter string `json:"quarter,omitempty"`
	Serves  string `json:"serves,omitempty"`
	Status  string `json:"status,omitempty"`
	Moved   string `json:"moved,omitempty"`
	// Annual roll-up (§2): serving-Rock counts, filled by the server from goals.md +
	// the current year's archives. Zero on Rocks/stages/tasks.
	RollupActive int        `json:"rollupActive,omitempty"`
	RollupWon    int        `json:"rollupWon,omitempty"`
	RollupLearn  int        `json:"rollupLearn,omitempty"`
	Children     []GoalView `json:"children,omitempty"`
}

func (d *Doc) View() DocView {
	d.assignIDs()
	areas := make([]AreaView, 0, len(d.Areas))
	for _, a := range d.Areas {
		areas = append(areas, AreaView{
			Name:      a.Name,
			NorthStar: a.NorthStar,
			Year:      a.yearLabel,
			Annuals:   goalViews(a.Annuals),
			Rocks:     goalViews(a.Rocks),
		})
	}
	return DocView{Areas: areas}
}

func goalViews(gs []*Goal) []GoalView {
	out := make([]GoalView, 0, len(gs))
	for _, g := range gs {
		out = append(out, GoalView{
			ID: g.ID, Text: g.Text, Checked: g.Checked,
			Owner: g.ResolvedOwner(),
			Quarter: g.Quarter, Serves: g.Serves, Status: g.Status, Moved: g.Moved,
			Children: goalViews(g.Children),
		})
	}
	return out
}

// ----- My Plate (open, owner==me items across the whole ladder) -----

type PlateItem struct {
	Source string `json:"source"`
	Area   string `json:"area"`
	GoalID string `json:"goalId,omitempty"`
	Text   string `json:"text"`
}

type PlateGroup struct {
	Area  string      `json:"area"`
	Items []PlateItem `json:"items"`
}

// MyPlate returns all open, owner==me goals (annuals, Rocks, stages, tasks) grouped
// by area.
func (d *Doc) MyPlate() []PlateGroup {
	d.assignIDs()
	var groups []PlateGroup
	for _, a := range d.Areas {
		var items []PlateItem
		for _, g := range a.allGoals() {
			if g.Checked || !g.ownerIsMe() {
				continue
			}
			items = append(items, PlateItem{Source: "goal", Area: a.Name, GoalID: g.ID, Text: g.Text})
		}
		if len(items) > 0 {
			groups = append(groups, PlateGroup{Area: a.Name, Items: items})
		}
	}
	return groups
}

// Pool returns the open, owner==me stages (each Rock's current tier) with ids —
// offered for quick-add when planning an unplanned future day.
func (d *Doc) Pool() []PlateItem {
	d.assignIDs()
	var items []PlateItem
	for _, a := range d.Areas {
		for _, rock := range a.Rocks {
			for _, st := range rock.Children { // stages
				if st.Checked || !st.ownerIsMe() {
					continue
				}
				items = append(items, PlateItem{Source: "goal", Area: a.Name, GoalID: st.ID, Text: st.Text})
			}
		}
	}
	items = sortStable(items)
	return items
}

// sortStable keeps Pool output deterministic (by area already grouped, then text).
func sortStable(items []PlateItem) []PlateItem {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Text < items[j].Text })
	return items
}
