package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ONE X POST, TWO ENTRANCES, ONE BODY.
//
// Every test here serves the bridge from an httptest server through
// Config.RSSHubBase, so "RSSHub" is a fixture and nothing dials the real one.

// The post as the bridge carries it: the whole thing, media link and all.
const xFullPost = "It’s obvious that we’re simultaneously in a period of great progress and precipitous decline." +
	"<br><br>Despite the daily hype-schizo-edits and monumental daily achievements of our nation, its deluded " +
	"to drone on about a “golden age.” The philosopher, Zyzz, was lauded for ceaselessly lifting in a garage " +
	"while the country he lived in forgot how to build anything at all."

// The same post as X's oEmbed answers for it: cut at the ellipsis, with the
// provider's media link stuck on the end.
const xClippedPost = "<p>It’s obvious that we’re simultaneously in a period of great progress and precipitous decline." +
	"<br><br>Despite the daily hype-schizo-edits and monumental daily achievements of our nation, its deluded " +
	"to drone on about a “golden age.” The philosopher, Zyzz, was lauded for ceaselessly… " +
	`<a href="https://t.co/hlS0fkVGSz">pic.twitter.com/hlS0fkVGSz</a></p>`

const xStatus = "https://x.com/ADoricko/status/1962778419282444649"

// rsshub plays the bridge: one account's timeline, one post in it.
func rsshub(t *testing.T, items string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/twitter/user/") {
			t.Errorf("the bridge was asked for %q, which is not a timeline", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Twitter @ADoricko</title>` + items + `</channel></rss>`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func xTimelineItem(body string) string {
	return `<item>
<title>` + body + `</title>
<link>` + xStatus + `</link>
<guid isPermaLink="false">` + xStatus + `</guid>
<pubDate>Tue, 02 Sep 2026 14:20:00 GMT</pubDate>
<author>ADoricko</author>
<description><![CDATA[` + body + `]]></description>
</item>`
}

// THE BUG. A pasted status used to be curated from X's oEmbed, which truncates
// — so the same post was whole through the lane and clipped through the paste.
func TestCurateURLXPostTakesTheWholePostFromTheBridge(t *testing.T) {
	hub := rsshub(t, xTimelineItem(xFullPost))
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{RSSHubBase: hub.URL, AllowPrivateCurateFetch: true})

	entry, err := s.CurateURL(context.Background(), xStatus, "the second paragraph is the argument")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "@ADoricko on X" {
		t.Errorf("title = %q, want the handle convention", entry.Title)
	}
	if entry.Mirror != MirrorFull {
		t.Errorf("mirror = %q — the bridge gave up the whole post", entry.Mirror)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "forgot how to build anything at all") {
		t.Fatalf("the post's tail is missing — this is the truncation bug:\n%s", note)
	}
	if strings.Contains(note, "…") {
		t.Errorf("the note still carries the provider's ellipsis:\n%s", note)
	}
	if !strings.Contains(note, "# @ADoricko on X") {
		t.Errorf("heading is not the handle convention:\n%s", note)
	}

	// The same paste again lands on the same note rather than forking it.
	again, err := s.CurateURL(context.Background(), xStatus, "still the second paragraph")
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != entry.Path {
		t.Fatalf("a refresh forked the note: %s then %s", entry.Path, again.Path)
	}
	if got := s.Curated(); len(got) != 1 {
		t.Fatalf("curated projection holds %d entries, want 1", len(got))
	}
}

// The bridge cannot always reach back to an old post. Then oEmbed is the
// fallback, and the note says what it is: a preview, not the whole piece.
func TestCurateURLXPostFallsBackToOEmbedAndSaysSo(t *testing.T) {
	hub := rsshub(t, "") // the account's timeline no longer holds it
	oembed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"rich","author_name":"Augustus Doricko","provider_name":"X",
			"html":"<blockquote class=\"twitter-tweet\">` + strings.ReplaceAll(xClippedPost, `"`, `\"`) +
			`</blockquote><script async src=\"https://platform.twitter.com/widgets.js\"></script>"}`))
	}))
	defer oembed.Close()
	withProviders(t, oembedProvider{
		hosts: []string{"x.com"}, endpoint: oembed.URL + "/oembed",
		kind: linkPost, skipPage: true, embed: func(string) string { return "" },
	})

	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{RSSHubBase: hub.URL, AllowPrivateCurateFetch: true})
	entry, err := s.CurateURL(context.Background(), xStatus, "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Title != "@ADoricko on X" {
		t.Errorf("title = %q", entry.Title)
	}
	if entry.Mirror != MirrorExcerpt {
		t.Errorf("mirror = %q — a clipped oEmbed body must not be published as whole", entry.Mirror)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "period of great progress") {
		t.Errorf("the preview was lost as well as the rest:\n%s", note)
	}
}

