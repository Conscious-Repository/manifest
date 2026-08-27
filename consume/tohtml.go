package consume

import (
	"html"
	"strings"
)

// markdown → HTML, the other direction from tomd.go, and the reason a wiped
// dataDir no longer costs the public feed its bodies.
//
// The curated note is the ARCHIVE. buildNote writes the whole article into
// extrinsic/ as markdown, versioned by the same git history as everything else
// the owner keeps, while <dataDir>/consume/snapshots is disposable cache. The
// public feed used to read only the snapshot, so losing that cache degraded a
// piece the owner had already published to a title and a link — with the whole
// thing sitting in the vault, unread, because nothing here could render it.
//
// The vocabulary is exactly what ToMarkdown emits, plus what a person types
// underneath it in Obsidian. This is a projection of prose, not a CommonMark
// implementation: reference links, tables, setext headings, HTML blocks and
// loose lists are not markup here, they are text, and degrading to their own
// characters is the right failure for a document nobody can re-render.
//
// The output goes through Sanitize before it leaves, so a note body is held to
// the same allowlist as a publisher's markup — the vault is trusted, but the
// public surface should not have to know that.

// ToHTML renders a curated note's markdown body as sanitized HTML.
func ToHTML(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	var b strings.Builder
	mdBlocks(&b, strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n"))
	return Sanitize(b.String())
}

// mdBlocks renders a run of lines. It recurses for blockquotes and nested
// lists, which is why it takes lines rather than a string.
func mdBlocks(b *strings.Builder, lines []string) {
	for i := 0; i < len(lines); {
		ln := lines[i]
		t := strings.TrimSpace(ln)
		switch {
		case t == "":
			i++
		case isFence(t):
			i = mdFence(b, lines, i)
		case isRule(t):
			b.WriteString("<hr>")
			i++
		case mdHeading(t) > 0:
			n := mdHeading(t)
			lvl := itoa(n)
			b.WriteString("<h" + lvl + ">" + inlineHTML(strings.TrimSpace(t[n:])) + "</h" + lvl + ">")
			i++
		case strings.HasPrefix(t, ">"):
			var inner []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				q := strings.TrimPrefix(strings.TrimSpace(lines[i]), ">")
				inner = append(inner, strings.TrimPrefix(q, " "))
				i++
			}
			b.WriteString("<blockquote>")
			mdBlocks(b, inner)
			b.WriteString("</blockquote>")
		case mdMarker(ln) != "":
			i = mdList(b, lines, i, mdIndent(ln))
		default:
			i = mdParagraph(b, lines, i)
		}
	}
}

// mdFence renders a ``` block. An unterminated fence takes the rest of the
// note as code rather than losing it.
func mdFence(b *strings.Builder, lines []string, i int) int {
	fence := strings.TrimSpace(lines[i])[:3]
	i++
	start := i
	for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
		i++
	}
	code := strings.Join(lines[start:i], "\n")
	if i < len(lines) {
		i++ // the closing fence
	}
	b.WriteString("<pre><code>" + html.EscapeString(code) + "\n</code></pre>")
	return i
}

// mdParagraph gathers consecutive prose lines into one <p>. A hand-wrapped
// paragraph in Obsidian is one paragraph, not four.
func mdParagraph(b *strings.Builder, lines []string, i int) int {
	var para []string
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" || isFence(t) || isRule(t) || mdHeading(t) > 0 ||
			strings.HasPrefix(t, ">") || mdMarker(lines[i]) != "" {
			break
		}
		para = append(para, t)
		i++
	}
	if s := inlineHTML(strings.Join(para, " ")); strings.TrimSpace(s) != "" {
		b.WriteString("<p>" + s + "</p>")
	}
	return i
}

// mdList renders one list at `indent`. A deeper marker on the next line is the
// current item's own list and is nested INSIDE its <li> — a sibling <ul> would
// be markup the parser has to repair.
func mdList(b *strings.Builder, lines []string, i, indent int) int {
	ordered := mdOrdered(lines[i])
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	b.WriteString("<" + tag + ">")
	for i < len(lines) {
		m := mdMarker(lines[i])
		if m == "" || mdIndent(lines[i]) != indent || mdOrdered(lines[i]) != ordered {
			break
		}
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), m))
		b.WriteString("<li>" + inlineHTML(text))
		i++
		if i < len(lines) && mdMarker(lines[i]) != "" && mdIndent(lines[i]) > indent {
			i = mdList(b, lines, i, mdIndent(lines[i]))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</" + tag + ">")
	return i
}

