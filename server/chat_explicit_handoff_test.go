package server

import (
	"manifest/artifacts"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitRelatedArtifactHandoff(t *testing.T) {
	for _, backend := range []string{"planning", "codex", "claude"} {
		t.Run(backend, func(t *testing.T) {
			s, st, _ := agentChatFixture(t, echoStub)
			a := explicitArtifactFixture(t, s)
			second, err := s.artifactReg.Put(artifacts.Put{Ref: "second-context", Content: []byte("SECOND_EXACT_CONTEXT")})
			if err != nil {
				t.Fatal(err)
			}
			s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}
			parent, err := st.Create("alfred", "", "Parent", "")
			if err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{"agent": "alfred", "title": "Selected output follow-up", "mode": "side", "requestId": "explicit-related-001", "artifacts": []artifactContextRef{{ID: a.ID, Revision: a.Head}, {ID: second.Artifact.ID, Revision: second.Artifact.Head}}}
			if backend != "planning" {
				payload["backend"] = "terminal"
				payload["agent"] = backend
			}
			endpoint := "/api/agents/chat/alfred/sessions/" + parent + "/related"
			if code, _ := agentChatJSON(t, s, "POST", endpoint, payload); code == 200 {
				t.Fatal("unlinked handoff accepted without explicit consent")
			}
			payload["explicitArtifacts"] = true
			code, out := agentChatJSON(t, s, "POST", endpoint, payload)
			if code != 200 {
				t.Fatal(code, out)
			}
			childID := out["id"].(string)
			var refs []artifactContextRef
			if backend == "planning" {
				child, body, queued, _ := st.Get("alfred", childID)
				if body != "" || len(queued) != 0 || len(child.Deliveries) != 0 || child.Task != "" || !child.Origin.ExplicitArtifacts {
					t.Fatal(child)
				}
				refs = child.Origin.Artifacts
			} else {
				child, ok := s.terminal.find(childID)
				if !ok || !child.isDraft() || !child.Origin.ExplicitArtifacts || child.Origin.Task != "" {
					t.Fatal(child)
				}
				refs = child.Origin.Artifacts
			}
			newer, err := s.artifactReg.Put(artifacts.Put{ID: a.ID, Content: []byte("NOT_HANDED_OVER")})
			if err != nil {
				t.Fatal(err)
			}
			context, err := s.scopedArtifactContext("", "child", refs, refs)
			if err != nil || len(refs) != 2 || !strings.Contains(context, "EXACT_PRIOR_OUTPUT") || !strings.Contains(context, "SECOND_EXACT_CONTEXT") || strings.Contains(context, "NOT_HANDED_OVER") {
				t.Fatal("retained handoff unavailable", context, err)
			}
			if _, err := s.scopedArtifactContext("", "child", []artifactContextRef{{ID: a.ID, Revision: newer.Artifact.Head}}, refs); err == nil {
				t.Fatal("handoff granted unreviewed revision")
			}
			code, retry := agentChatJSON(t, s, "POST", endpoint, payload)
			if code != 200 || retry["id"] != childID {
				t.Fatal(code, retry)
			}
			payload["explicitArtifacts"] = false
			if code, _ := agentChatJSON(t, s, "POST", endpoint, payload); code != 409 {
				t.Fatal("changed consent reused creation identity", code)
			}
			if backend == "planning" {
				if code, out := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+childID+"/messages", map[string]any{"text": "Use handed-over version", "requestId": "handoff-child-send-001", "artifacts": refs}); code != 200 {
					t.Fatal(code, out)
				}
				waitIdle(t, st, "alfred", childID)
			}
		})
	}
}

func TestExplicitHandoffRejectsSharedTerminalSource(t *testing.T) {
	s, se, _, sends := sharedInputFixture(t, false)
	for _, backend := range []string{"", "terminal"} {
		payload := map[string]any{"agent": "alfred", "mode": "side", "requestId": "private-shared-handoff", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: "0123456789abcdef", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
		if backend != "" {
			payload["backend"] = backend
			payload["agent"] = "codex"
		}
		code, _ := agentChatJSON(t, s, "POST", "/api/terminal/"+se.Kind+"/session/"+se.ID+"/related", payload)
		if code != 403 || sends.Load() != 0 {
			t.Fatal(code, sends.Load())
		}
	}
}
