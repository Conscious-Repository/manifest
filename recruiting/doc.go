package recruiting

import (
	"strings"

	"manifest/record"
)

// TODO(kernel): parseDocFM / emit / bodyLines / joinLines / bulletContent /
// composeLine are the aion/doc.go helpers, copied rather than promoted
// (plan §4.5). Promoting them to `record` in this pass would touch all eight
// aion parsers, whose fixpoint corpus is the live vault — doubling the blast
// radius of a new-domain pass. Schedule the extraction once recruiting is the
// third caller.

// parseDocFM splits a record into its frontmatter carrier and body,
// recovering the fence/body blank-line fact the kernel drops (length
// accounting — exact for LF files).
func parseDocFM(content string) (DocFM, string) {
	fm, body, ok := record.SplitFrontmatter(content)
	if !ok {
		return DocFM{}, content
	}
	base := len("---\n") + len("---\n") + len(body)
	for _, ln := range fm {
		base += len(ln) + 1
	}
	return DocFM{FM: fm, FMBlank: len(content) > base}, body
}

// emit renders the frontmatter block back around a body.
func (d DocFM) emit(body string) string {
	if d.FM == nil {
		return body
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, ln := range d.FM {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	if d.FMBlank {
		b.WriteByte('\n')
	}
	b.WriteString(body)
	return b.String()
}

// Get returns the frontmatter scalar for key ("" when absent).
func (d *DocFM) Get(key string) string {
	for _, ln := range d.FM {
		if k, v, ok := strings.Cut(ln, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Set upserts a frontmatter scalar, preserving the order and formatting of
// untouched keys. A missing key appends at the end of the block.
func (d *DocFM) Set(key, value string) {
	line := key + ":"
	if value != "" {
		line += " " + value
	}
	for i, ln := range d.FM {
		if k, _, ok := strings.Cut(ln, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			d.FM[i] = line
			return
		}
	}
	d.FM = append(d.FM, line)
}

// bodyLines splits a body into lines, canonicalizing to exactly one trailing
// newline on emit (joinLines).
func bodyLines(body string) []string {
	if body == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// bulletContent returns the content of a top-level plain bullet ("- …") that
// is NOT a checkbox line; ok=false otherwise.
func bulletContent(line string) (string, bool) {
	if !strings.HasPrefix(line, "- ") || record.CheckboxRe.MatchString(line) {
		return "", false
	}
	return line[2:], true
}

// composeLine renders "- " + text + fields.
func composeLine(prefix, text string, fields []string) string {
	parts := make([]string, 0, 1+len(fields))
	if text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, fields...)
	return prefix + strings.Join(parts, " ")
}

// ---- Row: the one row shape every recruiting record is built from ----

// Row is one fields-only bullet — `- [id:: …] [class:: …]` — held as its
// ORDERED field stream rather than a fixed struct. That is the field-order
// fixpoint (plan §4.5 point 5): a recognized value is re-read from the row at
// its own original position, so a hand-edit in Obsidian that reorders fields,
// or adds an unrecognized one, round-trips byte-identically. Sub holds the
// verbatim continuation lines beneath the bullet (an evidence blockquote).
type Row struct {
	Fields []record.Field `json:"fields"`
	Sub    []string       `json:"-"`
}

// parseRow reads one line as a fields-only bullet; ok=false when the line is
// anything else (then the caller preserves it verbatim).
func parseRow(line string) (*Row, bool) {
	content, ok := bulletContent(line)
	if !ok {
		return nil, false
	}
	text, fields := record.ParseFields(content)
	if text != "" || len(fields) == 0 {
		return nil, false // a recruiting row is fields-only
	}
	return &Row{Fields: fields}, true
}

// Get returns the first value for key ("" when absent).
func (r *Row) Get(key string) string {
	for _, f := range r.Fields {
		if strings.EqualFold(f.Key, key) {
			return f.Value
		}
	}
	return ""
}

// GetAll returns every value for a repeated key (evidence ids on a fit row).
func (r *Row) GetAll(key string) []string {
	var out []string
	for _, f := range r.Fields {
		if strings.EqualFold(f.Key, key) && f.Value != "" {
			out = append(out, f.Value)
		}
	}
	return out
}

func (r *Row) Has(key string) bool {
	for _, f := range r.Fields {
		if strings.EqualFold(f.Key, key) {
			return true
		}
	}
	return false
}

// Set rewrites key IN PLACE when the row already carries it (the position the
// owner wrote it in is preserved), and appends otherwise.
func (r *Row) Set(key, value string) {
	for i, f := range r.Fields {
		if strings.EqualFold(f.Key, key) {
			r.Fields[i].Value = value
			return
		}
	}
	r.Fields = append(r.Fields, record.Field{Key: key, Value: value})
}

// SetAll replaces every occurrence of a repeated key with the given values:
// the first slot is reused in place, surplus slots drop, extras append.
func (r *Row) SetAll(key string, values []string) {
	var kept []record.Field
	next := 0
	for _, f := range r.Fields {
		if !strings.EqualFold(f.Key, key) {
			kept = append(kept, f)
			continue
		}
		if next < len(values) {
			kept = append(kept, record.Field{Key: f.Key, Value: values[next]})
			next++
		}
	}
	for ; next < len(values); next++ {
		kept = append(kept, record.Field{Key: key, Value: values[next]})
	}
	r.Fields = kept
}

// Drop removes every occurrence of key.
func (r *Row) Drop(key string) {
	var kept []record.Field
	for _, f := range r.Fields {
		if !strings.EqualFold(f.Key, key) {
			kept = append(kept, f)
		}
	}
	r.Fields = kept
}

// emitLines renders the bullet plus its verbatim continuation lines.
func (r *Row) emitLines() []string {
	fields := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		fields = append(fields, record.EmitField(f.Key, record.StripBracket(f.Value, false)))
	}
	return append([]string{composeLine("- ", "", fields)}, r.Sub...)
}

// newRow builds a row from ordered key/value pairs; empty values are still
// emitted (an `[email:: ]` slot is how a blank contact field is written).
func newRow(pairs ...string) *Row {
	r := &Row{}
	for i := 0; i+1 < len(pairs); i += 2 {
		r.Fields = append(r.Fields, record.Field{Key: pairs[i], Value: pairs[i+1]})
	}
	return r
}

// ---- Line / Section: the shared body model ----

// Line is one body line: a recognized Row, or a verbatim passthrough.
type Line struct {
	Row *Row
	Raw string
}

// Section is one `## heading` block; Heading == "" is the implicit preamble.
type Section struct {
	Heading string
	Lines   []Line
}

// isContinuation reports whether a non-row line belongs UNDER the open row —
// an indented child or a blockquote (the evidence snippet shape).
func isContinuation(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	return record.IndentWidth(line) > 0 || strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

// parseRows reads a flat, frontmatter-optional row document (seeds.md,
// network/people.md, network/edges.md). recognized decides which fields-only
// bullets are rows of THIS document; everything else stays verbatim in place.
func parseRows(content string, recognized func(*Row) bool) (DocFM, []Line) {
	fm, body := parseDocFM(content)
	var lines []Line
	var open *Row
	for _, line := range bodyLines(body) {
		if r, ok := parseRow(line); ok && recognized(r) {
			lines = append(lines, Line{Row: r})
			open = r
			continue
		}
		if open != nil && isContinuation(line) {
			open.Sub = append(open.Sub, line)
			continue
		}
		open = nil
		lines = append(lines, Line{Raw: line})
	}
	return fm, lines
}

func serializeRows(fm DocFM, lines []Line) string {
	var out []string
	for _, ln := range lines {
		if ln.Row != nil {
			out = append(out, ln.Row.emitLines()...)
		} else {
			out = append(out, ln.Raw)
		}
	}
	return fm.emit(joinLines(out))
}

// parseSections reads a sectioned record (roles/*.md, candidates/*.md).
func parseSections(content string, recognized func(string, *Row) bool) (DocFM, []*Section) {
	fm, body := parseDocFM(content)
	cur := &Section{}
	secs := []*Section{cur}
	var open *Row
	for _, line := range bodyLines(body) {
		if strings.HasPrefix(line, "## ") {
			cur = &Section{Heading: strings.TrimPrefix(line, "## ")}
			secs = append(secs, cur)
			open = nil
			continue
		}
		if r, ok := parseRow(line); ok && recognized(cur.Heading, r) {
			cur.Lines = append(cur.Lines, Line{Row: r})
			open = r
			continue
		}
		if open != nil && isContinuation(line) {
			open.Sub = append(open.Sub, line)
			continue
		}
		open = nil
		cur.Lines = append(cur.Lines, Line{Raw: line})
	}
	if len(secs) > 1 && len(secs[0].Lines) == 0 && secs[0].Heading == "" {
		secs = secs[1:] // an unused implicit section keeps headed files canonical
	}
	return fm, secs
}

func serializeSections(fm DocFM, secs []*Section) string {
	var out []string
	for _, s := range secs {
		if s.Heading != "" {
			out = append(out, "## "+s.Heading)
		}
		for _, ln := range s.Lines {
			if ln.Row != nil {
				out = append(out, ln.Row.emitLines()...)
			} else {
				out = append(out, ln.Raw)
			}
		}
	}
	return fm.emit(joinLines(out))
}

// section returns the named section, or nil.
func section(secs []*Section, heading string) *Section {
	for _, s := range secs {
		if strings.EqualFold(s.Heading, heading) {
			return s
		}
	}
	return nil
}

// ensureSection returns the named section, appending it when absent. A new
// section is separated from what precedes it by one blank line, matching the
// hand-authored shape.
func ensureSection(secs *[]*Section, heading string) *Section {
	if s := section(*secs, heading); s != nil {
		return s
	}
	if n := len(*secs); n > 0 {
		last := (*secs)[n-1]
		if len(last.Lines) == 0 || strings.TrimSpace(last.Lines[len(last.Lines)-1].Raw) != "" ||
			last.Lines[len(last.Lines)-1].Row != nil {
			last.Lines = append(last.Lines, Line{Raw: ""})
		}
	}
	s := &Section{Heading: heading}
	*secs = append(*secs, s)
	return s
}

// appendRow adds a row to a section, before any trailing blank lines so the
// section separator stays where the owner put it.
func appendRow(s *Section, r *Row) {
	at := len(s.Lines)
	for at > 0 && s.Lines[at-1].Row == nil && strings.TrimSpace(s.Lines[at-1].Raw) == "" {
		at--
	}
	s.Lines = append(s.Lines[:at], append([]Line{{Row: r}}, s.Lines[at:]...)...)
}

// rows collects a section's recognized rows in order.
func rows(s *Section) []*Row {
	if s == nil {
		return nil
	}
	var out []*Row
	for _, ln := range s.Lines {
		if ln.Row != nil {
			out = append(out, ln.Row)
		}
	}
	return out
}
