package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalRelatedChatPreservesNativeSessionAndUnsentHandoff(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	dir := t.TempDir()
	s.UseTerminal(filepath.Join(dir, "terminals.json"), filepath.Join(dir, "tmux"), dir)
	se := termSession{ID: "abcdef123456", Kind: "codex", Name: "Coding source", Cwd: dir}
	s.terminal.upsert(se)
	if _, ok := s.terminal.find(se.ID); !ok {
		t.Fatal("fixture session unavailable")
	}
	endpoint := "/api/terminal/codex/session/" + se.ID + "/related"
	payload := map[string]any{"agent": "alfred", "title": "Research alongside coding", "prompt": "Reviewed excerpt only", "requestId": "terminal-related-001"}
	code, out := agentChatJSON(t, s, "POST", endpoint, payload)
	if code != 200 {
		t.Fatal(code, out)
	}
	id := out["id"].(string)
	child, body, q, ok := st.Get("alfred", id)
	if !ok || body != "" || len(q) != 0 || child.Turns != 0 || child.Status != "idle" || child.Origin.Backend != "terminal" || child.Origin.ID != se.ID {
		t.Fatal(child, body)
	}
	if got := s.relatedChats(child); len(got) != 1 || got[0].Route != "#/chat/a/codex/"+se.ID {
		t.Fatal(got)
	}
	if got := s.terminalRelatedChats(se); len(got) != 1 || got[0].ID != id {
		t.Fatal(got)
	}
	if current, _ := s.terminal.find(se.ID); current.Started {
		t.Fatal("handoff started source runtime")
	}
	// A lost creation reply remains recoverable even after the source is gone.
	s.terminal.remove(se.ID)
	code, retry := agentChatJSON(t, s, "POST", endpoint, payload)
	if code != 200 || retry["id"] != id {
		t.Fatal(code, retry)
	}
	payload["prompt"] = "changed"
	if code, _ = agentChatJSON(t, s, "POST", endpoint, payload); code != 409 {
		t.Fatal(code)
	}
	payload["requestId"] = "terminal-related-002"
	if code, _ = agentChatJSON(t, s, "POST", endpoint, payload); code != 404 {
		t.Fatal(code)
	}
	s.terminal.upsert(se)
	payload["task"] = "inbox/unrelated"
	if code, _ = agentChatJSON(t, s, "POST", endpoint, payload); code != 400 {
		t.Fatal("unverified task allowed", code)
	}
}

func TestRelatedChatCreatesUnsentDraftAndBidirectionalLinks(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, err := st.Create("alfred", "", "Source", "")
	if err != nil {
		t.Fatal(err)
	}
	st.AppendTurn("alfred", id, "user", "original", 0)
	call := func(payload map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(payload)
		r := httptest.NewRequest("POST", "/api/agents/chat/alfred/sessions/"+id+"/related", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	payload := map[string]any{"agent": "alfred", "title": "Related", "prompt": "Reviewed context", "requestId": "related-http-001"}
	w := call(payload)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &out)
	child, body, queue, ok := st.Get("alfred", out.ID)
	if !ok || child.Status != "idle" || child.Turns != 0 || body != "" || len(queue) != 0 || len(child.Deliveries) != 0 {
		t.Fatal(child, body)
	}
	parent, parentBody, _, _ := st.Get("alfred", id)
	if parent.Turns != 1 || !bytes.Contains([]byte(parentBody), []byte("original")) {
		t.Fatal(parent)
	}
	if len(s.relatedChats(parent)) != 1 || len(s.relatedChats(child)) != 1 {
		t.Fatal("missing bidirectional links")
	}
	w = call(payload)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var retry struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &retry)
	if retry.ID != out.ID {
		t.Fatal("retry created another chat")
	}
	payload["prompt"] = "different"
	if w = call(payload); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	payload["task"] = "inbox/unrelated"
	payload["requestId"] = "related-http-002"
	if w = call(payload); w.Code != 400 {
		t.Fatal("unrelated task accepted", w.Code)
	}
}

