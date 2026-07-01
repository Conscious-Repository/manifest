package server

import (
	"manifest/daily"
	"manifest/goals"
)

// goalsAdapter bridges the goals store to daily.GoalsProvider, resolving a picked
// 90-day goal slug into its text, its 30-day children (to choose among), the selected
// milestone, and that milestone's open tasks — all live from the cascade.
type goalsAdapter struct{ store *goals.Store }

// NewGoalsAdapter wires the goals store into the daily service's Focus resolution.
func NewGoalsAdapter(store *goals.Store) daily.GoalsProvider { return goalsAdapter{store} }

func (a goalsAdapter) ResolveFocus(id, milestoneID string) (daily.FocusResolution, bool) {
	_, g := a.store.Load().FindGoal(id)
	if g == nil {
		return daily.FocusResolution{}, false
	}
	res := daily.FocusResolution{Text: g.Text}
	if len(g.Children) == 0 {
		return res, true
	}
	// The 30-day children the picker offers.
	for _, c := range g.Children {
		res.Milestones = append(res.Milestones, daily.FocusNode{GoalID: c.ID, Text: c.Text, Checked: c.Checked})
	}
	// Selected milestone: the requested one, else the first child.
	sel := g.Children[0]
	for _, c := range g.Children {
		if c.ID == milestoneID {
			sel = c
			break
		}
	}
	res.Milestone = &daily.FocusNode{GoalID: sel.ID, Text: sel.Text, Checked: sel.Checked}
	for _, t := range sel.Children {
		if t.Checked {
			continue // only open tasks
		}
		res.Tasks = append(res.Tasks, daily.FocusNode{GoalID: t.ID, Text: t.Text, Checked: t.Checked})
	}
	return res, true
}
