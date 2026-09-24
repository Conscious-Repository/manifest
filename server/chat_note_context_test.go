package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/artifacts"
	"manifest/vaultindex"
)

func noteContextIndex(t *testing.T, s *Server) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"one/same.md": "---\naliases: [contextneedle]\n---\nEXACT_NOTE_OLD\r\n", "two/same.md": "different note", "system/secret.md": "private system", "extrinsic/import.md": "imported", "Agents/agent.md": "generated"}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	s.UseIndex(ix)
	return root
}
func TestNoteContextIdentityPreviewAndRetention(t *testing.T) {
	s, _, _ := artifactFixture(t)
	root := noteContextIndex(t, s)
	call := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		b, _ := json.Marshal(body)
		return artifactsDo(t, s, method, path, string(b))
	}
	code, found := call("GET", "/api/chat/notes?q=same", nil)
	if code != 200 || len(found["notes"].([]any)) != 2 {
		t.Fatal(code, found)
	}
	code, found = call("GET", "/api/chat/notes?q=contextneedle", nil)
	if code != 200 || len(found["notes"].([]any)) != 1 {
		t.Fatal(code, found)
	}
	code, found = call("GET", "/api/chat/notes", nil)
	if code != 200 || len(found["notes"].([]any)) != 2 {
		t.Fatal("knowledge boundary", code, found)
	}
	for _, path := range []string{"../escape.md", "system/secret.md", "extrinsic/import.md", "Agents/agent.md", "missing.md"} {
		if code, _ := call("GET", "/api/chat/notes/preview?path="+url.QueryEscape(path), nil); code == 200 {
			t.Fatal(path)
		}
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "private.md"), filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("GET", "/api/chat/notes/preview?path=link.md", nil); code == 200 {
		t.Fatal("symlink")
	}
	code, note := call("GET", "/api/chat/notes/preview?path=one/same.md", nil)
	if code != 200 || note["content"] != "---\naliases: [contextneedle]\n---\nEXACT_NOTE_OLD\r\n" {
		t.Fatal(code, note)
	}
	if len(s.artifactReg.List(artifacts.Filter{})) != 0 {
		t.Fatal("browsing retained context")
	}
	req := map[string]any{"path": "one/same.md", "revision": note["revision"]}
	if err := os.WriteFile(filepath.Join(root, "one/same.md"), []byte("new bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("POST", "/api/chat/notes/retain", req); code != 409 {
		t.Fatal("stale preview", code)
	}
	if len(s.artifactReg.List(artifacts.Filter{})) != 0 {
		t.Fatal("stale request retained context")
	}
	_, note = call("GET", "/api/chat/notes/preview?path=one/same.md", nil)
	req["revision"] = note["revision"]
	code, retained := call("POST", "/api/chat/notes/retain", req)
	if code != 200 {
		t.Fatal(code, retained)
	}
	code, retry := call("POST", "/api/chat/notes/retain", req)
	if code != 200 || retry["id"] != retained["id"] || len(s.artifactReg.List(artifacts.Filter{})) != 1 {
		t.Fatal("retention retry duplicated", code, retry)
	}
	a, _ := s.artifactReg.Get(retained["id"].(string))
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	if _, err := s.selectedArtifactContext(false, "", "", refs, nil); err == nil {
		t.Fatal("implicit context accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "one/same.md"), []byte("LATEST_SOURCE"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, err := s.selectedArtifactContext(true, "", "", refs, nil)
	if err != nil || !strings.Contains(ctx, "new bytes") || strings.Contains(ctx, "LATEST_SOURCE") {
		t.Fatal(ctx, err)
	}

	// A later generic artifact revision cannot change a retention retry's
	// reviewed reference, even if an owner changed the registry separately.
	if _, err := s.artifactReg.Put(artifacts.Put{ID: a.ID, Content: []byte("unreviewed registry revision")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one/same.md"), []byte("new bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	code, retry = call("POST", "/api/chat/notes/retain", req)
	if code != 200 || retry["revision"] != a.Head {
		t.Fatal("retry moved to latest registry bytes", code, retry)
	}
	links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]
	if len(links) != 1 || links[0].Kind != "note" || links[0].ID != "one/same.md" {
		t.Fatal(links)
	}
	w := httptest.NewRecorder()
	s.handleChatNoteSearch(w, httptest.NewRequest("GET", "/api/chat/notes?q=same", nil))
	if w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Header())
	}
}

func TestNoteContextPrivateDelivery(t *testing.T) {
	prompt := filepath.Join(t.TempDir(), "prompt")
	s, st, _ := agentChatFixture(t, strings.Replace(echoStub, "sleep 0.3", `printf '%s' "$prompt" > '`+prompt+`'`, 1))
	explicitArtifactFixture(t, s)
	root := noteContextIndex(t, s)
	code, note := artifactsDo(t, s, "GET", "/api/chat/notes/preview?path=one/same.md", "")
	if code != 200 {
		t.Fatal(code, note)
	}
	body, _ := json.Marshal(map[string]any{"path": "one/same.md", "revision": note["revision"]})
	code, ref := artifactsDo(t, s, "POST", "/api/chat/notes/retain", string(body))
	if code != 200 {
		t.Fatal(code, ref)
	}
	if err := os.WriteFile(filepath.Join(root, "one/same.md"), []byte("NOT_REVIEWED"), 0644); err != nil {
		t.Fatal(err)
	}
	id, err := st.Create("alfred", "", "Knowledge discussion", "")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"text": "Discuss this note", "requestId": "note-context-send-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}}
	endpoint := "/api/agents/chat/alfred/sessions/" + id + "/messages"
	if code, out := agentChatJSON(t, s, "POST", endpoint, payload); code != 200 {
		t.Fatal(code, out)
	}
	sess := waitIdle(t, st, "alfred", id)
	raw, err := os.ReadFile(prompt)
	if err != nil || !strings.Contains(string(raw), "EXACT_NOTE_OLD\r\n") || strings.Contains(string(raw), "NOT_REVIEWED") {
		t.Fatal(string(raw), err)
	}
	if !strings.Contains(string(raw), `source-note="one/same.md"`) {
		t.Fatal("source identity missing", string(raw))
	}
	if sess.Task != "" || len(sess.Deliveries) != 1 || sess.Deliveries[0].Context.Artifacts[0].Revision != ref["revision"] {
		t.Fatal(sess)
	}
	if code, out := agentChatJSON(t, s, "POST", endpoint, payload); code != 200 {
		t.Fatal(code, out)
	}
	sess = waitIdle(t, st, "alfred", id)
	if len(sess.Deliveries) != 1 {
		t.Fatal("retry created another delivery")
	}
}

func TestNoteContextBounds(t *testing.T) {
	s, _, _ := artifactFixture(t)
	root := noteContextIndex(t, s)
	for _, body := range []string{"", strings.Repeat("a", 64001), "invalid\x00text", "invalid\xfftext"} {
		if err := os.WriteFile(filepath.Join(root, "one/same.md"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		if code, _ := artifactsDo(t, s, "GET", "/api/chat/notes/preview?path=one/same.md", ""); code == 200 {
			t.Fatalf("unsupported text accepted (%d bytes)", len(body))
		}
	}
	if code, _ := artifactsDo(t, s, "GET", "/api/chat/notes?q="+strings.Repeat("a", 257), ""); code != 400 {
		t.Fatal(code)
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/chat/notes/retain", strings.NewReader(`{"path":"one/same.md","revision":"`+strings.Repeat("a", 64)+`"}`)))
	if w.Code == 200 {
		t.Fatal("portal exposed retention")
	}
}
