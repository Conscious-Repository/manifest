package server

import (
	"encoding/json"
	"manifest/tasks"
	"manifest/threads"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlannerHomeSharedNotes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	os.MkdirAll(home, 0700)
	write := func(p string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0600)
	}
	write(filepath.Join(home, "tasks.md"), []byte("## Home\n- [ ] Buy paint [todo:: home/paint]\n"))
	create := func(name string) *Server {
		private := filepath.Join(root, name)
		os.MkdirAll(private, 0700)
		write(filepath.Join(private, "tasks.md"), []byte("# Tasks\n## Inbox\n- [ ] Private [todo:: inbox/private]\n"))
		ts := tasks.NewStore(private, "tasks.md", write)
		ts.UseSharedHome(filepath.Join(home, "tasks.md"), write)
		s := &Server{}
		s.UseTasks(ts)
		s.UsePlannerNotes(private, home, name, write)
		return s
	}
	a, b := create("Benjamin"), create("Olga")
	if _, err := a.addPlannerComment("home/paint", "Warm orange", threads.Identity{ID: "Benjamin", Name: "Benjamin"}); err != nil {
		t.Fatal(err)
	}
	if len(b.plannerComments("home/paint")) != 1 {
		t.Fatal("comment not shared")
	}
	a.addPlannerComment("inbox/private", "Secret", threads.Identity{ID: "Benjamin"})
	if len(b.plannerComments("inbox/private")) != 0 {
		t.Fatal("private notes leaked")
	}
	request := func(s *Server, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handlePlannerNotes(w, httptest.NewRequest("POST", "/api/tasks/notes", strings.NewReader(body)))
		return w
	}
	payload := map[string]string{"id": "home/paint", "kind": "description", "description": "Two coats", "revision": noteRevision("")}
	raw, _ := json.Marshal(payload)
	if w := request(a, string(raw)); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if value, ok := b.plannerDescription("home/paint"); !ok || value != "Two coats" {
		t.Fatal(value, ok)
	}
	if w := request(b, string(raw)); w.Code != 409 {
		t.Fatal("stale description accepted", w.Code)
	}
	if w := request(b, `{"id":"../../secret","kind":"comment","comment":"oops"}`); w.Code != 404 {
		t.Fatal("unknown task accepted", w.Code)
	}
}
