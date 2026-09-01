package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CURATING A PASTED LINK, end to end: the address → the ladder → the same
// extrinsic/ note the lane's own button writes → the public feed.

func TestCurateURLWritesTheSameExtrinsicNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, ogArticlePage)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "the middle third is the argument")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(entry.Path, "extrinsic/") {
		t.Fatalf("note written outside extrinsic/: %s", entry.Path)
	}
	note := v.read(t, entry.Path)
	for _, want := range []string{
		"categories: [articles]",
		"source: \"Melissa's Newsletter\"",
		"author: Melissa",
		"published: 2026-08-21",
		"curated: ",
		"item: ext-url-",
		"the middle third is the argument",
		"# The Dictatorship of the Articulate",
		"fluency is mistaken for correctness",
		"Source: [Melissa's Newsletter]",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if entry.Mirror != MirrorFull {
		t.Errorf("mirror: %q — the page gave up its text", entry.Mirror)
	}
	if strings.Contains(note, "<p>") {
		t.Errorf("raw HTML reached the vault:\n%s", note)
	}
	if LinkKind(entry) != linkArticle {
		t.Errorf("kind: %q", LinkKind(entry))
	}
}

func TestCurateURLIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, ogArticlePage)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	first, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "first take")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "second take")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path {
		t.Fatalf("the same link wrote two notes: %s and %s", first.Path, second.Path)
	}
	if got := s.Curated(); len(got) != 1 {
		t.Fatalf("curated projection holds %d entries, want 1: %+v", len(got), got)
	}
	if second.Note != "second take" {
		t.Errorf("the note was not refreshed: %q", second.Note)
	}
}

// The same essay from two share sheets is one essay.
func TestCurateURLIgnoresTrackingParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, ogArticlePage)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	a, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship?utm_source=twitter&ref=home", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Path != b.Path {
		t.Errorf("tracking parameters forked the note: %s vs %s", a.Path, b.Path)
	}
	if ExternalURLID(srv.URL+"/p/dictatorship") != ExternalURLID(srv.URL+"/p/dictatorship?utm_source=x") {
		t.Error("the derived id is not stable across tracking parameters")
	}
}

// ⚠ The owner may write underneath the mirrored article, exactly as he may on
// a note the lane wrote. A second paste must not take that with it.
func TestRecurateURLDoesNotClobberTheOwnersBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, ogArticlePage)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "first take")
	if err != nil {
		t.Fatal(err)
	}
	edited := v.read(t, entry.Path) + "\n\n## My own reading\n\nThe procedural fix is the whole point.\n"
	if err := v.io().Write(entry.Path, []byte(edited)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "second take"); err != nil {
		t.Fatal(err)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "## My own reading") {
		t.Fatalf("a re-paste clobbered the owner's writing:\n%s", note)
	}
	if !strings.Contains(note, "second take") {
		t.Errorf("the note was not refreshed:\n%s", note)
	}
}

// ⚠ THE TRAP IN §5.2. notePath slugs the TITLE. A publisher who re-titles a
// post between two pastes would otherwise write a second note, and nothing
// would ever say so.
func TestCurateURLSurvivesATitleChange(t *testing.T) {
	title := "The Dictatorship of the Articulate"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, `<!doctype html><html><head>
<meta property="og:title" content="`+title+`" />
<meta property="og:site_name" content="Melissa's Newsletter" />
</head><body><article>
<p>The first claim is that fluency is mistaken for correctness in nearly every room where a decision is made, and the mistake compounds over time.</p>
<p>The second claim is that the correction is procedural rather than cultural, and it is cheap enough that no one has an excuse.</p>
</article></body></html>`)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	first, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "")
	if err != nil {
		t.Fatal(err)
	}
	title = "Why the Articulate Win Meetings (2026 revision)"
	second, err := s.CurateURL(context.Background(), srv.URL+"/p/dictatorship", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path {
		t.Fatalf("a re-title forked the note: %s → %s", first.Path, second.Path)
	}
	if len(s.Curated()) != 1 {
		t.Errorf("curated projection holds %d entries, want 1", len(s.Curated()))
	}
}

// A publisher who will not give up the text still gets published — the link
// and the owner's note, stamped excerpt, which is what a linkblog entry is.
func TestCurateBlockedArticleFallsBackToExcerpt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/p/paid", "worth the paywall")
	if err != nil {
		t.Fatalf("a 403 must not stop a curate: %v", err)
	}
	if entry.Mirror != MirrorExcerpt {
		t.Errorf("mirror: %q, want excerpt", entry.Mirror)
	}
	note := v.read(t, entry.Path)
	if !strings.Contains(note, "worth the paywall") {
		t.Errorf("the owner's note is missing:\n%s", note)
	}
	if !strings.Contains(note, srv.URL+"/p/paid") {
		t.Errorf("the link is missing:\n%s", note)
	}
}

