package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"manifest/consume"
)

// post is one Substack post, extracted from its live page.
type post struct {
	Slug        string
	Title       string
	Subtitle    string
	Description string
	Published   string // YYYY-MM-DD
	URL         string
	Markdown    string
	Images      []image

	imgOrig map[string]string // src → original file URL, learned in normalize
}

// image is one body image: Src is the URL as it appears in the markdown (the
// CDN transform URL Substack renders), Original the source file behind it
// (S3 original when the CDN URL wraps one — that is what gets mirrored).
type image struct {
	Src      string
	Original string
	Alt      string
}

var (
	reDatePublished = regexp.MustCompile(`"datePublished"\s*:\s*"([^"]+)"`)
	rePostDate      = regexp.MustCompile(`post_date\\?"\s*:\s*\\?"([^"\\]+)`)
	reYouTubeID     = regexp.MustCompile(`youtube(?:-nocookie)?\.com/embed/([A-Za-z0-9_-]{6,})`)
)

// extract turns a fetched post page into a post: metadata from the head,
// body from the article's available-content block, converted to markdown
// through the repo's own sanitizer + renderer.
func extract(page string, e siteEntry) (*post, error) {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil, err
	}
	p := &post{Slug: e.Slug, URL: e.URL}

	meta := metaMap(doc)
	p.Title = strings.TrimSpace(meta["og:title"])
	if p.Title == "" {
		if h := findFirst(doc, func(n *html.Node) bool { return n.Data == "h1" && hasClass(n, "post-title") }); h != nil {
			p.Title = strings.TrimSpace(nodeText(h))
		}
	}
	if p.Title == "" {
		if t := findFirst(doc, func(n *html.Node) bool { return n.Data == "title" }); t != nil {
			p.Title = strings.TrimSpace(strings.SplitN(nodeText(t), " - by ", 2)[0])
		}
	}
	if p.Title == "" {
		p.Title = e.Slug
	}
	p.Description = strings.TrimSpace(meta["og:description"])
	if h := findFirst(doc, func(n *html.Node) bool { return n.Data == "h3" && hasClass(n, "subtitle") }); h != nil {
		p.Subtitle = strings.TrimSpace(nodeText(h))
	}
	if p.Subtitle == p.Description {
		p.Description = "" // Substack mirrors the subtitle into og:description; keep one
	}
	if c := findFirst(doc, func(n *html.Node) bool {
		return n.Data == "link" && strings.EqualFold(attr(n, "rel"), "canonical")
	}); c != nil {
		if slug, canon := slugOf(attr(c, "href")); slug != "" {
			p.URL = canon
		}
	} else if slug, canon := slugOf(meta["og:url"]); slug != "" {
		p.URL = canon
	}
	p.Published = publishedDate(page, doc, e.LastMod)

	body := findFirst(doc, func(n *html.Node) bool { return hasClass(n, "available-content") })
	if body == nil {
		body = findFirst(doc, func(n *html.Node) bool { return n.Data == "article" })
	}
	if body == nil {
		return nil, errors.New("no <article> / available-content block on page (paywalled or not a post page?)")
	}
	p.imgOrig = map[string]string{}
	normalize(body, p)
	sanitized := consume.Sanitize(innerHTML(body))
	p.Markdown = dropEmptyHeadings(strings.TrimSpace(consume.ToMarkdown(sanitized)))
	p.Images = collectImages(sanitized, p.imgOrig)
	if p.Markdown == "" {
		return nil, errors.New("empty body after conversion")
	}
	return p, nil
}

// dropEmptyHeadings removes heading lines with no text (Substack emits an
// empty <h2> as the divider above footnotes) and the double blank they leave.
func dropEmptyHeadings(md string) string {
	lines := strings.Split(md, "\n")
	out := lines[:0]
	for _, ln := range lines {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "#") && strings.Trim(t, "# ") == "" {
			continue
		}
		if ln == "" && len(out) > 0 && out[len(out)-1] == "" {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// publishedDate prefers the page's own metadata (JSON-LD datePublished, then
// the preloaded post_date, then the visible <time>) over the sitemap lastmod,
// which is a MODIFIED date and drifts on edits.
func publishedDate(page string, doc *html.Node, fallback string) string {
	try := func(raw string) string {
		raw = strings.TrimSpace(raw)
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.UTC().Format("2006-01-02")
			}
		}
		return ""
	}
	if m := reDatePublished.FindStringSubmatch(page); m != nil {
		if d := try(m[1]); d != "" {
			return d
		}
	}
	if m := rePostDate.FindStringSubmatch(page); m != nil {
		if d := try(m[1]); d != "" {
			return d
		}
	}
	if t := findFirst(doc, func(n *html.Node) bool { return n.Data == "time" && attr(n, "datetime") != "" }); t != nil {
		if d := try(attr(t, "datetime")); d != "" {
			return d
		}
	}
	return fallback
}

