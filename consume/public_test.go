package consume

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// onlyEntries implements CuratedFeed and NOTHING else.
//
// This is the compile-time canary, the same device as portal.go's fakeLive:
// if a second method is ever added to CuratedFeed, this type stops satisfying
// it and this file stops compiling. Widening the interface that stands between
// a public port and a private vault should require deleting this comment.
type onlyEntries struct{ e []CuratedEntry }

func (o onlyEntries) Entries() []CuratedEntry { return o.e }

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// ⚠ THE ISOLATION TEST. Curated notes live in the vault, so the public handler
// reaches a projection over the owner's private tree. This stuffs that tree
// with things that must never be public and asserts none of them can be
// reached — through the feed, the index, or any other path.
func TestPublicFeedServesOnlyCuratedItems(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)

	write := func(name, body string) {
		if err := v.io().Write("extrinsic/"+name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	// Everything here is private and none of it is curated.
	write("private-research.md", "---\ncategories: [research]\n---\nCANARY-RESEARCH\n")
	write("a-book.md", "---\ncategories: [books]\nrating: 5\n---\nCANARY-BOOK\n")
	write("read-but-not-curated.md",
		"---\ncategories: [articles]\nurl: https://e.com/private\n---\n# CANARY-UNCURATED\n")
	write("uncurated-with-empty-field.md",
		"---\ncategories: [articles]\ncurated:\nurl: https://e.com/empty\n---\n# CANARY-EMPTYFIELD\n")

	// One thing IS curated.
	if _, err := s.Curate(context.Background(), it.ID, "the note"); err != nil {
		t.Fatal(err)
	}

	h := PublicHandler(s, PublicConfig{Title: "reading", BaseURL: "https://reading.example"})

	feed := get(t, h, "/feed.xml").Body.String()
	index := get(t, h, "/").Body.String()

	for _, canary := range []string{
		"CANARY-RESEARCH", "CANARY-BOOK", "CANARY-UNCURATED", "CANARY-EMPTYFIELD",
		"private-research", "a-book",
	} {
		if strings.Contains(feed, canary) {
			t.Errorf("PRIVATE DATA LEAKED INTO THE PUBLIC FEED: %q\n%s", canary, feed)
		}
		if strings.Contains(index, canary) {
			t.Errorf("PRIVATE DATA LEAKED INTO THE PUBLIC INDEX: %q", canary)
		}
	}
	if !strings.Contains(feed, "The Dictatorship of the Articulate") {
		t.Errorf("the curated item is missing from the feed:\n%s", feed)
	}
	if !strings.Contains(feed, "the note") {
		t.Error("the owner's note did not reach the feed")
	}

	// Every other path does not exist here — not 401, not 403, gone.
	//
	// A traversal-shaped path gets ServeMux's path-cleaning redirect rather
	// than a 404, so the check follows it: what matters is that no request
	// reaches content, not which status code says so.
	for _, path := range []string{
		"/api/consume", "/api/feed", "/api/day", "/js/app.js", "/index.html",
		"/extrinsic/private-research.md", "/feed.xml/../api/feed", "/curated",
		"/feed.xml/", "/subscriptions",
	} {
		w := get(t, h, path)
		if loc := w.Header().Get("Location"); w.Code >= 300 && w.Code < 400 && loc != "" {
			w = get(t, h, loc) // follow one hop and judge the destination
		}
		if w.Code != http.StatusNotFound {
			t.Errorf("%s returned %d (body %q); the public surface must expose nothing else",
				path, w.Code, truncate(w.Body.String(), 120))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// The whole feed must parse as RSS in a real reader.
func TestFeedXMLIsValidRSS(t *testing.T) {
	entries := []CuratedEntry{{
		ItemID: "consume:rss:s:abc", Title: `Tom & Jerry: "a study" <ha>`,
		URL: "https://e.com/p?a=1&b=2", Author: "A. Writer", Source: "The Source",
		Note: "why I kept it & liked it", Curated: "2026-08-24", Published: "2026-08-21",
		Mirror: MirrorFull, HTML: `<p>Body with <strong>markup</strong> & an ]]> escape attempt.</p>`,
	}}
	out := FeedXML(entries, PublicConfig{Title: "reading", BaseURL: "https://reading.example"})

	var parsed struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				Desc    string `xml:"description"`
				Encoded string `xml:"encoded"`
				GUID    string `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("feed does not parse as XML: %v\n%s", err, out)
	}
	if len(parsed.Channel.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(parsed.Channel.Items))
	}
	got := parsed.Channel.Items[0]
	if got.Title != `Tom & Jerry: "a study" <ha>` {
		t.Errorf("title did not survive escaping: %q", got.Title)
	}
	// The link is the ORIGINAL — credit and traffic go to the writer.
	if got.Link != "https://e.com/p?a=1&b=2" {
		t.Errorf("link is not the original: %q", got.Link)
	}
	if got.Desc != "why I kept it & liked it" {
		t.Errorf("the note is not the description: %q", got.Desc)
	}
	if !strings.Contains(got.Encoded, "<strong>markup</strong>") {
		t.Errorf("body missing from content:encoded: %q", got.Encoded)
	}
	if !strings.Contains(got.Encoded, "read at the source") {
		t.Errorf("attribution header missing: %q", got.Encoded)
	}
	if !strings.Contains(out, `isPermaLink="false"`) {
		t.Error("guid should not claim to be a permalink")
	}
}

// Byte-stability: the same content must render the same bytes, or every poll
// by every subscriber looks like a change.
func TestFeedXMLIsDeterministic(t *testing.T) {
	entries := []CuratedEntry{
		{ItemID: "a", Title: "A", URL: "https://e.com/a", Curated: "2026-08-24", HTML: "<p>a</p>"},
		{ItemID: "b", Title: "B", URL: "https://e.com/b", Curated: "2026-08-23", HTML: "<p>b</p>"},
	}
	cfg := PublicConfig{Title: "reading", BaseURL: "https://reading.example"}
	first := FeedXML(entries, cfg)
	time.Sleep(5 * time.Millisecond)
	if second := FeedXML(entries, cfg); first != second {
		t.Error("feed output changed between renders with identical content")
	}
	if !strings.Contains(first, "<lastBuildDate>") {
		t.Error("no lastBuildDate")
	}
}

// ⚠ THE DURABILITY TEST. dataDir is disposable and the vault is not, so losing
// the snapshot cache must cost the reader the publisher's exact markup and
// NOTHING ELSE: the note holds the same article as markdown and the feed
// renders that instead. This used to degrade to a title and a link — the piece
// was in the vault the whole time, and the feed could not read it.
func TestTheNoteCarriesTheBodyWhenTheSnapshotIsGone(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	if _, err := s.Curate(context.Background(), it.ID, "still worth reading"); err != nil {
		t.Fatal(err)
	}
	// Simulate a wiped dataDir: the note survives, the body cache does not.
	s.store.Forget(it.SubID)
	s.invalidateCurated()
	if body := s.store.Body(it.ID); body != "" {
		t.Fatalf("the snapshot is still there; this test proves nothing: %q", body)
	}

	entries := s.Entries()
	out := FeedXML(entries, PublicConfig{Title: "reading"})
	for _, want := range []string{
		"The Dictatorship of the Articulate",
		"still worth reading",
		"https://m.example/p/dictatorship",
		"A <strong>strong</strong> claim.", // the body, rendered from the note
		"read at the source",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the feed lost %q with the cache:\n%s", want, out)
		}
	}
	index := indexHTML(entries, PublicConfig{Title: "reading"})
	if !strings.Contains(index, "A <strong>strong</strong> claim.") {
		t.Errorf("the index lost the body with the cache:\n%s", index)
	}
}

// An excerpt-mode subscription must not mirror, even when the body is cached.
func TestExcerptModeDoesNotMirror(t *testing.T) {
	entries := []CuratedEntry{{
		ItemID: "a", Title: "A", URL: "https://e.com/a", Curated: "2026-08-24",
		Mirror: MirrorExcerpt, HTML: "<p>THE WHOLE ARTICLE</p>", Body: "the whole article in markdown",
	}}
	out := FeedXML(entries, PublicConfig{Title: "r"})
	if strings.Contains(out, "THE WHOLE ARTICLE") {
		t.Errorf("excerpt-mode entry mirrored its full body:\n%s", out)
	}
	if !strings.Contains(out, "the whole article in markdown") {
		t.Error("excerpt-mode entry should still carry an excerpt")
	}
	index := indexHTML(entries, PublicConfig{Title: "r"})
	if strings.Contains(index, "THE WHOLE ARTICLE") {
		t.Errorf("excerpt-mode entry mirrored its full body on the index:\n%s", index)
	}
	if !strings.Contains(index, "the whole article in markdown") {
		t.Error("the index should still carry the excerpt and the link")
	}
}

// ⚠ THE POINT OF THE PUBLIC PAGE. Somebody who follows a link here should land
// on the writing, not on a table of contents pointing back out — the index
// carries every mirrored body inline, exactly as the feed does.
func TestIndexCarriesFullBodiesInline(t *testing.T) {
	v := newVault(t)
	s, it := svcWithItem(t, v)
	if _, err := s.Curate(context.Background(), it.ID, "the middle third is the argument"); err != nil {
		t.Fatal(err)
	}
	h := PublicHandler(s, PublicConfig{Title: "reading", BaseURL: "https://reading.example"})
	index := get(t, h, "/").Body.String()

	for _, want := range []string{
		"The Dictatorship of the Articulate",     // the title
		"the middle third is the argument",       // the owner's note
		"A <strong>strong</strong> claim.",       // the BODY, inline
		"<blockquote>Quoted.</blockquote>",       // …with its structure
		`href="https://m.example/p/dictatorship`, // and the way back to the source
		"read at the source",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("the index is missing %q:\n%s", want, index)
		}
	}
	// Still exactly two routes: an index that holds the bodies needs no
	// per-entry permalink, and every extra path here is public surface.
	for _, path := range []string{"/p/the-dictatorship-of-the-articulate", "/1", "/entry/0"} {
		if w := get(t, h, path); w.Code != http.StatusNotFound {
			t.Errorf("%s returned %d; the public surface is / and /feed.xml", path, w.Code)
		}
	}
}

func TestEmptyFeedIsStillValid(t *testing.T) {
	h := PublicHandler(onlyEntries{}, PublicConfig{})
	w := get(t, h, "/feed.xml")
	if w.Code != http.StatusOK {
		t.Fatalf("empty feed: %d", w.Code)
	}
	var probe any
	if err := xml.Unmarshal(w.Body.Bytes(), &probe); err != nil {
		t.Errorf("empty feed is not valid XML: %v\n%s", err, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "rss+xml") {
		t.Errorf("content type: %q", ct)
	}
	if got := get(t, h, "/").Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Errorf("index is indexable; it would compete with the sources: %q", got)
	}
}

func TestFeedIsCapped(t *testing.T) {
	many := make([]CuratedEntry, feedCap+25)
	for i := range many {
		many[i] = CuratedEntry{ItemID: "i", Title: "T", URL: "https://e.com/x", Curated: "2026-08-24"}
	}
	if n := strings.Count(FeedXML(many, PublicConfig{}), "<item>"); n != feedCap {
		t.Errorf("want %d items, got %d", feedCap, n)
	}
}