func isFence(t string) bool {
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// isRule matches a thematic break: three or more of one marker and nothing
// else. buildNote writes `---` above the source line of every note.
func isRule(t string) bool {
	if len(t) < 3 {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	return strings.Trim(t, string(c)+" ") == ""
}

// mdHeading returns the ATX heading level, or 0.
func mdHeading(t string) int {
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(t) || t[n] != ' ' {
		return 0
	}
	return n
}

// mdMarker returns the list marker at the head of a line ("- ", "1. "), or "".
func mdMarker(ln string) string {
	t := strings.TrimLeft(ln, " \t")
	for _, m := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(t, m) {
			return m
		}
	}
	d := 0
	for d < len(t) && t[d] >= '0' && t[d] <= '9' {
		d++
	}
	if d > 0 && d+1 < len(t) && t[d] == '.' && t[d+1] == ' ' {
		return t[:d+2]
	}
	return ""
}

func mdOrdered(ln string) bool {
	m := mdMarker(ln)
	return m != "" && strings.HasSuffix(m, ". ")
}

func mdIndent(ln string) int {
	n := 0
	for _, r := range ln {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// inlineHTML renders one line's inline markup. Everything it does not
// recognize is escaped text, so a stray bracket reads as a stray bracket.
func inlineHTML(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				b.WriteString("<code>" + html.EscapeString(s[i+1:i+1+j]) + "</code>")
				i += j + 2
				continue
			}
		case s[i] == '!' && i+1 < len(s) && s[i+1] == '[':
			if alt, src, n, ok := linkAt(s[i+1:]); ok {
				b.WriteString(`<img src="` + html.EscapeString(src) + `" alt="` + html.EscapeString(alt) + `">`)
				i += n + 1
				continue
			}
		case s[i] == '[':
			if text, href, n, ok := linkAt(s[i:]); ok {
				b.WriteString(`<a href="` + html.EscapeString(href) + `">` + inlineHTML(text) + `</a>`)
				i += n
				continue
			}
		case strings.HasPrefix(s[i:], "**"):
			if j := strings.Index(s[i+2:], "**"); j > 0 {
				b.WriteString("<strong>" + inlineHTML(s[i+2:i+2+j]) + "</strong>")
				i += j + 4
				continue
			}
		case s[i] == '*':
			if j := closingEm(s[i+1:]); j > 0 {
				b.WriteString("<em>" + inlineHTML(s[i+1:i+1+j]) + "</em>")
				i += j + 2
				continue
			}
		}
		b.WriteString(html.EscapeString(s[i : i+1]))
		i++
	}
	return b.String()
}

// linkAt parses `[text](href)` at the head of s, returning what it consumed.
func linkAt(s string) (text, href string, n int, ok bool) {
	if len(s) == 0 || s[0] != '[' {
		return "", "", 0, false
	}
	end := strings.IndexByte(s, ']')
	if end < 0 || end+1 >= len(s) || s[end+1] != '(' {
		return "", "", 0, false
	}
	paren := strings.IndexByte(s[end+2:], ')')
	if paren < 0 {
		return "", "", 0, false
	}
	target := strings.TrimSpace(s[end+2 : end+2+paren])
	// `(url "a title")` — the title is not markup we carry.
	if sp := strings.IndexAny(target, " \t"); sp >= 0 {
		target = target[:sp]
	}
	return s[1:end], target, end + paren + 3, true
}

// closingEm finds the `*` that closes an emphasis run. Markdown does not open
// emphasis on "3 * 4 * 5", and neither does this: the marker must hug its
// text at both ends.
func closingEm(rest string) int {
	if rest == "" || rest[0] == ' ' || rest[0] == '*' {
		return 0
	}
	j := strings.IndexByte(rest, '*')
	if j <= 0 || rest[j-1] == ' ' {
		return 0
	}
	return j
}