// normalize rewrites Substack's component markup into the plain vocabulary the
// sanitizer keeps, so nothing meaningful is lost on the way to markdown:
//
//	image links      → the image alone (the link only opened the CDN file)
//	youtube embeds   → a watch link (iframes are dropped by the sanitizer)
//	tweets           → a blockquote with the tweet text + link
//	audio / video    → a link to the media file
//	footnotes        → markdown footnotes ([^n] / [^n]: …)
//	pullquotes       → blockquotes
//	subscribe/share  → removed (site chrome, not prose)
func normalize(root *html.Node, p *post) {
	// Collect first, mutate after: walking while re-parenting is unsafe.
	var nodes []*html.Node
	walk(root, func(n *html.Node) { nodes = append(nodes, n) })
	for _, n := range nodes {
		if n.Type != html.ElementNode || n.Parent == nil {
			continue
		}
		switch {
		case hasClass(n, "subscription-widget-wrap"), hasClass(n, "subscribe-widget"),
			hasClass(n, "visibility-check"), hasClass(n, "image-link-expand"),
			hasClass(n, "post-footer"), hasClass(n, "share-dialog"):
			n.Parent.RemoveChild(n)
		case n.Data == "a" && hasClass(n, "button") && isShareLink(attr(n, "href")):
			remove(n)
		case n.Data == "a" && hasClass(n, "image-link"):
			unwrap(n)
		case hasClass(n, "youtube-wrap"):
			id := ""
			var attrs struct {
				VideoID string `json:"videoId"`
			}
			if json.Unmarshal([]byte(attr(n, "data-attrs")), &attrs) == nil {
				id = attrs.VideoID
			}
			if id == "" {
				if f := findFirst(n, func(c *html.Node) bool { return c.Data == "iframe" }); f != nil {
					if m := reYouTubeID.FindStringSubmatch(attr(f, "src")); m != nil {
						id = m[1]
					}
				}
			}
			if id != "" {
				replace(n, linkPara("https://www.youtube.com/watch?v="+id, "YouTube: https://www.youtube.com/watch?v="+id))
			} else {
				n.Parent.RemoveChild(n)
			}
		case n.Data == "iframe":
			src := attr(n, "src")
			if m := reYouTubeID.FindStringSubmatch(src); m != nil {
				src = "https://www.youtube.com/watch?v=" + m[1]
			}
			if src != "" {
				replace(n, linkPara(src, src))
			} else {
				n.Parent.RemoveChild(n)
			}
		case hasClass(n, "twitter-embed"):
			// Substack wraps the whole card in a link to the tweet; the
			// quote replaces the link, not just the card inside it.
			target := n
			for target.Parent != nil && target.Parent.Type == html.ElementNode && target.Parent.Data == "a" {
				target = target.Parent
			}
			replace(target, tweetQuote(attr(n, "data-attrs")))
		case component(n) == "AudioEmbedPlayer" || component(n) == "VideoEmbedPlayer":
			// A rendered player (transport controls, time labels) around the
			// media element — or, for native video, only a placeholder whose
			// upload id names the file. Either way: one link to the media.
			var src, label string
			if m := findFirst(n, func(c *html.Node) bool { return c.Data == "audio" || c.Data == "video" }); m != nil {
				src, label = mediaSrc(m), mediaLabel(m)
			}
			if src == "" {
				if id := strings.TrimPrefix(attr(n, "id"), "media-"); id != "" && id != attr(n, "id") {
					kind := "audio"
					if component(n) == "VideoEmbedPlayer" {
						kind = "video"
					}
					src, label = fmt.Sprintf("%s/api/v1/%s/upload/%s/src", siteBase, kind, id), strings.ToUpper(kind[:1])+kind[1:]
				}
			}
			if src == "" {
				n.Parent.RemoveChild(n)
				break
			}
			replace(n, linkPara(src, label+": "+src))
		case n.Data == "audio" || n.Data == "video":
			src := mediaSrc(n)
			if src == "" {
				n.Parent.RemoveChild(n)
				break
			}
			replace(n, linkPara(src, mediaLabel(n)+": "+src))
		case n.Data == "a" && hasClass(n, "footnote-anchor"):
			replace(n, textNode("[^"+strings.TrimSpace(nodeText(n))+"]"))
		case n.Data == "div" && hasClass(n, "footnote"):
			replace(n, footnotePara(n))
		case hasClass(n, "pullquote"):
			n.Data, n.DataAtom = "blockquote", atom.Blockquote
		case n.Data == "img":
			// Point the mirror at the original file behind the CDN transform.
			src := attr(n, "src")
			if src == "" {
				remove(n)
				break
			}
			p.imgOrig[src] = originalImageURL(src, attr(n, "data-attrs"))
		}
	}
}

