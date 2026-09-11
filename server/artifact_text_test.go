package server

import (
	"encoding/json"
	"manifest/artifacts"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArtifactTextEdit(t *testing.T) {
	s, _, _ := artifactFixture(t)
	a, err := s.artifactReg.Put(artifacts.Put{Ref: "report.md", Content: []byte("before")})
	if err != nil {
		t.Fatal(err)
	}
	save := func(id, head, content string) int {
		payload, _ := json.Marshal(map[string]string{"id": id, "expectedRevision": head, "content": content})
		w := httptest.NewRecorder()
		s.handleArtifactText(w, httptest.NewRequest("POST", "/api/artifacts/text", strings.NewReader(string(payload))))
		return w.Code
	}
	if code := save(a.Artifact.ID, a.Artifact.Head, "after"); code != 200 {
		t.Fatal(code)
	}
	if code := save(a.Artifact.ID, a.Artifact.Head, "stale"); code != 409 {
		t.Fatal(code)
	}
	got, _ := s.artifactReg.Get(a.Artifact.ID)
	if len(got.Revisions) != 2 {
		t.Fatal(got)
	}
	for _, p := range []artifacts.Put{{Ref: "file.pdf", Content: []byte("%PDF")}, {Ref: "plan.md", Content: []byte("plan"), Provenance: artifacts.Provenance{Source: "task-plan"}}, {Ref: "fake.txt", Content: []byte{0, 1, 2}}} {
		blocked, err := s.artifactReg.Put(p)
		if err != nil {
			t.Fatal(err)
		}
		if code := save(blocked.Artifact.ID, blocked.Artifact.Head, "text"); code != 400 {
			t.Fatalf("%s: %d", p.Ref, code)
		}
	}
}