// THE RETROFIT. A note written before the bridge was asked first carries the
// clipped body; the backfill repairs it, once, and only when the recovered
// post is the same post.
func TestBackfillRepairsAClippedXNote(t *testing.T) {
	hub := rsshub(t, xTimelineItem(xFullPost))
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{RSSHubBase: hub.URL})

	old := `---
categories: [articles]
source: X
author: Augustus Doricko
url: ` + xStatus + `
published: 2026-09-02
curated: 2026-09-02
item: ext-url-4a1c2f
mirror: full
---

#article

# @ADoricko on X

It’s obvious that we’re simultaneously in a period of great progress and precipitous decline.

Despite the daily hype-schizo-edits and monumental daily achievements of our nation, its deluded to drone on about a “golden age.” The philosopher, Zyzz, was lauded for ceaselessly… [pic.twitter.com/hlS0fkVGSz](https://t.co/hlS0fkVGSz)

---

Source: [X](` + xStatus + `)
`
	if err := v.io().Write("extrinsic/augustus-doricko-on-x.md", []byte(old)); err != nil {
		t.Fatal(err)
	}
	s.invalidateCurated()

	if n := s.BackfillCurated(context.Background()); n == 0 {
		t.Fatal("the clipped note was not repaired")
	}
	after := v.read(t, "extrinsic/augustus-doricko-on-x.md")
	if !strings.Contains(after, "forgot how to build anything at all") {
		t.Fatalf("the post's tail was not recovered:\n%s", after)
	}
	if strings.Contains(after, "ceaselessly…") {
		t.Errorf("the clip survived the repair:\n%s", after)
	}
	if !strings.Contains(after, "Source: [X](") || !strings.Contains(after, "# @ADoricko on X") {
		t.Errorf("the repair ate the note's own furniture:\n%s", after)
	}
	if n := s.BackfillCurated(context.Background()); n != 0 {
		t.Fatalf("the repair is not a fixpoint; a second run changed %d notes", n)
	}
}

// The bridge answering with a DIFFERENT post repairs nothing. Missing text is
// never invented, and a note that cannot be recovered is left as it was.
func TestApplyXBodyRefusesADifferentPost(t *testing.T) {
	note := "---\nurl: " + xStatus + "\n---\n\n#article\n\n# @ADoricko on X\n\n" +
		"It’s obvious that we’re simultaneously in a period of great progress and precipitous…\n"
	if got := applyXBody(note, "an entirely different post about something else, at length, with words"); got != note {
		t.Errorf("a different post was written into the note:\n%s", got)
	}
	// The same post, continued, IS accepted.
	full := "It’s obvious that we’re simultaneously in a period of great progress and precipitous decline, and both are visible from here."
	if got := applyXBody(note, full); !strings.Contains(got, "visible from here") {
		t.Errorf("the same post was refused:\n%s", got)
	}
}

// A whole note is not a clipped one, whatever else it ends with.
func TestXLooksClipped(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"the post, entire.", false},
		{"was lauded for ceaselessly…", true},
		{"was lauded for ceaselessly... ", true},
		{"was lauded for ceaselessly… [pic.twitter.com/hlS0f](https://t.co/hlS0f)", true},
		{"was lauded for ceaselessly… https://t.co/hlS0f", true},
		{"a post that ends in a link https://example.com/x", false},
		{"", false},
	} {
		if got := xLooksClipped(tc.in); got != tc.want {
			t.Errorf("xLooksClipped(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The id is matched as a whole number: …/status/1234 is not …/status/12345.
func TestXRefersToMatchesWholeIDs(t *testing.T) {
	if xRefersTo("1234", "https://x.com/a/status/12345") {
		t.Error("a prefix of a longer id matched")
	}
	if !xRefersTo("12345", "https://x.com/a/status/12345#m") {
		t.Error("the post's own link did not match")
	}
	if !xRefersTo("12345", "", "tag:x.com,2026:12345") {
		t.Error("the guid did not match")
	}
}
