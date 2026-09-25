package server

import (
	"manifest/agentchat"
	"manifest/artifacts"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedNativeOutputDeliveredToSourceWithoutTask(t *testing.T) {
	promptPath := filepath.Join(t.TempDir(), "prompt")
	s, st, _ := agentChatFixture(t, strings.Replace(echoStub, "sleep 0.3", `printf '%s' "$prompt" > '`+promptPath+`'`, 1))
	ws, _ := workspaceFixture(t)
	s.UseArtifactRegistry(ws.artifactReg)
	id, err := st.Create("alfred", "", "Research", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Accept("alfred", id, "original-output-001", "Research", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Claim("alfred", id); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := st.Finish("alfred", id, "original-output-001", "alfred", "### Step 1 — say\n\nORIGINAL_SELECTED_FINDINGS", agentchat.DeliveryCompleted, "", 0); err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := st.Get("alfred", id)
	out := chatOutputs(sess, body)[0]
	base := "/api/agents/chat/alfred/sessions/" + id
	code, ref := agentChatJSON(t, s, "POST", base+"/output", map[string]any{"delivery": out.Delivery, "hash": out.Hash})
	if code != 200 {
		t.Fatal(code, ref)
	}
	refs := []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}
	if _, err := s.artifactReg.Put(artifacts.Put{ID: refs[0].ID, Content: []byte("LATER_UNSELECTED_FINDINGS")}); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"requestId": "discuss-output-001", "text": "Revise the selected findings", "artifacts": refs}
	other, err := st.Create("alfred", "", "Unrelated", "")
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+other+"/messages", request); code != 400 {
		t.Fatal("unrelated conversation accepted output", code)
	}
	if code, reply := agentChatJSON(t, s, "POST", base+"/messages", request); code != 200 {
		t.Fatal(code, reply)
	}
	finished := waitIdle(t, st, "alfred", id)
	last := finished.Deliveries[len(finished.Deliveries)-1]
	if last.State != agentchat.DeliveryCompleted || last.Context.Task != "" || last.Context.ExplicitArtifacts || len(last.Context.Artifacts) != 1 || last.Context.Artifacts[0] != refs[0] {
		t.Fatal(last)
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil || !strings.Contains(string(prompt), refs[0].Revision) || !strings.Contains(string(prompt), "ORIGINAL_SELECTED_FINDINGS") || strings.Contains(string(prompt), "LATER_UNSELECTED_FINDINGS") {
		t.Fatal("wrong selected context", string(prompt), err)
	}
	if code, _ := agentChatJSON(t, s, "POST", base+"/messages", request); code != 200 {
		t.Fatal("retry rejected", code)
	}
	if got := waitIdle(t, st, "alfred", id); len(got.Deliveries) != 2 {
		t.Fatal("retry duplicated work", got.Deliveries)
	}
}

func TestOutputScopePreservesContinuedSourceAndExactHandoff(t *testing.T) {
	s, _, _ := artifactFixture(t)
	source := agentchat.Session{Agent: "alfred", ID: "20260925-100000-abcd", Origin: &agentchat.Origin{Mode: "continue", Backend: "terminal", Agent: "codex", ID: "aaaabbbbccccdddd"}}
	key := sessionConversation(source).Key
	a, err := s.artifactReg.Put(artifacts.Put{Ref: "output.md", Content: []byte("first"), Provenance: artifacts.Provenance{Source: "chat-output", Session: key}})
	if err != nil {
		t.Fatal(err)
	}
	refs := []artifactContextRef{{ID: a.Artifact.ID, Revision: a.Artifact.Head}}
	if _, err := s.selectedArtifactContext(false, "", privateArtifactScope(source), refs, nil, key); err != nil {
		t.Fatal("continued source lost own output", err)
	}
	if _, err := s.selectedArtifactContext(false, "", "unrelated", refs, nil); err == nil {
		t.Fatal("unrelated scope accepted")
	}
	if _, err := s.selectedArtifactContext(false, "", "child", refs, refs); err != nil {
		t.Fatal("explicit handoff rejected", err)
	}
	newer, err := s.artifactReg.Put(artifacts.Put{ID: a.Artifact.ID, Content: []byte("later")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.selectedArtifactContext(false, "", "child", []artifactContextRef{{ID: a.Artifact.ID, Revision: newer.Artifact.Head}}, refs); err == nil {
		t.Fatal("handoff granted unselected revision")
	}
}
