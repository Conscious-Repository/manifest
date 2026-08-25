package consume

import (
	"strings"

	"golang.org/x/net/html"
)

// HTML → markdown, for the body of a curated note.
//
// This exists because there is no markdown renderer in this codebase at all —
// no goldmark, no blackfriday — so a curated note has to be authored AS
// markdown rather than converted on the way out. That is the better shape
// anyway: what lands in extrinsic/ should be a note the owner can read, edit
// and link to in Obsidian, not a blob of escaped markup.
//
// The input is always sanitizer output, so the tag vocabulary is small and
// known. Anything unexpected degrades to its text rather than being dropped.

// ToMarkdown converts sanitized article HTML to markdown.
func ToMarkdown(sanitized string) string {
	if strings.TrimSpace(sanitized) == "" {
		return ""
	}
	nodes, err := html.ParseFragment(strings.NewReader(sanitized), fragmentCtx())
	if err != nil {
		return Text(sanitized)
	}
	var b strings.Builder
	for _, n := range nodes {
		block(&b, n, "")
	}
	return tidy(b.String())
}

// block renders node n at list-indent prefix `indent`.
func block(b *strings.Builder, n *html.Node, indent string) {
	if n.Type == html.TextNode {
		if t := strings.TrimSpace(n.Data); t != "" {
			b.WriteString(inlineText(n.Data))
		}
		return
	}
	if n.Type != html.ElementNode {
		return
	}
	switch strings.ToLower(n.Data) {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level := int(n.Data[1] - '0')
		b.WriteString("\n" + strings.Repeat("#", level) + " " + inline(n) + "\n\n")
	case "p":
		if t := strings.TrimSpace(inline(n)); t != "" {
			b.WriteString(indent + t + "\n\n")
		}
	case "br":
		b.WriteString("\n")
	case "hr":
		b.WriteString("\n---\n\n")
	case "blockquote":
		var inner strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			block(&inner, c, "")
		}
		for _, ln := range strings.Split(strings.TrimSpace(inner.String()), "\n") {
			if strings.TrimSpace(ln) == "" {
				b.WriteString(indent + ">\n")
				continue
			}
			b.WriteString(indent + "> " + ln + "\n")
		}
		b.WriteString("\n")
	case "ul", "ol":
		ordered := strings.EqualFold(n.Data, "ol")
		i := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || !strings.EqualFold(c.Data, "li") {
				continue
			}
			i++
			marker := "- "
			if ordered {
				marker = itoa(i) + ". "
			}
			listItem(b, c, indent, marker)
		}
		b.WriteString("\n")
	case "pre":
		b.WriteString("\n```\n" + strings.TrimRight(Text(render(n)), "\n") + "\n```\n\n")
	case "figure":
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			block(b, c, indent)
		}
	case "figcaption":
		if t := strings.TrimSpace(inline(n)); t != "" {
			b.WriteString(indent + "*" + t + "*\n\n")
		}
	case "img":
		b.WriteString(indent + inlineOne(n) + "\n\n")
	default:
		// An inline element at block level, or a wrapper the sanitizer left:
		// render its text where it stands rather than losing it.
		if t := strings.TrimSpace(inline(n)); t != "" {
			b.WriteString(indent + t + "\n\n")
		}
	}
}

// listItem renders one <li>, keeping nested lists indented under it.
func listItem(b *strings.Builder, li *html.Node, indent, marker string) {
	var lead strings.Builder
	var nested strings.Builder
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (strings.EqualFold(c.Data, "ul") || strings.EqualFold(c.Data, "ol")) {
			block(&nested, c, indent+"  ")
			continue
		}
		lead.WriteString(inlineNode(c))
	}
	b.WriteString(indent + marker + strings.TrimSpace(collapseSpaces(lead.String())) + "\n")
	if s := strings.TrimRight(nested.String(), "\n"); s != "" {
		b.WriteString(s + "\n")
	}
}

// inline renders a node's children as one line of markdown.
func inline(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(inlineNode(c))
	}
	return collapseSpaces(b.String())
}

func inlineNode(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return inlineText(n.Data)
	case html.ElementNode:
		return inlineOne(n)
	}
	return ""
}

func inlineOne(n *html.Node) string {
	switch strings.ToLower(n.Data) {
	case "em", "i":
		return wrap(inline(n), "*")
	case "strong", "b":
		return wrap(inline(n), "**")
	case "code":
		if t := inline(n); t != "" {
			return "`" + t + "`"
		}
		return ""
	case "a":
		text := inline(n)
		href := attr(n, "href")
		if href == "" {
			return text
		}
		if text == "" {
			text = href
		}
		return "[" + text + "](" + href + ")"
	case "img":
		src := attr(n, "src")
		if src == "" {
			return ""
		}
		return "![" + attr(n, "alt") + "](" + src + ")"
	case "br":
		return "\n"
	default:
		return inline(n)
	}
}

// wrap applies an emphasis marker, skipping empty or whitespace-only content
// (markdown treats "** **" as literal asterisks, not emphasis).
func wrap(text, marker string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	lead := leading(text)
	trail := trailing(text)
	return lead + marker + strings.TrimSpace(text) + marker + trail
}

func leading(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func trailing(s string) string {
	return s[len(strings.TrimRight(s, " \t")):]
}

func inlineText(s string) string {
	// Escape only what would otherwise become markup at the start of a line;
	// over-escaping makes a note ugly to read in Obsidian, which defeats the
	// point of writing markdown at all.
	return strings.ReplaceAll(s, " ", " ")
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func render(n *html.Node) string {
	var b strings.Builder
	_ = html.Render(&b, n)
	return b.String()
}

func collapseSpaces(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteRune(' ')
		}
		space = false
		b.WriteRune(r)
	}
	if space && b.Len() > 0 {
		b.WriteRune(' ')
	}
	return b.String()
}

// tidy collapses runs of blank lines so the note reads cleanly.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		if ln == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}
