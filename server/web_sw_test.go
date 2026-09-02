package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The service worker is the one piece of the cockpit that can serve a file the
// server never sent, and it gets no coverage from anything else we run.
//
// This is not hypothetical. sw.js used to answer EVERY failed same-origin GET
// with `caches.match("/")`. versionedIndex re-hashes the ?v= on every js/css ref
// each build, so during a deploy's restart window those URLs are guaranteed
// cache misses — and a browser that asked for js/10-day.js?v=<new> got
// index.html back. "Unexpected token '<'", the module never defines load() or
// renderDay(), and the Day page paints its static SCHEDULE / GOALS / MILESTONES
// / TASKS headings over four empty panels while the rail looks perfectly fine.
// It presented as "Manifest renders blank", with the service up and every API
// answering 200.
//
// Two rules keep that shut: the shell is a NAVIGATION fallback, and only a real
// 200 goes in the cache.
func TestServiceWorkerShellFallbackIsNavigationOnly(t *testing.T) {
	b, err := fs.ReadFile(webFiles, "web/sw.js")
	if err != nil {
		t.Fatalf("read sw.js: %v", err)
	}
	sw := string(b)

	if !strings.Contains(sw, `e.request.mode === "navigate"`) {
		t.Error(`sw.js must gate the shell fallback on e.request.mode === "navigate" — ` +
			"a sub-resource that can't be fetched has to fail as a sub-resource, not as HTML")
	}
	// every `caches.match("/")` has to sit behind the navigate gate
	for _, line := range shellFallbackLines(sw) {
		if !strings.Contains(line, "navigate") {
			t.Errorf("sw.js serves the shell outside a navigate gate: %q", line)
		}
	}
	if !regexp.MustCompile(`if \(res\.ok`).MatchString(sw) {
		t.Error("sw.js must only cache res.ok responses — a cached 404/500 body " +
			"replays the error to every later offline load")
	}
	// /api/* stays live data, forever
	if !strings.Contains(sw, `url.pathname.startsWith("/api/")`) {
		t.Error("sw.js lost the /api/ bypass — API responses must never be cached")
	}
	// activate() only drops caches whose key differs, so the version in the name
	// IS the purge — a changed sw.js that reuses it strands every installed client.
	if !regexp.MustCompile(`const CACHE = "manifest-shell-v\d+"`).MatchString(sw) {
		t.Error(`sw.js must name its cache "manifest-shell-v<n>" and bump <n> whenever the file changes`)
	}

	idx, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if !regexp.MustCompile(`serviceWorker\.register\("sw\.js\?v=\d+"\)`).Match(idx) {
		t.Error("index.html must version the service worker registration so installed clients fetch sw.js updates")
	}
}

// shellFallbackLines returns every non-comment line that reaches for the cached
// shell, so the gate check above can't be fooled by the explanatory comment.
func shellFallbackLines(sw string) []string {
	var out []string
	for _, line := range strings.Split(sw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(trimmed, `caches.match("/")`) {
			out = append(out, trimmed)
		}
	}
	return out
}
