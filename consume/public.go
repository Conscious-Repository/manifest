package consume

import (
	_ "embed"
	"encoding/xml"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The public curation feed — the only thing in this system that faces the open
// internet unauthenticated. Everything else is loopback + Tailscale or
// OAuth-gated, so this file carries the whole weight of that difference.
//
// The isolation argument is structural, and it is ONE INTERFACE WIDE:
//
//	type CuratedFeed interface { Entries() []CuratedEntry }
//
// The handler is constructed holding a CuratedFeed and nothing else. It has no
// reference to the item cache, the subscription list, the vault, the server or
// the config beyond its own channel identity — so serving a private item is not
// something this code declines to do, it is something it has no way to express.
//
// ⚠ Curated notes live in the VAULT, which is what makes that claim worth
// re-checking rather than assuming: the projection behind this interface reads
// extrinsic/ directly. What keeps it honest is parseCurated's selection rule —
// a note must declare `categories: [articles]` AND carry a `curated:` date —
// and TestPublicFeedServesOnlyCuratedItems, which stuffs a vault with private
// notes and asserts none of them can be reached from this handler.
//
// A second method on this interface is a design decision, not a convenience.

// CuratedFeed is EXACTLY what the public handler may call.
type CuratedFeed interface {
	Entries() []CuratedEntry
}

// The Service is the real implementation; this is the compile-time canary that
// keeps the two in step.
var _ CuratedFeed = (*Service)(nil)

// PublicConfig is the feed's own identity. It carries no secrets and no paths.
type PublicConfig struct {
	Title       string // channel title
	Description string
	BaseURL     string // public base, for the channel link and self-reference
	Author      string
}

// feedCap bounds the feed. The full archive stays in the vault; a reader wants
// the recent past.
const feedCap = 50

// The channel's cover art. It lives HERE, inside the package that serves the
// public feed, rather than in the dashboard's asset tree: the isolation
// argument is that this listener holds a CuratedFeed and its own identity and
// reaches nothing else, and an image it reads out of another package's
// embedded filesystem would be a second thing it reaches.
//
//go:embed assets/feed.png
var feedImage []byte

// feedImagePath is the ONE asset path this listener serves, and the same
// string the channel points <image> and itunes:image at.
const feedImagePath = "/assets/feed.png"

// PublicHandler serves the curated feed and nothing else.
func PublicHandler(feed CuratedFeed, cfg PublicConfig) http.Handler {
	if strings.TrimSpace(cfg.Title) == "" {
		cfg.Title = "curated"
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write([]byte(FeedXML(feed.Entries(), cfg)))
	})

	// The third route, and the only one that is not text: the cover art the
	// channel names in <image>. A reader that shows a feed logo fetches this.
	mux.HandleFunc("GET "+feedImagePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(feedImage)
	})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The mirror must never compete with the source in search results.
		// Precedent: aionbio/public/_headers.
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		_, _ = w.Write([]byte(indexHTML(feed.Entries(), cfg)))
	})

	// Anything else — including every /api path the dashboard serves — is not
	// merely unauthorized here, it does not exist.
	return mux
}

// ---- RSS 2.0 ----

type rssChannelItem struct {
	XMLName xml.Name `xml:"item"`
	Title   string   `xml:"title"`
	Link    string   `xml:"link"`
	GUID    rssGUID  `xml:"guid"`
	PubDate string   `xml:"pubDate"`
	// dc:creator repeats, one element per author — the Dublin Core way to say
	// "eight people wrote this", and the reason a paper's byline survives into
	// a citation manager instead of collapsing to one string.
	Creators    []string  `xml:"dc:creator,omitempty"`
	Source      string    `xml:"source,omitempty"`
	Description string    `xml:"description"`
	Encoded     *rssCDATA `xml:"content:encoded,omitempty"`
	// The bibliographic fields, emitted only for PAPERS (see paperFields).
	// Crossref's recommendation names exactly these; a reader that knows none
	// of them ignores them and still renders title, link and description.
	DCTitle      string `xml:"dc:title,omitempty"`
	DCPublisher  string `xml:"dc:publisher,omitempty"`
	DCDate       string `xml:"dc:date,omitempty"`
	DCIdentifier string `xml:"dc:identifier,omitempty"`
	PrismDOI     string `xml:"prism:doi,omitempty"`
	PrismURL     string `xml:"prism:url,omitempty"`
	PrismPubName string `xml:"prism:publicationName,omitempty"`
	PrismPubDate string `xml:"prism:publicationDate,omitempty"`
	// The EPISODE fields, emitted only for a curated PODCAST (see
	// episodeFields). <enclosure> is what makes the item playable in every
	// podcast client and every RSS reader with a player; the itunes: elements
	// are what make it say "episode 412, 1:12:33" instead of "an attachment".
	Enclosure      *rssEnclosure `xml:"enclosure,omitempty"`
	ItunesDuration string        `xml:"itunes:duration,omitempty"`
	ItunesEpisode  string        `xml:"itunes:episode,omitempty"`
	ItunesSeason   string        `xml:"itunes:season,omitempty"`
}

