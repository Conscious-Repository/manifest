package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"manifest/aion"
	"manifest/goals"
	"manifest/record"
)

// aionExportInput assembles the owner-authored base contract in memory. The
// result is consumed by AionLive's safety gate; it is never written to a git
// checkout.
func (s *Server) aionExportInput(generatedAt string) aion.ExportInput {
	return aion.ExportInput{
		People: s.aion.LoadPeople(), VTO: s.aion.LoadVTO(), Backlog: s.aion.LoadBacklog(),
		Heuristics: s.aion.LoadHeuristics(), Finances: s.aion.LoadFinances(),
		HiringMD: []byte(s.aion.RawFile("hiring.md")), ReferencesMD: []byte(s.aion.RawFile("references.md")),
		Goals: s.aionExportGoals(), PublishedAt: generatedAt,
	}
}

func (s *Server) aionExportGoals() []aion.ExportGoal {
	area := s.aionGoalsArea()
	if area == nil {
		return []aion.ExportGoal{}
	}
	var out []aion.ExportGoal
	status := func(g goals.GoalView) string {
		if g.Checked || strings.EqualFold(g.Status, "done") {
			return "done"
		}
		return "open"
	}
	owner := func(g goals.GoalView) *string {
		if g.Owner == "" || record.OwnerIsMe(g.Owner) {
			return nil
		}
		o := g.Owner
		return &o
	}
	serving := map[string][]string{}
	for _, r := range area.Rocks {
		for _, sv := range r.Serves {
			serving[sv] = append(serving[sv], r.ID)
		}
	}
	nonNil := func(ids []string) []string {
		if ids == nil {
			return []string{}
		}
		return ids
	}
	for _, a := range area.Annuals {
		out = append(out, aion.ExportGoal{ID: a.ID, Title: a.Text, Horizon: "1yr", Status: status(a),
			Aliases: nonNil(a.Aliases), Owner: owner(a), Children: nonNil(serving[a.ID])})
	}
	for _, r := range area.Rocks {
		var serves *string
		if len(r.Serves) > 0 {
			v := r.Serves[0]
			serves = &v
		}
		var kids []string
		for _, c := range r.Children {
			kids = append(kids, c.ID)
			out = append(out, aion.ExportGoal{ID: c.ID, Title: c.Text, Horizon: "30", Status: status(c), Owner: owner(c), Children: []string{}})
		}
		out = append(out, aion.ExportGoal{ID: r.ID, Title: r.Text, Horizon: "rock", Status: status(r),
			Serves: serves, ServesAll: append([]string{}, r.Serves...), Aliases: nonNil(r.Aliases), Owner: owner(r),
			Quarter: r.Quarter, Start: r.Start, Due: r.Due, Children: nonNil(kids)})
	}
	live := map[string]bool{}
	for _, g := range out {
		live[g.ID] = true
	}
	if s.goals != nil {
		for _, aq := range s.goals.LoadAllArchives() {
			for _, e := range aq.Entries {
				if !strings.HasPrefix(e.GoalID, "aion/") || live[e.GoalID] {
					continue
				}
				live[e.GoalID] = true
				var serves *string
				if len(e.Serves) > 0 {
					v := e.Serves[0]
					serves = &v
				}
				out = append(out, aion.ExportGoal{ID: e.GoalID, Title: e.Text, Horizon: "rock", Status: "done",
					Serves: serves, ServesAll: append([]string{}, e.Serves...), Quarter: aq.Quarter,
					Closed: e.Closed, Children: []string{}})
			}
		}
	}
	return out
}

func aionInScopeQuarters(now time.Time) map[string]bool {
	cur := goals.CurrentQuarter(now)
	return map[string]bool{cur: true, nextQuarter(cur): true}
}

func nextQuarter(q string) string {
	if len(q) != 7 {
		return q
	}
	year, _ := strconv.Atoi(q[:4])
	switch q[6] {
	case '1':
		return fmt.Sprintf("%d-Q2", year)
	case '2':
		return fmt.Sprintf("%d-Q3", year)
	case '3':
		return fmt.Sprintf("%d-Q4", year)
	default:
		return fmt.Sprintf("%d-Q1", year+1)
	}
}
