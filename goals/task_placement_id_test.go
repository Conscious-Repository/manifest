package goals

import (
	"strings"
	"testing"
	"time"
)

func TestTaskPlacementWritesID(t *testing.T) {
	cur := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Human scale spec + team hired [goal:: aion/human-scale-spec] [owner:: RT]\n    - [ ] Prototype [goal:: aion/human-scale-spec/prototype] [owner:: Y]\n"
	doc := Parse(cur)
	if Serialize(doc) != cur {
		t.Fatalf("fixture not canonical:\n%s", Serialize(doc))
	}
	p := PlacementPayload{Mode: "add", Level: "task", Area: "Aion",
		ParentID: "aion/human-scale-spec/prototype", Title: "Get in contact with Test", Owner: "BA"}
	nxt, err := ApplyPlacement(cur, p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nxt, "[goal:: aion/human-scale-spec/prototype/get-in-contact-with-test]") {
		t.Errorf("task failed to get a goal id — this is why it's absent from the tasks list:\n%s", nxt)
	} else {
		t.Logf("task has id:\n%s", nxt)
	}
}
