package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/record"
	"manifest/vaultwriter"
)

func TestChatProjectsVaultMigrationAndConflict(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	s := New(nil, nil, nil)
	s.UseChatState(cache)
	legacy := json.RawMessage(`{"groups":{"ws-one":"First project"},"members":{"terminal:codex/a":"ws-one"},"future":{"preserve":true}}`)
	if _, err := s.chatState.Write("inbox", "workstreams", 0, legacy); err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(filepath.Join(cache, "inbox-workstreams.json"))
	vw := vaultwriter.New(root).Grant(vaultwriter.Capability{Name: "chat-projects", Zone: record.ZoneSystem, Pattern: "system/workbench/projects.md", Actor: vaultwriter.ActorUserAction})
	s.UseVault(vw)
	if err := s.UseChatProjects("system/workbench"); err != nil {
		t.Fatal(err)
	}
	call := func(method string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(method, "/api/chat/state/inbox/workstreams", bytes.NewReader(b)))
		return w
	}
	get := func() projectSnapshot {
		w := call("GET", nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out projectSnapshot
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := get()
	if !bytes.Contains(first.Value, []byte("First project")) || first.RecordVersion == "" {
		t.Fatal(first)
	}
	rel := filepath.Join(root, "system/workbench/projects.md")
	raw, _ := os.ReadFile(rel)
	raw = append(raw, []byte("\n## Owner notes\n\nNever lose this paragraph.\n")...)
	if err := os.WriteFile(rel, raw, 0600); err != nil {
		t.Fatal(err)
	}
	next := json.RawMessage(`{"groups":{"ws-one":"Renamed project"},"members":{"terminal:codex/a":"ws-one"}}`)
	if w := call("PUT", map[string]any{"revision": first.Revision, "record_version": first.RecordVersion, "value": next}); w.Code != 409 {
		t.Fatal("hand edit overwritten", w.Code, w.Body.String())
	}
	current := get()
	if w := call("PUT", map[string]any{"revision": current.Revision, "value": next}); w.Code != 428 {
		t.Fatal("missing full-record CAS accepted", w.Code)
	}
	if w := call("PUT", map[string]any{"revision": current.Revision, "record_version": current.RecordVersion, "value": next}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, _ = os.ReadFile(rel)
	if !bytes.Contains(raw, []byte("Never lose this paragraph.")) || !bytes.Contains(raw, []byte(`"preserve":true`)) {
		t.Fatal("owner/unknown fields lost", string(raw))
	}
	untouched, _ := os.ReadFile(filepath.Join(cache, "inbox-workstreams.json"))
	if !bytes.Equal(backup, untouched) {
		t.Fatal("legacy backup changed")
	}
	// Restart with a completely empty derived cache: projects remain intact.
	s.UseChatState(t.TempDir())
	if err := s.UseChatProjects("system/workbench"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(get().Value, []byte("Renamed project")) {
		t.Fatal("restart lost projects")
	}
	// A malformed owner edit must never be silently replaced with cache data.
	broken := []byte("# Owner edit\n```manifest-projects\n{broken}\n```\n")
	_ = os.WriteFile(rel, broken, 0600)
	if err := s.UseChatProjects("system/workbench"); err == nil {
		t.Fatal("corruption accepted")
	}
	if w := call("PUT", map[string]any{"revision": current.Revision, "record_version": current.RecordVersion, "value": next}); w.Code == 200 {
		t.Fatal("corruption overwritten")
	}
	after, _ := os.ReadFile(rel)
	if !bytes.Equal(broken, after) {
		t.Fatal("broken source lost")
	}
}

func TestChatProjectsRejectInvalidMembership(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"groups":{},"members":{"chat":"missing"}}`, `{"groups":{"id":" "},"members":{}}`} {
		if validateProjects(json.RawMessage(raw)) == nil {
			t.Fatal("accepted", raw)
		}
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/chat/state/inbox/workstreams", strings.NewReader("")))
	if w.Code == 200 {
		t.Fatal("private projects exposed")
	}
}
