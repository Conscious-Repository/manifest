package server

import (
	"encoding/json"
	"manifest/chatthreads"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func screenShared(s *Server, thread, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("thread", thread)
	r.SetPathValue("terminal", id)
	w := httptest.NewRecorder()
	s.AionChatTerminalScreen(w, r)
	return w
}
func TestSharedScreenUsesExactConsentAndRuntime(t *testing.T) {
	s, se, thread, sends := sharedInputFixture(t, false)
	w := screenShared(s, thread, se.ID)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "not-started") {
		t.Fatal(w.Code, w.Body.String())
	}
	if sends.Load() != 0 {
		t.Fatal("reading screen started agent")
	}
	for _, pair := range [][2]string{{thread, "not-shared"}, {"wrong-thread", se.ID}} {
		if w := screenShared(s, pair[0], pair[1]); w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	started := postSharedInput(s, se, thread, "member@aion.bio", `{"text":"fixture","requestId":"receipt-input-001"}`)
	if started.Code != 200 {
		t.Fatal(started.Code, started.Body.String())
	}
	w = screenShared(s, thread, se.ID)
	var screen struct {
		Live  bool     `json:"live"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &screen); err != nil || !screen.Live || strings.Join(screen.Lines, "\n") != "❯" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	if sends.Load() != 1 {
		t.Fatal("screen read sent input")
	}
	if _, err := s.chat.PatchThread(thread, map[string]any{"archived": true}, chatthreads.Identity{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := screenShared(s, thread, se.ID); w.Code != 403 {
		t.Fatal("archived screen exposed", w.Code)
	}
}
func TestPortalAuthenticatesBeforeScreen(t *testing.T) {
	called := false
	p := &portalAPI{opt: PortalOptions{ChatTerminalScreen: func(http.ResponseWriter, *http.Request) { called = true }}}
	w := httptest.NewRecorder()
	p.handleChatTerminalScreen(w, httptest.NewRequest("GET", "/", nil))
	if called || w.Code < 400 {
		t.Fatal("guest reached screen", w.Code)
	}
}
