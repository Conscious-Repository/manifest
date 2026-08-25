package consume

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// The RSS/Atom fetcher — the engine of the whole lane. X rides the same road
// (consume/x.go) but every bridge, mirror and self-hosted proxy that fronts X
// emits a feed, and those arrive here as ordinary subscriptions with no special
// code at all.
//
// This is stdlib encoding/xml rather than a feed library, matching the house
// preference (extract/extract.go: "nothing here is a new dependency"). Two
// settings carry that decision:
//
//	Strict = false      tolerates unescaped ampersands, unknown entities and
//	                    undeclared namespace prefixes — the three ways a real
//	                    feed is malformed. Without it a single stray "&" in one
//	                    post takes down the whole poll.
//	CharsetReader       feeds still declare windows-1252 and iso-8859-1 in
//	                    2026; without this they fail to parse outright.
//
// Struct tags match on LOCAL name only, never on namespace URI, because
// prefixes in the wild are declared inconsistently or not at all: `content:
// encoded` is matched as "encoded" whether or not the publisher bothered to
// bind the prefix.
//
// A parse failure is never silent — it becomes that subscription's lastErr and
// shows as a degraded dot in the manage panel (failure ≠ empty).

// maxFeed bounds one feed response. A poll is bounded work.
const maxFeed = 16 << 20

type rssFetcher struct {
	hc *http.Client
	// cookie is the site session, when the owner has signed in to this
	// publisher. Empty for the ordinary anonymous case.
	//
	// ⚠ Never formatted into an error, a log line or a response. The only
	// place it appears is the request header below.
	cookie string
	// lastTTL is the refresh hint the most recent poll read out of the feed,
	// in minutes. Surfaced through LastTTL so the scheduler can honour it.
	lastTTL int
}

// LastTTL implements metaFetcher.
func (r *rssFetcher) LastTTL() int { return r.lastTTL }

// retryAfterError is a publisher explicitly telling us when to come back
// (429/503 + Retry-After). It is an error — the poll did fail — but it carries
// a schedule the store honours instead of the usual back-off.
type retryAfterError struct {
	msg string
	at  time.Time
}

func (e *retryAfterError) Error() string { return e.msg }

