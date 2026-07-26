package server

import (
	"manifest/daily"
	"manifest/goals"
	"manifest/todos"
)

// goalsAdapter bridges the goals store to daily.GoalsProvider, resolving a picked
// Rock slug into its text, its stages (to choose among), the selected stage
// (defaulting to the current stage — the first unchecked one), and the Rock's
// open tethered todos from the TASK SUBSTRATE (to do.md) — goals.md holds no
// tasks (task-substrate split).
type goalsAdapter struct {
	store *goals.Store
	todos *todos.Store // nilable
}

// NewGoalsAdapter wires the goals store into the daily service's Focus resolution.
func NewGoalsAdapter(store *goals.Store, td *todos.Store) daily.GoalsProvider {
	return goalsAdapter{store, td}
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
	return res, true
}
