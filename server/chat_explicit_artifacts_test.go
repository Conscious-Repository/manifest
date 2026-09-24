package server

import (
	"encoding/json"
	"manifest/artifacts"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func explicitArtifactFixture(t *testing.T, s *Server) artifacts.Artifact {
	t.Helper()
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	s.UseArtifactRegistry(reg)
	a, err := reg.Put(artifacts.Put{Ref: "prior-output.txt", Content: []byte("EXACT_PRIOR_OUTPUT"), Provenance: artifacts.Provenance{Task: "unrelated-task", Session: "other-conversation"}})
	if err != nil {
		t.Fatal(err)
	}
	return a.Artifact
}
func TestPrivateExplicitArtifactDelivery(t *testing.T) {
	prompt := filepath.Join(t.TempDir(), "prompt")
	s, st, _ := agentChatFixture(t, strings.Replace(echoStub, "sleep 0.3", `printf '%s' "$prompt" > '`+prompt+`'`, 1))
	a := explicitArtifactFixture(t, s)
	id, err := st.Create("alfred", "", "Private target", "")
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"text": "Use this earlier output", "requestId": "explicit-artifact-001", "artifacts": []artifactContextRef{{ID: a.ID, Revision: a.Head}}}
	url := "/api/agents/chat/alfred/sessions/" + id + "/messages"
	if code, _ := agentChatJSON(t, s, "POST", url, body); code == 200 {
		t.Fatal("unlinked context implicitly accepted")
	}
	body["explicitArtifacts"] = true
	if code, out := agentChatJSON(t, s, "POST", url, body); code != 200 {
		t.Fatal(code, out)
	}
	sess := waitIdle(t, st, "alfred", id)
	if sess.Task != "" || !sess.Deliveries[0].Context.ExplicitArtifacts {
		t.Fatal(sess)
	}
	raw, err := os.ReadFile(prompt)
	if err != nil || !strings.Contains(string(raw), "EXACT_PRIOR_OUTPUT") {
		t.Fatal(string(raw), err)
	}
	_, err = s.artifactReg.Put(artifacts.Put{ID: a.ID, Content: []byte("NEWER_NOT_SELECTED")})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := agentChatJSON(t, s, "POST", url, body); code != 200 {
		t.Fatal(code)
	}
	if got := waitIdle(t, st, "alfred", id); got.Turns != 2 || got.Deliveries[0].Context.Artifacts[0].Revision != a.Head {
		t.Fatal(got)
	}
	body["explicitArtifacts"] = false
	if code, _ := agentChatJSON(t, s, "POST", url, body); code != 409 {
		t.Fatal("changed selection authority", code)
	}
}
func TestNativeExplicitArtifactDelivery(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, "EXACT_PRIOR_OUTPUT") {
			t.Error("missing exact context")
		}
	})
	a := explicitArtifactFixture(t, s)
	payload := map[string]any{"text": "Use prior output", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: a.ID, Revision: a.Head}}}
	body, _ := json.Marshal(payload)
	if w := receiptInput(s, se.ID, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
	if err != nil || !receipt.ExplicitArtifacts || receipt.Task != "" || receipt.Artifacts[0].Revision != a.Head {
		t.Fatal(receipt, err)
	}
	restarted := &Server{terminal: &termCfg{regPath: s.terminal.regPath}}
	if w := receiptInput(restarted, se.ID, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if sends.Load() != 1 {
		t.Fatal(sends.Load())
	}
	payload["explicitArtifacts"] = false
	body, _ = json.Marshal(payload)
	if w := receiptInput(restarted, se.ID, string(body)); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestNativeMultipleContextVersions(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		for _, want := range []string{"EXACT_PRIOR_OUTPUT", "SELECTED_PERSON", "SELECTED_TASK"} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %s", want)
			}
		}
		if strings.Contains(text, "UNSELECTED_HEAD") {
			t.Error("delivered newer head")
		}
	})
	a := explicitArtifactFixture(t, s)
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	for _, content := range []string{"SELECTED_PERSON", "SELECTED_TASK"} {
		result, err := s.artifactReg.Put(artifacts.Put{Ref: content, Content: []byte(content)})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, artifactContextRef{ID: result.Artifact.ID, Revision: result.Artifact.Head})
	}
	for _, ref := range refs {
		if _, err := s.artifactReg.Put(artifacts.Put{ID: ref.ID, Content: []byte("UNSELECTED_HEAD")}); err != nil {
			t.Fatal(err)
		}
	}
	payload := map[string]any{"text": "Use the selected context", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": refs}
	// One unavailable reference rejects the entire set before provider execution.
	broken := append([]artifactContextRef{}, refs...)
	broken[2].Revision = strings.Repeat("0", 64)
	payload["artifacts"] = broken
	body, _ := json.Marshal(payload)
	if w := receiptInput(s, se.ID, string(body)); w.Code == 200 || sends.Load() != 0 {
		t.Fatal(w.Code, sends.Load())
	}
	payload["artifacts"] = refs
	body, _ = json.Marshal(payload)
	if w := receiptInput(s, se.ID, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
	if err != nil || len(receipt.Artifacts) != 3 {
		t.Fatal(receipt, err)
	}
	restarted := &Server{terminal: &termCfg{regPath: s.terminal.regPath}}
	if w := receiptInput(restarted, se.ID, string(body)); w.Code != 200 || sends.Load() != 1 {
		t.Fatal(w.Code, sends.Load())
	}
	payload["artifacts"] = refs[:2]
	body, _ = json.Marshal(payload)
	if w := receiptInput(restarted, se.ID, string(body)); w.Code != 409 || sends.Load() != 1 {
		t.Fatal(w.Code, sends.Load())
	}
}
