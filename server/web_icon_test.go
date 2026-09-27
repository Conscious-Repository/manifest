package server

import (
	"net/http/httptest"
	"regexp"
	"testing"
)

// The shell declares its icon, and the icon serves: with no rel="icon" the
// browser asks for /favicon.ico, which is not embedded, and every page load
// logged a 404 on the console (single-chat UI pass, 2026-09-27).
func TestWebShellDeclaresServedIcon(t *testing.T) {
	h := WebHandler()
	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w
	}
	m := regexp.MustCompile(`<link rel="icon"[^>]* href="([^"]+)"`).FindStringSubmatch(get("/").Body.String())
	if m == nil {
		t.Fatal(`the shell declares no <link rel="icon">: browsers fall back to /favicon.ico`)
	}
	if w := get("/" + m[1]); w.Code != 200 {
		t.Fatalf("declared icon %s: %d", m[1], w.Code)
	}
}
