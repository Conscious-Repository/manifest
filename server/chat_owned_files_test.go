package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uploadOwned(t *testing.T, s *Server, owner, name string, data []byte) ownedChatFile {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/chat/files?owner="+url.QueryEscape(owner)+"&name="+url.QueryEscape(name), bytes.NewReader(data))
	w := httptest.NewRecorder()
	s.handleOwnedChatFiles(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		File ownedChatFile `json:"file"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	return result.File
}
func TestChatOwnedFilesLifetimeAndIsolation(t *testing.T) {
	s := &Server{}
	s.UseChatState(t.TempDir())
	f := uploadOwned(t, s, "draft:codex/new", "meta.json", []byte(`{"note":"reference"}`))
	text := "Use this\n[context-file:: " + f.ID + "]"
	context, e := s.ownedChatContext("terminal:codex/one", text)
	if e != nil || !strings.Contains(context, s.ownedFilePath(f)) {
		t.Fatal(context, e)
	}
	b, e := os.ReadFile(s.ownedFilePath(f))
	if e != nil || string(b) != `{"note":"reference"}` {
		t.Fatal("file overwritten by metadata", e)
	}
	if _, e = s.ownedChatContext("terminal:codex/two", text); e == nil {
		t.Fatal("cross-chat context accepted")
	}
	if _, e = s.ownedChatContext("terminal:codex/one", text); e != nil {
		t.Fatal("retry failed", e)
	}
	twin := uploadOwned(t, s, "terminal:codex/two", "meta.json", b)
	r := httptest.NewRequest("PUT", "/", strings.NewReader(`{"revision":0,"value":{"items":{"terminal:codex/one":"deleted"}}}`))
	r.SetPathValue("key", "inbox")
	r.SetPathValue("slot", "lifecycle")
	w := httptest.NewRecorder()
	s.handleChatState(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, e = os.Stat(filepath.Join(s.chatFilesRoot, f.ID)); !os.IsNotExist(e) {
		t.Fatal("deleted chat retained file", e)
	}
	if _, e = os.Stat(s.ownedFilePath(twin)); e != nil {
		t.Fatal("other chat lost identical file", e)
	}
	if _, e = s.ownedChatContext("terminal:codex/one", text); e == nil {
		t.Fatal("deleted context reusable")
	}
}
func TestChatOwnedFilesValidationAndRemove(t *testing.T) {
	s := &Server{}
	s.UseChatState(t.TempDir())
	for _, tt := range []struct {
		name string
		data []byte
	}{{"bad.html", []byte("<html>hi</html>")}, {"fake.pdf", []byte("hello")}, {"empty.txt", nil}, {"large.txt", bytes.Repeat([]byte("a"), chatFileLimit+1)}} {
		r := httptest.NewRequest("POST", "/?owner=draft:test&name="+tt.name, bytes.NewReader(tt.data))
		w := httptest.NewRecorder()
		s.handleOwnedChatFiles(w, r)
		if w.Code < 400 {
			t.Fatal("accepted", tt.name)
		}
	}
	f := uploadOwned(t, s, "draft:test", "../../test.pdf", []byte("%PDF-1.7\nfixture"))
	if f.Name != "test.pdf" {
		t.Fatal(f.Name)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("id", f.ID)
	w := httptest.NewRecorder()
	s.handleOwnedChatFile(w, r)
	if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(w.Code, w.Header())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "private, max-age=86400" {
		t.Fatal("bytes should be cacheable per id:", cc)
	}
	r = httptest.NewRequest("GET", "/?metadata=1", nil)
	r.SetPathValue("id", f.ID)
	w = httptest.NewRecorder()
	s.handleOwnedChatFile(w, r)
	if cc := w.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatal("metadata stays uncached:", cc)
	}
	r = httptest.NewRequest("DELETE", "/", nil)
	r.SetPathValue("id", f.ID)
	w = httptest.NewRecorder()
	s.handleOwnedChatFile(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if _, e := os.Stat(s.ownedFilePath(f)); !os.IsNotExist(e) {
		t.Fatal("draft file retained", e)
	}
}
func TestTerminalOwnedFilesDeliveryAndRetry(t *testing.T) {
	s, se, prompts := terminalReceiptFixture(t, false)
	s.UseChatState(t.TempDir())
	f := uploadOwned(t, s, "draft:claude/new", "screenshot.png", []byte("\x89PNG\r\n\x1a\nfixture"))
	g := uploadOwned(t, s, "draft:claude/new", "reference.pdf", []byte("%PDF-1.7\nfixture"))
	text := fmt.Sprintf("Review both files\n[context-file:: %s]\n[context-file:: %s]", f.ID, g.ID)
	raw, _ := json.Marshal(map[string]string{"text": text, "requestId": "receipt-input-001"})
	for i := 0; i < 2; i++ {
		w := receiptInput(s, se.ID, string(raw))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if prompts.Load() != 1 {
		t.Fatal("replayed attachment send")
	}
	receipt, e := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
	if e != nil || receipt.Text != text {
		t.Fatal("attachment references missing from receipt", e, receipt.Text)
	}
	context, err := s.ownedChatContext("terminal:claude/"+se.ID, text)
	if err != nil || receipt.SubmittedHash != hashTerminalText(text+context) {
		t.Fatal("runtime did not receive exact attachment paths", err)
	}
	for _, file := range []ownedChatFile{f, g} {
		saved, e := s.ownedFile(file.ID)
		if e != nil || saved.Owner != "terminal:claude/"+se.ID || !saved.Sent {
			t.Fatal(saved, e)
		}
	}
}

func TestChatOwnedAbandonedDraftCleanup(t *testing.T) {
	s := &Server{}
	s.UseChatState(t.TempDir())
	abandoned := uploadOwned(t, s, "draft:old", "old.txt", []byte("old"))
	kept := uploadOwned(t, s, "terminal:codex/saved", "saved.txt", []byte("saved"))
	for _, f := range []ownedChatFile{abandoned, kept} {
		f.Created = time.Now().Add(-8 * 24 * time.Hour)
		if e := s.saveOwnedFile(f); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.purgeDeletedChatFiles(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ownedFile(abandoned.ID); e == nil {
		t.Fatal("abandoned draft survived")
	}
	if _, e := s.ownedFile(kept.ID); e != nil {
		t.Fatal("conversation attachment expired", e)
	}
}
