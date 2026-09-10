package server

import (
	"encoding/json"
	"fmt"
	"manifest/agentchat"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativePlanRevisionRequiresMatchingSentInput(t *testing.T) {
	s, _ := workspaceFixture(t)
	task := "inbox/native-plan"
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	if err := s.writePlanSection("todo-plans", task, "plan", "Original"); err != nil {
		t.Fatal(err)
	}
	a := observePlan(t, s, task)
	se := termSession{ID: "abcdef123456", Kind: "codex"}
	r := terminalInputReceipt{ID: "native-plan-001", State: "sent", Fingerprint: strings.Repeat("a", 64), SubmittedHash: hashTerminalText("exact submitted instruction"), Task: task, Artifacts: []artifactContextRef{{ID: a.ID, Revision: a.Head}}}
	if err := s.terminal.writeInputReceipt(se.ID, r); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(chatPlanRevision{ArtifactID: a.ID, BaseRevision: a.Head, Content: "Revised"})
	reply := termTurn{ID: "native-reply", Who: "assistant", Blocks: []termBlock{{T: "say", Text: "```json\n" + string(b) + "\n```"}}}
	turns := []termTurn{{ID: "native-user", Who: "user", Text: "exact submitted instruction"}, reply}
	got := s.nativePlanRevisions(se, turns)
	if len(got) != 1 || got[reply.ID].Task != task {
		t.Fatal(got)
	}
	projected := conversationTimeline(agentchat.Session{}, "", []codingContinuationView{{ID: se.ID, Agent: se.Kind, Turns: turns, PlanRevisions: got}})
	if projected[1].PlanRevision == nil || projected[1].Native.ID != se.ID {
		t.Fatal("native identity lost", projected)
	}
	turns[0].Text = "unmatched input"
	if len(s.nativePlanRevisions(se, turns)) != 0 {
		t.Fatal("unmatched input accepted")
	}
	turns[0].Text = "exact submitted instruction"
	r.State = "unconfirmed"
	s.terminal.writeInputReceipt(se.ID, r)
	if len(s.nativePlanRevisions(se, turns)) != 0 {
		t.Fatal("unconfirmed input accepted")
	}
	r.State = "sent"
	s.terminal.writeInputReceipt(se.ID, r)
	turns = append(turns, termTurn{Who: "user", Text: "later input with no context"}, termTurn{ID: "later-reply", Who: "assistant", Blocks: reply.Blocks})
	if got := s.nativePlanRevisions(se, turns); len(got) != 1 || got["later-reply"].Content != "" {
		t.Fatal("old context leaked to later input", got)
	}
}

func TestPlanRevisionBoundToDeliveredVersion(t *testing.T) {
	s, _ := workspaceFixture(t)
	task := "inbox/proposed-plan"
	if err := s.writePlanSection("todo-plans", task, "plan", "Original plan"); err != nil {
		t.Fatal(err)
	}
	a := observePlan(t, s, task)
	sess := agentchat.Session{Deliveries: []agentchat.Delivery{{State: agentchat.DeliveryCompleted, ReplyTurn: 2, Context: &agentchat.MessageContext{Task: task, Artifacts: []agentchat.ArtifactReference{{ID: a.ID, Revision: a.Head}}}}}}
	proposal := chatPlanRevision{ArtifactID: a.ID, BaseRevision: a.Head, Content: "Revised plan", Task: "inbox/wrong", ReplyTurn: 999}
	body := func(p chatPlanRevision) string {
		b, _ := json.Marshal(p)
		return fmt.Sprintf("## Turn 2 — agent:alfred · 2026-09-09T12:00:00Z\n\nReview this.\n\n```manifest-plan-revision\n%s\n```\n", b)
	}
	text := body(proposal)
	got := s.chatPlanRevisions(sess, text)
	if plain := s.chatPlanRevisions(sess, strings.ReplaceAll(text, "```manifest-plan-revision", "```json")); len(plain) != 1 {
		t.Fatal("valid JSON-fenced proposal rejected", plain)
	}
	if len(got) != 1 || got[0].Task != task || got[0].ReplyTurn != 2 || got[0].Content != "Revised plan" {
		t.Fatal(got)
	}
	if current := observePlan(t, s, task); current.Head != a.Head {
		t.Fatal("projection wrote plan")
	}
	proposal.BaseRevision = strings.Repeat("b", 64)
	if len(s.chatPlanRevisions(sess, body(proposal))) != 0 {
		t.Fatal("undelivered revision accepted")
	}
	proposal.BaseRevision = a.Head
	proposal.ArtifactID = "unknown"
	if len(s.chatPlanRevisions(sess, body(proposal))) != 0 {
		t.Fatal("undelivered artifact accepted")
	}
	proposal.ArtifactID = a.ID
	sess.Deliveries[0].State = agentchat.DeliveryRunning
	if len(s.chatPlanRevisions(sess, body(proposal))) != 0 {
		t.Fatal("unfinished reply accepted")
	}
	sess.Deliveries[0].State = agentchat.DeliveryCompleted
	sess.Deliveries[0].Context.Artifacts = nil
	if len(s.chatPlanRevisions(sess, body(proposal))) != 0 {
		t.Fatal("unselected plan accepted")
	}
	if !strings.Contains(s.planRevisionInstructions([]artifactContextRef{{ID: a.ID, Revision: a.Head}}), a.Head) {
		t.Fatal("prompt lacks exact base")
	}
}
