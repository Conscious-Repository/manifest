package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// The inbox snapshot composes the rail's lists in one body: every part is
// present (null where a subsystem is not configured, so the client's
// fallbacks apply), and the agent map follows the roster.
func TestChatInboxSnapshot(t *testing.T) {
	srv, _ := panelFixture(t)
	w := httptest.NewRecorder()
	srv.handleChatInbox(w, httptest.NewRequest("GET", "/api/chat/inbox", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"at", "roster", "agents", "spirits", "terminal", "state", "review", "taskThreads"} {
		if _, ok := out[k]; !ok {
			t.Fatalf("missing %q in %s", k, w.Body.String())
		}
	}
	var terminal struct {
		Sessions []any `json:"sessions"`
		Enabled  bool  `json:"enabled"`
	}
	if err := json.Unmarshal(out["terminal"], &terminal); err != nil || terminal.Enabled {
		t.Fatalf("terminal part = %s (%v)", out["terminal"], err)
	}
	var threads struct{ Threads []any }
	if err := json.Unmarshal(out["taskThreads"], &threads); err != nil {
		t.Fatalf("taskThreads part = %s", out["taskThreads"])
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(out["state"], &state); err != nil || len(state) != 4 {
		t.Fatalf("state part = %s", out["state"])
	}
	if string(state["pins"]) != "null" {
		t.Fatalf("no chat state store: pins should be null, got %s", state["pins"])
	}
}
