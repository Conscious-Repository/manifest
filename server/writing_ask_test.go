package server

import (
	"bytes"
	"context"
	"encoding/json"
	"manifest/hermes"
	"manifest/record"
	"manifest/vaultwriter"
	"manifest/writing"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWritingAskDurableAnswerAndDeduplication(t *testing.T) {
	root := t.TempDir()
	raw := []byte("A selected passage with context.")
	os.WriteFile(filepath.Join(root, "draft.md"), raw, 0644)
	vw := vaultwriter.New(root).Grant(vaultwriter.Capability{Name: "writing", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorUserAction}, vaultwriter.Capability{Name: "writing-agent", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorApprovedProposal})
	s := New(nil, nil, nil)
	s.UseVault(vw)
	s.UseWriting("system/writing")
	a := writing.StampAnchor(raw, writing.Anchor{Revision: vaultwriter.Revision(raw), Start: 2, End: 18, Quote: "selected passage"})
	reply := writing.NewReply("question-one", "owner", "What does this mean?")
	_, err := s.writing.Append("draft.md", vaultwriter.Revision(nil), writing.Event{Type: "thread", ID: "thread-one", Anchor: &a, Reply: &reply}, false)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan any, 1)
	finish := make(chan struct{})
	s.writingComplete = func(ctx context.Context, p any) (hermes.AnnotationResult, error) {
		started <- p
		<-finish
		return hermes.AnnotationResult{Reply: "A concise answer.", Model: "fixture", InputTokens: 10, OutputTokens: 4}, nil
	}
	handler := s.Handler()
	call := func(method, url string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, url, bytes.NewReader(b)))
		return w
	}
	body := map[string]string{"path": "draft.md", "thread": "thread-one", "question": "question-one", "id": "ask-one-123"}
	w := call("POST", "/api/writing/ask", body)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	packet := <-started
	p, _ := json.Marshal(packet)
	if !bytes.Contains(p, []byte("context.")) {
		t.Fatal("missing surrounding context", string(p))
	}
	w = call("POST", "/api/writing/ask", body)
	if w.Code != 200 {
		t.Fatal("retry", w.Code)
	}
	w = call("POST", "/api/writing/move", map[string]string{"path": "draft.md", "to": "moved.md", "ifRevision": vaultwriter.Revision(raw)})
	if w.Code != 409 {
		t.Fatal("moved running ask", w.Code)
	}
	w = call("GET", "/api/writing/comments?path=draft.md", nil)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"state":"running"`)) {
		t.Fatal(w.Body.String())
	}
	close(finish)
	deadline := time.Now().Add(3 * time.Second)
	for {
		d, e := s.writing.Read("draft.md")
		if e != nil {
			t.Fatal(e)
		}
		if len(d.Turns) == 1 && d.Turns[0].State == "complete" {
			if len(d.Threads[0].Replies) != 2 || d.Threads[0].Replies[1].Author != "alfred" {
				t.Fatal(d)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("answer not saved")
		}
		time.Sleep(5 * time.Millisecond)
	}
	after, _ := os.ReadFile(filepath.Join(root, "draft.md"))
	if !bytes.Equal(raw, after) {
		t.Fatal("ask mutated prose")
	}
	// Orphaned requests are marked failed on the next read after restart.
	d, _ := s.writing.Read("draft.md")
	turn := writing.Turn{ID: "orphan-123", Thread: "thread-one", Question: "question-one", State: "running"}
	_, err = s.writing.Append("draft.md", d.Revision, writing.Event{Type: "ask", ID: turn.ID, Turn: &turn}, false)
	if err != nil {
		t.Fatal(err)
	}
	w = call("GET", "/api/writing/comments?path=draft.md", nil)
	if !bytes.Contains(w.Body.Bytes(), []byte("interrupted")) {
		t.Fatal(w.Body.String())
	}
}
