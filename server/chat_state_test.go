package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStateHTTPRequiresRevisionAndReportsConflict(t *testing.T) {
	s := New(nil, nil, nil)
	s.UseChatState(t.TempDir())
	path := "/api/chat/state/conversation-0123456789abcdef0123456789abcdef/draft"
	call := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := call("PUT", `{"value":{"text":"draft"}}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call("PUT", `{"revision":0,"value":{"text":"draft"}}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("PUT", `{"revision":0,"value":{"text":"stale"}}`); w.Code != 409 || !strings.Contains(w.Body.String(), `"draft"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", ""); w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestPortalCannotAccessOwnerDrafts(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"conversation-0123456789abcdef0123456789abcdef", "landing-0123456789abcdef0123456789abcdef", "inbox"} {
		slot := "draft"
		if key == "inbox" {
			slot = "pins"
		}
		for _, slot := range []string{slot, "deliveries"} {
			for _, method := range []string{"GET", "PUT"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(method, "/api/chat/state/"+key+"/"+slot, strings.NewReader(`{"revision":0,"value":{"text":"private"}}`)))
				if w.Code == 200 {
					t.Fatal("portal exposed private draft", method)
				}
			}
		}
	}
}