// collectImages lists the images that survived sanitizing, in body order,
// deduped by src. orig maps src → original file URL (learned before the
// sanitizer stripped data-attrs); an unknown src derives its own.
func collectImages(sanitized string, orig map[string]string) []image {
	nodes, err := html.ParseFragment(strings.NewReader(sanitized), &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"})
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []image
	for _, n := range nodes {
		walk(n, func(c *html.Node) {
			if c.Type != html.ElementNode || c.Data != "img" {
				return
			}
			src := attr(c, "src")
			if src == "" || seen[src] {
				return
			}
			seen[src] = true
			o := orig[src]
			if o == "" {
				o = originalImageURL(src, "")
			}
			out = append(out, image{Src: src, Original: o, Alt: attr(c, "alt")})
		})
	}
	return out
}

// originalImageURL unwraps a substackcdn.com/image/fetch/<transforms>/<encoded
// original> URL to the original file. data-attrs (when present) names it
// directly. Anything else is its own original.
func originalImageURL(src, dataAttrs string) string {
	if dataAttrs != "" {
		var a struct {
			Src string `json:"src"`
		}
		if json.Unmarshal([]byte(dataAttrs), &a) == nil && strings.HasPrefix(a.Src, "https://") {
			return a.Src
		}
	}
	if i := strings.Index(src, "/image/fetch/"); i >= 0 && strings.Contains(src, "substackcdn.com") {
		rest := src[i+len("/image/fetch/"):]
		if j := strings.Index(rest, "/http"); j >= 0 {
			if orig, err := url.PathUnescape(rest[j+1:]); err == nil && strings.HasPrefix(orig, "http") {
				return orig
			}
		}
	}
	return src
}

// component is Substack's data-component-name on an element ("" if none).
func component(n *html.Node) string { return attr(n, "data-component-name") }

// mediaSrc is the absolute URL of an <audio>/<video> element's file (its own
// src or its first <source>), "" if it has none.
func mediaSrc(n *html.Node) string {
	src := attr(n, "src")
	if src == "" {
		if s := findFirst(n, func(c *html.Node) bool { return c.Data == "source" }); s != nil {
			src = attr(s, "src")
		}
	}
	if src == "" {
		return ""
	}
	return absolute(src)
}

func mediaLabel(n *html.Node) string {
	if n.Data == "video" {
		return "Video"
	}
	return "Audio"
}

func isShareLink(href string) bool {
	return strings.Contains(href, "action=share") || strings.Contains(href, "/subscribe")
}

func absolute(src string) string {
	if strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "//") {
		return siteBase + src
	}
	return src
}

// tweetQuote renders a Substack tweet embed (its data-attrs JSON) as a
// blockquote: the tweet text, then "— @user" linking to the tweet.
func tweetQuote(dataAttrs string) *html.Node {
	var t struct {
		URL      string `json:"url"`
		FullText string `json:"full_text"`
		Username string `json:"username"`
	}
	_ = json.Unmarshal([]byte(dataAttrs), &t)
	bq := &html.Node{Type: html.ElementNode, DataAtom: atom.Blockquote, Data: "blockquote"}
	text := strings.TrimSpace(consume.Text(t.FullText))
	if text != "" {
		p := &html.Node{Type: html.ElementNode, DataAtom: atom.P, Data: "p"}
		p.AppendChild(textNode(text))
		bq.AppendChild(p)
	}
	if t.URL != "" {
		label := "tweet"
		if t.Username != "" {
			label = "@" + t.Username
		}
		p := &html.Node{Type: html.ElementNode, DataAtom: atom.P, Data: "p"}
		p.AppendChild(textNode("— "))
		a := &html.Node{Type: html.ElementNode, DataAtom: atom.A, Data: "a", Attr: []html.Attribute{{Key: "href", Val: t.URL}}}
		a.AppendChild(textNode(label))
		p.AppendChild(a)
		bq.AppendChild(p)
	}
	if bq.FirstChild == nil {
		bq.AppendChild(textNode(""))
	}
	return bq
}

