package server

import (
	"bytes"
	"encoding/json"
	"manifest/record"
	"manifest/vaultindex"
	"manifest/vaultwriter"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritingAPIExactSaveCreateMoveAndComments(t *testing.T) {
	root := t.TempDir()
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	vw := vaultwriter.New(root).Grant(vaultwriter.Capability{Name: "writing", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorUserAction})
	s := New(nil, nil, nil)
	s.UseVault(vw)
	s.UseIndex(ix)
	s.UseWriting("system/writing")
	handler := s.Handler()
	call := func(method, url string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, url, bytes.NewReader(b))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/api/writing/note", map[string]any{"path": "draft.md", "body": "🌿 selected words"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	get := call("GET", "/api/note?path=draft.md", nil)
	var note map[string]any
	_ = json.Unmarshal(get.Body.Bytes(), &note)
	rev := note["revision"].(string)
	if w := call("PUT", "/api/note", map[string]any{"path": "draft.md", "body": "bad"}); w.Code != 428 {
		t.Fatal("precondition optional", w.Code)
	}
	if w := call("PUT", "/api/note", map[string]any{"path": "draft.md", "body": "bad", "ifRevision": "wrong"}); w.Code != 409 {
		t.Fatal("stale save accepted", w.Code)
	}
	if w := call("POST", "/api/writing/comments", map[string]any{"path": "draft.md", "revision": vaultwriter.Revision(nil), "id": "comment-one", "body": "question", "anchor": map[string]any{"revision": rev, "start": 5, "end": 19, "quote": "selected words"}}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = os.Mkdir(filepath.Join(root, "drafts"), 0755)
	if w := call("POST", "/api/writing/move", map[string]any{"path": "draft.md", "to": "drafts/draft.md", "ifRevision": rev}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	comments := call("GET", "/api/writing/comments?path=drafts/draft.md", nil)
	if comments.Code != 200 || !bytes.Contains(comments.Body.Bytes(), []byte("question")) {
		t.Fatal("comments lost", comments.Body.String())
	}
	if w := call("PUT", "/api/note", map[string]any{"path": "drafts/draft.md", "body": "no final newline", "ifRevision": rev}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(root, "drafts/draft.md"))
	if string(raw) != "no final newline" {
		t.Fatal("bytes normalized", string(raw))
	}
}

func TestWritingBindingSurvivesAssignment(t *testing.T) {
	root := t.TempDir()
	vw := vaultwriter.New(root).Grant(vaultwriter.Capability{Name: "todo-plans", Zone: record.ZoneSystem, Pattern: "system/todo-plans/**", Actor: vaultwriter.ActorUserAction})
	s := New(nil, nil, nil)
	s.UseVault(vw)
	s.UseTaskPlans("system/todo-plans")
	raw := "---\ntodo: task-one\nassignee: agent:alfred\ndocument: \"drafts/one.md\"\nmode: write\ncustom: keep-me\n---\n\n## plan\nPreserve these words.\n"
	if err := vw.WriteCap("todo-plans", s.todoPlans.rel("task-one"), []byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := s.setPlanAssignee("task-one", "agent:codex"); err != nil {
		t.Fatal(err)
	}
	after, err := vw.ReadVaultFile(s.todoPlans.rel("task-one"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != strings.Replace(raw, "assignee: agent:alfred", "assignee: agent:codex", 1) {
		t.Fatal("assignment changed workspace or unknown data", string(after))
	}
	if err := s.relinkWritingTasks("drafts/one.md", "one.md"); err != nil {
		t.Fatal(err)
	}
	if got := s.readPlanRecord("task-one").Document; got != "one.md" {
		t.Fatal("task did not follow move", got)
	}
}

func TestWritingReferencesUseVaultWithoutCollectionSetup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "attention.md"), []byte("# Attention\n\nAttention makes space for discovery."), 0644); err != nil {
		t.Fatal(err)
	}
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err = ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil)
	s.UseVault(vaultwriter.New(root))
	s.UseIndex(ix)
	s.UseWriting("system/writing")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/writing/passages", strings.NewReader(`{"path":"draft.md","text":"attention discovery"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "attention.md") {
		t.Fatalf("vault references: %d %s", w.Code, w.Body.String())
	}
}

func TestNoteRefreshUsesExactCurrentBytes(t *testing.T) {
	root := t.TempDir()
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	s := New(nil, nil, nil)
	s.UseIndex(ix)
	s.UseVault(vaultwriter.New(root))
	get := func(tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/note?path=note.md", nil)
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	file := filepath.Join(root, "note.md")
	if err := os.WriteFile(file, []byte("original\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	first := get("")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag != `"`+vaultwriter.Revision([]byte("original\r\n"))+`"` {
		t.Fatal(first.Code, tag)
	}
	if w := get(tag); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("unchanged note should be empty 304", w.Code)
	}
	// External writers bypass the index. Refresh must see their exact bytes now.
	if err := os.WriteFile(file, []byte("external 🌿"), 0644); err != nil {
		t.Fatal(err)
	}
	changed := get(tag)
	if changed.Code != 200 || changed.Header().Get("ETag") == tag || !strings.Contains(changed.Body.String(), "external 🌿") {
		t.Fatal(changed.Code, changed.Body.String())
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if w := get(changed.Header().Get("ETag")); w.Code != 404 {
		t.Fatal("deleted note hidden by cache", w.Code)
	}
}