// rssEnclosure is the attached media file. RSS requires all three attributes,
// and length is the one publishers omit — a reader that pre-allocates on it
// copes with 0, so an unknown size is stated as 0 rather than guessed.
type rssEnclosure struct {
	XMLName xml.Name `xml:"enclosure"`
	URL     string   `xml:"url,attr"`
	Type    string   `xml:"type,attr"`
	Length  string   `xml:"length,attr"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type rssCDATA struct {
	Value string `xml:",cdata"`
}

type rssAtomLink struct {
	XMLName xml.Name `xml:"atom:link"`
	Href    string   `xml:"href,attr"`
	Rel     string   `xml:"rel,attr"`
	Type    string   `xml:"type,attr"`
}

type rssChannel struct {
	XMLName       xml.Name `xml:"channel"`
	Title         string   `xml:"title"`
	Link          string   `xml:"link"`
	Description   string   `xml:"description"`
	Generator     string   `xml:"generator"`
	LastBuildDate string   `xml:"lastBuildDate,omitempty"`
	AtomLink      *rssAtomLink
	// The channel artwork, twice: <image> is the RSS 2.0 element every reader
	// has understood for twenty years, itunes:image is the one podcast clients
	// and most modern readers actually look at. Both are nil without a
	// BaseURL, because artwork the reader cannot fetch is worse than none.
	Image       *rssImage
	ItunesImage *rssItunesImage
	Items       []rssChannelItem
}

// rssImage is the channel's logo. <width>/<height> are optional and omitted;
// readers scale the file they get.
type rssImage struct {
	XMLName xml.Name `xml:"image"`
	URL     string   `xml:"url"`
	Title   string   `xml:"title"`
	Link    string   `xml:"link"`
}

type rssItunesImage struct {
	XMLName xml.Name `xml:"itunes:image"`
	Href    string   `xml:"href,attr"`
}

type rssRoot struct {
	XMLName   xml.Name `xml:"rss"`
	Version   string   `xml:"version,attr"`
	ContentNS string   `xml:"xmlns:content,attr"`
	DCNS      string   `xml:"xmlns:dc,attr"`
	PrismNS   string   `xml:"xmlns:prism,attr"`
	AtomNS    string   `xml:"xmlns:atom,attr"`
	ItunesNS  string   `xml:"xmlns:itunes,attr"`
	Channel   rssChannel
}

// FeedXML renders the curated entries as RSS 2.0.
//
// The element choices are the convention, not an invention:
//
//	<link>              the ORIGINAL url — credit and traffic go to the writer
//	<description>       the owner's note (falling back to an excerpt)
//	<content:encoded>   the mirrored body
//	<guid isPermaLink="false">  stable identity across re-renders
//
// Putting a personal note in <description> alongside the body in
// <content:encoded> is exactly how linkblogs have always done it; every reader
// renders both correctly.
func FeedXML(entries []CuratedEntry, cfg PublicConfig) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	ch := rssChannel{
		Title:       cfg.Title,
		Link:        firstNonEmpty(base, "https://example.invalid"),
		Description: firstNonEmpty(cfg.Description, "Things worth reading."),
		Generator:   "manifest",
	}
	if base != "" {
		ch.AtomLink = &rssAtomLink{Href: base + "/feed.xml", Rel: "self", Type: "application/rss+xml"}
		// Derived from the channel's own identity — the artwork is served by
		// the same handler that serves this document, so there is nothing to
		// configure and nothing to keep in step.
		art := base + feedImagePath
		ch.Image = &rssImage{URL: art, Title: ch.Title, Link: ch.Link}
		ch.ItunesImage = &rssItunesImage{Href: art}
	}
	if len(entries) > feedCap {
		entries = entries[:feedCap]
	}
	// lastBuildDate comes from the newest curation, not from the clock, so the
	// same content always renders the same bytes (golden-testable, and a
	// conditional GET upstream stays meaningful).
	if len(entries) > 0 {
		ch.LastBuildDate = pubDate(entries[0].Curated)
	}

	for _, e := range entries {
		it := rssChannelItem{
			Title:       e.Title,
			Link:        e.URL,
			GUID:        rssGUID{IsPermaLink: "false", Value: firstNonEmpty(e.ItemID, e.URL, e.Path)},
			PubDate:     pubDate(firstNonEmpty(e.Curated, e.Published)),
			Creators:    creators(e),
			Source:      e.Source,
			Description: description(e),
		}
		paperFields(&it, e)
		episodeFieldsXML(&it, e)
		if body := bodyHTML(e); body != "" {
			it.Encoded = &rssCDATA{Value: attribution(e) + body}
		}
		ch.Items = append(ch.Items, it)
	}

	out, err := xml.MarshalIndent(rssRoot{
		Version:   "2.0",
		ContentNS: "http://purl.org/rss/1.0/modules/content/",
		DCNS:      "http://purl.org/dc/elements/1.1/",
		PrismNS:   "http://prismstandard.org/namespaces/basic/2.0/",
		AtomNS:    "http://www.w3.org/2005/Atom",
		ItunesNS:  "http://www.itunes.com/dtds/podcast-1.0.dtd",
		Channel:   ch,
	}, "", "  ")
	if err != nil {
		return xml.Header + "<rss version=\"2.0\"><channel><title>" +
			html.EscapeString(cfg.Title) + "</title></channel></rss>\n"
	}
	return xml.Header + string(out) + "\n"
}

// creators is the item's byline as Dublin Core. A paper's is its author list;
// everything else has the one name the note recorded.
func creators(e CuratedEntry) []string {
	if len(e.Authors) > 0 {
		return e.Authors
	}
	if a := strings.TrimSpace(e.Author); a != "" {
		return []string{a}
	}
	return nil
}

// paperFields adds the bibliographic elements Crossref's recommendation for
// scholarly feeds asks for — and adds them ONLY to papers.
//
// The DOI is the flag and the identity. A post has none, gets none of these,
// and its item is byte-identical to what this feed emitted before papers were
// a category: a blog or an X post is still title + link + description + the
// whole text in content:encoded.
//
// <link> stays the publisher's URL. The DOI is the work's IDENTITY, which is
// what dc:identifier and prism:doi are for; <link> is where a reader is being
// sent, and sending them to the page the owner actually read keeps this file's
// standing rule — credit and traffic go to the source.
func paperFields(it *rssChannelItem, e CuratedEntry) {
	doi := strings.TrimSpace(e.DOI)
	if doi == "" {
		return
	}
	date := firstNonEmpty(e.Published, e.Curated)
	it.DCTitle = e.Title
	it.DCPublisher = e.Journal
	it.DCDate = date
	it.DCIdentifier = "doi:" + doi
	it.PrismDOI = doi
	it.PrismURL = firstNonEmpty(e.URL, "https://doi.org/"+doi)
	it.PrismPubName = e.Journal
	it.PrismPubDate = date
}

// episodeFieldsXML attaches the audio to a curated PODCAST, and only to one.
//
// Audio is the flag and the payload, the way DOI is for a paper. An entry
// without it gets no enclosure and no itunes: element, and its item is
// byte-identical to what this feed emitted before podcasts were a thing here.
//
// The channel is still deliberately NOT dressed as a podcast feed — no
// itunes:category, no owner block, no explicit flag. It has artwork, which any
// feed may have and which is the linkblog's own logo; what it does not have is
// the metadata that would claim to be a SHOW, because that would be a claim
// about every other item in it. A subscriber's client plays the episode from
// the enclosure regardless; that is what the element is for.
func episodeFieldsXML(it *rssChannelItem, e CuratedEntry) {
	audio := strings.TrimSpace(e.Audio)
	if audio == "" {
		return
	}
	length := "0"
	if e.AudioBytes > 0 {
		length = strconv.FormatInt(e.AudioBytes, 10)
	}
	it.Enclosure = &rssEnclosure{
		URL:    audio,
		Type:   firstNonEmpty(strings.TrimSpace(e.AudioType), "audio/mpeg"),
		Length: length,
	}
	it.ItunesDuration = FormatDuration(e.Duration)
	if e.Episode > 0 {
		it.ItunesEpisode = strconv.Itoa(e.Episode)
	}
	if e.Season > 0 {
		it.ItunesSeason = strconv.Itoa(e.Season)
	}
}

// bodyHTML is the whole piece, ready to render, and it is what makes "carry
// the content" a property of the FEED rather than of the cache underneath it.
//
// Two sources, in order:
//
//	the dataDir snapshot   sanitized at fetch time, the publisher's own markup
//	the curated note       markdown in the vault, rendered by ToHTML
//
// The second is the durable one. A snapshot is disposable — pruned at 90 days,
// gone with the directory, absent for anything polled before this cache
// existed — while the note is versioned in the owner's vault and holds the
// same article. Reaching for it means a wiped dataDir costs the feed nothing
// but the publisher's exact markup; before, it cost the reader the piece.
//
// mirror: excerpt is the one thing that stops both: the owner said carry a
// link and an excerpt for this source, and neither source of body overrides
// that.
func bodyHTML(e CuratedEntry) string {
	if strings.EqualFold(e.Mirror, MirrorExcerpt) {
		return ""
	}
	if h := strings.TrimSpace(e.HTML); h != "" {
		return h // already allowlisted; Sanitize is idempotent, not free
	}
	return ToHTML(e.Body)
}

// description is what a reader shows in the list: the owner's note if he wrote
// one, otherwise an excerpt of the piece. The note is the reason to subscribe.
func description(e CuratedEntry) string {
	if n := strings.TrimSpace(e.Note); n != "" {
		return n
	}
	if b := strings.TrimSpace(e.Body); b != "" {
		return Excerpt(collapse(b), 280)
	}
	return e.Title
}

// attribution is the one-line header on every mirrored body, naming the writer
// and linking home.
func attribution(e CuratedEntry) string {
	who := firstNonEmpty(e.Author, e.Source)
	var b strings.Builder
	b.WriteString(`<p><em>`)
	if who != "" {
		b.WriteString("Originally by " + html.EscapeString(who))
		if e.URL != "" {
			b.WriteString(" — ")
		}
	}
	if e.URL != "" {
		b.WriteString(`<a href="` + html.EscapeString(e.URL) + `">read at the source</a>`)
	}
	b.WriteString(`</em></p>`)
	if note := strings.TrimSpace(e.Note); note != "" {
		b.WriteString(`<blockquote><p>` + html.EscapeString(note) + `</p></blockquote>`)
	}
	return b.String()
}

// pubDate renders a stored date as RFC1123Z, the RSS convention.
func pubDate(stored string) string {
	if t := parseDate(stored); !t.IsZero() {
		return t.Format(time.RFC1123Z)
	}
	return ""
}

// indexHTML is a deliberately plain page, and it carries the WHOLE piece the
// way the feed does: a reader who follows a link from anywhere should land on
// the writing, not on a table of contents pointing back out. No JavaScript, no
// tracking, no styling ambition — one document per curated note, in order.
//
// The surface stays exactly three routes — this page, the feed, and the
// channel artwork the feed names. There is no per-entry permalink here on
// purpose: every additional path on the one listener that faces the open
// internet is a thing the isolation test has to re-argue, and an index that
// already holds the bodies does not need one.
func indexHTML(entries []CuratedEntry, cfg PublicConfig) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString(`<meta name="robots" content="noindex, nofollow">`)
	b.WriteString(`<title>` + html.EscapeString(cfg.Title) + `</title>`)
	b.WriteString(`<link rel="alternate" type="application/rss+xml" title="` +
		html.EscapeString(cfg.Title) + `" href="feed.xml">`)
	// The same artwork the channel names, for anything that unfurls a link.
	// Absolute, because a preview fetcher does not resolve against this page.
	if base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"); base != "" {
		art := html.EscapeString(base + feedImagePath)
		b.WriteString(`<link rel="icon" href="` + art + `">`)
		b.WriteString(`<meta property="og:title" content="` + html.EscapeString(cfg.Title) + `">`)
		b.WriteString(`<meta property="og:image" content="` + art + `">`)
	}
	b.WriteString(`<style>
:root{color-scheme:light dark}
body{max-width:38rem;margin:4rem auto;padding:0 1.25rem;
  font:16px/1.6 ui-serif,Georgia,serif}
h1{font-size:1.25rem;letter-spacing:.02em;text-transform:uppercase;
  font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-weight:600}
.sub{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.75rem;
  opacity:.6;margin-bottom:3rem}
li{margin:0 0 4.5rem;list-style:none}
ul{padding:0}
a{color:inherit}
.meta{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;
  font-size:.7rem;opacity:.6;text-transform:uppercase;letter-spacing:.04em}
.note{margin:.35rem 0 0;opacity:.85}
.t{font-size:1.1rem;font-weight:600;margin:0}
.body{margin:1.5rem 0}
.body img{max-width:100%;height:auto}
.body pre{overflow-x:auto;font-size:.85rem;white-space:pre-wrap}
.body blockquote{margin:1rem 0;padding-left:1rem;border-left:2px solid;opacity:.85}
.body h1,.body h2,.body h3,.body h4{font-size:1rem;margin:1.75rem 0 .5rem}
.ep{width:100%;margin:.75rem 0 .25rem}
</style></head><body>`)
	b.WriteString(`<h1>` + html.EscapeString(cfg.Title) + `</h1>`)
	b.WriteString(`<p class="sub">` + html.EscapeString(firstNonEmpty(cfg.Description, "Things worth reading.")) +
		` &middot; <a href="feed.xml">rss</a></p>`)
	if len(entries) == 0 {
		b.WriteString(`<p class="meta">nothing yet</p>`)
	}
	b.WriteString(`<ul>`)
	for i, e := range entries {
		if i >= feedCap {
			break
		}
		b.WriteString(`<li><h2 class="t"><a href="` + html.EscapeString(e.URL) + `">` +
			html.EscapeString(e.Title) + `</a></h2>`)
		meta := firstNonEmpty(e.Author, e.Source)
		if e.Curated != "" {
			meta = strings.TrimPrefix(meta+" · "+e.Curated, " · ")
		}
		if meta != "" {
			b.WriteString(`<div class="meta">` + html.EscapeString(meta) + `</div>`)
		}
		if n := strings.TrimSpace(e.Note); n != "" {
			b.WriteString(`<p class="note">` + html.EscapeString(n) + `</p>`)
		}
		// An episode plays here too — the index carries the piece, and for a
		// podcast the piece is the audio.
		if audio := strings.TrimSpace(e.Audio); audio != "" {
			b.WriteString(`<audio class="ep" controls preload="none" src="` +
				html.EscapeString(audio) + `"></audio>`)
			if d := FormatDuration(e.Duration); d != "" {
				b.WriteString(`<div class="meta">` + html.EscapeString(d) + `</div>`)
			}
		}
		if body := bodyHTML(e); body != "" {
			b.WriteString(`<div class="body">` + body + `</div>`)
		} else if x := strings.TrimSpace(e.Body); x != "" {
			// mirror: excerpt — the owner asked for a taste and a link.
			b.WriteString(`<p>` + html.EscapeString(Excerpt(collapse(x), 280)) + `</p>`)
		}
		if e.URL != "" {
			b.WriteString(`<p class="meta"><a href="` + html.EscapeString(e.URL) +
				`">read at the source</a></p>`)
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul></body></html>`)
	return b.String()
}

// Entries implements CuratedFeed: the curated notes with their bodies
// resolved.
//
// This resolves the SNAPSHOT — the publisher's own markup, sanitized at fetch
// time — and only that. When it is gone (dataDir wiped, item aged out of the
// cache) the entry travels with its markdown body from the note, and bodyHTML
// renders that instead, so what the feed carries is the piece either way.
// Losing a cache must never cost a reader something the owner published.
func (s *Service) Entries() []CuratedEntry {
	out := s.curatedEntries()
	resolved := make([]CuratedEntry, 0, len(out))
	for _, e := range out {
		if e.ItemID != "" && !strings.EqualFold(e.Mirror, MirrorExcerpt) {
			e.HTML = s.store.Body(e.ItemID)
		}
		resolved = append(resolved, e)
	}
	return resolved
}
