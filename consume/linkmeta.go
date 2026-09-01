package consume

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// LINK METADATA — turning a pasted address into enough of a piece to curate.
//
// A subscription hands the lane a title, an author, a date and often the body.
// A pasted URL hands us nothing, so this file asks the open web the four
// questions it has standard answers for, in the order of how much they can be
// trusted:
//
//	oEmbed        the provider's own structured answer about its own URL
//	Open Graph    what the page tells every social card renderer
//	JSON-LD       schema.org PodcastEpisode, where a show publishes one
//	<title>       the last honest thing a page always has
//
// Each step is allowed to fail. An unresolvable link still curates — as a
// link, a title and the owner's note, which is what a linkblog entry always
// was. resolveLink therefore returns no error: there is no failure a caller
// could act on differently.
//
// ⚠ THE RULE THIS FILE EXISTS TO KEEP. Audio is set ONLY from a real media
// file: an og:audio (or JSON-LD contentUrl) whose type is audio/* or whose
// path ends in a media extension. A provider's oEmbed `html` is a PLAYER — an
// iframe pointing at a web page — and turning one into an <enclosure> would
// publish a link a podcast client will try to stream and fail on. public.go
// keys the enclosure off CuratedEntry.Audio, so declining to set it here is
// not a check that can be forgotten; it is the absence of a value.

// LinkMeta is what the ladder recovered. Every field is optional.
type LinkMeta struct {
	Title       string
	Author      string
	Source      string
	Description string
	Published   time.Time
	Image       string

	// The episode fields, set only when a real media file was found.
	Audio      string
	AudioType  string
	AudioBytes int64
	Duration   int // seconds
	Episode    int
	Season     int

	// Embed is an allowlisted `provider:kind:id` tuple parsed out of the
	// canonical URL — never the provider's own markup. The private reader
	// builds a player from it with a hardcoded per-provider template; the
	// public feed never sees it (see §5.5 of the plan and public.go).
	Embed string

	// Kind routes the note: article | episode | platform | video | post.
	// (paper is decided before this file runs — a paper is looked up, not
	// scraped.)
	Kind string

	// body is the readable article, extracted from the SAME fetch that read
	// the metadata, so curating a pasted link costs one request to the page
	// rather than two. Empty for platform pages, where "readable content" is
	// navigation furniture around a player.
	body string
}

// The kinds. They name what the piece IS, which is what decides how the note
// reads; they are not a new attention kind and never leave this package except
// as a word in the endpoint's answer.
const (
	linkArticle  = "article"
	linkEpisode  = "episode"
	linkPlatform = "platform"
	linkVideo    = "video"
	linkPost     = "post"
)

// maxOEmbed bounds one oEmbed response. Providers answer in kilobytes.
const maxOEmbed = 1 << 20

// resolveLink walks the ladder for one pasted page.
func (s *Service) resolveLink(ctx context.Context, pageURL string) LinkMeta {
	m := LinkMeta{Kind: linkArticle, Source: hostOf(pageURL)}

	// Step 1 — the provider's own answer, where we know the provider. This is
	// the only step that can produce an Embed, because it is the only step
	// that knows the URL's shape well enough to name what it points at.
	p, isProvider := providerFor(pageURL)
	if isProvider {
		m.Kind = p.kind
		m.Embed = p.embed(pageURL)
		if p.endpoint != "" {
			if doc, ok := s.fetchOEmbed(ctx, p.endpoint, pageURL); ok {
				m.applyOEmbed(doc, p)
			}
		}
	}

	// Steps 2–6 — one fetch of the page, one parse, one walk.
	if !(isProvider && p.skipPage && m.Title != "") {
		if doc, ok := s.fetchPage(ctx, pageURL); ok {
			head := scanHead(doc)
			// Step 2 — oEmbed by discovery, for a provider we do not know.
			if !isProvider && head.oembed != "" {
				if od, ok := s.fetchOEmbedAt(ctx, resolveRef(pageURL, head.oembed)); ok {
					m.applyOEmbed(od, oembedProvider{})
				}
			}
			m.applyOpenGraph(head)     // step 3
			m.applyJSONLD(head.jsonLD) // step 4
			m.applyNames(head)         // step 5
			if m.Title == "" {         // step 6
				m.Title = head.title
			}
			// A platform page's "article" is the furniture around a player.
			if m.Kind == linkArticle {
				m.body = Readable(doc)
			}
		}
	}

	if m.Title == "" {
		m.Title = firstNonEmpty(m.Source, hostOf(pageURL))
	}
	if m.Source == "" {
		m.Source = hostOf(pageURL)
	}
	// Audio is the flag, exactly as it is everywhere else in this package.
	if m.Audio != "" {
		m.Kind = linkEpisode
	}
	return m
}

