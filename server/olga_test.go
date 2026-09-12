package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOlgaIsolationAndPlanner(t *testing.T) {
	root := t.TempDir()
	pw := filepath.Join(t.TempDir(), "password")
	os.WriteFile(pw, []byte("test-password"), 0600)
	os.WriteFile(filepath.Join(root, "goals.md"), []byte("# OWNER PRIVATE\n"), 0600)
	h, err := NewOlgaHandler(root, pw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if path == "/login" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/api/goals", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	login := request("POST", "/login", url.Values{"password": {"test-password"}}.Encode(), nil)
	if login.Code != 303 || len(login.Result().Cookies()) != 1 {
		t.Fatal(login)
	}
	c := login.Result().Cookies()[0]
	if w := request("POST", "/login", url.Values{"password": {"wrong-password"}}.Encode(), nil); len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "/?error=1" {
		t.Fatal("incorrect password accepted", w)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie missing protections")
	}
	for _, path := range []string{"/api/agents", "/api/files", "/api/note", "/api/tasks/thread", "/api/tasks/delegate", "/api/chat", "/portal/", "/js/48-chat.js", "/system/olga/goals.md", "/fonts/%2e%2e/portal/data/goals.json", "/icons/%2e%2e/index.html"} {
		if w := request("GET", path, "", c); w.Code != 404 && w.Code != 405 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/goals", "/api/tasks", "/api/day?date=2026-09-12", "/", "/olga.js", "/css/10-day.css"} {
		if w := request("GET", path, "", c); w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("OWNER PRIVATE")) {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := request("POST", "/api/areas", `{"name":"Personal"}`, c); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := request("POST", "/api/goals/item", `{"area":"Personal","horizon":"rocks","text":"Finish a course"}`, c); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w := request("POST", "/api/tasks/item", `{"text":"Read chapter one","domain":"Personal"}`, c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var tasks struct {
		Rows []struct {
			ID string `json:"id"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &tasks)
	if len(tasks.Rows) != 1 {
		t.Fatal(w.Body)
	}
	id := tasks.Rows[0].ID
	w = request("POST", "/api/day/pull?date=2026-09-12", `{"taskId":"`+id+`"}`, c)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = request("POST", "/api/day?date=2026-09-12", `{"schedule":[{"time":"8A","label":"Read"}],"tasks":[{"text":"Read chapter one","taskId":"`+id+`","done":true}]}`, c)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	raw, e := os.ReadFile(filepath.Join(root, "system/olga/daily/2026-09-12.md"))
	if e != nil || !bytes.Contains(raw, []byte("Read")) {
		t.Fatal(e, string(raw))
	}
	raw, _ = os.ReadFile(filepath.Join(root, "system/olga/tasks.md"))
	if !bytes.Contains(raw, []byte("[x]")) {
		t.Fatal(string(raw))
	}
	raw, _ = os.ReadFile(filepath.Join(root, "goals.md"))
	if string(raw) != "# OWNER PRIVATE\n" {
		t.Fatal("owner goals changed")
	}
	r := httptest.NewRequest("POST", "/api/areas", strings.NewReader(`{"name":"bad"}`))
	r.AddCookie(c)
	r.Header.Set("Origin", "https://attacker.example")
	wr := httptest.NewRecorder()
	h.ServeHTTP(wr, r)
	if wr.Code != 403 {
		t.Fatal(wr.Code)
	}
	os.WriteFile(pw, []byte("new-password"), 0600)
	if w := request("GET", "/api/goals", "", c); w.Code != 401 {
		t.Fatal("password rotation did not revoke session")
	}
	os.Remove(pw)
	if w := request("GET", "/", "", c); w.Code != 503 {
		t.Fatal("missing password did not fail closed")
	}
}

func TestOlgaSessionSurvivesRestart(t *testing.T) {
	root, pw := t.TempDir(), filepath.Join(t.TempDir(), "password")
	os.WriteFile(pw, []byte("test-password"), 0600)
	start := func() http.Handler {
		h, e := NewOlgaHandler(root, pw, t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		return h
	}
	request := func(h http.Handler, method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	h := start()
	login := request(h, "POST", "/api/session", "password=test-password", nil)
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatal(login.Code, login.Body)
	}
	cookie := login.Result().Cookies()[0]
	for i := 0; i < 2; i++ {
		h = start()
		if w := request(h, "GET", "/api/session", "", cookie); w.Code != 200 {
			t.Fatal("restart revoked cookie", w.Code)
		}
	}
	info, e := os.Stat(pw + ".session-key")
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("session key permissions", e)
	}
	if w := request(h, "POST", "/api/session", "password=wrong", nil); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("wrong password accepted")
	}
	os.WriteFile(pw, []byte("rotated-password"), 0600)
	if w := request(h, "GET", "/api/session", "", cookie); w.Code != 401 {
		t.Fatal("rotation failed to revoke cookie")
	}
	// A rejected save must never reach the planner mutation handler.
	r := httptest.NewRequest("POST", "/api/areas", strings.NewReader(`{"name":"must-not-exist"}`))
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "system/olga/goals.md"))
	if strings.Contains(string(raw), "must-not-exist") {
		t.Fatal("unauthenticated request mutated data")
	}
	login = request(h, "POST", "/api/session", "password=rotated-password", nil)
	if login.Code != 200 {
		t.Fatal(login.Body)
	}
	if w := request(start(), "GET", "/api/goals", "", login.Result().Cookies()[0]); w.Code != 200 {
		t.Fatal("renewed session did not survive restart")
	}
}
