package consume

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Full-text extraction for feeds that publish only a teaser.
//
// A large share of feeds ship two paragraphs and a "read more" link, which
// defeats the point of a reader. When that happens we fetch the article page
// and pull the readable content out of it.
//
// This is a HEURISTIC, not a port of Mozilla's Readability. It scores candidate
// containers by how much paragraph text they hold, bonuses semantic elements
// (<article>, <main>, role=main) and penalizes the usual furniture (nav, aside,
// footer, comments, share widgets). That handles the ordinary blog and the
// ordinary newsletter; it will do worse than a real Readability port on hostile
// markup.
//
// Two properties keep that acceptable:
//
//   - It can only ever IMPROVE an item. If extraction yields less text than the
//     feed already gave us, the feed's own body wins (extractInto).
//   - It is one function behind a seam. If it disappoints on real feeds, the
//     swap to go-shiori/go-readability is this file and nothing else.
//
// Output goes through Sanitize like everything else — a page fetched from the
// open web is the LEAST trusted input in this package, and gets no exemption.

// maxArticle bounds one article fetch.
const maxArticle = 8 << 20

// negativeHints mark furniture: containers whose id/class say they are not the
// article. Matched as substrings against lowercased id+class.
var negativeHints = []string{
	"comment", "share", "sidebar", "footer", "header", "nav", "menu", "banner",
	"promo", "subscribe", "newsletter-signup", "related", "recirc", "paywall",
	"advert", "sponsor", "social", "meta", "byline", "breadcrumb", "pagination",
	"popup", "modal", "cookie", "author-bio", "tags", "toolbar",
}

// positiveHints mark the article itself.
var positiveHints = []string{
	"article", "post-content", "post-body", "entry-content", "story-body",
	"content-body", "markup", "prose", "postbody", "articlebody", "main-content",
	"available-content", "body-text",
}

// fetchArticle downloads a page and returns its readable HTML, sanitized.
func (s *Service) fetchArticle(ctx context.Context, pageURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	resp, err := s.hc.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return ""
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, maxArticle))
	if err != nil {
		return ""
	}
	return Readable(doc)
}

// Readable extracts the article from a parsed page and sanitizes it.
func Readable(doc *html.Node) string {
	best, bestScore := (*html.Node)(nil), 0.0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if sc := score(n); sc > bestScore {
				best, bestScore = n, sc
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if best == nil {
		return ""
	}
	return Sanitize(render(best))
}

// score rates a container as "the article". The signal that actually works is
// paragraph text: real articles are mostly <p>, furniture is mostly links.
func score(n *html.Node) float64 {
	tag := strings.ToLower(n.Data)
	switch tag {
	case "article", "main", "section", "div", "td", "body":
	default:
		return 0
	}
	if dropped[tag] {
		return 0
	}

	paraText, paras := 0, 0
	for _, p := range descendants(n, "p") {
		t := len(strings.TrimSpace(nodeText(p)))
		if t < 25 {
			continue // captions, bylines, "share this"
		}
		paraText += t
		paras++
	}
	if paras == 0 || paraText < 200 {
		return 0
	}

	sc := float64(paraText) + float64(paras)*25

	// Link density: a nav or a related-posts block is mostly anchor text.
	total := len(nodeText(n))
	if total > 0 {
		linkChars := 0
		for _, a := range descendants(n, "a") {
			linkChars += len(nodeText(a))
		}
		density := float64(linkChars) / float64(total)
		if density > 0.5 {
			return 0
		}
		sc *= 1 - density
	}

	hint := strings.ToLower(attr(n, "id") + " " + attr(n, "class") + " " + attr(n, "role") + " " + attr(n, "itemprop"))
	for _, bad := range negativeHints {
		if strings.Contains(hint, bad) {
			sc *= 0.25
			break
		}
	}
	for _, good := range positiveHints {
		if strings.Contains(hint, good) {
			sc *= 1.6
			break
		}
	}
	switch tag {
	case "article":
		sc *= 1.5
	case "main":
		sc *= 1.3
	case "body":
		// The body always contains every paragraph, so it would otherwise win
		// every time and bring the whole page's furniture with it.
		sc *= 0.4
	}
	return sc
}

// descendants collects every element with the given tag under n.
func descendants(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if strings.EqualFold(c.Data, tag) {
					out = append(out, c)
				}
				if !dropped[strings.ToLower(c.Data)] {
					walk(c)
				}
			}
		}
	}
	walk(n)
	return out
}

// nodeText is the plain text under a node, furniture elements excluded.
func nodeText(n *html.Node) string {
	var b strings.Builder
	textInto(&b, n)
	return collapse(b.String())
}

// fillFullText upgrades teaser items in place. It runs only on the items a poll
// just returned, so an article is fetched at most once, ever.
func (s *Service) fillFullText(ctx context.Context, sub Subscription, items []Item) []Item {
	mode := sub.FullText()
	if mode == FullTextOff {
		// Honoured HERE and not only at the call site: "off" is the owner
		// saying do not fetch this publisher's pages, and that has to hold
		// however this function is reached.
		return items
	}
	for i := range items {
		it := items[i]
		if it.URL == "" {
			continue
		}
		if mode == FullTextAuto && (!it.teaser || it.Chars >= teaserUnder) {
			// auto fetches only when the feed WITHHELD the article: a short
			// body that arrived as content:encoded is a short post, not a
			// teaser, and refetching it would be pure waste.
			continue
		}
		// Never let one slow site stall the rest of the poll.
		fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		body := s.fetchArticle(fetchCtx, it.URL)
		cancel()
		if body == "" {
			continue
		}
		text := Text(body)
		// ⚠ Extraction may only improve an item. A page behind a paywall or a
		// consent wall extracts to almost nothing, and silently replacing a
		// good teaser with "Please enable JavaScript" would be worse than
		// doing nothing at all.
		if len([]rune(text)) <= it.Chars {
			continue
		}
		items[i].Body = body
		items[i].Chars = len([]rune(text))
		items[i].Excerpt = Excerpt(text, 280)
	}
	return items
}
