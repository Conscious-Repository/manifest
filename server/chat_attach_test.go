package server

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/chatthreads"
)

// attachFixture is the chat fixture plus a live artifact pool.
func attachFixture(t *testing.T) (*Server, string) {
	t.Helper()
	srv, item := chatFixture(t)
	arts, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseArtifacts(arts)
	return srv, item
}

// upload drives the real HTTP handler so guards are exercised, not bypassed.
func upload(t *testing.T, srv *Server, domain, name string, body []byte) (int, string) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/chat/attach?thread=th/x&name="+name, bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChatAttach(domain, w, r, "ben@aion.bio", "Benjamin Anderson")
	return w.Code, w.Body.String()
}

func xlsxBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("xl/sharedStrings.xml")
	w.Write([]byte(`<sst><si><t>Roof bid</t></si><si><t>48200</t></si></sst>`))
	w2, _ := zw.Create("xl/worksheets/sheet1.xml")
	w2.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c>` +
		`<c r="B1" t="s"><v>1</v></c></row></sheetData></worksheet>`))
	zw.Close()
	return buf.Bytes()
}

// The whole point: attach a spreadsheet, ask a question, and the agent's work
// order actually contains the numbers from inside the file.
func TestAttachedSpreadsheetReachesTheAgent(t *testing.T) {
	srv, _ := attachFixture(t)
	_, _ = srv.chat.CreateThread("th/x", "bids", "", chatthreads.Identity{ID: "ben@aion.bio", Name: "Ben"}, time.Now())
	code, body := upload(t, srv, "aion", "bids.xlsx", xlsxBytes(t))
	if code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	if !strings.Contains(body, `"hash"`) || !strings.Contains(body, "file/") {
		t.Fatalf("upload response missing the ref/id: %s", body)
	}
	hash := srv.artifacts.List("aion")[0].Hash

	if err := srv.AionChatAsk("th/x", "what's the roof number?", "ask",
		[]string{"file/" + hash}, "ben@aion.bio", "Benjamin Anderson"); err != nil {
		t.Fatal(err)
	}
	req := srv.findHarness("kairos").Spirits.Queued()[0].Request
	for _, want := range []string{"ATTACHMENTS (1)", "bids.xlsx", "Roof bid", "48200", "full text:"} {
		if !strings.Contains(req, want) {
			t.Fatalf("order missing %q:\n%s", want, req)
		}
	}
	// the token still leads the order — that is the whole reason it moved
	if !strings.HasPrefix(req, "[chat:: th/x#") {
		t.Fatalf("order must open with the correlation token:\n%.120s", req)
	}
	// an attachment is NOT rendered as a rock by the catch-all context branch
	if strings.Contains(req, "- rock [file/") {
		t.Fatalf("attachment fell through to the rock branch:\n%s", req)
	}
	// the message carries the ref so the thread can render a chip
	msgs := srv.chat.Messages("th/x")
	if len(msgs) != 1 || len(msgs[0].Files) != 1 || msgs[0].Files[0].Name != "bids.xlsx" {
		t.Fatalf("message files: %+v", msgs)
	}
	// and the agent got a readable copy inside its own harness root
	dir := srv.attachDir(srv.kairosAgent())
	if _, err := os.Stat(filepath.Join(dir, hash+".txt")); err != nil {
		t.Fatalf("no extract in the harness root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, hash+".xlsx")); err != nil {
		t.Fatalf("no original in the harness root: %v", err)
	}
}

// An image has no text layer — the agent must get the FILE, and must not be
// told the document was empty.
func TestAttachedImageHandsOverThePath(t *testing.T) {
	srv, _ := attachFixture(t)
	_, _ = srv.chat.CreateThread("th/i", "roof", "", chatthreads.Identity{ID: "b@a.io", Name: "B"}, time.Now())
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 200)...)
	if code, body := upload(t, srv, "aion", "roof.png", png); code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	hash := srv.artifacts.List("aion")[0].Hash
	if err := srv.AionChatAsk("th/i", "what do you see?", "ask",
		[]string{"file/" + hash}, "b@a.io", "B"); err != nil {
		t.Fatal(err)
	}
	req := srv.findHarness("kairos").Spirits.Queued()[0].Request
	if !strings.Contains(req, "roof.png") || !strings.Contains(req, "  file: ") {
		t.Fatalf("image path not offered:\n%s", req)
	}
	if !strings.Contains(req, "no text layer") {
		t.Fatalf("order should say plainly there is no text:\n%s", req)
	}
}

func TestUploadGuards(t *testing.T) {
	srv, _ := attachFixture(t)
	// an executable extension is refused outright
	if code, _ := upload(t, srv, "aion", "payload.svg", []byte("<svg onload=alert(1)>")); code == 200 {
		t.Error("svg accepted")
	}
	if code, _ := upload(t, srv, "aion", "run.sh", []byte("#!/bin/sh\nrm -rf /")); code == 200 {
		t.Error("shell script accepted")
	}
	// contents must agree with the extension — an HTML file wearing .png is
	// exactly how a stored blob becomes stored XSS
	if code, body := upload(t, srv, "aion", "evil.png", []byte("<html><script>alert(1)</script></html>")); code == 200 {
		t.Errorf("html-as-png accepted: %s", body)
	}
	if code, _ := upload(t, srv, "aion", "empty.txt", nil); code == 200 {
		t.Error("empty file accepted")
	}
	// a real png passes
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 200)...)
	if code, body := upload(t, srv, "aion", "ok.png", png); code != 200 {
		t.Errorf("real png rejected: %d %s", code, body)
	}
}

// One pool, two businesses. The index is the access list.
func TestOnePoolNeverLeaksAcrossPortals(t *testing.T) {
	srv, _ := attachFixture(t)
	if code, _ := upload(t, srv, "aion", "cap-table.csv", []byte("holder,pct\nben,51\n")); code != 200 {
		t.Fatal("upload failed")
	}
	hash := srv.artifacts.List("aion")[0].Hash

	// the owning portal serves it, with the headers that stop it executing
	w := httptest.NewRecorder()
	srv.handleChatAttachGet("aion", w, httptest.NewRequest("GET", "/api/chat/attach/"+hash, nil), hash)
	if w.Code != 200 {
		t.Fatalf("owner got %d", w.Code)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff missing, got %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Errorf("a csv must download, not render inline: %q", got)
	}
	// the other portal does not have this file, hash or no hash
	w2 := httptest.NewRecorder()
	srv.handleChatAttachGet("ooda", w2, httptest.NewRequest("GET", "/api/chat/attach/"+hash, nil), hash)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("ooda resolved an aion artifact: %d", w2.Code)
	}
	// and an unowned hash in a context list is silently not context
	if err := srv.AionChatAsk("th/z", "x", "ask", []string{"file/" + strings.Repeat("a", 64)},
		"b@a.io", "B"); err != nil {
		t.Fatal(err)
	}
	req := srv.findHarness("kairos").Spirits.Queued()[0].Request
	if strings.Contains(req, "ATTACHMENTS") {
		t.Fatalf("an unowned hash produced an attachment block:\n%s", req)
	}
}

// The regression that motivated moving the token: a huge attachment must not
// cost the order its correlation token.
func TestBigAttachmentKeepsTheToken(t *testing.T) {
	srv, _ := attachFixture(t)
	_, _ = srv.chat.CreateThread("th/big", "big", "", chatthreads.Identity{ID: "b@a.io", Name: "B"}, time.Now())
	huge := []byte(strings.Repeat("Lorem ipsum dolor sit amet. ", 40000)) // ~1.1MB of text
	if code, body := upload(t, srv, "aion", "huge.txt", huge); code != 200 {
		t.Fatalf("upload: %d %s", code, body)
	}
	hash := srv.artifacts.List("aion")[0].Hash
	if err := srv.AionChatAsk("th/big", "summarize", "delegate",
		[]string{"file/" + hash}, "b@a.io", "B"); err != nil {
		t.Fatal(err)
	}
	req := srv.findHarness("kairos").Spirits.Queued()[0].Request
	if !strings.HasPrefix(req, "[chat:: th/big#") {
		t.Fatalf("token lost or displaced:\n%.200s", req)
	}
	if !chatTokenRe.MatchString(req) {
		t.Fatal("the sweep's own regex no longer finds the token — replies would orphan")
	}
	// the preview was budgeted rather than letting the spool hack the tail off
	if strings.Contains(req, "TRUNCATED by the spool") {
		t.Fatalf("the blunt spool cut fired — the attachment block was not budgeted")
	}
	if !strings.Contains(req, "preview only") {
		t.Fatalf("a clipped preview must say so:\n%s", req[:600])
	}
	if !strings.Contains(req, "CHANGES PROTOCOL") {
		t.Fatal("the delegate protocol was pushed out of the order")
	}
}