// footnotePara turns Substack's <div class="footnote"> (number link +
// footnote-content paragraphs) into one <p>[^n]: …</p>, paragraphs joined.
func footnotePara(div *html.Node) *html.Node {
	num := ""
	if a := findFirst(div, func(c *html.Node) bool { return c.Data == "a" && hasClass(c, "footnote-number") }); a != nil {
		num = strings.TrimSpace(nodeText(a))
	}
	p := &html.Node{Type: html.ElementNode, DataAtom: atom.P, Data: "p"}
	p.AppendChild(textNode("[^" + num + "]: "))
	content := findFirst(div, func(c *html.Node) bool { return hasClass(c, "footnote-content") })
	if content == nil {
		content = div
	}
	first := true
	var paras []*html.Node
	walk(content, func(c *html.Node) {
		if c.Type == html.ElementNode && c.Data == "p" {
			paras = append(paras, c)
		}
	})
	if len(paras) == 0 {
		paras = []*html.Node{content}
	}
	for _, para := range paras {
		if !first {
			p.AppendChild(textNode(" "))
		}
		first = false
		for c := para.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode && (c.Data == "a" && hasClass(c, "footnote-number")) {
				c = next
				continue
			}
			para.RemoveChild(c)
			p.AppendChild(c)
			c = next
		}
	}
	return p
}

// ---- small DOM helpers -------------------------------------------------

func metaMap(doc *html.Node) map[string]string {
	m := map[string]string{}
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "meta" {
			return
		}
		key := attr(n, "property")
		if key == "" {
			key = attr(n, "name")
		}
		if key != "" {
			if _, dup := m[key]; !dup {
				m[key] = attr(n, "content")
			}
		}
	})
	return m
}

func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func findFirst(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findFirst(c, pred); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return b.String()
}

func innerHTML(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&b, c)
	}
	return b.String()
}

func textNode(s string) *html.Node { return &html.Node{Type: html.TextNode, Data: s} }

func linkPara(href, label string) *html.Node {
	p := &html.Node{Type: html.ElementNode, DataAtom: atom.P, Data: "p"}
	a := &html.Node{Type: html.ElementNode, DataAtom: atom.A, Data: "a", Attr: []html.Attribute{{Key: "href", Val: href}}}
	a.AppendChild(textNode(label))
	p.AppendChild(a)
	return p
}

// replace swaps n for repl in n's parent.
func replace(n, repl *html.Node) {
	parent := n.Parent
	if parent == nil {
		return
	}
	parent.InsertBefore(repl, n)
	parent.RemoveChild(n)
}

// unwrap replaces n with its children.
func unwrap(n *html.Node) {
	parent := n.Parent
	if parent == nil {
		return
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		parent.InsertBefore(c, n)
		c = next
	}
	parent.RemoveChild(n)
}

func remove(n *html.Node) {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

// ---- note rendering -----------------------------------------------------

// renderNote writes the uniform samizdat frontmatter + the markdown body.
// Every post carries categories [samizdat, substack]: samizdat is the umbrella
// for anything externally published, substack the distribution it shipped on.
func renderNote(p *post) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\n", yamlQuote(p.Title))
	if p.Subtitle != "" {
		fmt.Fprintf(&b, "subtitle: %s\n", yamlQuote(p.Subtitle))
	}
	if p.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", yamlQuote(p.Description))
	}
	if p.Published != "" {
		fmt.Fprintf(&b, "published: %s\n", p.Published)
	}
	fmt.Fprintf(&b, "url: %s\n", p.URL)
	b.WriteString("source: substack\n")
	b.WriteString("categories: [samizdat, substack]\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(p.Markdown))
	b.WriteString("\n")
	return b.String()
}

// yamlQuote renders a double-quoted YAML scalar (always quoted: titles carry
// colons, quotes and leading symbols that would otherwise change meaning).
func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}
