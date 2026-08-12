package server

import (
	"strings"

	"manifest/aion"
	"manifest/daily"
	"manifest/goals"
	"manifest/todos"
)

// goalsAdapter bridges the goals store to daily.GoalsProvider, resolving a picked
// Rock slug into its text, its stages (to choose among), the selected stage
// (defaulting to the current stage — the first unchecked one), and the Rock's
// open tasks from the TASK SUBSTRATE — to-do.md tethers, plus (for aion/ rocks)
// the owner's open aion backlog tasks — goals.md holds no tasks (task-substrate
// split).
type goalsAdapter struct {
	store *goals.Store
	todos *todos.Store // nilable
	aion  *aion.Store  // nilable — aion/ rocks offer backlog tasks
	re    *aion.Store  // nilable — ooda-group/ rocks offer RE backlog tasks
	owner string       // initials that mean "me" (backlog tasks are owner-filtered)
}

// NewGoalsAdapter wires the goals store into the daily service's Focus resolution.
func NewGoalsAdapter(store *goals.Store, td *todos.Store, ai, re *aion.Store, owner string) daily.GoalsProvider {
	return goalsAdapter{store, td, ai, re, owner}
}

func (a goalsAdapter) ResolveFocus(id, milestoneID string) (daily.FocusResolution, bool) {
	_, g := a.store.Load().FindGoal(id)
	if g == nil {
		return daily.FocusResolution{}, false
	}
	res := daily.FocusResolution{Text: g.Text}
	if len(g.Children) == 0 {
		return res, true
	}
	// The stages the picker offers.
	for _, c := range g.Children {
		res.Milestones = append(res.Milestones, daily.FocusNode{GoalID: c.ID, Text: c.Text, Checked: c.Checked})
	}
	// Selected stage: the requested one, else the current stage (first unchecked),
	// else the first stage.
	sel := g.Children[0]
	for _, c := range g.Children {
		if !c.Checked {
			sel = c
			break
		}
	}
	for _, c := range g.Children {
		if c.ID == milestoneID {
			sel = c
			break
		}
	}
	res.Milestone = &daily.FocusNode{GoalID: sel.ID, Text: sel.Text, Checked: sel.Checked}
	// "From your focus" offers = the Rock's open tethered todos (TodoID carried
	// in GoalID's slot via the dedicated field below).
	if a.todos != nil {
		if doc, err := a.todos.Load(); err == nil {
			for _, dom := range doc.Domains {
				dom.AllTodos(func(_ *todos.Bucket, t *todos.Todo) {
					if t.Rock == g.ID && !t.Checked {
						res.Tasks = append(res.Tasks, daily.FocusNode{TodoID: t.ID, Text: t.Text})
					}
				})
			}
		}
	}
	// domain rocks: their tasks live in a DOMAIN backlog, not to-do.md — offer
	// MY open backlog tasks under this rock (<domain>:<id> pulls/syncs like a
	// todo). aion/ → the aion backlog; ooda-group/ → the real-estate backlog.
	for _, d := range []struct {
		st         *aion.Store
		rockPrefix string
		idPrefix   string
	}{{a.aion, "aion/", "aion:"}, {a.re, "ooda-group/", "re:"}} {
		if d.st == nil || !strings.HasPrefix(g.ID, d.rockPrefix) {
			continue
		}
		for _, it := range d.st.LoadBacklog().Items() {
			if it.Kind != aion.KindTask || it.Checked || it.Rock != g.ID || !a.mine(it.Owner) {
				continue
			}
			if it.Status != aion.StatusOpen && it.Status != aion.StatusInProgress && it.Status != "" {
				continue
			}
			res.Tasks = append(res.Tasks, daily.FocusNode{TodoID: d.idPrefix + it.ID, Text: it.Text})
		}
	}
	return res, true
}

// mine mirrors Server.isMine: empty / "me" / containing my initials.
func (a goalsAdapter) mine(owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" || strings.EqualFold(owner, "me") {
		return true
	}
	me := strings.ToUpper(strings.TrimSpace(a.owner))
	if me == "" {
		return false
	}
	for _, tok := range strings.Split(owner, "/") {
		if strings.ToUpper(strings.TrimSpace(tok)) == me {
			return true
		}
	}
	return false
}
