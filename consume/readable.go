package consume

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// paywallPhrases identify a page that is withholding the article behind a
// subscription. Substack's is the first; the others are the common variants.
var paywallPhrases = []string{
	"this post is for paid subscribers",
	"this post is for paying subscribers",
	"subscribe to keep reading",
	"keep reading with a 7-day free trial",
	"paid subscribers only",
	"this content is for paid",
	"become a paid subscriber",
	"already a paid subscriber",
	"days of free access to the full post archives",
	"upgrade to paid",
	"for full access to this post",
	"subscribe to read",
	"this post is for subscribers",
}

// paywallMarkup are machine signals in the page SOURCE rather than prose.
// Substack states the answer outright in its embedded JSON, which beats
// guessing at whatever wording the subscribe box happens to use this month.
var paywallMarkup = []string{
	`audience\":\"only_paid\"`,
	`"audience":"only_paid"`,
	`audience\":\"founding\"`,
}

// markerLinkTail matches a trailing "Read more"-style link — the affordance a
// publisher appends to a withheld article.
//
// A regex is acceptable here precisely because the input is not arbitrary HTML:
// it is OUR sanitizer's output, whose shape is normalized and predictable, and
// the pattern is anchored to the very end of the string.
var markerLinkTail = regexp.MustCompile(
	`(?is)(<p>)?\s*<a[^>]*>\s*(read more|continue reading|read the rest|keep reading|read the full)\s*…?\s*</a>\s*(</p>)?\s*$`)

// stripMarkerLink removes that trailing link. It runs ONLY on an item we have
// labelled as a preview, where the reading page renders its own, clearer
// explanation plus a link to the source — leaving both would give the reader
// two identical ways out and no reason for either.
//
// The article's own words are never touched; this removes one navigation
// affordance we have replaced, not content.
func stripMarkerLink(body string) string {
	return strings.TrimSpace(markerLinkTail.ReplaceAllString(body, ""))
}

// looksPaywalled reports whether a fetched page is withholding its article.
// rawHTML may be empty (when only extracted text is available); pageText is the
// visible text.
func looksPaywalled(pageText, rawHTML string) bool {
	t := strings.ToLower(pageText)
	for _, p := range paywallPhrases {
		if strings.Contains(t, p) {
			return true
		}
	}
	for _, m := range paywallMarkup {
		if strings.Contains(rawHTML, m) {
			return true
		}
	}
	return false
}

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

// fetchArticle downloads a page and returns its readable HTML plus whether the
// page announced a paywall. The second return is why a failed extraction can be
// explained to the owner instead of silently leaving a stub.
func (s *Service) fetchArticle(ctx context.Context, pageURL, cookie string) (string, bool) {
	body, paywalled, _ := s.fetchArticlePage(ctx, pageURL, cookie)
	return body, paywalled
}

// fetchArticlePage is fetchArticle that also says whether the page is a
// Substack post (its assets load from substackcdn.com), so a caller can ask
// Substack's post API for a second opinion without a request to any other
// publisher.
func (s *Service) fetchArticlePage(ctx context.Context, pageURL, cookie string) (string, bool, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", false, false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	// Signed in: the article page renders in full instead of a subscribe box.
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// A paywall often answers 403 rather than a page saying so.
		return "", resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusPaymentRequired, false
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "html") {
		return "", false, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxArticle))
	if err != nil {
		return "", false, false
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return "", false, false
	}
	return Readable(doc), looksPaywalled(nodeText(doc), string(raw)), bytes.Contains(raw, []byte("substackcdn.com"))
}

// substackPostPath is Substack's permalink shape, on its own domain or a
// publication's custom one.
var substackPostPath = regexp.MustCompile(`^/p/([A-Za-z0-9-]{1,200})/?$`)

