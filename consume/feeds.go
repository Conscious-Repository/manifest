package consume

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/record"
)

// extrinsic/feeds.md — the subscription list, as a record-kernel document.
//
// This is owner-authored data (§2 tier 1/2 reasoning: it is a decision, not a
// cache), so it lives in the vault where git versions it and Obsidian can edit
// it. That places it under the FIXPOINT guarantee (§3): parse→emit must be
// byte-identical, or every poll would churn the file and every hand edit would
// fight the app.
//
// The implementation earns that guarantee the same way the rest of the
// codebase does — by never regenerating the document. The file is held as its
// literal lines; a change rewrites the ONE line it affects, in place, and
// unrecognized inline fields on that line keep their original order and
// spelling. A [tag:: favourite] hand-added in Obsidian survives a rename made
// in the app.
//
// Grammar:
//
//	## essays                                        ← the list/group
//	- Astral Codex Ten [id:: acx] [kind:: rss] [url:: https://…] [mirror:: full]
//	- Melissa [id:: melissa] [kind:: x] [handle:: melissa] [min-chars:: 350]
//
// The id is written explicitly rather than derived from the title, so renaming
// a subscription cannot orphan its cached items or its curated notes.

const feedsPath = "extrinsic/feeds.md"

const ungrouped = "unfiled"

// feedsScaffold is written only when the file does not exist yet. One factual
// line, no authored content — the vault's prose is the owner's.
const feedsScaffold = `---
categories: [feeds]
---
#feeds

Sources the CONSUME lane polls. Hand edits are preserved; unknown fields survive.

## ` + ungrouped + `
`

// Doc is extrinsic/feeds.md held as lines plus the subscriptions parsed out of
// them. Every mutation is line-local.
type Doc struct {
	lines []string
	subs  []docSub
}

type docSub struct {
	line int // index into Doc.lines
	sub  Subscription
}

