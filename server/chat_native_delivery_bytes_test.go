package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/threads"
)

// Row 2's Manifest-side half (the live half is hermes_live_test.go): what a
// native Hermes chat hands the runner. A multi-KB instruction at the 24000
// limit arrives byte-exact; one byte more is refused in words before anything
// is dispatched; an owned file and a thread attachment both reach the runner.
func TestNativeChatInstructionAndAttachmentsReachRunnerExactly(t *testing.T) {
	dir := t.TempDir()
	stub := strings.Replace(echoStub, "sleep 0.3", `[ -n "$prompt" ] && printf '%s' "$prompt" > '`+dir+`'/prompt.$(ls '`+dir+`' | wc -l)`, 1)
	s, st, _ := agentChatFixture(t, stub)
	s.UseChatState(t.TempDir())
	id, err := st.Create("alfred", "", "Exact bytes", "")
	if err != nil {
		t.Fatal(err)
	}
	url := "/api/agents/chat/alfred/sessions/" + id + "/messages"
	prompts := func() []string {
		entries, _ := os.ReadDir(dir)
		out := []string{}
		for _, e := range entries {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			out = append(out, string(b))
		}
		return out
	}

	// 24000 bytes exactly: distinct lines so a truncation or reordering shows.
	var b strings.Builder
	for i := 0; b.Len() < agentChatMaxChars; i++ {
		fmt.Fprintf(&b, "line %05d of the exact instruction.\n", i)
	}
	nonce := make([]byte, 8)
	rand.Read(nonce)
	tail := " END-" + hex.EncodeToString(nonce)
	instruction := b.String()[:agentChatMaxChars-len(tail)] + tail
	if len(instruction) != agentChatMaxChars {
		t.Fatalf("fixture is %d bytes", len(instruction))
	}
	if code, out := agentChatJSON(t, s, "POST", url, map[string]any{"text": instruction, "requestId": "exact-bytes-0001"}); code != 200 {
		t.Fatal(code, out)
	}
	waitIdle(t, st, "alfred", id)
	got := prompts()
	if len(got) != 1 || !strings.Contains(got[0], instruction) {
		t.Fatalf("the 24000-byte instruction did not reach the runner byte-exact (%d prompts)", len(got))
	}

	// One byte over: refused in words, nothing dispatched, nothing recorded.
	sess, _, _, _ := st.Get("alfred", id)
	code, out := agentChatJSON(t, s, "POST", url, map[string]any{"text": instruction + "x", "requestId": "exact-bytes-0002"})
	if code != 400 || len(prompts()) != 1 {
		t.Fatalf("over-limit: %d %v, %d prompts", code, out, len(prompts()))
	}
	if after, _, _, _ := st.Get("alfred", id); after.Turns != sess.Turns || len(after.Deliveries) != len(sess.Deliveries) {
		t.Fatalf("a refused message left a turn or delivery: %+v", after)
	}
	over, _ := json.Marshal(map[string]string{"text": instruction + "x", "requestId": "exact-bytes-0003"})
	w := agentChatRaw(t, s, "POST", url, string(over))
	if !strings.Contains(w, "exceeds 24000") || !strings.Contains(w, "attach a file") {
		t.Fatalf("the refusal must say what to do: %q", w)
	}

	// An owned file and a thread attachment on the same native message.
	private, err := threads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.UseThreads(private, nil, nil, nil, "owner@example.test")
	thread, err := private.SaveBlob(strings.NewReader("THREAD_FILE_EXACT_BYTES"), "thread-notes.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	owned := uploadOwned(t, s, "agent:alfred/"+id, "owned-notes.txt", []byte("OWNED_FILE_EXACT_BYTES\n"))
	if code, out := agentChatJSON(t, s, "POST", url, map[string]any{"text": "read both files\n[context-file:: " + owned.ID + "]", "requestId": "exact-bytes-0004",
		"files": []map[string]string{{"hash": thread.Hash, "name": thread.Name}}}); code != 200 {
		t.Fatal(code, out)
	}
	waitIdle(t, st, "alfred", id)
	got = prompts()
	last := got[len(got)-1]
	path := s.ownedFilePath(owned)
	if !strings.Contains(last, "Attached reference file (inspect with file/PDF tools): "+path) {
		t.Fatalf("the owned file did not reach the runner:\n%s", last)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "OWNED_FILE_EXACT_BYTES\n" {
		t.Fatalf("the path handed over does not hold the exact bytes: %q %v", raw, err)
	}
	if !strings.Contains(last, "--- thread-notes.txt (attached file) ---\nTHREAD_FILE_EXACT_BYTES\n--- end thread-notes.txt ---") {
		t.Fatalf("the thread attachment was not inlined verbatim:\n%s", last)
	}
}

// agentChatRaw returns the response body as text (refusals are plain text).
func agentChatRaw(t *testing.T, s *Server, method, path, body string) string {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w.Body.String()
}