// substackPost reads one post through the publication's public post API
// (/api/v1/posts/<slug>). answered is false when the URL is not a Substack
// post or the API gave no usable answer, and the caller falls back to the page.
//
// For a free post ("audience": "everyone") the API returns the whole body,
// which is why a free reader never needs to sign in. For a paid post it
// returns no body unless the session belongs to a paying subscriber.
func (s *Service) substackPost(ctx context.Context, pageURL, cookie string) (body string, paid, answered bool) {
	u, err := url.Parse(pageURL)
	if err != nil || u.Host == "" {
		return "", false, false
	}
	m := substackPostPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", false, false
	}
	api := u.Scheme + "://" + u.Host + "/api/v1/posts/" + m[1]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", false, false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, false
	}
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); !strings.Contains(ct, "json") {
		return "", false, false
	}
	var post struct {
		Audience string `json:"audience"`
		BodyHTML string `json:"body_html"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxArticle)).Decode(&post); err != nil {
		return "", false, false
	}
	free := post.Audience == "" || strings.EqualFold(post.Audience, "everyone")
	if strings.TrimSpace(post.BodyHTML) == "" {
		if free {
			return "", false, false
		}
		return "", true, true
	}
	clean := Sanitize(post.BodyHTML)
	if free {
		return clean, false, true
	}
	// A paid post read without a session is the free part ending at the
	// paywall: paid, whatever its length. With a session it is whole unless
	// Substack still marks a paywall in it (a session that does not pay).
	if cookie == "" {
		return clean, true, true
	}
	return clean, looksPaywalled(Text(clean), post.BodyHTML) || strings.Contains(post.BodyHTML, "paywall"), true
}

// substackHealMark records that the one-time preview heal has run.
const substackHealMark = "heal-substack-previews-v1"

// HealSubstackPreviews re-asks Substack's post API about every stored post an
// earlier build labelled "paid" or "preview" by scraping its page. A free
// post comes back whole and loses the label; a paid one keeps "paid". It runs
// once per dataDir (a marker file), because the answer for a post does not
// change and asking again on every boot would be a request per paid post for
// nothing. Returns how many posts it completed.
func (s *Service) HealSubstackPreviews(ctx context.Context) int {
	mark := filepath.Join(s.store.root, substackHealMark)
	if _, err := os.Stat(mark); err == nil {
		return 0
	}
	healed := 0
	for _, sub := range s.Subscriptions() {
		if sub.Kind == KindX || s.fromRSSHub(sub) || sub.FullText() == FullTextOff {
			continue
		}
		for _, it := range s.store.Items(sub.ID) {
			if ctx.Err() != nil {
				return healed
			}
			if it.Preview == "" || it.DismissedAt != "" {
				continue
			}
			fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			body, paid, ok := s.substackPost(fetchCtx, it.URL, s.cookieFor(it.URL))
			cancel()
			if !ok {
				continue
			}
			text := Text(body)
			switch {
			case !paid && body != "" && len([]rune(text)) > it.Chars:
				it.Body, it.Preview = body, ""
				it.Chars, it.Excerpt = len([]rune(text)), Excerpt(text, 280)
				s.store.Complete(sub.ID, it)
				healed++
			case paid && it.Preview != PreviewPaid:
				it.Preview = PreviewPaid
				s.store.Complete(sub.ID, it)
			}
		}
	}
	if ctx.Err() == nil {
		_ = os.MkdirAll(filepath.Dir(mark), 0o755)
		_ = os.WriteFile(mark, []byte(s.now().Format(time.RFC3339)+"\n"), 0o644)
	}
	return healed
}

// readArticle is fetchArticle with Substack's post API as the second opinion
// for a Substack page that could not be completed — the path every "get me
// the whole piece" caller wants.
func (s *Service) readArticle(ctx context.Context, pageURL, cookie string) (string, bool) {
	body, paywalled, isSubstack := s.fetchArticlePage(ctx, pageURL, cookie)
	if isSubstack && (body == "" || paywalled) {
		if b, paid, ok := s.substackPost(ctx, pageURL, cookie); ok && (b != "" || paid) {
			return b, paid
		}
	}
	return body, paywalled
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
		// A video's page is a player, not an article: never scrape it.
		if it.URL == "" || it.Embed != "" {
			continue
		}
		if mode == FullTextAuto && !it.truncated && !(it.teaser && it.Chars < teaserUnder) {
			// auto fetches when the feed demonstrably WITHHELD the article —
			// it ends in a truncation marker, at ANY length — or when a short
			// body arrived as <description> and is probably a teaser.
			//
			// ⚠ The length test guards only the second, weaker case. Applying
			// it to the first suppressed exactly the publishers who write a
			// generous preview and stop: 10,000 characters ending "Read more"
			// is still a withheld article.
			continue
		}
		// Never let one slow site stall the rest of the poll.
		fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		// The page first, as for any publisher. When the page is a Substack
		// post that could not be completed, Substack's own post API is asked:
		// it says outright whether the post is free, and for a free post it
		// returns the whole body — no guessing, and no sign-in. Only a post the
		// API (or the page) calls paid is labelled paid.
		cookie := s.cookieFor(it.URL)
		body, paywalled, isSubstack := s.fetchArticlePage(fetchCtx, it.URL, cookie)
		answered := false
		if isSubstack && (body == "" || paywalled || len([]rune(Text(body))) <= it.Chars) {
			if b, paid, ok := s.substackPost(fetchCtx, it.URL, cookie); ok {
				body, paywalled, answered = b, paid, true
			}
		}
		cancel()

		text := Text(body)
		// ⚠ Extraction may only improve an item, and LENGTH ALONE DOES NOT
		// PROVE IT DID.
		//
		// A Substack subscribe box — "…get 7 days of free access to the full
		// post archives. Already a paid subscriber? Sign in" — is longer than
		// the 369-character teaser it replaces, so a length-only guard accepts
		// it and the reader shows a sales pitch where the article should be.
		// That is exactly what happened to Building Optimism, whose every post
		// carries audience:"only_paid".
		//
		// So a page that announces a paywall disqualifies its own extraction
		// outright, however much text came back: whatever we scraped is the
		// wrapper, not the writing.
		// Substack's post API said "free" and returned the body: that is the
		// publisher's own answer, not a scrape, so the page-furniture test
		// below does not apply to it (a free post may well mention paid tiers).
		trusted := answered && !paywalled && body != ""
		if trusted && len([]rune(text)) <= it.Chars {
			continue // the feed already carried the whole free post
		}
		if body == "" || paywalled || (!trusted && looksPaywalled(text, body)) || len([]rune(text)) <= it.Chars {
			// We tried and could not complete it. If we KNOW it was truncated,
			// that is worth recording — a 367-character stub ending "Read more"
			// with no explanation reads like a bug in the reader rather than a
			// decision by the publisher.
			if it.teaser || (answered && paywalled) {
				items[i].Preview = PreviewPartial
				if paywalled {
					items[i].Preview = PreviewPaid
				}
				// The reader now explains the truncation and offers the link
				// itself, so the publisher's own trailing "Read more" is a
				// duplicate route to the same place.
				items[i].Body = stripMarkerLink(items[i].Body)
			}
			continue
		}
		items[i].Preview = "" // completed after all
		items[i].Body = body
		items[i].Chars = len([]rune(text))
		items[i].Excerpt = Excerpt(text, 280)
	}
	return items
}
