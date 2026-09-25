package server

import (
	"manifest/agentchat"
	"manifest/artifacts"
	"testing"
	"time"
)

func TestContinuedNativeFilesIncludeOwnOutputs(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	ws, _ := workspaceFixture(t)
	s.UseArtifactRegistry(ws.artifactReg)
	id, err := st.CreateRelatedOnce("alfred", "", "Continued research", "", "discovery-source-001", agentchat.Origin{Mode: "continue", Backend: "terminal", Agent: "codex", ID: "aaaabbbbccccdddd"})
	if err != nil {
		t.Fatal(err)
	}
	source, _, _, _ := st.Get("alfred", id)
	logical, own := privateArtifactScope(source), sessionConversation(source).Key
	if logical == own {
		t.Fatal("fixture must exercise distinct scopes")
	}
	put := func(ref, scope, kind string, at time.Time) artifacts.Artifact {
		t.Helper()
		a, err := s.artifactReg.Put(artifacts.Put{Ref: ref, Kind: artifacts.KindReport, Harness: "alfred", Content: []byte("matching findings"), At: at, Provenance: artifacts.Provenance{Source: kind, Session: scope}})
		if err != nil {
			t.Fatal(err)
		}
		return a.Artifact
	}
	when := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	root := put("runtime.md", logical, "runtime-changes", when)
	output := put("output.md", own, "chat-output", when.Add(time.Second))
	put("unrelated.md", "another-conversation", "chat-output", when.Add(2*time.Second))
	put("other-kind.md", own, "manual", when.Add(3*time.Second))
	path := "/api/artifacts?conversation_backend=agent&conversation_agent=alfred&conversation_id=" + id
	code, result := artifactsDo(t, s, "GET", path+"&q=matching&sources=1", "")
	if code != 200 || result["count"] != float64(2) {
		t.Fatal(code, result)
	}
	rows := result["artifacts"].([]any)
	if rows[0].(map[string]any)["id"] != output.ID || rows[1].(map[string]any)["id"] != root.ID {
		t.Fatal("wrong scopes or ordering", rows)
	}
	if code, out := artifactsDo(t, s, "GET", path+"&ref=output.md", ""); code != 200 || out["count"] != float64(1) {
		t.Fatal("ref filter lost", code, out)
	}
	if code, out := artifactsDo(t, s, "GET", path+"&kind=image", ""); code != 200 || out["count"] != float64(0) {
		t.Fatal("kind filter lost", code, out)
	}
	other, err := st.Create("alfred", "", "Unrelated", "")
	if err != nil {
		t.Fatal(err)
	}
	if code, out := artifactsDo(t, s, "GET", "/api/artifacts?conversation_backend=agent&conversation_agent=alfred&conversation_id="+other, ""); code != 200 || out["count"] != float64(0) {
		t.Fatal("unrelated query widened", code, out)
	}
}
