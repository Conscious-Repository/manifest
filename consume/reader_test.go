package consume

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- the reader pass (owner decisions 2026-09-27) ----

const youtubeFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/" xmlns="http://www.w3.org/2005/Atom">
 <title>Veritasium</title>
 <entry>
  <id>yt:video:VKlulHwMxgU</id>
  <yt:videoId>VKlulHwMxgU</yt:videoId>
  <title>A short about balloons</title>
  <link rel="alternate" href="https://www.youtube.com/shorts/VKlulHwMxgU"/>
  <author><name>Veritasium</name></author>
  <published>2026-09-24T13:00:24+00:00</published>
  <media:group>
   <media:thumbnail url="https://i3.ytimg.com/vi/VKlulHwMxgU/hqdefault.jpg" width="480" height="360"/>
   <media:description>Take a balloon.</media:description>
  </media:group>
 </entry>
 <entry>
  <id>yt:video:JsBZOcqZerk</id>
  <yt:videoId>JsBZOcqZerk</yt:videoId>
  <title>The Real Engineering of the Enigma Machine</title>
  <link rel="alternate" href="https://www.youtube.com/watch?v=JsBZOcqZerk"/>
  <author><name>Veritasium</name></author>
  <published>2026-09-21T17:49:57+00:00</published>
  <media:group>
   <media:thumbnail url="https://i3.ytimg.com/vi/JsBZOcqZerk/hqdefault.jpg" width="480" height="360"/>
   <media:description>How the Enigma worked.

Chapters &lt;script&gt;alert(1)&lt;/script&gt; follow.</media:description>
  </media:group>
 </entry>
