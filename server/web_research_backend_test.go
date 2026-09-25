package server

import (
	"context"
	"encoding/json"
	"manifest/artifacts"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestResearchRevisionBrowserWithBackend(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
		t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
	}
	s, _, _ := artifactFixture(t)
	s.UseChatState(t.TempDir())
	const original = "# Research brief\n\nOriginal findings with a source."
	const revised = "# Research brief\n\nVerified findings and limitations."
	created, err := s.artifactReg.Put(artifacts.Put{Ref: "research.md", Title: "Research brief", Content: []byte(original), Actor: "alfred"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	config, err := json.Marshal(map[string]string{"url": server.URL, "id": created.Artifact.ID, "first": created.Artifact.Head, "second": artifacts.Hash([]byte(revised))})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "testdata/chat-research-revision-browser.cjs", string(config))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("research browser with backend: %v\n%s", err, out)
	}
	got, ok := s.artifactReg.Get(created.Artifact.ID)
	if !ok || got.Head != artifacts.Hash([]byte(revised)) || len(got.Revisions) != 2 {
		t.Fatalf("save retry or draft conflict changed artifact history: %+v", got)
	}
	for hash, want := range map[string]string{created.Artifact.Head: original, got.Head: revised} {
		content, err := s.artifactReg.Content(hash)
		if err != nil || string(content) != want {
			t.Fatalf("immutable content %s: %q, %v", hash, content, err)
		}
	}
	draft, err := s.chatState.Read("artifact-"+created.Artifact.ID, "edit")
	if err != nil {
		t.Fatal(err)
	}
	var value struct{ Text, BaseRevision string }
	if err := json.Unmarshal(draft.Value, &value); err != nil {
		t.Fatal(err)
	}
	if value.Text != "Phone research draft: verify the sample size." || value.BaseRevision != got.Head {
		t.Fatalf("explicit conflict resolution was not persisted: %+v", value)
	}
	conversation, err := s.chatState.Read("conversation-"+strings.Repeat("a", 32), "draft")
	if err != nil {
		t.Fatal(err)
	}
	var pending struct {
		Text      string
		Selection struct{ ID, Revision string }
	}
	if err := json.Unmarshal(conversation.Value, &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Text != "Review the revised brief before applying it." || pending.Selection.ID != got.ID || pending.Selection.Revision != got.Head {
		t.Fatalf("conversation draft lost exact context: %+v", pending)
	}
	workspace, err := s.chatState.Read("conversation-"+strings.Repeat("a", 32), "workspace")
	if err != nil {
		t.Fatal(err)
	}
	var layout struct {
		Tabs []struct{ Spec struct{ ID string } }
	}
	if err := json.Unmarshal(workspace.Value, &layout); err != nil {
		t.Fatal(err)
	}
	if len(layout.Tabs) != 1 || layout.Tabs[0].Spec.ID != got.ID {
		t.Fatalf("workspace did not retain the artifact tab: %+v", layout)
	}
}
