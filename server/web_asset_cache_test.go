package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Every local script and stylesheet in the shell is asked for by its own
// content hash, and only a URL naming the served bytes' hash is cached for
// good: a stale asset can never be pinned in a browser, and a page load (or
// a chat tile) costs no revalidations.
func TestWebAssetsImmutableByContentHash(t *testing.T) {
	h := WebHandler()
	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w
	}
	shell := get("/")
	if cc := shell.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("shell Cache-Control = %q, want no-cache", cc)
	}
	refs := regexp.MustCompile(`(?:src|href)="((?:js|css|vendor)/[^"]+\.(?:js|css)[^"]*)"`).FindAllStringSubmatch(shell.Body.String(), -1)
	if len(refs) < 50 {
		t.Fatalf("only %d asset refs in the shell", len(refs))
	}
	for _, m := range refs {
		ref := m[1]
		file, v, ok := strings.Cut(ref, "?v=")
		if !ok || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(v) {
			t.Fatalf("%s is not stamped with a content hash", ref)
		}
		w := get("/" + ref)
		if w.Code != 200 {
			t.Fatalf("%s: %d", ref, w.Code)
		}
		sum := sha256.Sum256(w.Body.Bytes())
		if hex.EncodeToString(sum[:8]) != v {
			t.Fatalf("%s: stamp does not name the served bytes", ref)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "private, max-age=31536000, immutable" {
			t.Fatalf("%s: Cache-Control = %q", ref, cc)
		}
		// the same file asked for by another hash, or none, revalidates
		for _, other := range []string{"/" + file + "?v=0123456789abcdef", "/" + file, "/" + file + "?v=20260927-fix1"} {
			if cc := get(other).Header().Get("Cache-Control"); cc != "no-cache" {
				t.Fatalf("%s: Cache-Control = %q, want no-cache", other, cc)
			}
		}
	}
}
