package consume

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// fragmentCtx is the parsing context every body is parsed against. It must
// carry a real DataAtom: html.ParseFragment refuses a context node it cannot
// identify, and a refusal here would silently route every article through the
// escape-everything fallback — safe, but unreadable.
func fragmentCtx() *html.Node {
	return &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
}

// The sanitizer is this package's whole safety story.
//
// Every FEED card built before CONSUME is assembled from text nodes —
// el(tag, cls, text) in the client — which is precisely WHY an untrusted portal
// title has never been able to run anything. The reader breaks that invariant
// on purpose: it renders third-party article bodies as real markup, so it is
// the first innerHTML sink in the feed. The client is allowed that one sink
// only because the bytes it receives passed through here first, server-side, at
// fetch time.
//
// The posture is an ALLOWLIST, and the three outcomes are deliberate:
//
//	drop     — the element and everything inside it disappear (script, iframe…)
//	unwrap   — the element disappears, its children survive (div, span, table…)
//	keep     — the element survives with an allowlisted subset of attributes
//
// Unwrapping rather than dropping unknown elements is what keeps a Substack
// post readable when its markup is wrapped in six layers of styling divs: the
// prose is in the leaves, and dropping a wrapper would take the article with
// it.

// maxHTML bounds one article body. Nothing downstream reads unbounded input,
// and a feed that serves a 500MB "post" should degrade to a truncated article
// rather than an allocation failure.
const maxHTML = 4 << 20

// dropped elements take their subtree with them. Everything with a script
// engine, a network fetch, a form post, or an embedding boundary lives here.
var dropped = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true,
	"embed": true, "applet": true, "form": true, "input": true,
	"button": true, "select": true, "option": true, "textarea": true,
	"svg": true, "math": true, "noscript": true, "link": true,
	"meta": true, "base": true, "frame": true, "frameset": true,
	"canvas": true, "audio": true, "video": true, "source": true,
	"track": true, "template": true, "portal": true, "dialog": true,
}

// kept elements are the vocabulary of prose. Anything absent is unwrapped, so
// this list grows only when a real feed proves it needs to.
var kept = map[string]bool{
	"p": true, "a": true, "em": true, "strong": true, "b": true, "i": true,
	"u": true, "s": true, "code": true, "pre": true, "blockquote": true,
	"ul": true, "ol": true, "li": true, "br": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"img": true, "figure": true, "figcaption": true, "sup": true, "sub": true,
}

// keptAttrs is the per-tag attribute allowlist. An attribute absent from this
// map is dropped, which is what disposes of every on* handler in one rule
// rather than by enumerating them.
var keptAttrs = map[string]map[string]bool{
	"a":   {"href": true, "title": true},
	"img": {"src": true, "alt": true, "title": true},
}

// Sanitize renders untrusted feed HTML down to the allowlist above. The output
// is a fragment (no <html>/<body> wrapper) safe to assign to innerHTML, and it
// is idempotent: Sanitize(Sanitize(x)) == Sanitize(x).
func Sanitize(in string) string {
	if len(in) > maxHTML {
		in = in[:maxHTML]
	}
	if strings.TrimSpace(in) == "" {
		return ""
	}
	nodes, err := html.ParseFragment(strings.NewReader(in), fragmentCtx())
	if err != nil {
		// A body we cannot parse is a body we cannot vouch for. Fall back to
		// the text, escaped — never to the raw input.
		return html.EscapeString(Text(in))
	}
	var b strings.Builder
	for _, n := range nodes {
		for _, c := range clean(n) {
			_ = html.Render(&b, c)
		}
	}
	return strings.TrimSpace(b.String())
}

// clean returns the sanitized replacements for one node — zero nodes (dropped),
// one (kept), or its children (unwrapped).
func clean(n *html.Node) []*html.Node {
	switch n.Type {
	case html.TextNode:
		return []*html.Node{{Type: html.TextNode, Data: n.Data}}
	case html.ElementNode:
		tag := strings.ToLower(n.Data)
		if dropped[tag] {
			return nil // subtree and all — never recurse into it
		}
		kids := cleanKids(n)
		if !kept[tag] {
			return kids // unwrap: the element goes, the prose stays
		}
		attr, ok := safeAttrs(tag, n.Attr)
		if !ok {
			// A kept tag whose own attributes disqualify it: an <a> with a
			// javascript: href unwraps (the reader keeps the words), an <img>
			// with an unusable src is simply gone.
			if tag == "img" {
				return nil
			}
			return kids
		}
		out := &html.Node{Type: html.ElementNode, DataAtom: n.DataAtom, Data: tag, Attr: attr}
		for _, k := range kids {
			out.AppendChild(k)
		}
		return []*html.Node{out}
	default:
		// Comments, doctypes, and anything else carry no prose.
		return nil
	}
}