// ParseFeeds reads the document. Anything that is not a recognizable
// subscription line — prose, frontmatter, comments, other headings — is kept
// verbatim and ignored.
func ParseFeeds(content string) *Doc {
	d := &Doc{}
	if content == "" {
		content = feedsScaffold
	}
	d.lines = strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	group := ""
	inFrontmatter := false
	for i, ln := range d.lines {
		trimmed := strings.TrimSpace(ln)
		// Frontmatter is opaque: a "- item" inside a YAML list is not a
		// subscription.
		if trimmed == "---" && (i == 0 || inFrontmatter) {
			inFrontmatter = !inFrontmatter
			continue
		}
		if inFrontmatter {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			group = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		if sub, ok := parseSubLine(trimmed, group); ok {
			d.subs = append(d.subs, docSub{line: i, sub: sub})
		}
	}
	return d
}

// parseSubLine reads one "- Title [k:: v] …" line. A bullet with no fields is
// prose in a list and is left alone.
func parseSubLine(trimmed, group string) (Subscription, bool) {
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	title, fields := record.ParseFields(body)
	if len(fields) == 0 {
		return Subscription{}, false
	}
	sub := Subscription{
		Title: strings.TrimSpace(strings.Trim(title, "[]")),
		List:  group,
	}
	for _, f := range fields {
		switch strings.ToLower(f.Key) {
		case "id":
			sub.ID = f.Value
		case "kind":
			sub.Kind = strings.ToLower(f.Value)
		case "url":
			sub.URL = f.Value
		case "handle":
			sub.Handle = strings.TrimPrefix(f.Value, "@")
		case "mirror":
			sub.Mirror = strings.ToLower(f.Value)
		case "min-chars":
			sub.MinChars, _ = strconv.Atoi(f.Value)
		case "fulltext":
			sub.Fulltext = strings.ToLower(f.Value)
		case "added":
			sub.Added = f.Value
		default:
			sub.Unknown = append(sub.Unknown, Field{Key: f.Key, Value: f.Value})
		}
	}
	if sub.Kind == "" {
		if sub.Handle != "" {
			sub.Kind = KindX
		} else {
			sub.Kind = KindRSS
		}
	}
	if sub.ID == "" {
		// A hand-added line with no id still works: derive one, and the next
		// app write will stamp it explicitly.
		sub.ID = record.Slug(firstNonEmpty(sub.Title, sub.Handle, sub.URL), 40)
	}
	if sub.ID == "" {
		return Subscription{}, false
	}
	return sub, true
}

// Subs returns the parsed subscriptions in document order.
func (d *Doc) Subs() []Subscription {
	out := make([]Subscription, 0, len(d.subs))
	for _, s := range d.subs {
		out = append(out, s.sub)
	}
	return out
}

// Find returns one subscription by id.
func (d *Doc) Find(id string) (Subscription, bool) {
	for _, s := range d.subs {
		if s.sub.ID == id {
			return s.sub, true
		}
	}
	return Subscription{}, false
}

// String renders the document. Untouched input round-trips byte-identically.
func (d *Doc) String() string { return strings.Join(d.lines, "\n") }

// Add appends a new subscription under its group heading, creating the heading
// if it does not exist yet.
func (d *Doc) Add(sub Subscription) {
	if sub.ID == "" {
		sub.ID = d.uniqueID(record.Slug(firstNonEmpty(sub.Title, sub.Handle, sub.URL), 40))
	}
	if sub.Added == "" {
		sub.Added = time.Now().UTC().Format("2006-01-02")
	}
	group := sub.List
	if group == "" {
		group = ungrouped
		sub.List = ""
	}
	at := d.groupEnd(group)
	line := renderSubLine("", sub)
	d.lines = append(d.lines, "")
	copy(d.lines[at+1:], d.lines[at:])
	d.lines[at] = line
	d.reindex(at, 1)
	d.subs = append(d.subs, docSub{line: at, sub: sub})
	d.sortSubs()
}

// Update rewrites one subscription's line in place. Fields the app does not
// know about keep their position and spelling; that is the fixpoint promise.
// A changed list moves the line under a different heading.
func (d *Doc) Update(sub Subscription) bool {
	for i, s := range d.subs {
		if s.sub.ID != sub.ID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(s.sub.List), strings.TrimSpace(sub.List)) {
			// Regrouping is a move, not an edit: drop the line and re-add it
			// under the new heading rather than leaving it stranded.
			d.Remove(sub.ID)
			d.Add(sub)
			return true
		}
		d.lines[s.line] = renderSubLine(d.lines[s.line], sub)
		d.subs[i].sub = sub
		return true
	}
	return false
}

// Remove deletes a subscription's line. The curated notes it produced are
// untouched — unsubscribing is not un-reading.
func (d *Doc) Remove(id string) bool {
	for i, s := range d.subs {
		if s.sub.ID != id {
			continue
		}
		d.lines = append(d.lines[:s.line], d.lines[s.line+1:]...)
		d.subs = append(d.subs[:i], d.subs[i+1:]...)
		d.reindex(s.line, -1)
		return true
	}
	return false
}

// reindex shifts cached line numbers after an insert or delete at `at`.
func (d *Doc) reindex(at, delta int) {
	for i := range d.subs {
		if d.subs[i].line >= at {
			d.subs[i].line += delta
		}
	}
}

func (d *Doc) sortSubs() {
	sort.SliceStable(d.subs, func(i, j int) bool { return d.subs[i].line < d.subs[j].line })
}

// uniqueID makes a slug collision-proof by suffixing, the same habit the
// competing-bid slugs use.
func (d *Doc) uniqueID(base string) string {
	if base == "" {
		base = "feed"
	}
	taken := map[string]bool{}
	for _, s := range d.subs {
		taken[s.sub.ID] = true
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		cand := base + "-" + strconv.Itoa(n)
		if !taken[cand] {
			return cand
		}
	}
}