// parseRetryAfter reads the header in both its legal forms: delta-seconds, or
// an HTTP date.
func parseRetryAfter(v string, now time.Time) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return now.Add(time.Duration(secs) * time.Second), true
	}
	if t, err := http.ParseTime(v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// xmlLink serves both dialects: RSS puts the URL in the element text, Atom in
// an href attribute with a rel that says what kind of link it is.
type xmlLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

type xmlAuthor struct {
	Name string `xml:"name"`
	Text string `xml:",chardata"`
}

type xmlContent struct {
	Type string `xml:"type,attr"`
	Text string `xml:",chardata"`
}

// xmlItem is one <item> (RSS) or <entry> (Atom). Every field either dialect
// might use is present; extraction picks the best available.
type xmlItem struct {
	Title     string     `xml:"title"`
	Links     []xmlLink  `xml:"link"`
	GUID      string     `xml:"guid"`
	ID        string     `xml:"id"`
	PubDate   string     `xml:"pubDate"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Date      string     `xml:"date"` // dc:date
	Desc      string     `xml:"description"`
	Encoded   string     `xml:"encoded"` // content:encoded — the full body
	Content   xmlContent `xml:"content"` // Atom
	Summary   string     `xml:"summary"`
	Creator   string     `xml:"creator"` // dc:creator
	Author    xmlAuthor  `xml:"author"`
}

type xmlFeed struct {
	XMLName xml.Name
	Title   string `xml:"title"` // Atom
	Channel struct {
		Title  string    `xml:"title"`
		TTL    string    `xml:"ttl"`             // RSS <ttl>, in minutes
		Period string    `xml:"updatePeriod"`    // sy:updatePeriod
		Freq   string    `xml:"updateFrequency"` // sy:updateFrequency
		Items  []xmlItem `xml:"item"`
	} `xml:"channel"` // RSS
	Entries []xmlItem `xml:"entry"` // Atom
}

func decodeFeed(r io.Reader) (*xmlFeed, error) {
	dec := xml.NewDecoder(io.LimitReader(r, maxFeed))
	dec.Strict = false
	dec.CharsetReader = charset.NewReaderLabel
	var f xmlFeed
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *xmlFeed) title() string {
	if t := strings.TrimSpace(f.Channel.Title); t != "" {
		return t
	}
	return strings.TrimSpace(f.Title)
}

// ttlMinutes reads the feed's own refresh hint. <ttl> is minutes outright;
// the syndication module expresses it as period+frequency ("hourly", 2 = twice
// an hour). A feed asking for less frequent polling gets it.
func (f *xmlFeed) ttlMinutes() int {
	if n, err := strconv.Atoi(strings.TrimSpace(f.Channel.TTL)); err == nil && n > 0 {
		return n
	}
	per := strings.ToLower(strings.TrimSpace(f.Channel.Period))
	if per == "" {
		return 0
	}
	base := map[string]int{"hourly": 60, "daily": 1440, "weekly": 10080, "monthly": 43200, "yearly": 525600}[per]
	if base == 0 {
		return 0
	}
	freq := 1
	if n, err := strconv.Atoi(strings.TrimSpace(f.Channel.Freq)); err == nil && n > 0 {
		freq = n
	}
	return base / freq
}

func (f *xmlFeed) items() []xmlItem {
	if len(f.Channel.Items) > 0 {
		return f.Channel.Items
	}
	return f.Entries
}

// Fetch polls one feed. Conditional GET means an unchanged feed costs one 304
// and no parsing; the ETag/Last-Modified pair rides in the cursor map so it
// persists across restarts like any other cursor.
func (r *rssFetcher) Fetch(ctx context.Context, sub Subscription, cur map[string]string) ([]Item, map[string]string, error) {
	if strings.TrimSpace(sub.URL) == "" {
		return nil, nil, fmt.Errorf("subscription %q has no feed url", sub.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")
	// Signed in: a paid publication returns full content:encoded rather than a
	// teaser. The transport drops this if a redirect leaves the site.
	if r.cookie != "" {
		req.Header.Set("Cookie", r.cookie)
	}
	if v := cur["etag"]; v != "" {
		req.Header.Set("If-None-Match", v)
	}
	if v := cur["lastModified"]; v != "" {
		req.Header.Set("If-Modified-Since", v)
	}

	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	// 304 is a SUCCESSFUL poll with nothing new — not an error, and crucially
	// not an empty result that would age the cache out.
	if resp.StatusCode == http.StatusNotModified {
		return nil, cur, nil
	}
	// A publisher asking us to back off is obeyed to the second, rather than
	// being treated as a generic failure and retried on our own schedule.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		msg := fmt.Sprintf("%s: %s", redactURL(sub.URL), resp.Status)
		if at, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			return nil, nil, &retryAfterError{msg: msg + " (retrying after " + at.Format(time.RFC822) + ")", at: at}
		}
		return nil, nil, fmt.Errorf("%s", msg)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: %s", redactURL(sub.URL), resp.Status)
	}

	feed, err := decodeFeed(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", redactURL(sub.URL), err)
	}

	next := map[string]string{}
	for k, v := range cur {
		next[k] = v
	}
	if v := resp.Header.Get("ETag"); v != "" {
		next["etag"] = v
	}
	if v := resp.Header.Get("Last-Modified"); v != "" {
		next["lastModified"] = v
	}

	r.lastTTL = feed.ttlMinutes()

	now := time.Now().UTC()
	feedTitle := feed.title()
	out := make([]Item, 0, len(feed.items()))
	for _, xi := range feed.items() {
		if it, ok := itemFrom(xi, sub, feedTitle, now); ok {
			out = append(out, it)
		}
	}
	return out, next, nil
}

// itemFrom projects one feed entry into an Item. An entry with neither a title
// nor a body is dropped: it is a tracking pixel or a malformed row, not
// something to read.
func itemFrom(xi xmlItem, sub Subscription, feedTitle string, now time.Time) (Item, bool) {
	link := cleanLink(bestLink(xi.Links))

	// ⚠ Identity prefers the canonical LINK, not the guid.
	//
	// The spec says guid is the identifier, and for a well-behaved feed it is.
	// But ~41% of feeds in the wild regenerate their guid on every fetch (and
	// ~29% emit duplicate ones), which would turn every poll into a fresh batch
	// of "new" items — a duplicate flood, hourly, forever. A permalink is the
	// thing that actually stays put. Guid is the fallback for feeds that
	// publish no link at all, and title the last resort.
	//
	// Commit also collapses items whose normalized URL matches one already
	// held, which is what silently migrates ids from the old guid-first scheme
	// without resurfacing anything as unread.
	external := firstNonEmpty(link, strings.TrimSpace(xi.GUID), strings.TrimSpace(xi.ID), strings.TrimSpace(xi.Title))
	if external == "" {
		return Item{}, false
	}

	// content:encoded and Atom <content> carry the FULL post; description and
	// summary usually carry a teaser. Prefer the long one — the whole point of
	// the reader is not having to leave for the rest of the article.
	full := firstNonEmpty(xi.Encoded, xi.Content.Text)
	raw := firstNonEmpty(full, xi.Desc, xi.Summary)
	body := Sanitize(raw)
	text := Text(raw)

	title := collapse(html.UnescapeString(strings.TrimSpace(xi.Title)))
	if title == "" {
		title = Excerpt(text, 80)
	}
	if title == "" && body == "" {
		return Item{}, false
	}

	source := firstNonEmpty(strings.TrimSpace(sub.Title), feedTitle)
	author := collapse(firstNonEmpty(
		strings.TrimSpace(xi.Creator),
		strings.TrimSpace(xi.Author.Name),
		strings.TrimSpace(xi.Author.Text),
	))

	return Item{
		ID:          itemID(KindRSS, sub.ID, external),
		SubID:       sub.ID,
		Source:      source,
		List:        sub.List,
		Author:      author,
		Title:       title,
		URL:         link,
		Excerpt:     Excerpt(text, 280),
		Chars:       len([]rune(text)),
		PublishedAt: parseDate(xi.PubDate, xi.Published, xi.Updated, xi.Date),
		FetchedAt:   now,
		Body:        body,
		// Truncated by provenance OR by marker. The second half is what
		// catches Substack, which withholds inside content:encoded.
		teaser: full == "" || LooksTruncated(text),
	}, true
}

// bestLink picks the human permalink: Atom's rel="alternate" first, then any
// href, then RSS's element text. rel="self"/"hub"/"replies" are machine links
// and must never become the card's destination.
func bestLink(links []xmlLink) string {
	var anyHref, text string
	for _, l := range links {
		href := strings.TrimSpace(l.Href)
		body := strings.TrimSpace(l.Text)
		rel := strings.ToLower(strings.TrimSpace(l.Rel))
		if href != "" {
			if rel == "alternate" || rel == "" {
				if l.Type == "" || strings.Contains(l.Type, "html") {
					return href
				}
			}
			if anyHref == "" && rel != "self" && rel != "hub" {
				anyHref = href
			}
			continue
		}
		if body != "" && text == "" {
			text = body
		}
	}
	return firstNonEmpty(text, anyHref)
}

// dateLayouts covers what feeds actually emit. RFC1123Z is the RSS spec's
// answer; everything after it is a publisher not following it.
var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822,
	time.RFC3339, time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02T15:04:05",
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
	"2006-01-02",
}

// parseDate returns the first candidate that parses. An unparseable date
// yields the zero time, which sorts the item to the bottom rather than
// dropping it — a post with a broken date is still a post.
func parseDate(candidates ...string) time.Time {
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, c); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}

// trackingParams are the query keys that identify the reader rather than the
// article. Stripping them from the STORED url (not just the dedupe key) is the
// Miniflux habit: it stops a click leaking where the link was found, and it
// makes two copies of the same essay collapse into one.
var trackingParams = []string{
	"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "utm_id",
	"fbclid", "gclid", "dclid", "msclkid", "twclid", "igshid", "mc_cid", "mc_eid",
	"ref", "ref_src", "referrer", "source", "spm", "yclid", "_hsenc", "_hsmi",
}

// cleanLink strips tracking parameters and the fragment while leaving the URL
// otherwise exactly as the publisher wrote it (scheme and host untouched —
// unlike curateKey, which normalizes aggressively because it is only ever
// compared, never displayed or followed).
func cleanLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	q := u.Query()
	for _, k := range trackingParams {
		q.Del(k)
	}
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---- subscribe-time feed discovery ----

// Discover resolves what the owner pasted into a real feed URL and the feed's
// own title. It accepts a feed URL, a site URL, or a bare hostname, because
// nobody knows where their favourite blog hides its RSS link and being made to
// find it is exactly the friction that stops a reader being used.
func (r *rssFetcher) Discover(ctx context.Context, input string) (feedURL, title string, err error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", "", fmt.Errorf("nothing to subscribe to")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	// Substack hides its feed at a fixed path and its homepage does not always
	// advertise it. Knowing this one convention removes a whole class of "why
	// didn't it work" — and Substack is the reason this lane exists.
	if u := strings.TrimRight(raw, "/"); strings.Contains(u, ".substack.com") && !strings.HasSuffix(u, "/feed") {
		if fu, ft, e := r.probe(ctx, u+"/feed"); e == nil {
			return fu, ft, nil
		}
	}
	if fu, ft, e := r.probe(ctx, raw); e == nil {
		return fu, ft, nil
	}
	// Not a feed — try to read it as HTML and follow its <link rel=alternate>.
	alt, e := r.autodiscover(ctx, raw)
	if e != nil {
		return "", "", e
	}
	return r.probe(ctx, alt)
}

// probe fetches a candidate URL and reports whether it parsed as a feed.
func (r *rssFetcher) probe(ctx context.Context, u string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := r.hc.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("%s: %s", redactURL(u), resp.Status)
	}
	feed, err := decodeFeed(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("%s is not a feed", redactURL(u))
	}
	if len(feed.items()) == 0 && feed.title() == "" {
		return "", "", fmt.Errorf("%s is not a feed", redactURL(u))
	}
	return u, feed.title(), nil
}

// autodiscover pulls the first RSS/Atom <link rel="alternate"> out of an HTML
// page — the standard every blog engine emits and no reader should make a
// person hunt for by hand.
func (r *rssFetcher) autodiscover(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := r.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", redactURL(pageURL), resp.Status)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, maxFeed))
	if err != nil {
		return "", fmt.Errorf("could not read %s", redactURL(pageURL))
	}
	href := findFeedLink(doc)
	if href == "" {
		return "", fmt.Errorf("no RSS or Atom feed advertised at %s", redactURL(pageURL))
	}
	return resolveRef(pageURL, href), nil
}

// resolveRef turns a possibly-relative feed href into an absolute URL against
// the page it was advertised on. Most blogs advertise "/feed", not the full
// URL, so skipping this step breaks the common case rather than the rare one.
func resolveRef(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func findFeedLink(n *html.Node) string {
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, "link") {
		var rel, typ, href string
		for _, a := range n.Attr {
			switch strings.ToLower(a.Key) {
			case "rel":
				rel = strings.ToLower(a.Val)
			case "type":
				typ = strings.ToLower(a.Val)
			case "href":
				href = a.Val
			}
		}
		if strings.Contains(rel, "alternate") && href != "" &&
			(strings.Contains(typ, "rss") || strings.Contains(typ, "atom")) {
			return href
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if got := findFeedLink(c); got != "" {
			return got
		}
	}
	return ""
}
