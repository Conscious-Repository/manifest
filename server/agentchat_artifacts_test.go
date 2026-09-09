package server

import (
	"strings"
	"testing"

	"manifest/agentchat"
	"manifest/artifacts"
)

func TestAgentChatRetainsSelectedPlanVersionAndRejectsUnlinkedContext(t *testing.T) {
	s, st, _ := agentChatFixture(t, strings.Replace(echoStub, " | tail -n 3 | head -n 1", "", 1))
	workspace, _ := workspaceFixture(t)
	s.todoPlans, s.vault, s.artifactReg = workspace.todoPlans, workspace.vault, workspace.artifactReg
	task := "inbox/context-plan"
	if err := s.writePlanSection("todo-plans", task, "plan", "ORIGINAL_SELECTED_PLAN"); err != nil {
		t.Fatal(err)
	}
	first := observePlan(t, s, task)
	id, err := st.Create("alfred", "", "Plan discussion", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTask("alfred", id, task); err != nil {
		t.Fatal(err)
	}
	if err := s.saveTaskPlanVersion(task, "LATER_WORKING_PLAN", first.Head); err != nil {
		t.Fatal(err)
	}
	second := observePlan(t, s, task)
	path := "/api/agents/chat/alfred/sessions/" + id + "/messages"
	payload := map[string]any{"text": "Discuss the selected version", "requestId": "selected-plan-1", "artifacts": []artifactContextRef{{ID: first.ID, Revision: first.Head}}}
	code, _ := agentChatJSON(t, s, "POST", path, payload)
	if code != 200 {
		t.Fatalf("send: %d", code)
	}
	sess := waitIdle(t, st, "alfred", id)
	_, body, _, _ := st.Get("alfred", id)
	if !strings.Contains(body, "ORIGINAL_SELECTED_PLAN") || strings.Contains(body, "LATER_WORKING_PLAN") {
		t.Fatalf("wrong version delivered: %s", body)
	}
	if len(sess.Deliveries) != 1 || sess.Deliveries[0].Context == nil || sess.Deliveries[0].Context.Artifacts[0].Revision != first.Head {
		t.Fatalf("missing durable version context: %+v", sess.Deliveries)
	}
	if err := st.SetTask("alfred", id, "inbox/other"); err != nil {
		t.Fatal(err)
	}
	code, _ = agentChatJSON(t, s, "POST", path, payload)
	if code != 200 {
		t.Fatalf("accepted retry lost after reassignment: %d", code)
	}
	payload["artifacts"] = []artifactContextRef{{ID: first.ID, Revision: second.Head}}
	code, _ = agentChatJSON(t, s, "POST", path, payload)
	if code != 409 {
		t.Fatalf("changed context retry must conflict: %d", code)
	}
	payload["requestId"] = "selected-plan-2"
	code, _ = agentChatJSON(t, s, "POST", path, payload)
	if code != 400 {
		t.Fatalf("unlinked context accepted: %d", code)
	}
	if _, ok := st.Receipt("alfred", id, "selected-plan-2"); ok {
		t.Fatal("rejected context was accepted")
	}
	final, _, _, _ := st.Get("alfred", id)
	if final.Turns != 2 {
		t.Fatalf("retry executed again: %d", final.Turns)
	}
}

func TestSelectedPDFRequiresTextExtraction(t *testing.T) {
	s, _ := workspaceFixture(t)
	a, err := s.artifactReg.Put(artifacts.Put{Kind: artifacts.KindFile, Title: "Reference PDF", Harness: "vault", Ref: "reference.pdf", Content: []byte("%PDF-1.7\nASCII bytes are not a readable document"), Provenance: artifacts.Provenance{Task: "inbox/reference"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.taskArtifactContext("inbox/reference", []artifactContextRef{{ID: a.Artifact.ID, Revision: a.Artifact.Head}}); err == nil {
		t.Fatal("raw PDF advertised as text context")
	}
}

func TestAgentChatUnavailableAcceptedContextFailsBeforeInvocation(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, _ := st.Create("alfred", "", "", "")
	ctx := &agentchat.MessageContext{Conversation: "test", Agent: "alfred", Artifacts: []agentchat.ArtifactReference{{ID: "gone", Revision: "gone"}}}
	if _, err := st.Accept("alfred", id, "missing-context", "Review", ctx); err != nil {
		t.Fatal(err)
	}
	s.startAgentChatDelivery("alfred", id)
	sess := waitIdle(t, st, "alfred", id)
	if sess.Deliveries[0].State != agentchat.DeliveryFailed {
		t.Fatal(sess.Deliveries)
	}
	_, body, _, _ := st.Get("alfred", id)
	if strings.Contains(body, "REPLY to:") || !strings.Contains(body, "Selected artifact context is unavailable") {
		t.Fatal(body)
	}
}
