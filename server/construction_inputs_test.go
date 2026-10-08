package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"manifest/construction"
)

// Images, PDFs and plain text are retained byte-exact as private inputs and
// served back with a safe policy; active or container formats are refused by
// content, whatever the file name or client header claims.
func TestConstructionInputsUploadSniffAndServe(t *testing.T) {
	f := constructionFixture(t)
	v := f.create(t, fixtureBase, "create-in-0001", "Inputs", nil)
	id := viewProblem(v)["id"].(string)
	rev := viewRev(v, "problem")
	png := syntheticPNG()
	v = f.upload(t, fixtureBase, id, rev, "upload-png-01", "site-photo.png", png, "photo")
	rev = viewRev(v, "problem")
	pdf1 := []byte("%PDF-1.4\n% SYNTHETIC historical drawing, revision A\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n")
	v = f.upload(t, fixtureBase, id, rev, "upload-pdf-01", "drawing.pdf", pdf1, "drawing")
	rev = viewRev(v, "problem")
	pdf2 := []byte("%PDF-1.4\n% SYNTHETIC historical drawing, revision B\n1 0 obj << >> endobj\ntrailer << >>\n%%EOF\n")
	v = f.upload(t, fixtureBase, id, rev, "upload-pdf-02", "drawing.pdf", pdf2, "drawing")
	rev = viewRev(v, "problem")
	inputs := viewProblem(v)["inputs"].([]any)
	if len(inputs) != 3 {
		t.Fatalf("inputs %d", len(inputs))
	}
	for i, want := range []struct {
		content []byte
		mime    string
		disp    string
	}{{png, "image/png", "inline"}, {pdf1, "application/pdf", "attachment"}, {pdf2, "application/pdf", "attachment"}} {
		in := inputs[i].(map[string]any)
		if in["mime"] != want.mime || in["verification"] != "not-field-verified" || in["revision"] != construction.Token(want.content) {
			t.Fatalf("input %d %v", i, in)
		}
		r := f.do(t, "GET", fixtureBase+"/problems/"+id+"/artifacts/"+in["artifactId"].(string)+"?revision="+in["revision"].(string), nil)
		if r.Code != 200 || !bytes.Equal(r.Body, want.content) {
			t.Fatalf("download %d: %d", i, r.Code)
		}
		if r.Hdr.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(r.Hdr.Get("Content-Security-Policy"), "sandbox") ||
			!strings.HasPrefix(r.Hdr.Get("Content-Disposition"), want.disp) || r.Hdr.Get("Content-Type") != want.mime {
			t.Fatalf("download %d headers %v", i, r.Hdr)
		}
	}
	// the old drawing revision stays readable beside the new one
	if inputs[1].(map[string]any)["revision"] == inputs[2].(map[string]any)["revision"] {
		t.Fatal("drawing revisions collapsed")
	}
	refused := map[string][]byte{
		"evil.svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"page.html":  []byte("<!doctype html><html><script>alert(1)</script></html>"),
		"notes.txt":  []byte("   \n<svg onload=alert(1)>"),
		"bundle.zip": []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00synthetic"),
		"tool.exe":   []byte("MZ\x90\x00\x03\x00\x00\x00synthetic"),
		"photo.png":  []byte("<html>not really a png</html>"),
	}
	for name, content := range refused {
		if r := f.uploadRaw(t, fixtureBase, id, rev, "upload-bad-"+strings.ReplaceAll(name, ".", "-"), name, content, "photo"); r.Code != 422 {
			t.Fatalf("%s accepted: %d %s", name, r.Code, r.Body)
		}
	}
	big := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("0"), construction.MaxInputBytes)...)
	if r := f.uploadRaw(t, fixtureBase, id, rev, "upload-big-001", "big.pdf", big, "document"); r.Code != 413 {
		t.Fatalf("oversized upload: %d", r.Code)
	}
	// stale problem revision: refused, nothing retained
	stale := viewRev(f.create(t, fixtureBase, "create-in-0002", "Other", nil), "problem")
	if r := f.uploadRaw(t, fixtureBase, id, stale, "upload-stale-1", "x.txt", []byte("plain synthetic note"), "document"); r.Code != 409 {
		t.Fatalf("stale upload: %d %s", r.Code, r.Body)
	}
	// wrong revision / unknown artifact → 404
	in := inputs[0].(map[string]any)
	for _, q := range []string{"?revision=" + strings.Repeat("a", 64), "?revision=../../etc/passwd", ""} {
		if r := f.do(t, "GET", fixtureBase+"/problems/"+id+"/artifacts/"+in["artifactId"].(string)+q, nil); r.Code != 404 {
			t.Fatalf("artifact %s: %d", q, r.Code)
		}
	}
	f.assertSourcesUntouched(t)
}

// A replayed upload (lost ACK) retains one input, and the generic artifact
// routes — bound to the global registry — never list or read construction
// content, even given its exact id and hash.
func TestConstructionInputsReplayAndGenericRoutesBlind(t *testing.T) {
	f := constructionFixture(t)
	v := f.create(t, fixtureBase, "create-in-0003", "Replay inputs", nil)
	id, rev := viewProblem(v)["id"].(string), viewRev(v, "problem")
	content := []byte("%PDF-1.4\n% SYNTHETIC drawing for replay\n%%EOF\n")
	a := f.uploadRaw(t, fixtureBase, id, rev, "upload-replay-1", "d.pdf", content, "drawing")
	b := f.uploadRaw(t, fixtureBase, id, rev, "upload-replay-1", "d.pdf", content, "drawing")
	if a.Code != 200 || b.Code != 200 {
		t.Fatalf("%d %d %s", a.Code, b.Code, b.Body)
	}
	va, vb := a.json(t), b.json(t)
	if va["receipt"].(map[string]any)["id"] != vb["receipt"].(map[string]any)["id"] || len(viewProblem(vb["view"].(map[string]any))["inputs"].([]any)) != 1 {
		t.Fatal("replayed upload retained twice")
	}
	in := viewProblem(vb["view"].(map[string]any))["inputs"].([]any)[0].(map[string]any)
	art, hash := in["artifactId"].(string), in["revision"].(string)
	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", "http://"+cHost+path, nil)
		req.Host = cHost
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	code, body := get("/api/artifacts")
	if strings.Contains(body, art) || strings.Contains(body, hash) {
		t.Fatalf("generic artifact list exposed construction content (%d)", code)
	}
	if code, body := get("/api/artifacts/get?id=" + art + "&content=1"); code == 200 || strings.Contains(body, "SYNTHETIC drawing") {
		t.Fatalf("generic get read construction content: %d", code)
	}
	if code, body := get("/api/artifacts/content?hash=" + hash); code == 200 || strings.Contains(body, "SYNTHETIC drawing") {
		t.Fatalf("generic content read construction bytes: %d", code)
	}
	f.assertSourcesUntouched(t)
}