func cleanKids(n *html.Node) []*html.Node {
	var kids []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		kids = append(kids, clean(c)...)
	}
	return kids
}

// safeAttrs filters one element's attributes. ok=false means the element itself
// cannot be rendered safely (a link or image whose target we refuse).
func safeAttrs(tag string, in []html.Attribute) ([]html.Attribute, bool) {
	allow := keptAttrs[tag]
	out := make([]html.Attribute, 0, len(in))
	for _, a := range in {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" || !allow[key] {
			continue // drops every on*, style, id, class, data-* in one rule
		}
		switch {
		case tag == "a" && key == "href":
			if !safeURL(a.Val, false) {
				return nil, false
			}
		case tag == "img" && key == "src":
			// https only: an http image on an https page is a mixed-content
			// block in the reader and a privacy leak in the public feed.
			if !safeURL(a.Val, true) {
				return nil, false
			}
		}
		out = append(out, html.Attribute{Key: key, Val: a.Val})
	}
	if tag == "a" && !has(out, "href") {
		return nil, false // a link to nowhere is just its text
	}
	if tag == "img" && !has(out, "src") {
		return nil, false
	}
	if tag == "a" {
		// Defence in depth for the reader, which renders into the dashboard's
		// own document: a curated link must not reach window.opener.
		out = append(out, html.Attribute{Key: "rel", Val: "noopener noreferrer"})
		out = append(out, html.Attribute{Key: "target", Val: "_blank"})
	}
	if tag == "img" {
		out = append(out, html.Attribute{Key: "loading", Val: "lazy"})
	}
	return out, true
}

func has(attrs []html.Attribute, key string) bool {
	for _, a := range attrs {
		if a.Key == key {
			return true
		}
	}
	return false
}

// safeURL admits http(s) absolute URLs and protocol-relative ones. Everything
// else — javascript:, data:, vbscript:, file:, and the whitespace/control
// tricks used to smuggle them past naive prefix checks — is refused.
//
// The control-character strip happens FIRST because "java\tscript:alert(1)" is
// a scheme browsers honour and a prefix test does not.
func safeURL(raw string, httpsOnly bool) bool {
	u := strings.Map(func(r rune) rune {
		if r <= 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, raw)
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "//") {
		return true // protocol-relative: inherits the page's https
	}
	lower := strings.ToLower(u)
	if i := strings.Index(lower, ":"); i >= 0 {
		// A colon before any / ? # is a scheme; anything after is just a path.
		if j := strings.IndexAny(lower, "/?#"); j < 0 || i < j {
			scheme := lower[:i]
			if httpsOnly {
				return scheme == "https"
			}
			return scheme == "http" || scheme == "https"
		}
	}
	// No scheme at all — a relative URL. Feed bodies are rendered outside their
	// origin, so a relative link resolves against the wrong host and is worse
	// than useless.
	return false
}

// Text strips markup to plain text, for excerpts and length filters. Block
// boundaries become newlines so an excerpt does not run two paragraphs
// together.
func Text(in string) string {
	if len(in) > maxHTML {
		in = in[:maxHTML]
	}
	nodes, err := html.ParseFragment(strings.NewReader(in), fragmentCtx())
	if err != nil {
		return collapse(in)
	}
	var b strings.Builder
	for _, n := range nodes {
		textInto(&b, n)
	}
	return collapse(b.String())
}

var blockish = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "blockquote": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"tr": true, "pre": true, "figcaption": true, "hr": true, "section": true,
}

func textInto(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		if dropped[strings.ToLower(n.Data)] {
			return
		}
	case html.DocumentNode:
		// A whole parsed page, not a fragment. Recurse rather than bail —
		// html.Parse returns one of these, and returning here made the text of
		// every full page come back empty.
	default:
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		textInto(b, c)
	}
	if blockish[strings.ToLower(n.Data)] {
		b.WriteString("\n")
	}
}

// collapse normalizes whitespace: runs of spaces become one, runs of blank
// lines become one blank line. Paragraph structure survives; feed indentation
// does not.
func collapse(s string) string {
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, ln := range lines {
		ln = strings.Join(strings.Fields(ln), " ")
		if ln == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Excerpt returns the first n runes of the plain text, cut at a word boundary,
// with an ellipsis when it actually truncated.
func Excerpt(plain string, n int) string {
	plain = strings.Join(strings.Fields(plain), " ")
	if len([]rune(plain)) <= n {
		return plain
	}
	r := []rune(plain)[:n]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:—-") + "…"
}
