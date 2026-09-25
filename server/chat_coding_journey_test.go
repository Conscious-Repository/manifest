package server

import (
	"bytes"
	"encoding/json"
	"manifest/agentchat"
	"manifest/threads"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This journey uses the real launch journal, result ingestion and HTTP result
// capture with a fake herdr socket. It does not execute a provider or a push.
func TestPlanningCodingResultJourney(t *testing.T) {
	for _, owner := range []string{"codex", "claude"} {
		t.Run(owner, func(t *testing.T) {
			s := codingFixture(t)
			workspace, _ := workspaceFixture(t)
			s.UseArtifactRegistry(workspace.artifactReg)
			chatDir := filepath.Join(t.TempDir(), "chats")
			chats := agentchat.New(chatDir)
			s.UseAgentChat(chats)
			id, err := chats.Create("alfred", "", "Plan the fence", "")
			if err != nil {
				t.Fatal(err)
			}
			task := "inbox/wire-the-fence"
			if _, err := chats.AppendTurn("alfred", id, "user", "Implement the reviewed fence plan.", 0); err != nil {
				t.Fatal(err)
			}
			if err := chats.SetTask("alfred", id, task); err != nil {
				t.Fatal(err)
			}
			if _, err := s.addThreadEntry(s.ownerIdentity(), task, threads.ActComment, "Planning origin", nil, nil, map[string]any{"chat": map[string]any{"agent": "alfred", "id": id, "canonical": true}}); err != nil {
				t.Fatal(err)
			}
			_, originalTranscript, _, _ := chats.Get("alfred", id)
			const reviewedPlan = "REVIEWED_PLAN: reject stale fence writes and verify the accepted version."
			if err := s.writePlanSection("todo-plans", task, "plan", reviewedPlan); err != nil {
				t.Fatal(err)
			}
			first := observePlan(t, s, task)
			if _, err := s.assignTask(s.ownerIdentity(), task, "agent:"+owner); err != nil {
				t.Fatal(err)
			}
			// Assignment deliberately adds owner/identity metadata to the task.
			// Result ingestion must preserve this assigned, still-open record.
			originalTasks, err := os.ReadFile(s.tasksStore.Path())
			if err != nil || strings.Count(string(originalTasks), "- [ ]") != 1 {
				t.Fatal("expected one open assigned task", err, string(originalTasks))
			}
			runs := s.findHarness(owner).Spirits.Runs()
			sessions := s.terminal.load()
			if len(runs) != 1 || len(sessions) != 1 || sessions[0].Kind != owner || !sessions[0].Started {
				t.Fatalf("wrong dispatch: runs=%+v sessions=%+v", runs, sessions)
			}
			brief, err := os.ReadFile(sessions[0].BoardBrief)
			if err != nil || !strings.Contains(string(brief), reviewedPlan) {
				t.Fatalf("missing reviewed plan: %v %s", err, brief)
			}
			// A later owner edit must not change the already dispatched order.
			if err := s.saveTaskPlanVersion(task, "LATER_PLAN: a separate owner revision", first.Head); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(sessions[0].BoardBrief)
			if err != nil || !bytes.Equal(brief, after) {
				t.Fatal("dispatched plan changed", err)
			}
			endpoint := "/api/agents/chat/alfred/sessions/" + id
			results := func() []any {
				t.Helper()
				code, out := agentChatJSON(t, s, "GET", endpoint, nil)
				if code != 200 {
					t.Fatal(code, out)
				}
				return out["codingResults"].([]any)
			}
			if got := results(); len(got) != 0 {
				t.Fatal("running work presented as a result", got)
			}
			const deliverable = "DELIVERED_RESULT: stale writes rejected; fixture validation passed."
			body, err := json.Marshal(codingResult{Status: "completed", Summary: deliverable})
			if err != nil {
				t.Fatal(err)
			}
			if err := boardWrite(filepath.Join(filepath.Dir(sessions[0].BoardBrief), "result.json"), body); err != nil {
				t.Fatal(err)
			}
			// Reopen durable chat and terminal state before ingesting the result.
			// The server itself and the record stores remain fixture instances.
			s.UseAgentChat(agentchat.New(chatDir))
			s.UseTerminal(s.terminal.regPath, s.terminal.tmuxTmp, s.terminal.defaultWd)
			index := s.delegationIndex()
			if index[task].State != "done" {
				t.Fatal("durable result not recovered", index[task])
			}
			s.agentLoopSweep(index)
			s.agentLoopSweep(s.delegationIndex())
			got := results()
			if len(got) != 1 {
				t.Fatal("result missing or duplicated", got)
			}
			result := got[0].(map[string]any)
			if result["agent"] != owner || result["id"] != runs[0].ID || result["task"] != task || result["outcome"] != "completed" || !strings.Contains(result["body"].(string), deliverable) {
				t.Fatal("wrong producing run or result", result)
			}
			capture := map[string]any{"agent": owner, "run": runs[0].ID, "hash": result["hash"]}
			code, ref := agentChatJSON(t, s, "POST", endpoint+"/coding-result", capture)
			if code != 200 {
				t.Fatal(code, ref)
			}
			// Repeating a lost capture acknowledgment resolves the same version.
			code, retry := agentChatJSON(t, s, "POST", endpoint+"/coding-result", capture)
			if code != 200 || retry["id"] != ref["id"] || retry["revision"] != ref["revision"] {
				t.Fatal("capture retry changed identity", code, retry, ref)
			}
			artifact, ok := s.artifactReg.Get(ref["id"].(string))
			if !ok || artifact.Provenance.Run != runs[0].ID || artifact.Provenance.Task != task || artifact.Provenance.Source != "run" || len(artifact.Revisions) != 1 {
				t.Fatal("missing provenance or duplicate revision", artifact)
			}
			context, err := s.taskArtifactContext(task, []artifactContextRef{{ID: artifact.ID, Revision: ref["revision"].(string)}})
			if err != nil || !strings.Contains(context, deliverable) {
				t.Fatal("captured result unavailable for discussion", context, err)
			}
			_, transcript, _, _ := s.agentChat.store.Get("alfred", id)
			if transcript != originalTranscript {
				t.Fatal("coding result rewrote planning conversation")
			}
			currentTasks, err := os.ReadFile(s.tasksStore.Path())
			if err != nil || !bytes.Equal(originalTasks, currentTasks) {
				t.Fatal("finished result changed task completion or created a task", err, string(currentTasks))
			}
			if got := s.readPlanRecord(task).Plan; !strings.Contains(got, "LATER_PLAN") {
				t.Fatal("coding result replaced owner's current plan", got)
			}
			count := 0
			for _, entry := range s.listThread(task) {
				if entry.Author == "agent:"+owner && strings.Contains(entry.Text, "result delivered") {
					count++
				}
			}
			if count != 1 {
				t.Fatal("result receipt duplicated", count)
			}
		})
	}
}
