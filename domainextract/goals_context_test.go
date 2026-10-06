package domainextract

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/approvals"
)

// Every AION task serves an existing goal: the extractor sees the open goal
// list, a tether that names no listed goal becomes the operations placement,
// a real one is kept, and the derived list never enters the snapshot (editing
// a goal must not hold a pending card).
func TestAionTasksLandOnAnExistingGoal(t *testing.T) {
	vault := t.TempDir()
	input := inputFixture()
	writeInput(t, vault, input)
	goalsMD := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Mouse data [goal:: aion/mouse-to-pig]\n    - [ ] Mice up\n- [ ] Operations & Company Health [goal:: aion/operations-health]\n- [x] Old rock [goal:: aion/old]\n\n## Real Estate\n\n### Rocks (90-day)\n- [ ] Fund I [goal:: ooda-group/fund-i]\n"
	if err := os.WriteFile(filepath.Join(vault, "goals.md"), []byte(goalsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := ReadInput(vault, "aion", []Document{{Name: input.Documents[0].Name}})
	if err != nil {
		t.Fatal(err)
	}
	list := in.Context[AionGoalsContext]
	for _, want := range []string{"aion/mouse-to-pig — Mouse data", "  aion/mouse-to-pig/mice-up — Mice up", "aion/operations-health"} {
		if !strings.Contains(list, want) {
			t.Fatalf("the open Aion goals and milestones ride the context:\n%s", list)
		}
	}
	if strings.Contains(list, "aion/old") || strings.Contains(list, "ooda-group") {
		t.Fatalf("only open Aion goals: %s", list)
	}
	if !strings.Contains(in.promptUnchecked(), "Never invent a goal id") {
		t.Fatal("the prompt asks for an existing goal")
	}
	reply := func(rock string) string {
		return strings.Replace(replyFixture, `"confidence":0.8`, `"confidence":0.8,"rock":"`+rock+`"`, 1)
	}
	for rock, want := range map[string]string{"aion/regulatory-strategy": AionFallbackRock, "aion/mouse-to-pig/mice-up": "aion/mouse-to-pig/mice-up", "": AionFallbackRock} {
		ps, dropped, err := EvaluateReply(in, reply(rock))
		if err != nil || len(ps) != 1 {
			t.Fatalf("rock %q: %v %v", rock, err, dropped)
		}
		pl, _ := aion.ParsePayloadFence(ps[0].Body, aion.PayloadFence)
		if pl.Rock != want {
			t.Fatalf("rock %q landed on %q, want %q", rock, pl.Rock, want)
		}
		raw, _ := base64.RawURLEncoding.DecodeString(ps[0].ExtractionSnapshot)
		var snap approvals.ExtractionSnapshot
		json.Unmarshal(raw, &snap)
		for name := range snap.Files {
			if strings.HasPrefix(name, "derived:") {
				t.Fatal("the derived goal list must not be a snapshot dependency")
			}
		}
	}
}
