package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const substackish = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:dc="http://purl.org/dc/elements/1.1/">
<channel>
  <title>Melissa's Newsletter</title>
  <link>https://melissa.substack.com</link>
  <item>
    <title>The Dictatorship of the Articulate</title>
    <link>https://melissa.substack.com/p/dictatorship</link>
    <guid isPermaLink="false">https://melissa.substack.com/p/dictatorship</guid>
    <pubDate>Fri, 21 Aug 2026 14:02:00 GMT</pubDate>
    <dc:creator>Melissa</dc:creator>
    <description>A teaser that is much shorter.</description>
    <content:encoded><![CDATA[<p>There is a particular kind of person who can make <em>any</em> position sound reasonable.</p><script>alert(1)</script>]]></content:encoded>
  </item>
  <item>
    <title>Second Post</title>
    <link>https://melissa.substack.com/p/second</link>
    <guid isPermaLink="false">guid-second</guid>
    <pubDate>Mon, 18 Aug 2026 09:00:00 GMT</pubDate>
    <description>Only a description here.</description>
  </item>
</channel>
</rss>`

const atomish = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>An Atom Blog</title>
  <link href="https://atom.example/feed" rel="self"/>
  <link href="https://atom.example/" rel="alternate" type="text/html"/>
  <entry>
    <title>Atom Entry</title>
    <link href="https://atom.example/self" rel="self"/>
    <link href="https://atom.example/posts/1" rel="alternate" type="text/html"/>
    <id>tag:atom.example,2026:1</id>
    <published>2026-08-20T10:00:00Z</published>
    <author><name>A. Writer</name></author>
    <content type="html">&lt;p&gt;Atom body.&lt;/p&gt;</content>
  </entry>
</feed>`