func TestRelatedChatArtifactHandoffKeepsReviewedRevision(t *testing.T) {
	s, st, _ := agentChatFixture(t, strings.Replace(echoStub, " | tail -n 3 | head -n 1", "", 1))
	workspace, _ := workspaceFixture(t)
	s.todoPlans, s.vault, s.artifactReg = workspace.todoPlans, workspace.vault, workspace.artifactReg
	task := "inbox/related-plan"
	if err := s.writePlanSection("todo-plans", task, "plan", "REVIEWED_PLAN_BYTES"); err != nil {
		t.Fatal(err)
	}
	first := observePlan(t, s, task)
	parent, err := st.Create("alfred", "", "Origin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetTask("alfred", parent, task); err != nil {
		t.Fatal(err)
	}
	before, original, _, _ := st.Get("alfred", parent)
	if err = s.saveTaskPlanVersion(task, "NEW_UNREVIEWED_BYTES", first.Head); err != nil {
		t.Fatal(err)
	}
	observePlan(t, s, task)
	payload := map[string]any{"agent": "alfred", "title": "Plan follow-up", "prompt": "Discuss this version", "requestId": "related-artifact-001", "task": task, "artifacts": []artifactContextRef{{ID: first.ID, Revision: first.Head}}}
	code, raw := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+parent+"/related", payload)
	if code != 200 {
		t.Fatalf("create: %d %v", code, raw)
	}
	outID, _ := raw["id"].(string)
	child, body, queue, ok := st.Get("alfred", outID)
	if !ok || body != "" || child.Turns != 0 || len(queue) != 0 || len(child.Deliveries) != 0 {
		t.Fatal("creation executed work", child)
	}
	if child.Origin == nil || len(child.Origin.Artifacts) != 1 || child.Origin.Artifacts[0].Revision != first.Head {
		t.Fatal("lost reviewed context", child)
	}
	if err = st.SetTask("alfred", parent, "inbox/reassigned"); err != nil {
		t.Fatal(err)
	}
	if err = st.Rename("alfred", outID, "Renamed after creation"); err != nil {
		t.Fatal(err)
	}
	relatedPath := "/api/agents/chat/alfred/sessions/" + parent + "/related"
	code, raw = agentChatJSON(t, s, "POST", relatedPath, payload)
	if code != 200 || raw["id"] != outID {
		t.Fatalf("accepted creation lost after source reassignment: %d %v", code, raw)
	}
	payload["requestId"] = "related-artifact-new"
	code, _ = agentChatJSON(t, s, "POST", relatedPath, payload)
	if code != 400 {
		t.Fatalf("new unlinked handoff accepted: %d", code)
	}
	payload["requestId"] = "related-artifact-001"
	code, raw = agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+outID+"/messages", map[string]any{"text": child.Origin.Prompt, "requestId": "related-artifact-send", "task": task, "artifacts": child.Origin.Artifacts})
	if code != 200 {
		t.Fatalf("send: %d %v", code, raw)
	}
	child = waitIdle(t, st, "alfred", outID)
	_, body, _, _ = st.Get("alfred", outID)
	if !strings.Contains(body, "REVIEWED_PLAN_BYTES") || strings.Contains(body, "NEW_UNREVIEWED_BYTES") {
		t.Fatal("wrong artifact version in handoff", body)
	}
	after, remaining, _, _ := st.Get("alfred", parent)
	if after.Turns != before.Turns || remaining != original {
		t.Fatal("handoff changed source")
	}
	if err = st.Delete("alfred", parent); err != nil {
		t.Fatal(err)
	}
	code, raw = agentChatJSON(t, s, "POST", relatedPath, payload)
	if code != 200 || raw["id"] != outID {
		t.Fatalf("accepted child lost after source deletion: %d %v", code, raw)
	}
	payload["prompt"] = "Different intent with the same request"
	code, _ = agentChatJSON(t, s, "POST", relatedPath, payload)
	if code != 409 {
		t.Fatalf("changed intent retry accepted: %d", code)
	}
}
