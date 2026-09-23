package aion

import (
	"strings"
	"testing"
)

// The card preview of a heuristic is the heuristics.md text Confirm lands.
func TestRenderHeuristicPreview(t *testing.T) {
	p := ProposalPayload{Kind: KindHeuristic, Title: "If it is not a hell yes, it is a hell no", Captured: "2026-09-22", Sources: []string{"log/2026-09-22 hiring brainstorm.md"}, Heuristic: HeuristicIntent{Mode: HeuristicModeNew}}
	got := RenderHeuristicPreview(p)
	want := "- If it is not a hell yes, it is a hell no [first:: 2026-09-22]\n    - [[log/2026-09-22 hiring brainstorm.md]] [date:: 2026-09-22]"
	if got != want {
		t.Fatalf("new:\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "[ ]") || strings.Contains(got, "kind::") {
		t.Fatal("preview rendered a backlog line")
	}
	p.Heuristic = HeuristicIntent{Mode: HeuristicModeReinforce, Target: "Take the longer path that gets you there faster"}
	got = RenderHeuristicPreview(p)
	if !strings.HasPrefix(got, "- Take the longer path that gets you there faster") || !strings.Contains(got, "[[log/2026-09-22 hiring brainstorm.md]] [date:: 2026-09-22]") {
		t.Fatalf("reinforce:\n%q", got)
	}
}