func serve(t *testing.T, body string, hdr map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchRSSExtractsFullBody(t *testing.T) {
	srv := serve(t, substackish, nil)
	f := rssFetcher{hc: srv.Client()}
	items, _, err := f.Fetch(context.Background(), Subscription{ID: "melissa", Kind: KindRSS, URL: srv.URL, Title: "Melissa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	got := items[0]
	if got.Title != "The Dictatorship of the Articulate" {
		t.Errorf("title: %q", got.Title)
	}
	if got.Author != "Melissa" {
		t.Errorf("dc:creator not read: %q", got.Author)
	}
	if got.URL != "https://melissa.substack.com/p/dictatorship" {
		t.Errorf("link: %q", got.URL)
	}
	if got.PublishedAt.IsZero() {
		t.Error("pubDate did not parse")
	}
	// content:encoded must beat description — the whole point of the reader.
	if !strings.Contains(got.Body, "particular kind of person") {
		t.Errorf("content:encoded not preferred over description: %q", got.Body)
	}
	if strings.Contains(got.Body, "script") {
		t.Errorf("body was not sanitized: %q", got.Body)
	}
	if got.Chars == 0 {
		t.Error("plain-text length not computed")
	}
	// Falls back to description when there is no content:encoded.
	if !strings.Contains(items[1].Body, "Only a description") {
		t.Errorf("description fallback failed: %q", items[1].Body)
	}
}

func TestFetchAtom(t *testing.T) {
	srv := serve(t, atomish, nil)
	f := rssFetcher{hc: srv.Client()}
	items, _, err := f.Fetch(context.Background(), Subscription{ID: "atom", Kind: KindRSS, URL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 entry, got %d", len(items))
	}
	it := items[0]
	if it.Author != "A. Writer" {
		t.Errorf("atom author: %q", it.Author)
	}
	// rel="self" is a machine link; the card must point at the human page.
	if it.URL != "https://atom.example/posts/1" {
		t.Errorf("picked the wrong link (rel=self leaked?): %q", it.URL)
	}
	if !strings.Contains(it.Body, "Atom body") {
		t.Errorf("atom content: %q", it.Body)
	}
	if it.Source != "An Atom Blog" {
		t.Errorf("feed title not used as source: %q", it.Source)
	}
}

// Identity must be stable across re-polls or every poll re-notifies the owner
// about writing he already read.
func TestItemIDsAreStableAcrossPolls(t *testing.T) {
	srv := serve(t, substackish, nil)
	f := rssFetcher{hc: srv.Client()}
	sub := Subscription{ID: "melissa", Kind: KindRSS, URL: srv.URL}
	a, _, err := f.Fetch(context.Background(), sub, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := f.Fetch(context.Background(), sub, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("item %d id changed between polls: %s vs %s", i, a[i].ID, b[i].ID)
		}
	}
	if strings.Contains(a[0].ID, "/") {
		t.Errorf("id contains a slash and will break {id} routing: %s", a[0].ID)
	}
}

// A 304 is a successful poll with no news. Treating it as an error would
// degrade the subscription; treating it as an empty result would age the
// cache out. It must be neither.
func TestConditionalGETNotModified(t *testing.T) {
	var sawINM string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawINM = r.Header.Get("If-None-Match")
		if sawINM == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(substackish))
	}))
	defer srv.Close()

	f := rssFetcher{hc: srv.Client()}
	sub := Subscription{ID: "s", Kind: KindRSS, URL: srv.URL}
	items, cur, err := f.Fetch(context.Background(), sub, nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("first poll: %v items=%d", err, len(items))
	}
	if cur["etag"] != `"v1"` {
		t.Fatalf("etag not captured: %v", cur)
	}
	items, cur2, err := f.Fetch(context.Background(), sub, cur)
	if err != nil {
		t.Fatalf("304 must not be an error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("304 should yield no items, got %d", len(items))
	}
	if cur2["etag"] != `"v1"` {
		t.Errorf("cursor lost across a 304: %v", cur2)
	}
}

// Real feeds are malformed. Strict=false is the reason this passes; a naked
// ampersand in one post must not take down a whole subscription.
func TestParsesMalformedFeeds(t *testing.T) {
	cases := map[string]string{
		"naked ampersand": `<rss><channel><title>T</title><item><title>Tom & Jerry</title><link>https://e.com/1</link><guid>1</guid></item></channel></rss>`,
		"undeclared namespace prefix": `<rss><channel><title>T</title><item><title>X</title><guid>2</guid>` +
			`<content:encoded>&lt;p&gt;body&lt;/p&gt;</content:encoded><dc:creator>Someone</dc:creator></item></channel></rss>`,
		"unknown entity": `<rss><channel><title>T</title><item><title>caf&eacute;</title><guid>3</guid></item></channel></rss>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, body, nil)
			f := rssFetcher{hc: srv.Client()}
			items, _, err := f.Fetch(context.Background(), Subscription{ID: "s", URL: srv.URL}, nil)
			if err != nil {
				t.Fatalf("malformed feed rejected: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("want 1 item, got %d", len(items))
			}
		})
	}
}

// Feeds still declare legacy encodings; without a CharsetReader these do not
// parse at all.
func TestParsesLegacyCharset(t *testing.T) {
	body := "<?xml version=\"1.0\" encoding=\"iso-8859-1\"?>" +
		"<rss><channel><title>T</title><item><title>caf\xe9</title><guid>1</guid></item></channel></rss>"
	srv := serve(t, body, nil)
	f := rssFetcher{hc: srv.Client()}
	items, _, err := f.Fetch(context.Background(), Subscription{ID: "s", URL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("iso-8859-1 feed rejected: %v", err)
	}
	if len(items) != 1 || !strings.Contains(items[0].Title, "café") {
		t.Fatalf("charset not decoded: %+v", items)
	}
}

func TestFetchErrorsAreErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	f := rssFetcher{hc: srv.Client()}
	if _, _, err := f.Fetch(context.Background(), Subscription{ID: "s", URL: srv.URL}, nil); err == nil {
		t.Fatal("a 500 must surface as an error, not as an empty poll")
	}
	if _, _, err := f.Fetch(context.Background(), Subscription{ID: "s", URL: ""}, nil); err == nil {
		t.Fatal("an empty url must be an error")
	}
}

func TestDiscoverFeedFromHTMLPage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(substackish))
	})
	var base string
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head>` +
			`<link rel="alternate" type="application/rss+xml" href="/feed.xml">` +
			`</head><body>hi</body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	f := rssFetcher{hc: srv.Client()}
	got, title, err := f.Discover(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if got != base+"/feed.xml" {
		t.Errorf("relative href not resolved against the page: %q", got)
	}
	if title != "Melissa's Newsletter" {
		t.Errorf("feed title not returned as the default name: %q", title)
	}
}

func TestDiscoverAcceptsADirectFeedURL(t *testing.T) {
	srv := serve(t, substackish, nil)
	f := rssFetcher{hc: srv.Client()}
	got, title, err := f.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got != srv.URL || title == "" {
		t.Errorf("direct feed url: got %q %q", got, title)
	}
}

func TestDiscoverRejectsNonFeeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>no feed here</body></html>`))
	}))
	defer srv.Close()
	f := rssFetcher{hc: srv.Client()}
	if _, _, err := f.Discover(context.Background(), srv.URL); err == nil {
		t.Fatal("a page with no feed must fail loudly, not subscribe to nothing")
	}
	if _, _, err := f.Discover(context.Background(), "  "); err == nil {
		t.Fatal("empty input must error")
	}
}

func TestParseDate(t *testing.T) {
	for _, in := range []string{
		"Fri, 21 Aug 2026 14:02:00 GMT",
		"Fri, 21 Aug 2026 14:02:00 +0000",
		"2026-08-21T14:02:00Z",
		"2026-08-21",
		"Fri, 21 Aug 2026 14:02:00 -0500",
	} {
		if parseDate(in).IsZero() {
			t.Errorf("failed to parse %q", in)
		}
	}
	// An unparseable date must not drop the post.
	if !parseDate("last tuesday").IsZero() {
		t.Error("nonsense date should yield the zero time")
	}
}
