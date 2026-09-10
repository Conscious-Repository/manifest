package server

import (
	"manifest/agentchat"
	"manifest/artifacts"
	"path/filepath"
	"testing"
)

func TestSnapshotOnwardHandoffUsesOnlyInheritedExactVersion(t *testing.T) {
	for _, nativeSource := range []bool{false, true} {
		for _, nativeTarget := range []bool{false, true} {
			s, st, _ := agentChatFixture(t, echoStub)
			ws, _ := workspaceFixture(t)
			s.artifactReg = ws.artifactReg
			s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}
			put := artifacts.Put{Kind: artifacts.KindReport, Title: "Changes", Ref: "artifacts/runtime/original/changes.diff", Content: []byte("first snapshot"), Provenance: artifacts.Provenance{Source: "runtime-changes", Session: "original-scope"}}
			first, err := s.artifactReg.Put(put)
			if err != nil {
				t.Fatal(err)
			}
			ref := artifactContextRef{ID: first.Artifact.ID, Revision: first.Artifact.Head}
			put.Content = []byte("later snapshot")
			later, err := s.artifactReg.Put(put)
			if err != nil {
				t.Fatal(err)
			}
			origin := agentchat.Origin{Agent: "codex", ID: "aaaabbbbccccdddd", Backend: "terminal", Artifacts: []artifactContextRef{ref}}
			id, err := st.CreateRelatedOnce("alfred", "", "Inherited snapshot", "", "source-request", origin)
			if err != nil {
				t.Fatal(err)
			}
			endpoint := "/api/agents/chat/alfred/sessions/" + id + "/related"
			if nativeSource {
				s.terminal.upsert(termSession{ID: "abcdef123456", Kind: "codex", Backend: "herdr", LaunchPhase: "draft", Origin: &origin})
				endpoint = "/api/terminal/codex/session/abcdef123456/related"
			}
			payload := map[string]any{"agent": "alfred", "requestId": "onward-valid", "artifacts": []artifactContextRef{ref}}
			if nativeTarget {
				payload["backend"] = "terminal"
				payload["agent"] = "claude"
			}
			code, out := agentChatJSON(t, s, "POST", endpoint, payload)
			if code != 200 {
				t.Fatalf("native source=%v target=%v: %d %v", nativeSource, nativeTarget, code, out)
			}
			childID := out["id"].(string)
			var received *agentchat.Origin
			if nativeTarget {
				child, _ := s.terminal.find(childID)
				received = child.Origin
			} else {
				child, _, _, _ := st.Get("alfred", childID)
				received = child.Origin
			}
			if received == nil || len(received.Artifacts) != 1 || received.Artifacts[0] != ref {
				t.Fatal("lost exact selection", received)
			}
			payload["requestId"] = "onward-unshared-version"
			payload["artifacts"] = []artifactContextRef{{ID: later.Artifact.ID, Revision: later.Artifact.Head}}
			if code, out := agentChatJSON(t, s, "POST", endpoint, payload); code != 400 {
				t.Fatal("unshared revision accepted", code, out)
			}
			payload["requestId"] = "onward-no-selection"
			delete(payload, "artifacts")
			code, out = agentChatJSON(t, s, "POST", endpoint, payload)
			if code != 200 {
				t.Fatal(code, out)
			}
			childID = out["id"].(string)
			if nativeTarget {
				child, _ := s.terminal.find(childID)
				received = child.Origin
			} else {
				child, _, _, _ := st.Get("alfred", childID)
				received = child.Origin
			}
			if len(received.Artifacts) != 0 {
				t.Fatal("automatically forwarded inherited artifacts")
			}
		}
	}
}
