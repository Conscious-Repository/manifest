package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"manifest/record"
)

// candidate is an existing TOP-LEVEL vault note whose frontmatter marks it as
// writing/published — the only notes that can be a stray copy of a post.
// intrinsic/ (daily drafts) and every other folder are never candidates.
type candidate struct {
	Name string // file name, e.g. "being a lizard.md"
	slug string // slugified stem
	key  string // normalized opening prose (≥ minKey chars) or ""
}

// writingTags are the categories that mark a note as an output piece
// (categories/_taxonomy.md: essays / writing / published / substack).
var writingTags = map[string]bool{"substack": true, "published": true, "essays": true, "writing": true}

const (
	minKey  = 60  // shortest opening-prose key we trust for a body match
	keyLen  = 150 // how much opening prose the key holds
	headLen = 600 // how far into the post the key may sit
)

func scanCandidates(vault string) ([]candidate, error) {
	entries, err := os.ReadDir(vault)
	if err != nil {
		return nil, err
	}
	var out []candidate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(vault, e.Name()))
		if err != nil {
			continue
		}
		block, body, ok := record.SplitFrontmatter(string(raw))
		if !ok || !hasWritingTag(block) {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		out = append(out, candidate{Name: e.Name(), slug: slugify(stem), key: proseKey(body, keyLen)})
	}
	return out, nil
}

// hasWritingTag reads `categories:` in either the inline `[a, b]` form or
// the block `- a` form.
func hasWritingTag(block []string) bool {
	inList := false
	for _, ln := range block {
		t := strings.TrimSpace(ln)
		if k, v, ok := strings.Cut(ln, ":"); ok && !strings.HasPrefix(t, "-") {
			inList = strings.TrimSpace(k) == "categories"
			if inList {
				for _, c := range record.ParseList(v) {
					if writingTags[strings.ToLower(c)] {
						return true
					}
				}
			}
			continue
		}
		if inList && strings.HasPrefix(t, "-") {
			if writingTags[strings.ToLower(record.Unquote(strings.TrimPrefix(t, "-")))] {
				return true
			}
		}
	}
	return false
}

// matches reports whether this note is the post: same slug, same title, or
// the note's opening prose sits in the post's opening prose.
func (c candidate) matches(p *post) bool {
	if c.slug != "" && (c.slug == p.Slug || c.slug == slugify(p.Title)) {
		return true
	}
	if len(c.key) >= minKey {
		head := proseKey(p.Markdown, headLen)
		if strings.Contains(head, c.key) {
			return true
		}
	}
	return false
}

// slugify approximates Substack's slug: lowercase, apostrophes dropped,
// every other non-alphanumeric run → "-".
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("'", "", "’", "", "‘", "").Replace(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

var (
	reMDLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	reWiki    = regexp.MustCompile(`\[\[([^\]]*)\]\]`)
	reHTMLTag = regexp.MustCompile(`<[^>]+>`)
)

// proseKey reduces a markdown body to its opening prose: headings, italic-only
// lines (subtitles / bylines), image lines and "published …" stamps are
// skipped, links reduced to their text, then everything but letters and
// digits is dropped and the result cut to n runes.
func proseKey(body string, n int) string {
	var b strings.Builder
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "!") || strings.HasPrefix(t, "[^") ||
			strings.HasPrefix(t, "published") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "---") ||
			isEmphasisOnly(t) {
			continue
		}
		t = reMDLink.ReplaceAllString(t, "$1")
		t = reWiki.ReplaceAllString(t, "$1")
		t = reHTMLTag.ReplaceAllString(t, "")
		for _, r := range strings.ToLower(t) {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		if b.Len() >= n*4 { // bytes; runes may be multi-byte — cut below
			break
		}
	}
	runes := []rune(b.String())
	if len(runes) > n {
		runes = runes[:n]
	}
	return string(runes)
}

// isEmphasisOnly: a line that is entirely one italic/bold span (a subtitle or
// byline, not body prose).
func isEmphasisOnly(t string) bool {
	for _, m := range []string{"_", "*", "**", "__"} {
		if len(t) > 2*len(m) && strings.HasPrefix(t, m) && strings.HasSuffix(t, m) &&
			!strings.Contains(strings.TrimSuffix(strings.TrimPrefix(t, m), m), m) {
			return true
		}
	}
	return false
}
