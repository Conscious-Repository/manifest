package domainextract

import (
	"os"
	"strings"

	"manifest/goals"
)

// EVERY TASK SERVES AN EXISTING GOAL (owner, 2026-10-06: "all new tasks
// should fall under an existing rock / be assigned to closest existing rock.
// anything without a clear rock gets the operations placement"). The
// extractor used to invent tethers ("aion/regulatory-strategy",
// "aion/ultrasound-platform") because it never saw the goal list. Now the
// Aion area's open goals and milestones ride the context, the prompt asks
// for the closest one, and code enforces it: a tether that names no listed
// goal becomes aion/operations-health before any card exists.

// AionGoalsContext is a DERIVED context entry (computed from goals.md, not a
// file): the extraction snapshot skips "derived:" entries, so editing a goal
// never holds a pending card.
const AionGoalsContext = "derived:aion-goals"

// AionFallbackRock is where a task without a clear goal goes.
const AionFallbackRock = "aion/operations-health"

const derivedContextPrefix = "derived:"

// aionGoalsList renders the Aion area's open goals and milestones, one
// "id — title" line each (children indented), from the vault's goals.md.
// "" when there is no goals file or no Aion area.
func aionGoalsList(root *os.Root) string {
	b, err := root.ReadFile("goals.md")
	if err != nil {
		return ""
	}
	view := goals.Parse(string(b)).View()
	var lines []string
	var walk func([]goals.GoalView, int)
	walk = func(gs []goals.GoalView, depth int) {
		for _, g := range gs {
			if g.Checked || g.ID == "" {
				continue
			}
			lines = append(lines, strings.Repeat("  ", depth)+g.ID+" — "+strings.TrimSpace(g.Text))
			walk(g.Children, depth+1)
		}
	}
	for _, a := range view.Areas {
		if strings.EqualFold(a.Name, "Aion") {
			walk(a.Rocks, 0)
		}
	}
	return strings.Join(lines, "\n")
}

// aionGoalIDs reads the ids back out of the rendered list.
func aionGoalIDs(list string) map[string]bool {
	ids := map[string]bool{}
	for _, l := range strings.Split(list, "\n") {
		if id, _, ok := strings.Cut(strings.TrimSpace(l), " — "); ok && id != "" {
			ids[id] = true
		}
	}
	return ids
}