</feed>`

func TestYouTubeEntriesAreVideosAndShortsAreDropped(t *testing.T) {
	s, _, _ := liveSvc(t)
	feed := serve(t, youtubeFeed, nil)
	s.hc = feed.Client()
	sub, err := s.Subscribe(context.Background(), feed.URL, "", "watch", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	cards := s.Cards(Query{View: "all", Sub: sub.ID})
	if len(cards) != 1 {
		t.Fatalf("a Short must be dropped by default; got %d cards", len(cards))
	}
	c := cards[0]
	if c.Type != TypeVideo || MediaType(c.Type) != MediaVideo {
		t.Errorf("a YouTube entry is a video card: %+v", c)
	}
	if c.Embed != "youtube:video:JsBZOcqZerk" || !strings.Contains(c.Image, "JsBZOcqZerk") {
		t.Errorf("video card needs its player and thumbnail: embed=%q image=%q", c.Embed, c.Image)
	}
	it, _, _ := s.Get(c.ID)
	if strings.Contains(it.Body, "<script") || !strings.Contains(it.Body, "&lt;script&gt;") {
		t.Errorf("the description is text, escaped, never markup: %q", it.Body)
	}
	if strings.Count(it.Body, "<p>") != 2 {
		t.Errorf("blank lines in the description become paragraphs: %q", it.Body)
	}
	if got := s.Cards(Query{View: "all", Type: MediaArticle}); len(got) != 0 {
		t.Errorf("the article filter must not show videos: %d", len(got))
	}

	// The channel's switch keeps its Shorts on the next poll.
	on := true
	if err := s.SetSubSwitches(sub.ID, nil, &on); err != nil {
		t.Fatal(err)
	}
	if err := s.PollNow(context.Background(), sub.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Cards(Query{View: "all", Sub: sub.ID})); n != 2 {
		t.Errorf("with shorts on, both entries arrive; got %d", n)
	}
}

func TestYouTubeChannelURLNamesItsFeed(t *testing.T) {
	got := youtubeChannelFeed("https://www.youtube.com/channel/UCHnyfMqiRRG1u-2MsSQLbXA/videos")
	if got != "https://www.youtube.com/feeds/videos.xml?channel_id=UCHnyfMqiRRG1u-2MsSQLbXA" {
		t.Errorf("channel id → feed: %q", got)
	}
	for _, not := range []string{"https://www.youtube.com/@veritasium", "https://example.com/channel/UCHnyfMqiRRG1u-2MsSQLbXA"} {
		if youtubeChannelFeed(not) != "" {
			t.Errorf("%s is not a channel-id link", not)
		}
	}
}

// substackServer serves a Substack-shaped publication: a feed that truncates
// both posts, and the public post API that says which one is free.
func substackServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	whole := "<p>" + strings.Repeat("The whole free essay, every word of it. ", 80) + "</p>"
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed":
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprintf(w, `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>Letters</title>
<item><title>A Free Post</title><link>%[1]s/p/a-free-post</link><pubDate>Mon, 21 Sep 2026 10:00:00 GMT</pubDate>
<content:encoded><![CDATA[<p>It starts well.</p><p><a href="%[1]s/p/a-free-post">Read more</a></p>]]></content:encoded></item>
<item><title>A Paid Post</title><link>%[1]s/p/a-paid-post</link><pubDate>Sun, 20 Sep 2026 10:00:00 GMT</pubDate>
<content:encoded><![CDATA[<p>For subscribers.</p><p><a href="%[1]s/p/a-paid-post">Read more</a></p>]]></content:encoded></item>
</channel></rss>`, srv.URL)
		// the post pages are Substack's app shell: its assets load from
		// substackcdn.com and the article is not in the markup a scraper sees
		case "/p/a-free-post":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><link rel="stylesheet" href="https://substackcdn.com/bundle/main.css"></head><body><div id="entry"></div></body></html>`)
		case "/p/a-paid-post":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><link rel="stylesheet" href="https://substackcdn.com/bundle/main.css"></head><body><p>This post is for paid subscribers</p></body></html>`)
		case "/api/v1/posts/a-free-post":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"audience":"everyone","body_html":%q}`, whole)
		case "/api/v1/posts/a-paid-post":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"audience":"only_paid","body_html":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSubstackFreePostsArriveWholeWithoutSignIn(t *testing.T) {
	s, _, _ := liveSvc(t)
	srv := substackServer(t)
	s.hc = srv.Client()
	sub, err := s.Subscribe(context.Background(), srv.URL+"/feed", "", "", MirrorFull)
	if err != nil {
		t.Fatal(err)
	}
	var free, paid Card
	for _, c := range s.Cards(Query{View: "all"}) {
		switch c.Title {
		case "A Free Post":
			free = c
		case "A Paid Post":
			paid = c
		}
	}
	if free.Preview != "" || free.Chars < 1000 {
		t.Errorf("a free Substack post must be completed from the post API: %+v", free)
	}
	if paid.Preview != PreviewPaid {
		t.Errorf("the API's own 'only_paid' is what labels a post paid: %+v", paid)
	}
	var st SubStatus
	for _, x := range s.Statuses() {
		if x.ID == sub.ID {
			st = x
		}
	}
	if !st.Paid || st.PaidPosts != 1 || st.Pays || st.SignedIn {
		t.Errorf("one paid post is counted, and nothing says the owner pays: %+v", st)
	}
}

