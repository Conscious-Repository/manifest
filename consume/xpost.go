package consume

import (
	"net/url"
	"regexp"
	"strings"

	"manifest/mdfm"
)

// X POSTS — one title convention, whichever entrance wrote the note.
//
// A curated X post reaches the vault two ways, and until this file existed
// they disagreed about what the post is CALLED:
//
//	the lane's button   on an item RSSHub delivered, whose feed <title> is the
//	                    post TEXT — so the public feed's title was a wall of
//	                    the post, said twice
//	a pasted address    whose title comes from X's oEmbed, which states the
//	                    author's DISPLAY NAME — "Augustus Doricko on X"
//
// The owner's call is the second shape with the first's precision: an X post
// is titled `@handle on X`, and its body is the post. The handle comes from
// the canonical URL, which is the only place it is stated exactly, casing and
// all; a display name is the last resort, not the convention.
//
// Nothing here writes anything. writeCurated applies it — once, for both
// entrances — and backfillXPosts replays it over notes curated before it
// existed, which is the same normalization run twice rather than a second
// idea of what an X note looks like.

// xStatusPath is the canonical shape of a post's address: /<handle>/status/<id>.
// A handle is 1–15 of [A-Za-z0-9_]; the id is digits. `/statuses/` is the older
// spelling and still redirects.
var xStatusPath = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})/status(?:es)?/[0-9]+`)

// xHandleWord is a handle standing on its own, as `@melissa` does in the
// source and author lines an RSSHub subscription writes.
var xHandleWord = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)

// IsXStatusURL reports whether a URL is one X post's canonical address.
//
// It is deliberately strict — host AND path — because it is the SELECTION rule
// for the retrofit: a note this returns false for is never rewritten, and the
// owner's extrinsic/ is full of notes that are none of this lane's business.
func IsXStatusURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	return isXHost(u.Hostname()) && xStatusPath.MatchString(u.Path)
}

func isXHost(host string) bool {
	h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	return h == "x.com" || h == "twitter.com" ||
		strings.HasSuffix(h, ".x.com") || strings.HasSuffix(h, ".twitter.com")
}

// xHandleIn reads the handle out of a status path, WITHOUT asking about the
// host. Its callers already know they are looking at an X post — the oEmbed
// provider answered for it — and the host they hold may be a mirror.
func xHandleIn(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	m := xStatusPath.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return m[1]
}

// xPostTitle is the public title of one X post, in the order the handle can be
// known: stated by the URL, stated by a field the vault already writes as a
// handle, and only then a display name.
//
// The last rung is not `@`-prefixed on purpose. Inventing a handle out of
// "Augustus Doricko" would publish a false address; naming the writer is the
// honest thing left to say.
func xPostTitle(rawURL, author, source string) string {
	if h := firstNonEmpty(xHandleIn(rawURL), atHandle(author), atHandle(source)); h != "" {
		return "@" + h + " on X"
	}
	if name := xDisplayName(author, source); name != "" {
		return name + " on X"
	}
	return "A post on X"
}

// atHandle reads a handle out of a field that already states one.
func atHandle(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "@") {
		return ""
	}
	if v = strings.TrimPrefix(v, "@"); !xHandleWord.MatchString(v) {
		return ""
	}
	return v
}

// xDisplayName is the writer's name, skipping the values that name the
// PLATFORM — "X" is what the oEmbed provider calls itself, and "X on X" is not
// a title.
func xDisplayName(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		switch strings.ToLower(v) {
		case "", "x", "twitter", "x.com", "twitter.com":
			continue
		}
		return v
	}
	return ""
}

// xPostHTML narrows X's oEmbed rendering to the post itself.
//
// The provider answers with a blockquote holding the post in a <p>, followed
// by its own byline — "— Augustus Doricko (@ADoricko) September 2, 2026" —
// and a widget script. Sanitize takes the script; this takes the byline, which
// is duplicated furniture: the note already carries a Source: footer and the
// public feed already renders its own attribution line.
//
// Anything that is not that shape is returned untouched. A body we did not
// recognize is a body we do not edit.
func xPostHTML(in string) string {
	open := strings.Index(in, "<blockquote")
	if open < 0 {
		return in
	}
	gt := strings.Index(in[open:], ">")
	if gt < 0 {
		return in
	}
	inner := in[open+gt+1:]
	if end := strings.Index(inner, "</blockquote>"); end >= 0 {
		inner = inner[:end]
	}
	first := strings.Index(inner, "<p")
	last := strings.LastIndex(inner, "</p>")
	if first < 0 || last < first {
		return in
	}
	return strings.TrimSpace(inner[first : last+len("</p>")])
}

// xAttribution matches the byline X's oEmbed blockquote ends with, in the
// markdown it became: an em dash, a name, and the handle in parentheses.
//
// Matching it is also the GATE on xCleanMirror below. Its presence is proof
// the body is the provider's render rather than the owner's own writing, and
// without that proof nothing is stripped.
var xAttribution = regexp.MustCompile(`^>?\s*(?:—|&mdash;|--)\s*\S.*\(@[A-Za-z0-9_]{1,15}\)`)

// xCleanMirror turns a mirrored oEmbed blockquote back into the post's own
// paragraphs and drops the byline it ends with. A body with no byline is not
// that render and comes back exactly as it went in.
func xCleanMirror(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	hit := -1
	for i, ln := range lines {
		if xAttribution.MatchString(strings.TrimSpace(ln)) {
			hit = i
		}
	}
	if hit < 0 {
		return body
	}
	out := append(append([]string{}, lines[:hit]...), lines[hit+1:]...)
	for i, ln := range out {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, ">") {
			t = strings.TrimSpace(strings.TrimPrefix(t, ">"))
		}
		out[i] = t
	}
	return strings.TrimSpace(strings.Join(squeezeBlanks(out), "\n"))
}

// squeezeBlanks collapses the runs of empty lines unwrapping a blockquote
// leaves behind.
func squeezeBlanks(in []string) []string {
	out := make([]string, 0, len(in))
	for _, ln := range in {
		if strings.TrimSpace(ln) == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// xNoteBody re-renders one X note's body onto the convention: the tag line if
// it had one, the handle heading, then everything the note already said with
// the provider's byline taken out.
//
// Everything below the heading is the owner's and survives — this is the same
// fixpoint discipline setFrontmatter applies to one key and paperNoteBody
// applies to one section, so running it on a note it already produced returns
// that note.
func xNoteBody(body, title string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")), "\n")
	i, tag := 0, ""
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "#article") {
		tag = strings.TrimSpace(lines[i])
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "# ") {
		i++
	}
	rest := xCleanMirror(strings.TrimSpace(strings.Join(lines[i:], "\n")))

	var b strings.Builder
	if tag != "" {
		b.WriteString(tag + "\n\n")
	}
	b.WriteString("# " + strings.TrimSpace(title) + "\n")
	if rest != "" {
		b.WriteString("\n" + rest + "\n")
	}
	return b.String()
}

// applyXPost normalizes one curated X note in place. It is what both a fresh
// curate and the retrofit run, which is why a retrofitted note and a freshly
// written one are the same document.
func applyXPost(content, title string) string {
	if strings.TrimSpace(title) == "" {
		return content
	}
	fm, body := mdfm.Split(content)
	// A `title:` line, where a note carries one, is what parseCurated reads
	// FIRST — leaving it behind would leave the two disagreeing. It is never
	// added here: the heading is this vault's way of titling a note.
	if _, ok := fm["title"]; ok {
		content = setFrontmatter(content, "title", yamlScalar(title))
		_, body = mdfm.Split(content)
	}
	return replaceBody(content, xNoteBody(body, title))
}
