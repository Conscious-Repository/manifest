package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzGet(t *testing.T, h http.Handler, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	Gzip(h).ServeHTTP(w, r)
	return w
}

func TestGzipCompressesJSONAndRoundTrips(t *testing.T) {
	body := `{"backlog":[` + strings.Repeat(`{"id":"aion-bl/x","text":"the same row"},`, 999) + `{}]}`
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	})
	w := gzGet(t, h, nil)
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("json not compressed: %q", got)
	}
	if w.Header().Get("Content-Length") != "" {
		t.Fatal("stale Content-Length survived compression")
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(zr)
	if string(out) != body {
		t.Fatal("round-trip corrupted the body")
	}
	if w.Body.Len() >= len(body)/4 {
		t.Fatalf("barely compressed: %d of %d", w.Body.Len(), len(body))
	}
	// a client that never asked gets identity bytes
	r2 := httptest.NewRequest("GET", "/", nil)
	w2 := httptest.NewRecorder()
	Gzip(h).ServeHTTP(w2, r2)
	if w2.Header().Get("Content-Encoding") != "" || w2.Body.String() != body {
		t.Fatal("no-gzip client did not get identity bytes")
	}
}

func TestGzipLeavesStreamsAndBlobsAlone(t *testing.T) {
	// SSE: frames must flush per event, so the stream passes through untouched
	sse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: hello\n\n")
		w.(http.Flusher).Flush()
	})
	if w := gzGet(t, sse, nil); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("SSE was compressed — event flushing would stall")
	}
	// a binary blob keeps its bytes
	pdf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 …"))
	})
	if w := gzGet(t, pdf, nil); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("a blob was compressed")
	}
	// a Range request passes through — compressing a range corrupts it
	ranged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "0123456789")
	})
	if w := gzGet(t, ranged, map[string]string{"Range": "bytes=0-4"}); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("a range response was compressed")
	}
	// an Upgrade (websocket) request bypasses the wrapper entirely
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(*gzipResponseWriter); ok {
			t.Fatal("upgrade request was wrapped — Hijack path put at risk")
		}
	})
	gzGet(t, up, map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"})
	// 204 stays empty-bodied and unencoded
	nc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	if w := gzGet(t, nc, nil); w.Header().Get("Content-Encoding") != "" || w.Body.Len() != 0 {
		t.Fatal("204 mangled")
	}
}

func TestGzipUnheaderedWriteStillCompresses(t *testing.T) {
	// writeJSON always sets the type, but a bare handler that never does must
	// still behave: sniff → decide → compress textual output
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("plain text line\n", 200))
	})
	w := gzGet(t, h, nil)
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("sniffed text not compressed (type %q)", w.Header().Get("Content-Type"))
	}
	zr, _ := gzip.NewReader(w.Body)
	out, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(out), "plain text line") {
		t.Fatal("body corrupted")
	}
}