// ---- step 1: the provider table ----

// oembedProvider is one platform we know by sight. The table is deliberately
// short: discovery (step 2) covers everything else, and every entry that rots
// degrades to "link plus the owner's note" rather than to an error.
type oembedProvider struct {
	hosts    []string
	endpoint string
	kind     string
	// skipPage marks a provider whose page is a JavaScript shell — fetching
	// it costs a request and yields a login wall.
	skipPage bool
	// embed parses the allowlisted descriptor out of the URL, or "" when the
	// URL is not a shape we can build a player from.
	embed func(raw string) string
}

var oembedProviders = []oembedProvider{
	{
		hosts:    []string{"open.spotify.com", "spotify.com"},
		endpoint: "https://open.spotify.com/oembed",
		kind:     linkPlatform,
		embed:    spotifyEmbed,
	},
	{
		hosts:    []string{"youtube.com", "youtu.be", "m.youtube.com", "music.youtube.com"},
		endpoint: "https://www.youtube.com/oembed",
		kind:     linkVideo,
		embed:    youtubeEmbed,
	},
	{
		hosts:    []string{"vimeo.com", "player.vimeo.com"},
		endpoint: "https://vimeo.com/api/oembed.json",
		kind:     linkVideo,
		embed:    vimeoEmbed,
	},
	{
		// X's endpoint needs no bearer token, which is the whole reason this
		// path exists: consume/x.go draws the line at a paid API, and RSSHub
		// carries the subscriptions. One pasted post is not a subscription.
		hosts:    []string{"x.com", "twitter.com", "mobile.x.com", "mobile.twitter.com"},
		endpoint: "https://publish.x.com/oembed",
		kind:     linkPost,
		skipPage: true,
		embed:    func(string) string { return "" }, // a script tag is not an embed we will build
	},
	{
		hosts:    []string{"soundcloud.com"},
		endpoint: "https://soundcloud.com/oembed",
		kind:     linkPlatform,
		embed:    func(string) string { return "" },
	},
	{
		// Apple publishes no oEmbed for podcasts; its Open Graph tags are
		// good, and step 3 reads those.
		hosts: []string{"podcasts.apple.com", "music.apple.com"},
		kind:  linkPlatform,
		embed: func(string) string { return "" },
	},
}

func providerFor(raw string) (oembedProvider, bool) {
	h := hostOf(raw)
	if h == "" {
		return oembedProvider{}, false
	}
	for _, p := range oembedProviders {
		for _, want := range p.hosts {
			if h == want || strings.HasSuffix(h, "."+want) {
				return p, true
			}
		}
	}
	return oembedProvider{}, false
}

// embedID is what an allowlisted descriptor's id may contain. Anything else is
// not an id we recognized, and a descriptor we did not parse is one the reader
// will not build a frame from.
var embedID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var spotifyPath = regexp.MustCompile(`^/(?:intl-[a-z-]+/)?(episode|track|album|playlist|show|artist)/([A-Za-z0-9]+)`)

func spotifyEmbed(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	m := spotifyPath.FindStringSubmatch(u.Path)
	if m == nil || !embedID.MatchString(m[2]) {
		return ""
	}
	return "spotify:" + m[1] + ":" + m[2]
}

func youtubeEmbed(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	id := ""
	switch {
	case strings.HasSuffix(strings.ToLower(u.Hostname()), "youtu.be"):
		id = strings.TrimPrefix(u.Path, "/")
	case strings.HasPrefix(u.Path, "/shorts/"):
		id = strings.TrimPrefix(u.Path, "/shorts/")
	case strings.HasPrefix(u.Path, "/embed/"):
		id = strings.TrimPrefix(u.Path, "/embed/")
	default:
		id = u.Query().Get("v")
	}
	id = strings.Trim(id, "/")
	if !embedID.MatchString(id) {
		return ""
	}
	return "youtube:video:" + id
}

