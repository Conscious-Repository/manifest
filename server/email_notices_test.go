package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPortalCannotAccessPrivateEmailNotices(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/manifest/operations/sha256:" + strings.Repeat("a", 64) + "/email-receipt", ""},
		{"POST", "/api/manifest/operations/sha256:" + strings.Repeat("a", 64) + "/email-watch", `{"enabled":true}`},
		{"POST", "/api/manifest/operations/sha256:" + strings.Repeat("a", 64) + "/email-reconcile", `{}`},
		{"POST", "/api/agents/chat/alfred/sessions/private/interrupt", `{"requestId":"private-request"}`},
		{"POST", "/api/portals/item/dismiss", `{"id":"email-reply:private"}`},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code < 400 {
			t.Fatal("public portal admitted private email action", tc.path, w.Code)
		}
	}
}