func TestPartialPreviewNoLongerMarksAPublicationPaid(t *testing.T) {
	s, _, _ := liveSvc(t)
	d := ParseFeeds("")
	d.Add(Subscription{ID: "free", Title: "Free Letter", Kind: KindRSS, URL: "https://free.example/feed"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.store.Commit("free", now, true, []Item{{
		ID: "consume:rss:free:aaaaaaaaaaaa", SubID: "free", Title: "Cut short",
		URL: "https://free.example/p/cut", PublishedAt: now, Preview: PreviewPartial,
	}}, nil, "")
	for _, st := range s.Statuses() {
		if st.ID == "free" && (st.Paid || st.PaidPosts != 0) {
			t.Errorf("a partial preview is not a paywall; the owner must not be asked to sign in: %+v", st)
		}
	}
}

func TestPaysSwitchRoundTripsThroughTheVaultLine(t *testing.T) {
	s, v, _ := liveSvc(t)
	d := ParseFeeds("")
	d.Add(Subscription{ID: "acx", Title: "ACX", Kind: KindRSS, URL: "https://acx.example/feed"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	if err := s.SetSubSwitches("acx", &yes, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.read(t, feedsPath), "[pays:: yes]") {
		t.Fatalf("the switch is written on the line:\n%s", v.read(t, feedsPath))
	}
	if subs := s.Subscriptions(); !subs[0].Pays || subs[0].Shorts {
		t.Errorf("parsed back: %+v", subs[0])
	}
	if err := s.SetSubSwitches("acx", &no, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(v.read(t, feedsPath), "pays") {
		t.Errorf("switching off removes the field:\n%s", v.read(t, feedsPath))
	}
}

func TestRenameStreamMovesEverySourceUnderIt(t *testing.T) {
	s, v, _ := liveSvc(t)
	d := ParseFeeds("")
	d.Add(Subscription{ID: "a", Title: "A", Kind: KindRSS, URL: "https://a.example/feed", List: "essays"})
	d.Add(Subscription{ID: "b", Title: "B", Kind: KindRSS, URL: "https://b.example/feed", List: "essays"})
	d.Add(Subscription{ID: "c", Title: "C", Kind: KindRSS, URL: "https://c.example/feed", List: "science"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameStream("essays", "long reads"); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, sub := range s.Subscriptions() {
		got[sub.ID] = sub.List
	}
	if got["a"] != "long reads" || got["b"] != "long reads" || got["c"] != "science" {
		t.Errorf("rename moved the wrong sources: %v\n%s", got, v.read(t, feedsPath))
	}
	// Renaming into an existing stream merges rather than doubling a heading.
	if err := s.RenameStream("science", "long reads"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(v.read(t, feedsPath), "## long reads"); n != 1 {
		t.Errorf("one heading per stream, got %d:\n%s", n, v.read(t, feedsPath))
	}
	if err := s.RenameStream("unfiled", "x"); err == nil {
		t.Error("unfiled is not a stream and cannot be renamed")
	}
}

func TestTodayTypesAndNavCounts(t *testing.T) {
	s, _, _ := liveSvc(t)
	d := ParseFeeds("")
	d.Add(Subscription{ID: "w", Title: "W", Kind: KindRSS, URL: "https://w.example/feed", List: "essays"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// The first successful poll archives (the backfill rule); these arrive
	// on the second, so they are unread.
	s.store.Commit("w", now.Add(-2*time.Hour), true, nil, nil, "")
	s.store.Commit("w", now.Add(-time.Hour), true, []Item{
		{ID: "consume:rss:w:000000000001", SubID: "w", Title: "fresh essay", URL: "https://w.example/1", PublishedAt: now.Add(-2 * time.Hour)},
		{ID: "consume:rss:w:000000000002", SubID: "w", Title: "old essay", URL: "https://w.example/2", PublishedAt: now.Add(-72 * time.Hour)},
		{ID: "consume:rss:w:000000000003", SubID: "w", Title: "an episode", URL: "https://w.example/3", PublishedAt: now.Add(-3 * time.Hour), Audio: "https://w.example/3.mp3"},
	}, nil, "")
	s.MarkRead("consume:rss:w:000000000001")

	if got := s.Cards(Query{View: "today"}); len(got) != 2 {
		t.Errorf("today is the last day, read or not: %d", len(got))
	}
	if got := s.Cards(Query{View: "unread", Type: MediaPodcast}); len(got) != 1 || got[0].Title != "an episode" {
		t.Errorf("type filter: %+v", got)
	}
	n := s.Nav()
	if n.Unread != 2 || n.Today != 2 || n.Types[MediaPodcast] != 1 || n.Types[MediaArticle] != 1 || n.Streams["essays"] != 2 || n.Subs["w"] != 2 {
		t.Errorf("nav counts: %+v", n)
	}
	if marked := s.MarkViewRead(Query{Type: MediaPodcast}); marked != 1 || s.Unread("") != 1 {
		t.Errorf("mark-read on a type view marks exactly that view: marked %d, unread %d", marked, s.Unread(""))
	}
}

func TestLaterSavesAFeedItemThatOutlivesTheFeed(t *testing.T) {
	s, v, _ := liveSvc(t)
	d := ParseFeeds("")
	d.Add(Subscription{ID: "w", Title: "W", Kind: KindRSS, URL: "https://w.example/feed"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	item := Item{ID: "consume:rss:w:000000000001", SubID: "w", Title: "Keep this", URL: "https://w.example/keep", PublishedAt: now, Body: "<p>the body</p>"}
	s.store.Commit("w", now, true, []Item{item}, nil, "")
	s.store.putBody(item.ID, item.Body)

	e, err := s.SaveLaterItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.SaveLaterItem(item.ID); err != nil || again.ID != e.ID {
		t.Errorf("saving twice is one entry: %v %+v", err, again)
	}
	line := v.read(t, laterPath)
	if strings.Count(line, "[url:: https://w.example/keep]") != 1 || !strings.Contains(line, "[item:: "+item.ID+"]") {
		t.Fatalf("the queue line is in the vault:\n%s", line)
	}
	// The feed card knows it is queued.
	if c := s.Cards(Query{View: "all"}); len(c) != 1 || !c[0].Later || c[0].LaterID != e.ID {
		t.Errorf("the feed card is flagged: %+v", c)
	}
	// The feed forgets it (unsubscribe); the queue still reads it.
	if err := s.Unsubscribe("w"); err != nil {
		t.Fatal(err)
	}
	cards := s.Cards(Query{View: "later"})
	if len(cards) != 1 || cards[0].Title != "Keep this" || cards[0].Pending {
		t.Fatalf("the queue keeps its own copy: %+v", cards)
	}
	got, _, ok := s.Get(cards[0].ID)
	if !ok || got.Body != "<p>the body</p>" {
		t.Errorf("the queued copy opens in the reader with its body: %v %q", ok, got.Body)
	}
	// Opening it does not remove it; done does, and undone brings it back.
	s.MarkRead(cards[0].ID)
	if len(s.Cards(Query{View: "later"})) != 1 {
		t.Error("opening an item must not take it out of Later")
	}
	if err := s.SetLaterDone(e.ID, true); err != nil {
		t.Fatal(err)
	}
	if len(s.Cards(Query{View: "later"})) != 0 || len(s.Cards(Query{View: "later-done"})) != 1 {
		t.Error("done moves it to Later · done")
	}
	if s.Nav().Later != 0 {
		t.Error("the Later count is the queue, not the finished list")
	}
	if err := s.RemoveLater(e.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(v.read(t, laterPath), "keep") {
		t.Error("remove deletes the line")
	}
}

func TestLaterPastedLinkResolvesAndHandEditsSurvive(t *testing.T) {
	v := newVault(t)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>An Essay Worth Keeping</title>
<meta property="og:title" content="An Essay Worth Keeping"><meta property="og:site_name" content="Somewhere"></head>
<body><article><p>`+strings.Repeat("A paragraph of real prose that goes on. ", 60)+`</p></article></body></html>`)
	}))
	t.Cleanup(page.Close)
	s := New(t.TempDir(), v.io(), Config{AllowPrivateCurateFetch: true})
	s.hc = page.Client()

	// A hand-added line with only a URL, and a hand field on it.
	if err := v.io().Write(laterPath, []byte(laterScaffold+"- by hand [url:: https://hand.example/x] [why:: owner note]\n")); err != nil {
		t.Fatal(err)
	}
	e, err := s.SaveLaterURL(context.Background(), page.URL+"/essay?utm_source=share")
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "An Essay Worth Keeping" || e.Kind != "article" {
		t.Errorf("the line takes the piece's own title and kind: %+v", e)
	}
	doc := v.read(t, laterPath)
	if !strings.Contains(doc, "[why:: owner note]") || !strings.Contains(doc, "- by hand [url:: https://hand.example/x]") {
		t.Errorf("hand-written lines are left exactly as typed:\n%s", doc)
	}
	cards := s.Cards(Query{View: "later"})
	if len(cards) != 2 || cards[0].Title != "An Essay Worth Keeping" || cards[0].Minutes < 1 {
		t.Fatalf("newest first, resolved: %+v", cards)
	}
	if !cards[1].Pending {
		t.Errorf("an unresolved hand line still shows, marked pending: %+v", cards[1])
	}
	if _, fresh, err := s.QueueLaterURL(page.URL + "/essay"); err != nil || fresh {
		t.Errorf("the same piece under another spelling is already queued: fresh=%v err=%v", fresh, err)
	}
}

func TestHealSubstackPreviewsRunsOnceAndCompletesFreePosts(t *testing.T) {
	s, _, _ := liveSvc(t)
	srv := substackServer(t)
	s.hc = srv.Client()
	d := ParseFeeds("")
	d.Add(Subscription{ID: "letters", Title: "Letters", Kind: KindRSS, URL: srv.URL + "/feed"})
	if err := s.save(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// what an older build stored: both posts labelled by scraping
	s.store.Commit("letters", now, true, []Item{
		{ID: "consume:rss:letters:000000000001", SubID: "letters", Title: "A Free Post", URL: srv.URL + "/p/a-free-post", PublishedAt: now, Preview: PreviewPartial, Chars: 20},
		{ID: "consume:rss:letters:000000000002", SubID: "letters", Title: "A Paid Post", URL: srv.URL + "/p/a-paid-post", PublishedAt: now, Preview: PreviewPartial, Chars: 20},
	}, nil, "")
	if n := s.HealSubstackPreviews(context.Background()); n != 1 {
		t.Fatalf("one free post completes: %d", n)
	}
	free, _, _ := s.Get("consume:rss:letters:000000000001")
	paid, _, _ := s.Get("consume:rss:letters:000000000002")
	if free.Preview != "" || !strings.Contains(free.Body, "whole free essay") {
		t.Errorf("the free post is whole now: preview=%q", free.Preview)
	}
	if paid.Preview != PreviewPaid {
		t.Errorf("the paid post says paid: %q", paid.Preview)
	}
	if n := s.HealSubstackPreviews(context.Background()); n != 0 {
		t.Errorf("the heal runs once per cache: %d", n)
	}
}

func TestYouTubeChannelIDIsNotMistakenForASecret(t *testing.T) {
	u := "https://www.youtube.com/feeds/videos.xml?channel_id=UCHnyfMqiRRG1u-2MsSQLbXA"
	if got := youtubeChannelIDRe.ReplaceAllString(u, "channel_id=youtube-channel"); strings.Contains(got, "UCHny") {
		t.Errorf("the channel id is scrubbed before the secret scan: %s", got)
	}
}

func TestSubstackPaidPreviewReadAnonymouslyIsPaid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"audience":"only_paid","body_html":%q}`, "<p>"+strings.Repeat("The free part of a paid post. ", 60)+"</p>")
	}))
	t.Cleanup(srv.Close)
	s, _, _ := liveSvc(t)
	s.hc = srv.Client()
	body, paid, ok := s.substackPost(context.Background(), srv.URL+"/p/paid-with-preview", "")
	if !ok || !paid || body == "" {
		t.Errorf("a paid post's preview, read without a session, is paid however long: ok=%v paid=%v", ok, paid)
	}
}
