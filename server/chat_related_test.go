package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRelatedChatCreatesUnsentDraftAndBidirectionalLinks(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, err := st.Create("alfred", "", "Source", "")
	if err != nil {
		t.Fatal(err)
	}
	st.AppendTurn("alfred", id, "user", "original", 0)
	call := func(payload map[string]any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(payload)
		r := httptest.NewRequest("POST", "/api/agents/chat/alfred/sessions/"+id+"/related", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	payload := map[string]any{"agent": "alfred", "title": "Related", "prompt": "Reviewed context", "requestId": "related-http-001"}
	w := call(payload)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &out)
	child, body, queue, ok := st.Get("alfred", out.ID)
	if !ok || child.Status != "idle" || child.Turns != 0 || body != "" || len(queue) != 0 || len(child.Deliveries) != 0 {
		t.Fatal(child, body)
	}
	parent, parentBody, _, _ := st.Get("alfred", id)
	if parent.Turns != 1 || !bytes.Contains([]byte(parentBody), []byte("original")) {
		t.Fatal(parent)
	}
	if len(s.relatedChats(parent)) != 1 || len(s.relatedChats(child)) != 1 {
		t.Fatal("missing bidirectional links")
	}
	w = call(payload)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var retry struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &retry)
	if retry.ID != out.ID {
		t.Fatal("retry created another chat")
	}
	payload["prompt"] = "different"
	if w = call(payload); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	payload["task"] = "inbox/unrelated"
	payload["requestId"] = "related-http-002"
	if w = call(payload); w.Code != 400 {
		t.Fatal("unrelated task accepted", w.Code)
	}
}
