package server

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRelatedCodingChatPreservesSourceAndRecoversAfterChanges(t *testing.T) {
	for _, kind := range []string{"codex", "claude"} {
		t.Run(kind, func(t *testing.T) {
			s, st, _ := agentChatFixture(t, echoStub)
			s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}
			id, _ := st.Create("alfred", "", "Planning source", "")
			st.AppendTurn("alfred", id, "user", "original conversation", 0)
			before, body, _, _ := st.Get("alfred", id)
			payload := map[string]any{"backend": "terminal", "agent": kind, "title": "Coding follow-up", "prompt": "Reviewed excerpt", "requestId": "coding-related-001"}
			endpoint := "/api/agents/chat/alfred/sessions/" + id + "/related"
			code, out := agentChatJSON(t, s, "POST", endpoint, payload)
			if code != 200 {
				t.Fatal(code, out)
			}
			childID := out["id"].(string)
			child, ok := s.terminal.find(childID)
			if !ok || !child.isDraft() || child.Origin.ID != id || child.Origin.Prompt != "Reviewed excerpt" || child.Origin.Backend != "" || child.Model == "" {
				t.Fatalf("bad draft: %+v", child)
			}
			if kind == "claude" && child.ResumeID == "" {
				t.Fatal("missing native identity")
			}
			after, afterBody, queue, _ := st.Get("alfred", id)
			if before.Turns != after.Turns || body != afterBody || len(queue) != 0 {
				t.Fatal("source changed")
			}
			if links := s.relatedChats(after); len(links) != 1 || links[0].ID != childID {
				t.Fatal(links)
			}
			if links := s.terminalRelatedChats(child); len(links) != 1 || links[0].ID != id {
				t.Fatal(links)
			}
			// Rename the child and change the parent's task; accepted identity does
			// not depend on mutable display names, defaults or source membership.
			s.terminal.updateTermMetadata(childID, func(row *termSession) { row.Name = "Owner renamed" })
			st.SetTask("alfred", id, "inbox/moved")
			st.Delete("alfred", id)
			code, retry := agentChatJSON(t, s, "POST", endpoint, payload)
			if code != 200 || retry["id"] != childID {
				t.Fatal(code, retry)
			}
			payload["prompt"] = "different"
			if code, _ := agentChatJSON(t, s, "POST", endpoint, payload); code != 409 {
				t.Fatal("changed intent accepted", code)
			}
			payload["requestId"] = "coding-related-002"
			if code, _ := agentChatJSON(t, s, "POST", endpoint, payload); code != 404 {
				t.Fatal("missing source accepted", code)
			}
		})
	}
}

func TestRelatedCodingExactArtifactSentOnlyAfterReview(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}
	workspace, _ := workspaceFixture(t)
	s.todoPlans, s.vault, s.artifactReg = workspace.todoPlans, workspace.vault, workspace.artifactReg
	task := "inbox/coding-context"
	if err := s.writePlanSection("todo-plans", task, "plan", "REVIEWED_VERSION_BYTES"); err != nil {
		t.Fatal(err)
	}
	first := observePlan(t, s, task)
	id, _ := st.Create("alfred", "", "Source", "")
	st.SetTask("alfred", id, task)
	endpoint := "/api/agents/chat/alfred/sessions/" + id + "/related"
	refs := []artifactContextRef{{ID: first.ID, Revision: first.Head}}
	payload := map[string]any{"backend": "terminal", "agent": "claude", "requestId": "coding-artifact-001", "prompt": "Read this selected version", "task": task, "artifacts": refs}
	code, out := agentChatJSON(t, s, "POST", endpoint, payload)
	if code != 200 {
		t.Fatal(code, out)
	}
	childID := out["id"].(string)
	child, _ := s.terminal.find(childID)
	if !child.isDraft() || len(child.Origin.Artifacts) != 1 || child.Origin.Artifacts[0].Revision != first.Head {
		t.Fatal(child)
	}
	if err := s.saveTaskPlanVersion(task, "NEW_UNSELECTED_VERSION", first.Head); err != nil {
		t.Fatal(err)
	}
	observePlan(t, s, task)
	var prompt string
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		case "agent.prompt":
			prompt = r.Params["text"].(string)
			herdrFixtureReply(c, map[string]any{})
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	code, _ = agentChatJSON(t, s, "POST", "/api/terminal/session/"+childID+"/input", map[string]any{"text": "Edited handoff", "task": "inbox/unrelated", "artifacts": refs})
	if code != 400 {
		t.Fatal("unrelated artifact accepted", code)
	}
	if row, _ := s.terminal.find(childID); !row.isDraft() {
		t.Fatal("invalid context launched")
	}
	code, out = agentChatJSON(t, s, "POST", "/api/terminal/session/"+childID+"/input", map[string]any{"text": "Edited handoff", "task": task, "artifacts": refs})
	if code != 200 {
		t.Fatal(code, out)
	}
	if !strings.HasPrefix(prompt, "Edited handoff") || !strings.Contains(prompt, "REVIEWED_VERSION_BYTES") || !strings.Contains(prompt, first.Head) || strings.Contains(prompt, "NEW_UNSELECTED_VERSION") {
		t.Fatal("wrong prompt", prompt)
	}
}

func TestRelatedCodingConcurrentRetriesCreateOneDraft(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}
	id, _ := st.Create("alfred", "", "Source", "")
	body := []byte(`{"backend":"terminal","agent":"codex","requestId":"concurrent-coding-001","prompt":"Reviewed"}`)
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/api/agents/chat/alfred/sessions/"+id+"/related", bytes.NewReader(body))
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			var out struct{ ID string }
			json.Unmarshal(w.Body.Bytes(), &out)
			if w.Code != 200 {
				results <- "error: " + w.Body.String()
			} else {
				results <- out.ID
			}
		}()
	}
	wg.Wait()
	close(results)
	rows := s.terminal.load()
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	for got := range results {
		if got != rows[0].ID {
			t.Fatal(got)
		}
	}
}
