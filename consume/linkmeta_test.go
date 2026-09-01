package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// THE METADATA LADDER. Every test here serves the page from an httptest
// server, so "the open web" in these tests is a fixture and nothing dials out.

// curateSvc is a service that will fetch a pasted link from a loopback test
// server — the seam described on Config.AllowPrivateCurateFetch.
func curateSvc(t *testing.T, v *fakeVault) *Service {
	t.Helper()
	return New(t.TempDir(), v.io(), Config{AllowPrivateCurateFetch: true})
}

// withProviders swaps the provider table for one pointed at a test server, so
// the platform paths can be exercised without touching Spotify or X.
func withProviders(t *testing.T, ps ...oembedProvider) {
	t.Helper()
	prior := oembedProviders
	oembedProviders = ps
	t.Cleanup(func() { oembedProviders = prior })
}

func htmlPage(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

const ogArticlePage = `<!doctype html><html><head>
<meta property="og:title" content="The Dictatorship of the Articulate" />
<meta property="og:site_name" content="Melissa's Newsletter" />
<meta property="og:description" content="A claim about who gets heard." />
<meta property="article:published_time" content="2026-08-21T14:00:00Z" />
<meta name="author" content="Melissa" />
<title>The Dictatorship of the Articulate — Melissa's Newsletter</title>
</head><body><article>
<p>The first claim is that fluency is mistaken for correctness in nearly every room where a decision is actually made, and the mistake compounds.</p>
<p>The second claim is that the correction is procedural rather than cultural: write it down before you say it, and the articulate lose their edge immediately.</p>
<p>What follows from both is a way of running a meeting that is duller and considerably more accurate than the one it replaces.</p>
</article></body></html>`

func TestResolveLinkReadsOpenGraph(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, ogArticlePage)
	}))
	defer srv.Close()

	s := curateSvc(t, newVault(t))
	m := s.resolveLink(context.Background(), srv.URL+"/p/dictatorship")
	if m.Title != "The Dictatorship of the Articulate" {
		t.Errorf("title: %q", m.Title)
	}
	if m.Source != "Melissa's Newsletter" {
		t.Errorf("source: %q", m.Source)
	}
	if m.Author != "Melissa" {
		t.Errorf("author: %q", m.Author)
	}
	if m.Description == "" {
		t.Error("no description — nothing to fall back to when the fetch fails")
	}
	if m.Published.IsZero() || m.Published.Format("2006-01-02") != "2026-08-21" {
		t.Errorf("published: %v", m.Published)
	}
	if m.Kind != linkArticle {
		t.Errorf("kind: %q", m.Kind)
	}
	if !strings.Contains(m.body, "fluency is mistaken") {
		t.Errorf("the readable body did not come off the same fetch:\n%s", m.body)
	}
	if m.Audio != "" {
		t.Errorf("an article grew audio: %q", m.Audio)
	}
}

func TestCurateFallsBackToPageTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, `<!doctype html><html><head><title>  Just a  Title </title></head>
<body><p>Nothing structured here at all.</p></body></html>`)
	}))
	defer srv.Close()

	s := curateSvc(t, newVault(t))
	m := s.resolveLink(context.Background(), srv.URL+"/bare")
	if m.Title != "Just a Title" {
		t.Errorf("title fallback: %q", m.Title)
	}
	if m.Source == "" {
		t.Error("source should fall back to the host")
	}
}

// A page that advertises oEmbed by discovery and says nothing else about
// itself still gets a title and an author.
func TestCurateOEmbedFallbackWhenNoOG(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oembed" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"rich","title":"A Discovered Piece",
				"author_name":"H. Discovery","provider_name":"Somewhere"}`))
			return
		}
		htmlPage(w, `<!doctype html><html><head>
<link rel="alternate" type="application/json+oembed" href="`+srv.URL+`/oembed" />
</head><body><p>no open graph here</p></body></html>`)
	}))
	defer srv.Close()

	s := curateSvc(t, newVault(t))
	m := s.resolveLink(context.Background(), srv.URL+"/piece")
	if m.Title != "A Discovered Piece" || m.Author != "H. Discovery" || m.Source != "Somewhere" {
		t.Errorf("discovery ladder: %+v", m)
	}
}

// ⚠ THE POSITIVE HALF of the enclosure rule: a page that declares a real
// media file gets one.
func TestCuratePodcastEpisodeCarriesEnclosure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, `<!doctype html><html><head>
<meta property="og:title" content="Episode 12 — The Loop" />
<meta property="og:site_name" content="A Real Show" />
<meta property="og:audio" content="https://cdn.example/audio/ep12.mp3" />
<meta property="og:audio:type" content="audio/mpeg" />
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"PodcastEpisode","name":"Episode 12 — The Loop",
 "duration":"PT1H02M03S","episodeNumber":12,
 "partOfSeason":{"@type":"PodcastSeason","seasonNumber":3},
 "associatedMedia":{"@type":"MediaObject","contentUrl":"https://cdn.example/audio/ep12.mp3",
   "encodingFormat":"audio/mpeg","contentSize":"48210000"}}
