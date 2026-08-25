package aion

import (
	"strings"

	"manifest/record"
)

// ParseBacklog reads backlog.md. The owner's corpus is a FLAT list (items
// directly under the H1, newest first); `## …` headings, when present, open
// named sections — both shapes parse and round-trip byte-identically. Lines
// before the first heading live in an implicit unnamed section. Checkbox
// lines AND plain bullets carrying a [kind::] field are items (the corpus
// writes decisions both ways; each line keeps its style); indented
// bullet/checkbox lines nest as children; any other indented line is
// preserved verbatim under its item; unclaimed lines are preserved in place.
func ParseBacklog(content string) *BacklogDoc {
	d := &BacklogDoc{}
	var body string
	d.DocFM, body = parseDocFM(content)
	cur := &BacklogSection{} // the implicit unnamed section
	d.Sections = append(d.Sections, cur)
	var frames record.Frames[*BacklogItem]
	for _, line := range bodyLines(body) {
		if strings.HasPrefix(line, "## ") {
			cur = &BacklogSection{Heading: strings.TrimPrefix(line, "## ")}
			d.Sections = append(d.Sections, cur)
			frames.Reset()
			continue
		}
		indent, it := parseBacklogItemLine(line)
		if it == nil {
			// unclaimed line: verbatim under the deepest open item when
			// indented, else at section level
			if top, ok := frames.Top(); ok && record.IndentWidth(line) > 0 && strings.TrimSpace(line) != "" {
				top.RawChildren = append(top.RawChildren, line)
			} else {
				cur.Lines = append(cur.Lines, BacklogLine{Raw: line})
				if strings.TrimSpace(line) != "" && record.IndentWidth(line) == 0 {
					frames.Reset()
				}
			}
			continue
		}
		depth, parent := frames.Descend(indent)
		if depth == 0 {
			cur.Lines = append(cur.Lines, BacklogLine{Item: it})
		} else {
			parent.Children = append(parent.Children, it)
		}
		frames.Push(indent, it)
	}
	// drop an unused implicit section so headed files stay canonical
	if len(d.Sections) > 1 && len(d.Sections[0].Lines) == 0 && d.Sections[0].Heading == "" {
		d.Sections = d.Sections[1:]
	}
	return d
}

// parseBacklogItemLine reads one line as an item; nil when the line is not
// an item (then the caller preserves it verbatim).
func parseBacklogItemLine(line string) (indent int, it *BacklogItem) {
	if ind, checked, rest, ok := record.ParseCheckboxLine(line); ok {
		it = &BacklogItem{Checked: checked}
		it.Text, it.Sources, it.Unknown, it.toks = parseLineFields(rest)
		applyRecognized(it, rest)
		if it.Kind == "" {
			it.Kind = KindTask
		}
		if it.ID == "" {
			it.ID = ItemID(it.Kind, it.Text)
		}
		return ind, it
	}
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "- ") {
		return 0, nil
	}
	text, sources, unknown, toks := parseLineFields(trimmed[2:])
	hasKind := false
	for _, tok := range toks {
		if !tok.Wiki && strings.EqualFold(tok.Key, "kind") {
			hasKind = true
			break
		}
	}
	if !hasKind {
		return 0, nil // a plain bullet without [kind::] is not an item
	}
	it = &BacklogItem{Text: text, Sources: sources, Unknown: unknown, toks: toks, Plain: true}
	applyRecognized(it, trimmed[2:])
	if it.ID == "" {
		it.ID = ItemID(it.Kind, it.Text)
	}
	return record.IndentWidth(line[:len(line)-len(trimmed)]), it
}

// applyRecognized re-scans a line's generic fields into the struct (the
// tokenizer records positions; this records values for recognized keys).
func applyRecognized(it *BacklogItem, content string) {
	for _, m := range record.FieldRe.FindAllStringSubmatch(content, -1) {
		key, val := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if recognizedBacklogKey(key) && !strings.EqualFold(key, "source") {
			setBacklogField(it, key, val)
		}
	}
}

// SerializeBacklog is the fixpoint emitter for backlog.md.
func SerializeBacklog(d *BacklogDoc) string {
	var lines []string
	for _, s := range d.Sections {
		if s.Heading != "" {
			lines = append(lines, "## "+s.Heading)
		}
		for _, ln := range s.Lines {
			if ln.Item != nil {
				lines = append(lines, emitBacklogItem(ln.Item, 0)...)
			} else {
				lines = append(lines, ln.Raw)
			}
		}
	}
	return d.emit(joinLines(lines))
}

func emitBacklogItem(it *BacklogItem, depth int) []string {
	prefix := strings.Repeat(record.IndentUnit, depth)
	if it.Plain {
		prefix += "- "
	} else if it.Checked {
		prefix += "- [x] "
	} else {
		prefix += "- [ ] "
	}
	out := []string{composeLine(prefix, it.Text, backlogFields(it))}
	for _, c := range it.Children {
		out = append(out, emitBacklogItem(c, depth+1)...)
	}
	out = append(out, it.RawChildren...)
	return out
}

// AppendItem adds one new item. In a section-headed file (the seed shape)
// it lands at the bottom of its kind's section; in the owner's flat file it
// lands at the TOP of the list (newest first — the corpus convention),
// after any leading non-item lines.
func (d *BacklogDoc) AppendItem(it *BacklogItem) {
	// Mint whenever the item does not carry a PERSISTED id — not just when the
	// id is empty. The approval lane arrives here with the legacy derived hash
	// already stamped (BacklogItemFromPayload), which defeated an empty-only
	// guard: the line landed with no [id::] token, the live projection papered
	// over it with a transient aion-bl/<slug> the write path didn't recognize,
	// and the item was visible everywhere but editable nowhere until the next
	// boot migration ("item not found", 2026-08-24).
	if it.ID == "" || !it.IDPersisted {
		taken := map[string]bool{}
		for _, have := range d.AllItems() {
			if have.IDPersisted {
				taken[have.ID] = true
			}
		}
		it.ID = PortalItemID(it.Text, taken)
		it.IDPersisted = true
	}
	want := "Tasks"
	if it.Kind == KindDecision {
		want = "Decisions"
	}
	for _, s := range d.Sections {
		if strings.EqualFold(strings.TrimSpace(s.Heading), want) {
			// insert before trailing blank lines so section spacing stays put
			at := len(s.Lines)
			for at > 0 && s.Lines[at-1].Item == nil && strings.TrimSpace(s.Lines[at-1].Raw) == "" {
				at--
			}
			s.Lines = append(s.Lines[:at], append([]BacklogLine{{Item: it}}, s.Lines[at:]...)...)
			return
		}
	}
	// flat file: first section, before its first item (newest first)
	if len(d.Sections) == 0 {
		d.Sections = append(d.Sections, &BacklogSection{})
	}
	s := d.Sections[0]
	at := len(s.Lines)
	for i, ln := range s.Lines {
		if ln.Item != nil {
			at = i
			break
		}
	}
	s.Lines = append(s.Lines[:at], append([]BacklogLine{{Item: it}}, s.Lines[at:]...)...)
}
