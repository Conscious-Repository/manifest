package goals

import (
	"strings"
	"testing"
	"time"
)

// The fixture mirrors the live file's shape: two areas, a rock with a
// milestone and frozen depth-2 history (one frozen line carrying a phantom
// [goal::] pin — the live file has one at goals.md:58).
func canonicalFixture(t *testing.T) string {
	t.Helper()
	raw := "# Goals\n\n" +
		"## Home\n> North Star: Ready to start a family in it.\n\n" +
		"### 1-year — 2026\n" +
		"- [ ] Back extension is enclosed [goal:: home/back-extension-is-enclosed]\n\n" +
		"### Rocks (90-day)\n" +
		"- [ ] Backyard [goal:: home/backyard] [quarter:: 2026-Q3]\n" +
		"    - [ ] Metal up\n" +
		"        - [x] Grind concrete\n" +
		"        - [x] pick-up tablesaw [goal:: home/backyard/metal-up/pick-up-tablesaw]\n" +
		"    - [x] Yard done [goal:: home/backyard/yard-done]\n" +
		"- [ ] Basement plan [quarter:: 2026-Q3]\n\n" +
		"## Personal\n\n### Rocks (90-day)\n" +
		"- [ ] Reading habit [quarter:: 2026-Q3]\n"
	canon := Serialize(Parse(raw))
	if Serialize(Parse(canon)) != canon {
		t.Fatal("fixture did not canonicalize to a fixpoint")
	}
	return canon
}

