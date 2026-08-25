package consume

import (
	"encoding/xml"
	"html"
	"net/http"
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
	XMLName     xml.Name  `xml:"item"`
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	GUID        rssGUID   `xml:"guid"`
	PubDate     string    `xml:"pubDate"`
	Creator     string    `xml:"dc:creator,omitempty"`
	Source      string    `xml:"source,omitempty"`
	Description string    `xml:"description"`
	Encoded     *rssCDATA `xml:"content:encoded,omitempty"`
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
	Items         []rssChannelItem
}

type rssRoot struct {
	XMLName   xml.Name `xml:"rss"`
	Version   string   `xml:"version,attr"`
	ContentNS string   `xml:"xmlns:content,attr"`
	DCNS      string   `xml:"xmlns:dc,attr"`
	AtomNS    string   `xml:"xmlns:atom,attr"`
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
			Creator:     e.Author,
			Source:      e.Source,
			Description: description(e),
		}
		if body := strings.TrimSpace(e.HTML); body != "" && !strings.EqualFold(e.Mirror, MirrorExcerpt) {
			it.Encoded = &rssCDATA{Value: attribution(e) + body}
		}
		ch.Items = append(ch.Items, it)
	}

	out, err := xml.MarshalIndent(rssRoot{
		Version:   "2.0",
		ContentNS: "http://purl.org/rss/1.0/modules/content/",
		DCNS:      "http://purl.org/dc/elements/1.1/",
		AtomNS:    "http://www.w3.org/2005/Atom",
		Channel:   ch,
	}, "", "  ")
	if err != nil {
		return xml.Header + "<rss version=\"2.0\"><channel><title>" +
			html.EscapeString(cfg.Title) + "</title></channel></rss>\n"
	}
	return xml.Header + string(out) + "\n"
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

// indexHTML is a deliberately plain page: a list of what the owner is reading,
// each linking to the original, plus the feed. No JavaScript, no tracking, no
// styling ambition.
func indexHTML(entries []CuratedEntry, cfg PublicConfig) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString(`<meta name="robots" content="noindex, nofollow">`)
	b.WriteString(`<title>` + html.EscapeString(cfg.Title) + `</title>`)
	b.WriteString(`<link rel="alternate" type="application/rss+xml" title="` +
		html.EscapeString(cfg.Title) + `" href="feed.xml">`)
	b.WriteString(`<style>
:root{color-scheme:light dark}
body{max-width:38rem;margin:4rem auto;padding:0 1.25rem;
  font:16px/1.6 ui-serif,Georgia,serif}
h1{font-size:1.25rem;letter-spacing:.02em;text-transform:uppercase;
  font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-weight:600}
.sub{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.75rem;
  opacity:.6;margin-bottom:3rem}
li{margin:0 0 1.75rem;list-style:none}
ul{padding:0}
a{color:inherit}
.meta{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;
  font-size:.7rem;opacity:.6;text-transform:uppercase;letter-spacing:.04em}
.note{margin:.35rem 0 0;opacity:.85}
.t{font-weight:600}
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
		b.WriteString(`<li><a class="t" href="` + html.EscapeString(e.URL) + `">` +
			html.EscapeString(e.Title) + `</a>`)
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
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul></body></html>`)
	return b.String()
}

// Entries implements CuratedFeed: the curated notes with their bodies
// resolved.
//
// The body comes from the dataDir snapshot when it is still there. When it is
// not — dataDir was wiped, or the item aged out of the cache — the entry keeps
// its metadata and its note and the feed degrades to a link. Losing a cache
// must never silently drop something the owner published.
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
