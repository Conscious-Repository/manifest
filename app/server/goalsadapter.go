package server

import (
	"manifest/daily"
	"manifest/goals"
)

// goalsAdapter bridges the goals store to daily.GoalsProvider, resolving a picked
// Rock slug into its text, its stages (to choose among), the selected stage
// (defaulting to the current stage — the first unchecked one), and that stage's open
// tasks — all live from the ladder.
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
	for _, t := range sel.Children {
		if t.Checked {
			continue // only open tasks
		}
		res.Tasks = append(res.Tasks, daily.FocusNode{GoalID: t.ID, Text: t.Text, Checked: t.Checked})
	}
	return res, true
}