var vimeoPath = regexp.MustCompile(`^/(?:video/)?(\d{6,12})`)

func vimeoEmbed(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	m := vimeoPath.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return "vimeo:video:" + m[1]
}

// ---- oEmbed ----

// oembedDoc is the subset of the oEmbed response [oembed.com] this lane can
// use. `html` is read for a POST only, where the provider's blockquote IS the
// piece; it goes through Sanitize like every other fetched fragment.
type oembedDoc struct {
	Type         string `json:"type"`
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	ProviderName string `json:"provider_name"`
	ThumbnailURL string `json:"thumbnail_url"`
	HTML         string `json:"html"`
}

func (s *Service) fetchOEmbed(ctx context.Context, endpoint, pageURL string) (oembedDoc, bool) {
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	return s.fetchOEmbedAt(ctx, endpoint+sep+"format=json&url="+url.QueryEscape(pageURL))
}

func (s *Service) fetchOEmbedAt(ctx context.Context, endpoint string) (oembedDoc, bool) {
	if _, err := s.guardPasted(endpoint); err != nil {
		return oembedDoc{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := s.getGuarded(ctx, endpoint, "application/json")
	if err != nil {
		return oembedDoc{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return oembedDoc{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxOEmbed))
	if err != nil {
		return oembedDoc{}, false
	}
	var doc oembedDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return oembedDoc{}, false
	}
	return doc, true
}

func (m *LinkMeta) applyOEmbed(d oembedDoc, p oembedProvider) {
	m.Title = firstNonEmpty(m.Title, strings.TrimSpace(d.Title))
	m.Author = firstNonEmpty(m.Author, strings.TrimSpace(d.AuthorName))
	m.Source = firstNonEmpty(strings.TrimSpace(d.ProviderName), m.Source)
	m.Image = firstNonEmpty(m.Image, strings.TrimSpace(d.ThumbnailURL))
	if strings.EqualFold(d.Type, "video") && m.Kind == linkArticle {
		m.Kind = linkVideo
	}
	// A POST is short enough that the provider's own rendering IS the piece.
	// Sanitize strips the script tag X ships beside the blockquote.
	if p.kind == linkPost && strings.TrimSpace(d.HTML) != "" {
		if body := strings.TrimSpace(Sanitize(d.HTML)); body != "" {
			m.body = body
			if m.Author != "" {
				m.Title = firstNonEmpty(m.Title, m.Author+" on "+firstNonEmpty(m.Source, "X"))
			}
		}
	}
}

// ---- the page: one fetch, one parse, one walk ----

// headMeta is everything one pass over the document collected.
type headMeta struct {
	meta   map[string]string // og:*/twitter:*/name=… → content, first wins
	oembed string            // <link rel=alternate type=application/json+oembed>
	title  string
	jsonLD []string
}

func (s *Service) fetchPage(ctx context.Context, pageURL string) (*html.Node, bool) {
	ctx, cancel := context.WithTimeout(ctx, guardTimeout)
	defer cancel()
	resp, err := s.getGuarded(ctx, pageURL, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return nil, false
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, maxArticle))
	if err != nil {
		return nil, false
	}
	return doc, true
}

// scanHead collects every <meta>, the oEmbed <link>, the <title> and the
// JSON-LD blocks in ONE walk — the shape of findFeedLink (rss.go), generalized
// so six properties do not cost six traversals.
func scanHead(n *html.Node) headMeta {
	h := headMeta{meta: map[string]string{}}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "meta":
				var key, content string
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "property", "name", "itemprop":
						if key == "" {
							key = strings.ToLower(strings.TrimSpace(a.Val))
						}
					case "content":
						content = strings.TrimSpace(a.Val)
					}
				}
				if key != "" && content != "" {
					if _, seen := h.meta[key]; !seen {
						h.meta[key] = content
					}
				}
			case "link":
				var rel, typ, href string
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "rel":
						rel = strings.ToLower(a.Val)
					case "type":
						typ = strings.ToLower(a.Val)
					case "href":
						href = strings.TrimSpace(a.Val)
					}
				}
				if h.oembed == "" && href != "" && strings.Contains(typ, "oembed") &&
					(rel == "" || strings.Contains(rel, "alternate")) {
					h.oembed = href
				}
			case "title":
				if h.title == "" {
					h.title = collapseSpaces(nodeText(n))
				}
			case "script":
				for _, a := range n.Attr {
					if strings.EqualFold(a.Key, "type") && strings.Contains(strings.ToLower(a.Val), "ld+json") {
						if t := strings.TrimSpace(rawText(n)); t != "" {
							h.jsonLD = append(h.jsonLD, t)
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return h
}

// rawText is the literal text under a node. nodeText (sanitize.go) is the
// right reader everywhere else in this package precisely because it drops
// script and style — which is exactly the content JSON-LD lives in.
func rawText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// ---- step 3: Open Graph ----

func (m *LinkMeta) applyOpenGraph(h headMeta) {
	m.Title = firstNonEmpty(m.Title, h.meta["og:title"], h.meta["twitter:title"])
	m.Description = firstNonEmpty(m.Description, h.meta["og:description"], h.meta["twitter:description"], h.meta["description"])
	m.Source = firstNonEmpty(h.meta["og:site_name"], m.Source)
	m.Image = firstNonEmpty(m.Image, h.meta["og:image"], h.meta["twitter:image"])
	m.Author = firstNonEmpty(m.Author, h.meta["article:author"], h.meta["author"])
	if m.Published.IsZero() {
		m.Published = parseMetaTime(firstNonEmpty(
			h.meta["article:published_time"], h.meta["og:article:published_time"],
			h.meta["datepublished"], h.meta["date"]))
	}
	// og:audio is a provider DECLARING a media file. It is the one property
	// that can create an enclosure, and only when it actually names one.
	audio := firstNonEmpty(h.meta["og:audio:secure_url"], h.meta["og:audio:url"], h.meta["og:audio"])
	m.setAudio(audio, h.meta["og:audio:type"], 0)
	if m.Kind == linkArticle && strings.EqualFold(h.meta["og:type"], "video.other") {
		m.Kind = linkVideo
	}
}

func (m *LinkMeta) applyNames(h headMeta) {
	m.Author = firstNonEmpty(m.Author, h.meta["author"], h.meta["twitter:creator"])
	m.Source = firstNonEmpty(m.Source, h.meta["application-name"])
}

// setAudio is THE gate. It accepts a URL only when the page said it is audio
// or the address itself is a media file; a player page, an iframe src or a
// share link is declined, and Audio stays empty.
func (m *LinkMeta) setAudio(raw, mime string, size int64) {
	raw = strings.TrimSpace(raw)
	if raw == "" || externalURL(raw) == "" {
		return
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	isAudio := strings.HasPrefix(mime, "audio/")
	if !isAudio && !mediaFilePath(raw) {
		return
	}
	// The FIRST accepted declaration is the file. A later step naming the same
	// file may still fill in what the first one left out — og:audio states a
	// URL and a type, JSON-LD states the size — but a different URL is a
	// second candidate, and picking between them is not a guess to make.
	if m.Audio == "" {
		m.Audio = raw
		m.AudioType = firstNonEmpty(mimeIf(isAudio, mime), mimeForMedia(raw))
	} else if !SameLink(m.Audio, raw) {
		return
	}
	if size > 0 && m.AudioBytes == 0 {
		m.AudioBytes = size
	}
}

func mimeIf(ok bool, mime string) string {
	if ok {
		return mime
	}
	return ""
}

// mediaExt matches an audio file's extension anywhere a path can end it —
// including mid-path, which is how the redirect-prefix hosts (podtrac and
// friends) write a URL.
var mediaExt = regexp.MustCompile(`(?i)\.(mp3|m4a|m4b|aac|ogg|oga|opus|wav|flac|mpga)($|/)`)

func mediaFilePath(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return mediaExt.MatchString(u.Path)
}

func mimeForMedia(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "audio/mpeg"
	}
	m := mediaExt.FindStringSubmatch(u.Path)
	if m == nil {
		return "audio/mpeg"
	}
	switch strings.ToLower(m[1]) {
	case "m4a", "m4b", "aac":
		return "audio/mp4"
	case "ogg", "oga", "opus":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "flac":
		return "audio/flac"
	}
	return "audio/mpeg"
}

func parseMetaTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// ---- step 4: schema.org PodcastEpisode ----

// applyJSONLD reads the episode fields off a page's structured data. It is a
// cleaner source than parsing a duration out of prose, and it is where a show
// that publishes properly states its own episode and season numbers.
func (m *LinkMeta) applyJSONLD(blobs []string) {
	for _, blob := range blobs {
		var v any
		if err := json.Unmarshal([]byte(blob), &v); err != nil {
			continue
		}
		walkJSONLD(v, func(obj map[string]any) {
			if !ldTypeIn(obj["@type"], "podcastepisode", "episode", "audioobject", "radioepisode") {
				return
			}
			m.Title = firstNonEmpty(m.Title, ldString(obj["name"]), ldString(obj["headline"]))
			m.Description = firstNonEmpty(m.Description, ldString(obj["description"]))
			if m.Duration == 0 {
				m.Duration = parseISODuration(ldString(obj["duration"]))
			}
			if m.Episode == 0 {
				m.Episode = ldInt(obj["episodeNumber"])
			}
			if m.Season == 0 {
				if season, ok := obj["partOfSeason"].(map[string]any); ok {
					m.Season = ldInt(season["seasonNumber"])
				}
			}
			if series, ok := obj["partOfSeries"].(map[string]any); ok {
				m.Source = firstNonEmpty(ldString(series["name"]), m.Source)
			}
			if m.Published.IsZero() {
				m.Published = parseMetaTime(ldString(obj["datePublished"]))
			}
			for _, media := range ldObjects(obj["associatedMedia"], obj["audio"]) {
				m.setAudio(ldString(media["contentUrl"]),
					ldString(media["encodingFormat"]), ldInt64(media["contentSize"]))
				if m.Duration == 0 {
					m.Duration = parseISODuration(ldString(media["duration"]))
				}
			}
			// contentUrl directly on the episode, which some shows emit.
			m.setAudio(ldString(obj["contentUrl"]), ldString(obj["encodingFormat"]), 0)
		})
	}
}

// walkJSONLD visits every object in a JSON-LD document, including @graph
// arrays and nested nodes.
func walkJSONLD(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, child := range t {
			walkJSONLD(child, fn)
		}
	case []any:
		for _, child := range t {
			walkJSONLD(child, fn)
		}
	}
}

func ldTypeIn(v any, want ...string) bool {
	var have []string
	switch t := v.(type) {
	case string:
		have = []string{t}
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				have = append(have, s)
			}
		}
	}
	for _, h := range have {
		h = strings.ToLower(strings.TrimSpace(h))
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}