</script></head><body><p>show notes</p></body></html>`)
	}))
	defer srv.Close()

	s := curateSvc(t, newVault(t))
	m := s.resolveLink(context.Background(), srv.URL+"/ep/12")
	if m.Audio != "https://cdn.example/audio/ep12.mp3" {
		t.Fatalf("audio: %q", m.Audio)
	}
	if m.AudioType != "audio/mpeg" {
		t.Errorf("audio type: %q", m.AudioType)
	}
	if m.Duration != 3723 {
		t.Errorf("duration: %d, want 3723", m.Duration)
	}
	if m.Episode != 12 || m.Season != 3 {
		t.Errorf("episode/season: %d/%d", m.Episode, m.Season)
	}
	if m.AudioBytes != 48210000 {
		t.Errorf("audio bytes: %d", m.AudioBytes)
	}
	if m.Kind != linkEpisode {
		t.Errorf("kind: %q", m.Kind)
	}
}

// ⚠ THE TEST THIS FEATURE EXISTS TO PASS. A platform episode page has a
// player, not a file. Apple's requirements are explicit that an enclosure is a
// media object a client will fetch and range-request; a Spotify page satisfies
// none of that, and publishing one as an enclosure would break subscribers'
// podcast clients on every curated platform link.
func TestCuratePlatformPageGetsNoEnclosure(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oembed" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"rich","title":"Ep. 12 — The Loop",
				"provider_name":"Spotify","author_name":"A Real Show",
				"html":"<iframe src=\"https://open.spotify.com/embed/episode/4rOoJ6Egrf8K2\"></iframe>"}`))
			return
		}
		// The page's og:audio points at the PLAYER, which is the trap.
		htmlPage(w, `<!doctype html><html><head>
<meta property="og:title" content="Ep. 12 — The Loop" />
<meta property="og:audio" content="https://open.spotify.com/embed/episode/4rOoJ6Egrf8K2" />
<meta property="og:description" content="a conversation about loops" />
</head><body><p>player furniture</p></body></html>`)
	}))
	defer srv.Close()

	withProviders(t, oembedProvider{
		hosts:    []string{hostOf(srv.URL)},
		endpoint: srv.URL + "/oembed",
		kind:     linkPlatform,
		embed:    spotifyEmbed,
	})

	s := curateSvc(t, newVault(t))
	m := s.resolveLink(context.Background(), srv.URL+"/episode/4rOoJ6Egrf8K2")
	if m.Audio != "" {
		t.Fatalf("a player page produced an enclosure URL: %q", m.Audio)
	}
	if m.Kind != linkPlatform {
		t.Errorf("kind: %q", m.Kind)
	}
	if m.Embed != "spotify:episode:4rOoJ6Egrf8K2" {
		t.Errorf("embed descriptor: %q", m.Embed)
	}
	if strings.Contains(m.body, "<iframe") {
		t.Errorf("provider markup reached the body:\n%s", m.body)
	}
}

func TestCurateVideoGetsNoEnclosure(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oembed" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"video","title":"A Talk","author_name":"A Speaker",
				"provider_name":"YouTube","html":"<iframe src=\"https://www.youtube.com/embed/dQw4w9WgXcQ\"></iframe>"}`))
			return
		}
		htmlPage(w, `<!doctype html><html><head><title>A Talk</title></head><body></body></html>`)
	}))
	defer srv.Close()

	withProviders(t, oembedProvider{
		hosts: []string{hostOf(srv.URL)}, endpoint: srv.URL + "/oembed",
		kind: linkVideo, embed: youtubeEmbed,
	})

	s := curateSvc(t, newVault(t))
	m := s.resolveLink(context.Background(), srv.URL+"/watch?v=dQw4w9WgXcQ")
	if m.Audio != "" {
		t.Errorf("a video produced an enclosure: %q", m.Audio)
	}
	if m.Kind != linkVideo {
		t.Errorf("kind: %q", m.Kind)
	}
	if m.Embed != "youtube:video:dQw4w9WgXcQ" {
		t.Errorf("embed descriptor: %q", m.Embed)
	}
}

// X: the provider's blockquote IS the post, and the endpoint needs no bearer
// token — which is the reason this path exists beside RSSHub at all.
func TestCurateXPostUsesOEmbed(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oembed" {
			if r.Header.Get("Authorization") != "" {
				t.Error("the X oEmbed path sent a credential")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"rich","author_name":"melissa","provider_name":"X",
				"html":"<blockquote class=\"twitter-tweet\"><p>the whole post, which is the whole piece</p></blockquote><script async src=\"https://platform.twitter.com/widgets.js\"></script>"}`))
			return
		}
		t.Errorf("the page was fetched for an X post: %s", r.URL.Path)
	}))
	defer srv.Close()

	withProviders(t, oembedProvider{
		hosts: []string{hostOf(srv.URL)}, endpoint: srv.URL + "/oembed",
		kind: linkPost, skipPage: true, embed: func(string) string { return "" },
	})

	s := curateSvc(t, newVault(t))
	if s.xToken != nil {
		t.Fatal("this path must run with no X token wired")
	}
	m := s.resolveLink(context.Background(), srv.URL+"/melissa/status/1")
	if m.Kind != linkPost {
		t.Errorf("kind: %q", m.Kind)
	}
	if m.Title != "melissa on X" {
		t.Errorf("title: %q", m.Title)
	}
	if !strings.Contains(m.body, "the whole post") {
		t.Errorf("the post text is not the body:\n%s", m.body)
	}
	if strings.Contains(strings.ToLower(m.body), "<script") {
		t.Errorf("the widget script survived Sanitize:\n%s", m.body)
	}
}