// groupEnd returns the line index at which a new entry for `group` should be
// inserted, appending the heading if the group is new.
func (d *Doc) groupEnd(group string) int {
	start, found := -1, false
	for i, ln := range d.lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "## ") && strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(t, "## ")), group) {
			start, found = i, true
			break
		}
	}
	if !found {
		// New group: a blank line, then the heading, at the end of the file.
		// The entry goes directly under the heading (index len-1, before the
		// trailing blank) — a gap between a heading and its first line reads
		// as an empty section in Obsidian.
		for len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) == "" {
			d.lines = d.lines[:len(d.lines)-1]
		}
		d.lines = append(d.lines, "", "## "+group, "")
		return len(d.lines) - 1
	}
	// Last bullet under this heading, or immediately after it.
	end := start + 1
	for i := start + 1; i < len(d.lines); i++ {
		t := strings.TrimSpace(d.lines[i])
		if strings.HasPrefix(t, "## ") {
			break
		}
		if strings.HasPrefix(t, "- ") {
			end = i + 1
		}
	}
	return end
}

// renderSubLine produces the line for a subscription. When `prior` is a real
// existing line, recognized fields are rewritten IN PLACE and everything else
// on the line is left exactly as the owner typed it.
func renderSubLine(prior string, sub Subscription) string {
	known := [][2]string{
		{"id", sub.ID},
		{"kind", sub.Kind},
		{"url", sub.URL},
		{"handle", sub.Handle},
		{"mirror", sub.Mirror},
		{"min-chars", minCharsValue(sub)},
		{"fulltext", fulltextValue(sub)},
		{"added", sub.Added},
	}
	if strings.TrimSpace(prior) == "" {
		var b strings.Builder
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(sub.Title))
		for _, kv := range known {
			if kv[1] != "" {
				b.WriteString(" " + record.EmitField(kv[0], kv[1]))
			}
		}
		for _, f := range sub.Unknown {
			b.WriteString(" " + record.EmitField(f.Key, f.Value))
		}
		return b.String()
	}

	indent := prior[:len(prior)-len(strings.TrimLeft(prior, " \t"))]
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prior), "- "))
	_, fields := record.ParseFields(body)
	present := map[string]bool{}
	for _, f := range fields {
		present[strings.ToLower(f.Key)] = true
	}

	out := body
	for _, kv := range known {
		out = setField(out, kv[0], kv[1], present[kv[0]])
	}
	// Retitle: the text before the first field.
	if i := record.FieldRe.FindStringIndex(out); i != nil {
		out = strings.TrimSpace(sub.Title) + " " + strings.TrimSpace(out[i[0]:])
	} else {
		out = strings.TrimSpace(sub.Title) + " " + strings.TrimSpace(out)
	}
	return indent + "- " + strings.TrimSpace(out)
}

// fulltextValue writes the field only when it is not the default, so an
// ordinary line stays uncluttered.
func fulltextValue(sub Subscription) string {
	if v := sub.FullText(); v != FullTextAuto {
		return v
	}
	return ""
}

func minCharsValue(sub Subscription) string {
	if sub.Kind != KindX || sub.MinChars <= 0 {
		return ""
	}
	return strconv.Itoa(sub.MinChars)
}

// setField rewrites one [key:: value] in place, appends it when missing, or
// removes it when the value is now empty.
func setField(line, key, value string, present bool) string {
	if present {
		done := false
		out := record.FieldRe.ReplaceAllStringFunc(line, func(m string) string {
			sm := record.FieldRe.FindStringSubmatch(m)
			if done || !strings.EqualFold(strings.TrimSpace(sm[1]), key) {
				return m
			}
			done = true
			if value == "" {
				return ""
			}
			// Keep the key exactly as the owner spelled it.
			return record.EmitField(strings.TrimSpace(sm[1]), value)
		})
		return strings.Join(strings.Fields(out), " ")
	}
	if value == "" {
		return line
	}
	return strings.TrimSpace(line) + " " + record.EmitField(key, value)
}
