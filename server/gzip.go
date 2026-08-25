package server

import (
	"bufio"
	"compress/gzip"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Gzip compresses textual responses. The dashboard's aggregates are large —
// /api/aion serves ~456 KB of JSON and /api/properties ~256 KB, uncompressed,
// over the tailnet on every page load — and JSON squeezes roughly 10:1. This
// is pure transport: nothing about what is computed, cached or re-derived
// changes, which is what keeps it inside the platform's re-derive-everything
// values.
//
// It stays out of the way of everything streaming or already-encoded:
//   - WebSocket upgrades pass through untouched (the terminal's PTY),
//   - Range requests pass through (compressing a range corrupts it),
//   - text/event-stream passes through (SSE frames must flush per event),
//   - responses that already declare a Content-Encoding pass through,
//   - only allowlisted textual types compress — a served blob keeps its bytes.
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			r.Header.Get("Upgrade") != "" || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

// gzipPool amortizes compressor allocations across requests.
var gzipPool = sync.Pool{New: func() any {
	// BestSpeed: at level 1 the JSON aggregates still shrink ~8:1, and the
	// point is latency, not archive density.
	zw, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
	return zw
}}

func compressible(contentType string) bool {
	ct := contentType
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	switch ct {
	case "application/json", "text/html", "text/css", "text/plain",
		"application/javascript", "text/javascript", "text/markdown",
		"image/svg+xml", "application/manifest+json", "text/csv":
		return true
	}
	return false
}

// gzipResponseWriter decides ONCE, at the first write, whether this response
// compresses — by which time the handler has set its Content-Type.
type gzipResponseWriter struct {
	http.ResponseWriter
	zw      *gzip.Writer
	decided bool
	status  int
}

func (g *gzipResponseWriter) WriteHeader(status int) {
	if g.decided {
		g.ResponseWriter.WriteHeader(status)
		return
	}
	g.decided = true
	g.status = status
	h := g.Header()
	if status == http.StatusNoContent || status == http.StatusNotModified ||
		h.Get("Content-Encoding") != "" || !compressible(h.Get("Content-Type")) ||
		strings.EqualFold(h.Get("Content-Type"), "text/event-stream") {
		g.ResponseWriter.WriteHeader(status)
		return
	}
	// the compressed length is unknowable up front
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	g.ResponseWriter.WriteHeader(status)
	g.zw = gzipPool.Get().(*gzip.Writer)
	g.zw.Reset(g.ResponseWriter)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.decided {
		// mirror net/http: an unheadered write implies 200, and sniffs the
		// type first so the compress decision sees what the client will
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.zw != nil {
		return g.zw.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// Flush keeps SSE and other streaming handlers working through the wrapper:
// the compressor's buffer drains before the underlying flush.
func (g *gzipResponseWriter) Flush() {
	if g.zw != nil {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack lets WebSocket handshakes that slipped past the Upgrade-header check
// take the raw connection (never compressed — zw is nil before a hijack).
func (g *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := g.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (g *gzipResponseWriter) Close() {
	if g.zw != nil {
		_ = g.zw.Close()
		gzipPool.Put(g.zw)
		g.zw = nil
	}
}
