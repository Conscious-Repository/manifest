package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const rssFixture = `<?xml version="1.0"?>
<rss version="2.0"><channel>
  <title>The Imaging Podcast</title>
  <link>https://imaging.test</link>
  <description>Conversations about &lt;b&gt;magnets&lt;/b&gt; and the people who build them.</description>
  <item>
    <title>Low-field MRI with Dana Reyes</title>
    <link>https://imaging.test/ep/41</link>
    <description>We talk portable scanners with Dana Reyes of Hyperfine.</description>
    <pubDate>Tue, 02 Sep 2026 09:00:00 GMT</pubDate>
  </item>
  <item>
    <title>Building coils with less noise</title>
    <link>https://imaging.test/ep/40</link>
    <description>A technical episode about gradient design.</description>
  </item>
  <item>
    <title>Episode 39</title>
    <link>https://imaging.test/ep/39</link>
    <description>Guest: Kai Okonkwo, who runs the WashU coil lab, joined by Priya Raman.</description>
  </item>
</channel></rss>`

const atomFixture = `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Field Notes</title>
  <subtitle>a blog</subtitle>
  <link rel="alternate" href="https://notes.test"/>
  <entry>
    <title>An interview with Jane Q Smith</title>
    <link rel="alternate" href="https://notes.test/p/1"/>
    <summary>On building things.</summary>
  </entry>
</feed>`

func newFeedServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFeedPreviewNamesTheShow(t *testing.T) {
	srv := newFeedServer(t, rssFixture, 200)
	f := Feed{Client: *srv.Client()}
	got, err := f.Preview(context.Background(), srv.URL+"/rss")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "media" || got.Name != "The Imaging Podcast" {
		t.Fatalf("preview: %+v", got)
	}
	if got.URL != "https://imaging.test" || got.Feed != srv.URL+"/rss" {
		t.Fatalf("site and feed are different things: %+v", got)
	}
	if got.Total != 3 {
		t.Fatalf("episodes: %d", got.Total)
	}
	if strings.Contains(got.Note, "<b>") {
		t.Fatalf("markup reached the scaffold: %q", got.Note)
	}
	if len(got.Facts) == 0 {
		t.Fatal("every filled field names where it came from")
	}
	for _, fct := range got.Facts {
		if fct.Source != "feed" || fct.URL == "" {
			t.Fatalf("provenance: %+v", fct)
		}
	}
}

// Explicit credits only. "Building coils with less noise" must name nobody —
// that is the whole reason the pattern demands capitalised words.
func TestFeedSearchTakesOnlyExplicitCredits(t *testing.T) {
	srv := newFeedServer(t, rssFixture, 200)
	f := Feed{Client: *srv.Client()}
	got, err := f.Search(context.Background(), Scope{
		Role: "role/mri-engineer", Max: 25,
		Fields: map[string]string{"feed_url": srv.URL + "/rss"},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range got {
		names[d.Name] = true
	}
	for _, want := range []string{"Dana Reyes", "Kai Okonkwo", "Priya Raman"} {
		if !names[want] {
			t.Fatalf("missed an explicit credit %q: %v", want, names)
		}
	}
	for got := range names {
		if strings.EqualFold(got, "less noise") || strings.HasPrefix(got, "Less") {
			t.Fatalf("a sentence became a person: %q", got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("three credited people: %+v", names)
	}
	for _, d := range got {
		if d.Role != "role/mri-engineer" {
			t.Fatalf("role: %+v", d)
		}
		if len(d.Evidence) == 0 || d.Evidence[0].Trust != TrustLow {
			t.Fatalf("an episode is low trust: %+v", d.Evidence)
		}
		if !strings.HasPrefix(d.Evidence[0].URLOrFile, "https://imaging.test/ep/") {
			t.Fatalf("the citation is the EPISODE: %q", d.Evidence[0].URLOrFile)
		}
		if !strings.Contains(d.Evidence[0].Snippet, "The Imaging Podcast") {
			t.Fatalf("the snippet carries the show and the episode: %q", d.Evidence[0].Snippet)
		}
		// appearing on a show is not a relationship
		if len(d.Edges) != 0 {
			t.Fatalf("%s: a guest slot is not an edge: %+v", d.Name, d.Edges)
		}
	}
}

func TestFeedReadsAtomToo(t *testing.T) {
	srv := newFeedServer(t, atomFixture, 200)
	f := Feed{Client: *srv.Client()}
	prev, err := f.Preview(context.Background(), srv.URL+"/atom")
	if err != nil {
		t.Fatal(err)
	}
	if prev.Name != "Field Notes" || prev.URL != "https://notes.test" || prev.Total != 1 {
		t.Fatalf("atom preview: %+v", prev)
	}
	got, err := f.Search(context.Background(), Scope{Max: 5,
		Fields: map[string]string{"feed_url": srv.URL + "/atom"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Jane Q Smith" {
		t.Fatalf("atom credits: %+v", got)
	}
	if got[0].Evidence[0].URLOrFile != "https://notes.test/p/1" {
		t.Fatalf("atom entry link: %q", got[0].Evidence[0].URLOrFile)
	}
}

func TestFeedQueryFiltersEpisodes(t *testing.T) {
	srv := newFeedServer(t, rssFixture, 200)
	f := Feed{Client: *srv.Client()}
	got, _ := f.Search(context.Background(), Scope{Max: 25, Query: "portable scanners",
		Fields: map[string]string{"feed_url": srv.URL + "/rss"}})
	if len(got) != 1 || got[0].Name != "Dana Reyes" {
		t.Fatalf("the filter scopes which episodes are read: %+v", got)
	}
}

func TestFeedRefusesWhatIsNotAFeed(t *testing.T) {
	f := Feed{}
	if _, err := f.Preview(context.Background(), "not a url"); err == nil {
		t.Error("a bare string is not a feed")
	}
	srv := newFeedServer(t, "<html><body>hello</body></html>", 200)
	f = Feed{Client: *srv.Client()}
	if _, err := f.Preview(context.Background(), srv.URL); err == nil {
		t.Error("an HTML page is not a feed")
	}
	srv404 := newFeedServer(t, "", 404)
	f = Feed{Client: *srv404.Client()}
	if _, err := f.Preview(context.Background(), srv404.URL); err == nil ||
		!strings.Contains(err.Error(), "404") {
		t.Errorf("a 404 says so: %v", err)
	}
}

func TestFeedScopeAndIdentity(t *testing.T) {
	if (Feed{}).ID() != "feed" || (Feed{}).Kind() != KindWeb {
		t.Error("id/kind")
	}
	sc := (Feed{}).Scope()
	if len(sc) != 4 || sc[1].Key != "feed_url" || !sc[1].Required {
		t.Fatalf("a feed run needs the feed: %+v", sc)
	}
	if _, err := (Feed{}).Search(context.Background(), Scope{}); err == nil {
		t.Error("a run with no feed is refused")
	}
}