func TestCuratePaywalledArticleFallsBackToExcerpt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, `<!doctype html><html><head>
<meta property="og:title" content="A Paid Post" />
<meta property="og:description" content="the publisher's own summary" />
</head><body><article>
<p>This post is for paid subscribers. Subscribe to keep reading, and get the archive as well as everything that comes next.</p>
<p>Already a paid subscriber? Sign in to read the rest of this post and the whole archive going back several years.</p>
</article></body></html>`)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/p/paid", "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Mirror != MirrorExcerpt {
		t.Errorf("mirror: %q — a subscribe box was published as the article", entry.Mirror)
	}
	note := v.read(t, entry.Path)
	if strings.Contains(note, "Subscribe to keep reading") {
		t.Errorf("the subscribe box reached the vault:\n%s", note)
	}
	if !strings.Contains(note, "the publisher's own summary") {
		t.Errorf("the honest excerpt is missing:\n%s", note)
	}
}

// ---- papers ----

// stubCrossref answers one work, the way api.crossref.org does.
func stubCrossref(t *testing.T, doi string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, doi) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","message":{
			"DOI":"` + doi + `",
			"title":["Sub-millivolt Signalling in Cortical Microcircuits"],
			"container-title":["Nature Communications"],
			"publisher":"Springer Nature",
			"abstract":"<jats:p>We report a sparse error-signal population.</jats:p>",
			"issued":{"date-parts":[[2026,7,14]]},
			"author":[{"given":"A.","family":"Rivera"},{"given":"B.","family":"Okafor"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCurateDOIURLProducesPaperMetadata(t *testing.T) {
	const doi = "10.1038/s41467-026-76758-z"
	cr := stubCrossref(t, doi)

	v := newVault(t)
	s := curateSvc(t, v)
	s.crossref = cr.URL

	entry, err := s.CurateURL(context.Background(), "https://doi.org/"+doi, "the lesion evidence")
	if err != nil {
		t.Fatal(err)
	}
	if entry.DOI != doi {
		t.Fatalf("doi: %q", entry.DOI)
	}
	if entry.Journal != "Nature Communications" {
		t.Errorf("journal: %q", entry.Journal)
	}
	if len(entry.Authors) != 2 {
		t.Errorf("authors: %v", entry.Authors)
	}
	if LinkKind(entry) != "paper" {
		t.Errorf("kind: %q", LinkKind(entry))
	}
	// The registry's title, not the URL's host — this is what notePath slugs.
	if !strings.Contains(entry.Path, "sub-millivolt") {
		t.Errorf("the note is not named after the paper: %s", entry.Path)
	}
	note := v.read(t, entry.Path)
	for _, want := range []string{"## Abstract", "## Citation", "the lesion evidence"} {
		if !strings.Contains(note, want) {
			t.Errorf("paper note missing %q:\n%s", want, note)
		}
	}

	// …and the public feed carries it as a paper.
	feed := get(t, PublicHandler(s, PublicConfig{Title: "reading"}), "/feed.xml").Body.String()
	for _, want := range []string{"prism:doi", "dc:identifier", doi} {
		if !strings.Contains(feed, want) {
			t.Errorf("the public feed is missing %q:\n%s", want, feed)
		}
	}
	if strings.Contains(feed, "<enclosure") {
		t.Error("a paper grew an enclosure")
	}
}

// A journal page with the DOI hidden in its article id takes the paper branch
// too — and never fetches the page, because a paper is looked up.
func TestCurateJournalPageWithEmbeddedDOI(t *testing.T) {
	cr := stubCrossref(t, "10.1038/s41467-026-76758-z")
	v := newVault(t)
	s := curateSvc(t, v)
	s.crossref = cr.URL

	entry, err := s.CurateURL(context.Background(),
		"https://www.nature.com/articles/s41467-026-76758-z", "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.DOI == "" {
		t.Fatalf("the publisher rule did not resolve a DOI: %+v", entry)
	}
	if entry.URL != "https://www.nature.com/articles/s41467-026-76758-z" {
		t.Errorf("the <link> should stay the page the owner read: %q", entry.URL)
	}
}

// ---- the public feed ----

// ⚠ The negative case, asserted where it actually matters: in the XML a
// subscriber's podcast client parses.
func TestPublicFeedGivesAPlatformLinkNoEnclosure(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oembed" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"type":"rich","title":"Ep. 12 — The Loop","provider_name":"Spotify",
				"html":"<iframe src=\"https://open.spotify.com/embed/episode/4rOoJ6Egrf8K2\"></iframe>"}`))
			return
		}
		htmlPage(w, `<!doctype html><html><head>
<meta property="og:title" content="Ep. 12 — The Loop" />
<meta property="og:audio" content="https://open.spotify.com/embed/episode/4rOoJ6Egrf8K2" />
<meta property="og:description" content="a conversation about loops" />
</head><body></body></html>`)
	}))
	defer srv.Close()
	withProviders(t, oembedProvider{
		hosts: []string{hostOf(srv.URL)}, endpoint: srv.URL + "/oembed",
		kind: linkPlatform, embed: spotifyEmbed,
	})

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/episode/4rOoJ6Egrf8K2", "worth an hour")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Audio != "" {
		t.Fatalf("a platform page produced an enclosure URL: %q", entry.Audio)
	}
	if entry.Embed != "spotify:episode:4rOoJ6Egrf8K2" {
		t.Errorf("the private-side descriptor is missing: %q", entry.Embed)
	}

	feed := get(t, PublicHandler(s, PublicConfig{Title: "reading"}), "/feed.xml").Body.String()
	for _, unwanted := range []string{"<enclosure", "<itunes:duration", "<iframe", "spotify:episode"} {
		if strings.Contains(feed, unwanted) {
			t.Errorf("the public feed grew %q:\n%s", unwanted, feed)
		}
	}
	if !strings.Contains(feed, "Ep. 12") || !strings.Contains(feed, "worth an hour") {
		t.Errorf("the public feed lost the link or the note:\n%s", feed)
	}
}

// The positive half, in the XML: a real media file is re-attached.
func TestPublicFeedCarriesACuratedEpisodeEnclosure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, `<!doctype html><html><head>
<meta property="og:title" content="Episode 12 — The Loop" />
<meta property="og:site_name" content="A Real Show" />
<meta property="og:audio" content="https://cdn.example/audio/ep12.mp3" />
<meta property="og:audio:type" content="audio/mpeg" />
<script type="application/ld+json">
{"@type":"PodcastEpisode","duration":"PT45M","episodeNumber":12}
</script></head><body><article>
<p>The show notes for this episode run long, because the conversation covered three separate arguments and each of them needs its own summary here.</p>
<p>The second argument is the one worth the hour, and it starts about twenty minutes in after the usual throat-clearing about the news.</p>
</article></body></html>`)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateURL(context.Background(), srv.URL+"/ep/12", "")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Audio != "https://cdn.example/audio/ep12.mp3" {
		t.Fatalf("audio: %q", entry.Audio)
	}
	if LinkKind(entry) != linkEpisode {
		t.Errorf("kind: %q", LinkKind(entry))
	}
	feed := get(t, PublicHandler(s, PublicConfig{Title: "reading"}), "/feed.xml").Body.String()
	for _, want := range []string{`<enclosure`, "cdn.example/audio/ep12.mp3", `type="audio/mpeg"`, "<itunes:duration>45:00"} {
		if !strings.Contains(feed, want) {
			t.Errorf("the public feed is missing %q:\n%s", want, feed)
		}
	}
}

// ---- boundaries ----

func TestCurateURLNeedsTheWriteCapability(t *testing.T) {
	s := New(t.TempDir(), VaultIO{}, Config{AllowPrivateCurateFetch: true})
	if _, err := s.CurateURL(context.Background(), "https://example.com/p/x", ""); err == nil {
		t.Fatal("curated with no write capability")
	}
}

func TestCurateURLRejectsABadLinkBeforeFetching(t *testing.T) {
	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{}) // the guard as production runs it
	for _, raw := range []string{"", "not a link", "file:///etc/passwd", "http://127.0.0.1/x"} {
		if _, err := s.CurateURL(context.Background(), raw, ""); err == nil {
			t.Errorf("CurateURL accepted %q", raw)
		}
	}
	if v.writes != 0 {
		t.Errorf("a rejected link still wrote %d times to the vault", v.writes)
	}
}

// ⚠ THE BRIDGE'S OUTPUT IS UNCHANGED. ExternalRef grew optional media fields
// for this feature; a FEED card sets none of them, and its note must be the
// note it was — no audio, no embed, no itunes anything.
func TestFeedCurateOutputUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlPage(w, ogArticlePage)
	}))
	defer srv.Close()

	v := newVault(t)
	s := curateSvc(t, v)
	entry, err := s.CurateExternal(context.Background(), ExternalRef{
		ID: "card-1", Title: "The Dictatorship of the Articulate",
		URL: srv.URL + "/p/dictatorship", Source: "arXiv",
		Fallback: "the first lesion evidence",
	}, "a note")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ItemID != "ext-card-1" {
		t.Errorf("a bridged card's id changed: %q", entry.ItemID)
	}
	note := v.read(t, entry.Path)
	for _, unwanted := range []string{"audio:", "audioType:", "embed:", "duration:", "episode:", "season:"} {
		if strings.Contains(note, unwanted) {
			t.Errorf("a bridged card's note grew %q:\n%s", unwanted, note)
		}
	}
}
