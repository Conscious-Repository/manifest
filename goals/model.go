package goals

import (
	"sort"
	"strings"

	"manifest/record"
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
	Quarter    string   // "2026-Q3"; set at creation, updated on carry
	Start      string   // ISO YYYY-MM-DD; explicit timeline start (portal §7). Emitted for rocks.
	Due        string   // ISO YYYY-MM-DD; explicit timeline end (portal §7). Emitted for rocks.
	Serves     []string // annual slugs this Rock serves (1:many); empty = needs setup
	Aliases    []string // portal-matcher vocabulary that resolves to this goal (exported)
	Status     string   // "" (active) | "blocked" | "at-risk"
	RolledFrom string   // "2026-Q2" when carried across a quarter
	Moved      string   // last-movement date (YYYY-MM-DD); stamped when work lands beneath it

	Fields   []Field
	Children []*Goal
	// Frozen: verbatim task-history lines under a STAGE (task-substrate split —
	// goals.md holds no live tasks; pre-split checked lines are preserved
	// byte-identical, render muted, and archive with their Rock on close).
	Frozen []string
}

// ResolvedOwner returns the effective owner ("me" when unset).
func (g *Goal) ResolvedOwner() string {
	if g.Owner == "" {
		return "me"
	}
	return g.Owner
}

func (g *Goal) ownerIsMe() bool { return record.OwnerIsMe(g.ResolvedOwner()) }

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
	// The area slug is the id PREFIX of every derived id beneath it — pin
	// every unpinned goal first so identity never re-derives out from under
	// the references (the kernel identity rule; tasks.Bucket.Rename precedent).
	var pinAll func(gs []*Goal)
	pinAll = func(gs []*Goal) {
		for _, g := range gs {
			g.pin()
			pinAll(g.Children)
		}
	}
	pinAll(a.Annuals)
	pinAll(a.Rocks)
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
	Start   *string
	Due     *string
	Serves  *[]string // full replacement list (1:many)
	Aliases *[]string // full replacement list (portal-matcher vocabulary)
	Status  *string
}

// stripBracket removes `]` so a value can never break the [key:: value] regex.
func stripBracket(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "]", "") }

// dedupeNonEmpty strips brackets, drops blanks, and de-duplicates (order
// preserved) — the full-replace shape for Serves/Aliases lists.
func dedupeNonEmpty(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range in {
		v = stripBracket(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// pin freezes the goal's CURRENT identity as an explicit [goal:: id] field
// (idempotent). References store ids — to do.md [rock::], daily [goal::],
// serves chains, the portal — so a pinned id makes any later rename safe.
func (g *Goal) pin() {
	if g.explicitID() == "" && g.ID != "" {
		g.Fields = append(g.Fields, Field{Key: "goal", Value: g.ID})
	}
}

// isTopLevel reports whether g is a rock or annual (vs a stage/task) — the
// roles whose old-name slugs join the alias vocabulary on rename.
func (d *Doc) isTopLevel(g *Goal) bool {
	for _, a := range d.Areas {
		for _, an := range a.Annuals {
			if an == g {
				return true
			}
		}
		for _, r := range a.Rocks {
			if r == g {
				return true
			}
		}
	}
	return false
}

func (d *Doc) EditGoal(id string, e GoalEdit) bool {
	_, g := d.FindGoal(id)
	if g == nil {
		return false
	}
	if e.Text != nil {
		neu := strings.TrimSpace(*e.Text)
		if neu != g.Text && neu != "" {
			// pin-before-rename: the id must never move under a reference
			g.pin()
			// rocks/annuals: the outgoing name's slug joins [alias::] so
			// portal-boundary resolvers keep matching hand-written refs that
			// used it (skipped when it already equals the id tail, which the
			// resolvers cover, or an existing alias)
			if d.isTopLevel(g) {
				oldSlug := slug(g.Text)
				tail := g.identity()
				if i := strings.LastIndex(tail, "/"); i >= 0 {
					tail = tail[i+1:]
				}
				dup := oldSlug == "" || oldSlug == tail || oldSlug == slug(neu)
				for _, al := range g.Aliases {
					if strings.EqualFold(slug(al), oldSlug) {
						dup = true
					}
				}
				if !dup {
					g.Aliases = append(g.Aliases, oldSlug)
				}
			}
		}
		g.Text = neu
	}
	if e.Owner != nil {
		g.Owner = strings.TrimSpace(*e.Owner)
	}
	if e.Quarter != nil {
		g.Quarter = strings.TrimSpace(*e.Quarter)
	}
	if e.Start != nil {
		g.Start = stripBracket(*e.Start)
	}
	if e.Due != nil {
		g.Due = stripBracket(*e.Due)
	}
	if e.Serves != nil {
		g.Serves = dedupeNonEmpty(*e.Serves)
	}
	if e.Aliases != nil {
		g.Aliases = dedupeNonEmpty(*e.Aliases)
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

// MoveGoal re-parents a goal (with its subtree) inside its own area — the
// ladder-connection edit: an orphan Rock nests under another Rock (becoming
// its stage / 30-day item), a stage moves between Rocks, or — with
// parentID "" — a stage is promoted to a top-level Rock. The target must be
// a TOP-LEVEL Rock (depth stays rock → stage; anything deeper is frozen
// history by the parse contract). Same-area only; cycles refused.
func (d *Doc) MoveGoal(id, parentID string) bool {
	if id == "" || id == parentID {
		return false
	}
	area, cp, g := d.container(id)
	if g == nil {
		return false
	}
	detach := func() {
		for i, x := range *cp {
			if x == g {
				*cp = append((*cp)[:i], (*cp)[i+1:]...)
				return
			}
		}
	}
	if parentID == "" {
		// promote to a top-level Rock in the same area
		for _, r := range area.Rocks {
			if r == g {
				return false // already top-level
			}
		}
		detach()
		area.Rocks = append(area.Rocks, g)
		area.hasRocks = true
		return true
	}
	tArea, target := d.FindGoal(parentID)
	if target == nil || tArea != area || target == g || subtreeContains(g, parentID) {
		return false
	}
	if rock := d.RockOf(parentID); rock == nil || rock.ID != parentID {
		return false // only a top-level Rock may gain children
	}
	detach()
	target.Children = append(target.Children, g)
	return true
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
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Checked bool     `json:"checked"`
	Owner   string   `json:"owner"`
	Quarter string   `json:"quarter,omitempty"`
	Start   string   `json:"start,omitempty"`
	Due     string   `json:"due,omitempty"`
	Serves  []string `json:"serves,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	Status  string   `json:"status,omitempty"`
	Moved   string   `json:"moved,omitempty"`
	// Annual roll-up (§2): serving-Rock counts, filled by the server from goals.md +
	// the current year's archives. Zero on Rocks/stages/tasks.
	RollupActive int        `json:"rollupActive,omitempty"`
	RollupWon    int        `json:"rollupWon,omitempty"`
	RollupLearn  int        `json:"rollupLearn,omitempty"`
	Children     []GoalView `json:"children,omitempty"`
	Frozen       []string   `json:"frozen,omitempty"` // muted task history (verbatim lines)
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
			Owner:   g.ResolvedOwner(),
			Quarter: g.Quarter, Start: g.Start, Due: g.Due, Serves: g.Serves, Aliases: g.Aliases, Status: g.Status, Moved: g.Moved,
			Children: goalViews(g.Children), Frozen: g.Frozen,
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
