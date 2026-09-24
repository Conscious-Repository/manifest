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

func TestArtifactTextSaveReceipt(t *testing.T) {
	s, _, _ := artifactFixture(t)
	a, err := s.artifactReg.Put(artifacts.Put{Ref: "file.txt", Content: []byte("before")})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"id":"` + a.Artifact.ID + `","expectedRevision":"` + a.Artifact.Head + `","content":"after","requestID":"request-12345678"}`
	save := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleArtifactText(w, httptest.NewRequest("POST", "/api/artifacts/text", strings.NewReader(body)))
		return w
	}
	first := save(body)
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	var receipt struct {
		SavedRevision, SaveRequestID string
		SavedVersion                 int
	}
	if err := json.Unmarshal(first.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.SaveRequestID != "request-12345678" || receipt.SavedRevision != artifacts.Hash([]byte("after")) || receipt.SavedVersion != 2 {
		t.Fatal(receipt)
	}
	newer, err := s.artifactReg.Put(artifacts.Put{ID: a.Artifact.ID, Content: []byte("newer")})
	if err != nil {
		t.Fatal(err)
	}
	retry := save(body)
	if retry.Code != 200 {
		t.Fatal(retry.Code, retry.Body.String())
	}
	if !strings.Contains(retry.Body.String(), `"savedRevision":"`+receipt.SavedRevision+`"`) {
		t.Fatal(retry.Body.String())
	}
	current, _ := s.artifactReg.Get(a.Artifact.ID)
	if current.Head != newer.Artifact.Head || len(current.Revisions) != 3 {
		t.Fatal(current)
	}
	if w := save(strings.Replace(body, `"after"`, `"altered"`, 1)); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
