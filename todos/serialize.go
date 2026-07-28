package todos

import (
	"strings"

	"manifest/record"
)

// Serialize renders the Doc back to markdown, canonical order per domain:
// loose todos → buckets → ### issues → ### backlog → extra. The output is a
// fixpoint: re-parsing and re-serializing yields identical bytes (hand edits
// normalize once on the first programmatic save, then stay byte-stable).
func Serialize(d *Doc) string {
	var b strings.Builder
	for _, ln := range trimTrailingBlank(d.preamble) {
		b.WriteString(ln + "\n")
	}
	for i, dom := range d.Domains {
		if i > 0 || len(d.preamble) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## " + dom.Name + "\n")
		for _, t := range dom.Todos {
			b.WriteString(emitTodo(t) + "\n")
		}
		for _, bk := range dom.Buckets {
			b.WriteString("\n" + bucketHeading(bk) + "\n")
			for _, t := range bk.Todos {
				b.WriteString(emitTodo(t) + "\n")
			}
		}
		if len(dom.Issues) > 0 {
			b.WriteString("\n### issues\n")
			for _, is := range dom.Issues {
				b.WriteString(emitIssue(is) + "\n")
			}
		}
		if len(dom.Backlog) > 0 {
			b.WriteString("\n### backlog\n")
			for _, ln := range dom.Backlog {
				b.WriteString(ln + "\n")
			}
		}
		for _, ln := range dom.extra {
			b.WriteString(ln + "\n")
		}
	}
	return b.String()
}

func bucketHeading(bk *Bucket) string {
	line := "### " + bk.Name
	if len(bk.Links) > 0 {
		line += " ·"
		for _, l := range bk.Links {
			line += " [[" + l + "]]"
		}
	}
	if bk.pinned {
		line += " [bucket:: " + bk.Slug + "]"
	}
	return line
}

func emitTodo(t *Todo) string {
	mark := " "
	if t.Checked {
		mark = "x"
	}
	line := "- [" + mark + "] " + t.Text
	// canonical field order: todo (only when pinned) · rock · issue · stage ·
	// waiting · since · added · done · unknown passthrough
	if id := t.explicitID(); id != "" {
		line += " [todo:: " + id + "]"
	}
	if t.Rock != "" {
		line += " [rock:: " + stripBracket(t.Rock, false) + "]"
	}
	if t.Issue != "" {
		line += " [issue:: " + stripBracket(t.Issue, false) + "]"
	}
	if t.Stage != "" {
		line += " [stage:: " + stripBracket(t.Stage, false) + "]"
	}
	if t.Waiting != "" {
		line += " [waiting:: " + stripBracket(t.Waiting, true) + "]"
	}
	if t.Since != "" {
		line += " [since:: " + stripBracket(t.Since, false) + "]"
	}
	if t.Added != "" {
		line += " [added:: " + stripBracket(t.Added, false) + "]"
	}
	if t.Done != "" {
		line += " [done:: " + stripBracket(t.Done, false) + "]"
	}
	for _, f := range t.Fields {
		if strings.EqualFold(f.Key, "todo") {
			continue // identity already emitted
		}
		line += " [" + f.Key + ":: " + stripBracket(f.Value, false) + "]"
	}
	return line
}

func emitIssue(is *Issue) string {
	mark := " "
	if is.Checked {
		mark = "x"
	}
	line := "- [" + mark + "] " + is.Text
	if is.ID != "" {
		line += " [issue:: " + is.ID + "]"
	}
	if is.Resolution != "" {
		line += " [resolution:: " + stripBracket(is.Resolution, false) + "]"
	}
	if is.Added != "" {
		line += " [added:: " + is.Added + "]"
	}
	if is.Done != "" {
		line += " [done:: " + is.Done + "]"
	}
	for _, f := range is.Fields {
		line += " [" + f.Key + ":: " + stripBracket(f.Value, false) + "]"
	}
	return line
}

// stripBracket is the kernel emit guard (record.StripBracket) — waiting
// values may be a full [[wikilink]]; anything else loses lone brackets.
func stripBracket(v string, allowWikilink bool) string {
	return record.StripBracket(v, allowWikilink)
}

func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}
