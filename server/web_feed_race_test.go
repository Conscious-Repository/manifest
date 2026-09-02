package server

import (
	"io/fs"
	"strings"
	"testing"
)

// ---- the stale-paint race between FEED and CONSUME ----
//
// FEED and CONSUME are two surfaces over ONE host node (els.feedList), fed by
// two endpoints of very different weight: /api/feed assembles signals,
// proposals, portal notices and bank rows, while /api/consume is a directory
// read. The CONSUME chip is one click away from the inbox, so this interleaving
// is routine, not exotic:
//
//	loadFeed()      → fetch /api/feed          … slow, in flight
//	  user clicks CONSUME → loadFeed() → loadConsume() → renderConsume() paints
//	/api/feed resolves → renderFeed()          … paints over it
//
// and renderFeed, seeing state.feedFilter === "consume", hides every lane but
// the consume one — which holds only UNREAD items, so it is empty the moment
// the backlog has been read. The user gets a single row reading "Nothing here."
// on top of a 571-item reading list, and only a hard reload clears it.
//
// The fix is a monotonic render token: every async load claims one before it
// awaits and drops its response if a newer load has since claimed the surface,
// plus a bail at the top of each renderer so neither can paint the other's
// host. These are four lines that a refactor could quietly drop or reorder —
// the ordering is the whole guarantee, so it is asserted here rather than left
// to a browser nobody runs.
func TestFeedConsumeStalePaintGuards(t *testing.T) {
	feed := readWebJS(t, "web/js/45-feed.js")
	consume := readWebJS(t, "web/js/46-consume.js")

	if !strings.Contains(feed, "function feedClaimRender()") || !strings.Contains(feed, "function feedRenderStale(") {
		t.Fatal("45-feed.js must define feedClaimRender/feedRenderStale — the shared render token both surfaces order by")
	}

	// loadFeed: claim BEFORE the await, check AFTER it, and check before
	// anything observable happens (the cache write is observable — a stale
	// payload left in feedCache paints on the next unrelated repaint).
	load := jsBody(t, feed, "async function loadFeed() {")
	claim := mustIndex(t, load, "loadFeed", "feedClaimRender()")
	fetchAt := mustIndex(t, load, "loadFeed", `await fetch("/api/feed`)
	stale := mustIndex(t, load, "loadFeed", "feedRenderStale(token)")
	cacheAt := mustIndex(t, load, "loadFeed", "feedCache = next")
	paint := mustIndex(t, load, "loadFeed", "renderFeed()")
	if claim > fetchAt {
		t.Error("loadFeed must claim the render token BEFORE it awaits /api/feed — a token claimed after the response cannot detect that the surface changed while it was in flight")
	}
	if stale < fetchAt {
		t.Error("loadFeed's staleness check must come AFTER the fetch resolves")
	}
	if stale > cacheAt || stale > paint {
		t.Error("loadFeed must bail on a stale token BEFORE writing feedCache or calling renderFeed — this is the exact line that paints \"Nothing here.\" over CONSUME")
	}
	// The consume branch must hand its own token down, so the two surfaces
	// share one ordering instead of racing each other.
	if !strings.Contains(load, "loadConsume(token)") {
		t.Error("loadFeed's CONSUME branch must pass its token into loadConsume(token) so a later inbox load supersedes an earlier consume load and vice versa")
	}

	// loadConsume: same shape, mirrored.
	lc := jsBody(t, consume, "async function loadConsume(token) {")
	cClaim := mustIndex(t, lc, "loadConsume", "feedClaimRender()")
	cFetch := mustIndex(t, lc, "loadConsume", `fetch("/api/consume`)
	cStale := mustIndex(t, lc, "loadConsume", "feedRenderStale(token)")
	cCache := mustIndex(t, lc, "loadConsume", "consumeCache = next")
	cPaint := mustIndex(t, lc, "loadConsume", "renderConsume()")
	if cClaim > cFetch {
		t.Error("loadConsume must settle its token before awaiting /api/consume")
	}
	if cStale < cFetch || cStale > cCache || cStale > cPaint {
		t.Error("loadConsume must bail on a stale token after the fetch and before writing consumeCache or painting")
	}

	// The renderers: each refuses the other's surface before it clears the
	// shared host. Belt and braces — a synchronous caller (a "show more"
	// button, a poll) carries no token at all.
	rf := jsBody(t, feed, "function renderFeed() {")
	guard := mustIndex(t, rf, "renderFeed", `if (feedFilter() === "consume") return;`)
	if wipe := mustIndex(t, rf, "renderFeed", "host.innerHTML"); guard > wipe {
		t.Error("renderFeed must return on the CONSUME filter BEFORE it wipes els.feedList — otherwise it blanks the consume view even when it paints nothing itself")
	}
	rc := jsBody(t, consume, "function renderConsume() {")
	cGuard := mustIndex(t, rc, "renderConsume", "if (!consumeIsActiveView()) return;")
	if wipe := mustIndex(t, rc, "renderConsume", "host.innerHTML"); cGuard > wipe {
		t.Error("renderConsume must return when the CONSUME chip is off BEFORE it wipes els.feedList — FEED owns the host then")
	}
}

func readWebJS(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(webFiles, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// jsBody returns the source of the function opened by `header`, brace-matched
// so a nested block cannot end it early. String and comment contents are not
// parsed — these two functions carry no braces inside literals, and a test that
// tried to be a JS parser would be the more fragile thing.
func jsBody(t *testing.T, src, header string) string {
	t.Helper()
	i := strings.Index(src, header)
	if i < 0 {
		t.Fatalf("cannot find %q — the guard test needs updating alongside the signature", header)
	}
	depth, start := 0, i+len(header)-1
	for j := start; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return src[start : j+1]
			}
		}
	}
	t.Fatalf("unbalanced braces after %q", header)
	return ""
}

func mustIndex(t *testing.T, body, fn, needle string) int {
	t.Helper()
	i := strings.Index(body, needle)
	if i < 0 {
		t.Fatalf("%s no longer contains %q — the stale-paint guard has been removed or rewritten; see the note above this test", fn, needle)
	}
	return i
}
