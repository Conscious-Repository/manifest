package server

import (
	"encoding/json"
	"errors"
	"manifest/artifacts"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s, v := panelFixture(t)
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	s.UseArtifactRegistry(reg)
	return s, v
}
func observePlan(t *testing.T, s *Server, id string) artifactView {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleTaskPlanWorkspace(rr, httptest.NewRequest("GET", "/api/tasks/plan/workspace?id="+id, nil))
	if rr.Code != 200 {
		t.Fatalf("workspace: %d %s", rr.Code, rr.Body)
	}
	var a artifactView
	if err := json.Unmarshal(rr.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	return a
}
func TestChatPlanVersionEditRestoreAndExternalChange(t *testing.T) {
	s, v := workspaceFixture(t)
	id := "aion:plan-test"
	if err := s.writePlanSection("todo-plans", id, "plan", "Original plan"); err != nil {
		t.Fatal(err)
	}
	first := observePlan(t, s, id)
	if err := s.saveTaskPlanVersion(id, "Revised plan", first.Head); err != nil {
		t.Fatal(err)
	}
	second := observePlan(t, s, id)
	if first.ID != second.ID || len(second.Revisions) != 2 {
		t.Fatalf("unstable plan versions: %+v", second)
	}
	if err := s.saveTaskPlanVersion(id, "Stale overwrite", first.Head); !errors.Is(err, errPlanRevision) {
		t.Fatalf("stale write accepted: %v", err)
	}
	if got := s.readPlanRecord(id).Plan; got != "Revised plan" {
		t.Fatal(got)
	}
	raw, err := os.ReadFile(filepath.Join(v, s.todoPlans.rel(id)))
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "Revised plan", "External edit", 1))
	if err := os.WriteFile(filepath.Join(v, s.todoPlans.rel(id)), raw, 0644); err != nil {
		t.Fatal(err)
	}
	third := observePlan(t, s, id)
	if len(third.Revisions) != 3 || third.Content != "External edit\n" {
		t.Fatal(third)
	}
	if err := s.saveTaskPlanVersion(id, first.Content, third.Head); err != nil {
		t.Fatal(err)
	}
	restored := observePlan(t, s, id)
	if len(restored.Revisions) != 4 || restored.Head != first.Head {
		t.Fatalf("restore must append history: %+v", restored)
	}
	bytes, err := s.artifactReg.Content(second.Head)
	if err != nil || string(bytes) != "Revised plan\n" {
		t.Fatal("historical bytes changed")
	}
	if s.readPlanRecord(id).State != "open" {
		t.Fatal("edit changed execution state")
	}
}
func TestChatArtifactContextUsesExactLinkedRevision(t *testing.T) {
	s, _ := workspaceFixture(t)
	id := "aion:context-test"
	if err := s.writePlanSection("todo-plans", id, "plan", "Old bytes"); err != nil {
		t.Fatal(err)
	}
	a := observePlan(t, s, id)
	if err := s.saveTaskPlanVersion(id, "New bytes", a.Head); err != nil {
		t.Fatal(err)
	}
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	text, err := s.taskArtifactContext(id, refs)
	if err != nil || !strings.Contains(text, "Old bytes") || strings.Contains(text, "New bytes") {
		t.Fatalf("wrong context: %s %v", text, err)
	}
	if _, err := s.taskArtifactContext("aion:other", refs); err == nil {
		t.Fatal("unlinked artifact disclosed")
	}
	c, err := s.postAndDispatchContext(id, "comment", "", nil, nil, "Discuss this", refs, text)
	if err != nil {
		t.Fatal(err)
	}
	comments := s.listThread(id)
	if len(comments) != 1 || comments[0].ID != c.ID || comments[0].Meta["context"] == nil {
		t.Fatalf("context lost on team comment: %+v", comments)
	}
	if strings.Contains(comments[0].Text, "Old bytes") {
		t.Fatal("private artifact bytes published in team transcript")
	}
}
func TestChatArtifactRawContentCannotServeHTML(t *testing.T) {
	s, _ := workspaceFixture(t)
	a, err := s.artifactReg.Put(artifacts.Put{Content: []byte("<!doctype html><script>alert(1)</script>")})
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	s.handleArtifactContent(rr, httptest.NewRequest(http.MethodGet, "/api/artifacts/content?id="+a.Artifact.ID, nil))
	if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/plain") || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(rr.Header())
	}
}

func TestPortalCannotReadPrivateArtifactWorkspace(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/artifacts/get?id=0123456789abcdef&content=1", "/api/artifacts/content?id=0123456789abcdef", "/api/tasks/plan/workspace?id=aion:context-test"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code == 200 {
			t.Fatalf("portal exposed private workspace route %s", path)
		}
	}
}