// The endpoint is down or the URL is not a post: publish the link anyway.
func TestCurateXPostWithoutOEmbedStillPublishes(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oembed" {
			http.NotFound(w, r)
			return
		}
		htmlPage(w, `<!doctype html><html><head><title>melissa on X</title></head><body></body></html>`)
	}))
	defer srv.Close()

	withProviders(t, oembedProvider{
		hosts: []string{hostOf(srv.URL)}, endpoint: srv.URL + "/oembed",
		kind: linkPost, skipPage: true, embed: func(string) string { return "" },
	})

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/melissa/status/1", "worth reading")
	if err != nil {
		t.Fatalf("a dead oEmbed endpoint should not stop a curate: %v", err)
	}
	if entry.Title != "melissa on X" {
		t.Errorf("title: %q", entry.Title)
	}
	if entry.Note != "worth reading" {
		t.Errorf("note: %q", entry.Note)
	}
}

func TestParseISODuration(t *testing.T) {
	for raw, want := range map[string]int{
		"PT1H02M03S": 3723,
		"PT45M":      2700,
		"PT30S":      30,
		"P1DT2H":     93600,
		"01:12:33":   4353, // a clock time, which parseSeconds already read
		"":           0,
		"nonsense":   0,
	} {
		if got := parseISODuration(raw); got != want {
			t.Errorf("parseISODuration(%q) = %d, want %d", raw, got, want)
		}
	}
}

// The gate itself, in isolation: what may become an enclosure and what may not.
func TestSetAudioAcceptsOnlyMediaFiles(t *testing.T) {
	accept := []string{
		"https://cdn.example/ep.mp3",
		"https://dts.podtrac.com/redirect.mp3/cdn.example/ep",
		"https://cdn.example/ep.m4a?token=abc",
	}
	for _, raw := range accept {
		var m LinkMeta
		m.setAudio(raw, "", 0)
		if m.Audio != raw {
			t.Errorf("setAudio declined a media file: %q", raw)
		}
	}
	decline := []string{
		"https://open.spotify.com/embed/episode/4rOoJ6Egrf8K2",
		"https://podcasts.apple.com/us/podcast/x/id123?i=456",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"/relative/ep.mp3",
		"javascript:alert(1)",
		"",
	}
	for _, raw := range decline {
		var m LinkMeta
		m.setAudio(raw, "", 0)
		if m.Audio != "" {
			t.Errorf("setAudio accepted a player page as a file: %q", raw)
		}
	}
	// A declared audio type is enough on its own — some hosts serve a file
	// from an extensionless address.
	var m LinkMeta
	m.setAudio("https://cdn.example/stream/12345", "audio/mpeg", 0)
	if m.Audio == "" || m.AudioType != "audio/mpeg" {
		t.Errorf("a declared audio/* type was declined: %+v", m)
	}
}

func TestEmbedDescriptorsAreParsedFromCanonicalURLs(t *testing.T) {
	for raw, want := range map[string]string{
		"https://open.spotify.com/episode/4rOoJ6Egrf8K2":          "spotify:episode:4rOoJ6Egrf8K2",
		"https://open.spotify.com/intl-de/track/6habFhsOp2NvshLv": "spotify:track:6habFhsOp2NvshLv",
		"https://open.spotify.com/":                               "",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":             "youtube:video:dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ":                            "youtube:video:dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/abc123":                   "youtube:video:abc123",
		"https://www.youtube.com/":                                "",
	} {
		got := ""
		if strings.Contains(raw, "spotify") {
			got = spotifyEmbed(raw)
		} else {
			got = youtubeEmbed(raw)
		}
		if got != want {
			t.Errorf("embed for %q = %q, want %q", raw, got, want)
		}
	}
	if got := vimeoEmbed("https://vimeo.com/347119375"); got != "vimeo:video:347119375" {
		t.Errorf("vimeo embed: %q", got)
	}
}
