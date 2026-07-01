package goals

import (
	"sort"
	"strings"
)

// Goal is one checkbox line in goals.md. In the cascade a goal nests: an area's
// root goals are 90-day goals; a 90-day's Children are its 30-day goal(s)
// (exactly one expected); a 30-day's Children are tasks. Depth carries the tier.
type Goal struct {
	ID       string
	Text     string
	Checked  bool
	Owner    string // "", "me", "team", or a name; "" resolves to "me"
	Due      string // "" or YYYY-MM-DD
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

// Area is a "## " section: a North Star plus the 90-day goal roots (the cascade).
type Area struct {
	Name      string
	NorthStar string // text after "> North Star:"; "" when absent

	Goals []*Goal // 90-day roots; each owns its 30-day children, which own tasks

	has90 bool     // a "### 90-day" section exists (even if empty)
	nsRaw string   // original "> ..." line
	extra []string // unrecognized lines within the area, preserved verbatim
}

// allGoals returns every goal in the area depth-first (root, its children, …).
func (a *Area) allGoals() []*Goal {
	var out []*Goal
	var rec func(gs []*Goal)
	rec = func(gs []*Goal) {
		for _, g := range gs {
			out = append(out, g)
			rec(g.Children)
		}
	}
	rec(a.Goals)
	return out
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

// FindGoal locates a goal anywhere in the cascade by id, returning its area.
func (d *Doc) FindGoal(id string) (*Area, *Goal) {
	for _, a := range d.Areas {
		if _, g := findIn(nil, a.Goals, id); g != nil {
			return a, g
		}
	}
	return nil, nil
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
		if cp, g := containerIn(&a.Goals, id); g != nil {
			return a, cp, g
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
	a := &Area{Name: name, has90: true}
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

// AddGoal adds a goal to the cascade. When parentID is "", the goal becomes a new
// 90-day root in the named area; otherwise it is appended as a child of parentID
// (a 30-day under a 90-day, or a task under a 30-day — tier is structural).
func (d *Doc) AddGoal(area, parentID, text, owner, due string) (*Goal, bool) {
	g := &Goal{Text: strings.TrimSpace(text), Owner: strings.TrimSpace(owner), Due: normalizeDue(due)}
	if parentID == "" {
		a := d.FindArea(area)
		if a == nil {
			return nil, false
		}
		a.has90 = true
		a.Goals = append(a.Goals, g)
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
	Text  *string
	Owner *string
	Due   *string
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
	if e.Due != nil {
		g.Due = normalizeDue(*e.Due)
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

// ReorderGoals reorders siblings: roots of an area when parentID is "", otherwise
// the children of parentID.
func (d *Doc) ReorderGoals(area, parentID string, ids []string) bool {
	var cp *[]*Goal
	if parentID == "" {
		a := d.FindArea(area)
		if a == nil {
			return false
		}
		cp = &a.Goals
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
	Goals     []GoalView `json:"goals"`
}

type GoalView struct {
	ID       string     `json:"id"`
	Text     string     `json:"text"`
	Checked  bool       `json:"checked"`
	Owner    string     `json:"owner"`
	Due      string     `json:"due"`
	Children []GoalView `json:"children,omitempty"`
}

func (d *Doc) View() DocView {
	d.assignIDs()
	areas := make([]AreaView, 0, len(d.Areas))
	for _, a := range d.Areas {
		areas = append(areas, AreaView{
			Name:      a.Name,
			NorthStar: a.NorthStar,
			Goals:     goalViews(a.Goals),
		})
	}
	return DocView{Areas: areas}
}

func goalViews(gs []*Goal) []GoalView {
	out := make([]GoalView, 0, len(gs))
	for _, g := range gs {
		out = append(out, GoalView{
			ID: g.ID, Text: g.Text, Checked: g.Checked,
			Owner: g.ResolvedOwner(), Due: g.Due,
			Children: goalViews(g.Children),
		})
	}
	return out
}

// ----- My Plate (open, owner==me items across the whole cascade) -----

type PlateItem struct {
	Source string `json:"source"`
	Area   string `json:"area"`
	GoalID string `json:"goalId,omitempty"`
	Text   string `json:"text"`
	Due    string `json:"due,omitempty"`
}

type PlateGroup struct {
	Area  string      `json:"area"`
	Items []PlateItem `json:"items"`
}

// MyPlate returns all open, owner==me goals (every tier of the cascade) grouped
// by area, sorted by due date (undated last).
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
				Source: "goal", Area: a.Name,
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

// Pool returns the open, owner==me 30-day goals (the cascade's second tier) with
// ids — offered for quick-add when planning an unplanned future day.
func (d *Doc) Pool() []PlateItem {
	d.assignIDs()
	var items []PlateItem
	for _, a := range d.Areas {
		for _, root := range a.Goals {
			for _, m := range root.Children { // 30-day goals
				if m.Checked || !m.ownerIsMe() {
					continue
				}
				items = append(items, PlateItem{
					Source: "goal", Area: a.Name,
					GoalID: m.ID, Text: m.Text, Due: m.Due,
				})
			}
		}
	}
	return items
}