func mustApply(t *testing.T, current string, p PlacementPayload) string {
	t.Helper()
	next, err := ApplyPlacement(current, p, time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func mustRefuse(t *testing.T, current string, p PlacementPayload, wants string) {
	t.Helper()
	_, err := ApplyPlacement(current, p, time.Now())
	if err == nil {
		t.Fatalf("expected refusal (%s), got success", wants)
	}
	if wants != "" && !strings.Contains(err.Error(), wants) {
		t.Fatalf("refusal should mention %q, got: %v", wants, err)
	}
}

func TestAddMilestoneIsOneLine(t *testing.T) {
	cur := canonicalFixture(t)
	next := mustApply(t, cur, PlacementPayload{
		Mode: ModeAdd, Level: LevelMilestone, Area: "Home",
		ParentID: "home/backyard", Title: "Back pad complete",
	})
	if strings.Count(next, "\n") != strings.Count(cur, "\n")+1 {
		t.Fatalf("add must be exactly one line:\n%s", next)
	}
	if !strings.Contains(next, "    - [ ] Back pad complete [goal:: home/backyard/back-pad-complete]\n") {
		t.Fatalf("milestone not placed under the rock:\n%s", next)
	}
	// the result is itself a fixpoint and re-appliable state
	if Serialize(Parse(next)) != next {
		t.Fatal("post-add not a fixpoint")
	}
	// frozen history is byte-identical
	if !strings.Contains(next, "        - [x] pick-up tablesaw [goal:: home/backyard/metal-up/pick-up-tablesaw]") {
		t.Fatal("frozen depth-2 history disturbed")
	}
}

func TestAddRockStampsQuarter(t *testing.T) {
	next := mustApply(t, canonicalFixture(t), PlacementPayload{
		Mode: ModeAdd, Level: LevelRock, Area: "Home",
		Title: "Mini split upstairs",
	})
	// the quarter is never carried by a payload — a new rock stamps the
	// CURRENT quarter (owner call 2026-08-19)
	if !strings.Contains(next, "- [ ] Mini split upstairs [goal:: home/mini-split-upstairs] [quarter:: 2026-Q3]") {
		t.Fatalf("rock line wrong:\n%s", next)
	}
}

func TestAddRefusals(t *testing.T) {
	cur := canonicalFixture(t)
	mustRefuse(t, cur, PlacementPayload{Mode: ModeAdd, Level: LevelRock, Area: "Garage", Title: "X"}, "no area")
	mustRefuse(t, cur, PlacementPayload{Mode: ModeAdd, Level: LevelRock, Area: "Home", Title: "Backyard"}, "already exists")
	// a milestone may not hang off a milestone (the depth rule)
	mustRefuse(t, cur, PlacementPayload{
		Mode: ModeAdd, Level: LevelMilestone, Area: "Home",
		ParentID: "home/backyard/metal-up", Title: "Deeper",
	}, "top-level rock")
	// rock-only fields refused on milestones at validate time
	mustRefuse(t, cur, PlacementPayload{
		Mode: ModeAdd, Level: LevelMilestone, Area: "Home",
		ParentID: "home/backyard", Title: "X", Due: "2026-09-30",
	}, "rock-only")
}

func TestEditRenamesInPlaceWithAnchor(t *testing.T) {
	cur := canonicalFixture(t)
	next := mustApply(t, cur, PlacementPayload{
		Mode: ModeEdit, Level: LevelRock, Area: "Home",
		TargetID: "home/basement-plan", AnchorText: "Basement plan",
		Title: "Basement realignment gameplan",
	})
	if !strings.Contains(next, "Basement realignment gameplan") || strings.Contains(next, "- [ ] Basement plan ") {
		t.Fatalf("rename missing:\n%s", next)
	}
	// pin-before-rename: the old id is frozen on the line (EditGoal's doing)
	if !strings.Contains(next, "[goal:: home/basement-plan]") {
		t.Fatalf("rename must pin the old id:\n%s", next)
	}
	// the line count is unchanged — edit is in place
	if strings.Count(next, "\n") != strings.Count(cur, "\n") {
		t.Fatal("edit changed the line count")
	}
}

func TestStaleAnchorRefuses(t *testing.T) {
	cur := canonicalFixture(t)
	for _, mode := range []string{ModeEdit, ModeMove} {
		mustRefuse(t, cur, PlacementPayload{
			Mode: mode, Level: LevelRock, Area: "Home",
			TargetID: "home/basement-plan", AnchorText: "Basement scheme (renamed since)",
			Title: "X", ParentID: "",
		}, "moved under this proposal")
	}
	mustRefuse(t, cur, PlacementPayload{
		Mode: ModeEdit, Level: LevelRock, Area: "Home",
		TargetID: "home/gone", AnchorText: "whatever", Title: "X",
	}, "no goal")
}

func TestMoveReparentsUnderAnotherRock(t *testing.T) {
	cur := canonicalFixture(t)
	next := mustApply(t, cur, PlacementPayload{
		Mode: ModeMove, Level: LevelMilestone, Area: "Home",
		TargetID: "home/backyard/metal-up", AnchorText: "Metal up",
		ParentID: "home/basement-plan",
	})
	if !strings.Contains(next, "- [ ] Basement plan [goal:: home/basement-plan] [quarter:: 2026-Q3]\n    - [ ] Metal up") {
		t.Fatalf("milestone did not move:\n%s", next)
	}
	// identity froze before the move so references survive
	if !strings.Contains(next, "[goal:: home/backyard/metal-up]") {
		t.Fatalf("move must pin the pre-move id:\n%s", next)
	}
	if strings.Count(next, "\n") != strings.Count(cur, "\n") {
		t.Fatal("move changed the line count")
	}
	if Serialize(Parse(next)) != next {
		t.Fatal("post-move not a fixpoint")
	}
}

func TestMoveGuards(t *testing.T) {
	cur := canonicalFixture(t)
	// only a top-level rock may take children
	mustRefuse(t, cur, PlacementPayload{
		Mode: ModeMove, Level: LevelMilestone, Area: "Home",
		TargetID: "home/backyard/yard-done", AnchorText: "Yard done",
		ParentID: "home/backyard/metal-up",
	}, "top-level rock")
	// a move may not cross areas
	mustRefuse(t, cur, PlacementPayload{
		Mode: ModeMove, Level: LevelRock, Area: "Personal",
		TargetID: "home/basement-plan", AnchorText: "Basement plan",
	}, "not in area")
}

func TestNonCanonicalInputRefuses(t *testing.T) {
	// a hand-written file that Serialize would canonicalize (comma serves,
	// legacy field order) must refuse rather than be rewritten wholesale
	raw := "# Goals\n\n## Home\n\n### Rocks (90-day)\n- [ ] Backyard [quarter::2026-Q3]\n"
	if Serialize(Parse(raw)) == raw {
		t.Skip("fixture unexpectedly canonical")
	}
	mustRefuse(t, raw, PlacementPayload{Mode: ModeAdd, Level: LevelRock, Area: "Home", Title: "X"}, "canonical")
}

func TestValidateTable(t *testing.T) {
	bad := []PlacementPayload{
		{Mode: "merge", Level: LevelRock, Area: "Home", Title: "x"},
		{Mode: ModeAdd, Level: "task", Area: "Home", Title: "x"},
		{Mode: ModeAdd, Level: LevelRock, Title: "x"},
		{Mode: ModeAdd, Level: LevelRock, Area: "Home"},
		{Mode: ModeAdd, Level: LevelMilestone, Area: "Home", Title: "x"},
		{Mode: ModeEdit, Level: LevelRock, Area: "Home", AnchorText: "x"},
		{Mode: ModeEdit, Level: LevelRock, Area: "Home", TargetID: "home/x"},
		{Mode: ModeAdd, Level: LevelRock, Area: "Home", Title: "x", Due: "next week"},
	}
	for i, p := range bad {
		if err := p.Validate(); err == nil {
			t.Fatalf("case %d should refuse: %+v", i, p)
		}
	}
}

func TestParsePlacementFence(t *testing.T) {
	body := "evidence line\n\n```goals\n{\"mode\":\"add\",\"level\":\"rock\",\"area\":\"Home\",\"title\":\"Back pad complete\"}\n```\n"
	p, ok := ParsePlacement(body)
	if !ok || p.Title != "Back pad complete" || p.Mode != ModeAdd {
		t.Fatalf("fence parse failed: %+v ok=%v", p, ok)
	}
	if _, ok := ParsePlacement("no fence here"); ok {
		t.Fatal("no-fence body must not parse")
	}
}