func ldString(v any) string {
	switch t := v.(type) {
	case string:
		return collapseSpaces(strings.TrimSpace(t))
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		if len(t) > 0 {
			return ldString(t[0])
		}
	case map[string]any:
		return ldString(t["name"])
	}
	return ""
}

func ldInt(v any) int { return int(ldInt64(v)) }

func ldInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// ldObjects flattens the "one object or a list of them" shape JSON-LD uses
// everywhere.
func ldObjects(vals ...any) []map[string]any {
	var out []map[string]any
	for _, v := range vals {
		switch t := v.(type) {
		case map[string]any:
			out = append(out, t)
		case []any:
			for _, x := range t {
				if o, ok := x.(map[string]any); ok {
					out = append(out, o)
				}
			}
		}
	}
	return out
}

// isoDuration matches the ISO 8601 form schema.org states a duration in.
var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// parseISODuration turns PT1H2M3S into seconds. A page may also state a bare
// count or a clock time; parseSeconds (rss.go) already reads both.
func parseISODuration(raw string) int {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return 0
	}
	m := isoDuration.FindStringSubmatch(raw)
	if m == nil {
		return parseSeconds(raw)
	}
	atoi := func(s string) int {
		n, _ := strconv.Atoi(s)
		return n
	}
	secs := atoi(m[1])*86400 + atoi(m[2])*3600 + atoi(m[3])*60
	if m[4] != "" {
		f, _ := strconv.ParseFloat(m[4], 64)
		secs += int(f)
	}
	return secs
}
