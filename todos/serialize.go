package todos

import "strings"

// Serialize renders the Doc back to markdown. The output is a fixpoint:
// re-parsing and re-serializing yields identical bytes (hand edits normalize
// once on the first programmatic save, then stay byte-stable).
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
		for _, ln := range dom.extra {
			b.WriteString(ln + "\n")
		}
	}
	return b.String()
}

func emitTodo(t *Todo) string {
	mark := " "
	if t.Checked {
		mark = "x"
	}
	line := "- [" + mark + "] " + t.Text
	// canonical field order: todo (only when pinned) · waiting · since ·
	// added · done · unknown passthrough
	if id := t.explicitID(); id != "" {
		line += " [todo:: " + id + "]"
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

// stripBracket keeps a value from breaking the field grammar. Waiting values
// may be a full [[wikilink]] (parsed by the dedicated pattern); anything else
// loses lone brackets.
func stripBracket(v string, allowWikilink bool) string {
	v = strings.TrimSpace(v)
	if allowWikilink && strings.HasPrefix(v, "[[") && strings.HasSuffix(v, "]]") {
		return v
	}
	return strings.NewReplacer("[", "", "]", "").Replace(v)
}

func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}
