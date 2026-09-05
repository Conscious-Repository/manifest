package record

import "strings"

// ROW DOCUMENTS — the one flat record shape three domains now share
// (ARCHITECTURE §3): a frontmatter-optional file whose body is a stream of
// fields-only bullets (`- [id:: …] [kind:: …]`) interleaved with verbatim
// lines. recruiting (seeds, network/people, network/edges, outreach) built it
// first; aion holds an older copy; graph (P2) is the third caller the
// recruiting/doc.go TODO scheduled the promotion for. The contract:
//
//   - a recognized row is held as its ORDERED field stream, never a fixed
//     struct — a value re-emits at the position the owner wrote it in, and an
//     unrecognized `[foo:: bar]` survives load → mutate → save (field-order
//     fixpoint);
//   - anything that is not a recognized row is a verbatim Line.Raw, in place;
//   - indented / blockquote lines under a row are its Sub continuation
//     (an evidence snippet), emitted back verbatim;
//   - parse→emit is byte-identical for LF files (the blank between the
//     frontmatter fence and the body is recovered by length accounting).
//
// No regex lives here beyond the kernel's own FieldRe / CheckboxRe.

// DocFM is the shared frontmatter carrier: raw block lines (nil = no block)
// plus whether a blank line separated the fence from the body — preserved so
// parse→emit stays byte-identical.
type DocFM struct {
	FM      []string `json:"-"`
	FMBlank bool     `json:"-"`
}

// ParseDocFM splits a record into its frontmatter carrier and body,
// recovering the fence/body blank-line fact SplitFrontmatter drops (length
// accounting — exact for LF files).
func ParseDocFM(content string) (DocFM, string) {
	fm, body, ok := SplitFrontmatter(content)
	if !ok {
		return DocFM{}, content
	}
	base := len("---\n") + len("---\n") + len(body)
	for _, ln := range fm {
		base += len(ln) + 1
	}
	return DocFM{FM: fm, FMBlank: len(content) > base}, body
}

// Emit renders the frontmatter block back around a body.
func (d DocFM) Emit(body string) string {
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

// BodyLines splits a body into lines, canonicalizing to exactly one trailing
// newline on emit (JoinLines).
func BodyLines(body string) []string {
	if body == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}

// JoinLines is the inverse of BodyLines.
func JoinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// BulletContent returns the content of a top-level plain bullet ("- …") that
// is NOT a checkbox line; ok=false otherwise.
func BulletContent(line string) (string, bool) {
	if !strings.HasPrefix(line, "- ") || CheckboxRe.MatchString(line) {
		return "", false
	}
	return line[2:], true
}

// ComposeLine renders prefix + text + fields, space-joined.
func ComposeLine(prefix, text string, fields []string) string {
	parts := make([]string, 0, 1+len(fields))
	if text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, fields...)
	return prefix + strings.Join(parts, " ")
}

// ---- Row ----

// Row is one fields-only bullet — `- [id:: …] [class:: …]` — held as its
// ORDERED field stream rather than a fixed struct (the field-order fixpoint).
// Sub holds the verbatim continuation lines beneath the bullet.
type Row struct {
	Fields []Field  `json:"fields"`
	Sub    []string `json:"-"`
}

// ParseRow reads one line as a fields-only bullet; ok=false when the line is
// anything else (then the caller preserves it verbatim).
func ParseRow(line string) (*Row, bool) {
	content, ok := BulletContent(line)
	if !ok {
		return nil, false
	}
	text, fields := ParseFields(content)
	if text != "" || len(fields) == 0 {
		return nil, false // a row is fields-only
	}
	return &Row{Fields: fields}, true
}

// NewRow builds a row from ordered key/value pairs; empty values are still
// emitted (an `[email:: ]` slot is how a blank contact field is written).
func NewRow(pairs ...string) *Row {
	r := &Row{}
	for i := 0; i+1 < len(pairs); i += 2 {
		r.Fields = append(r.Fields, Field{Key: pairs[i], Value: pairs[i+1]})
	}
	return r
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

// GetAll returns every value for a repeated key.
func (r *Row) GetAll(key string) []string {
	var out []string
	for _, f := range r.Fields {
		if strings.EqualFold(f.Key, key) && f.Value != "" {
			out = append(out, f.Value)
		}
	}
	return out
}

// Has reports whether the row carries key at all.
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
	r.Fields = append(r.Fields, Field{Key: key, Value: value})
}

// SetAll replaces every occurrence of a repeated key with the given values:
// the first slot is reused in place, surplus slots drop, extras append.
func (r *Row) SetAll(key string, values []string) {
	var kept []Field
	next := 0
	for _, f := range r.Fields {
		if !strings.EqualFold(f.Key, key) {
			kept = append(kept, f)
			continue
		}
		if next < len(values) {
			kept = append(kept, Field{Key: f.Key, Value: values[next]})
			next++
		}
	}
	for ; next < len(values); next++ {
		kept = append(kept, Field{Key: key, Value: values[next]})
	}
	r.Fields = kept
}

// Drop removes every occurrence of key.
func (r *Row) Drop(key string) {
	var kept []Field
	for _, f := range r.Fields {
		if !strings.EqualFold(f.Key, key) {
			kept = append(kept, f)
		}
	}
	r.Fields = kept
}

// EmitLines renders the bullet plus its verbatim continuation lines.
func (r *Row) EmitLines() []string {
	fields := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		fields = append(fields, EmitField(f.Key, StripBracket(f.Value, false)))
	}
	return append([]string{ComposeLine("- ", "", fields)}, r.Sub...)
}

// UnknownFields collects the fields of a row outside a recognized vocabulary,
// so a hand-added `[foo:: bar]` survives a load → mutate → save.
func (r *Row) UnknownFields(known ...string) []Field {
	var out []Field
	for _, f := range r.Fields {
		hit := false
		for _, k := range known {
			if strings.EqualFold(k, f.Key) {
				hit = true
				break
			}
		}
		if !hit {
			out = append(out, f)
		}
	}
	return out
}

// ---- Line / row documents ----

// Line is one body line: a recognized Row, or a verbatim passthrough.
type Line struct {
	Row *Row
	Raw string
}

// IsContinuation reports whether a non-row line belongs UNDER the open row —
// an indented child or a blockquote (the evidence snippet shape).
func IsContinuation(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	return IndentWidth(line) > 0 || strings.HasPrefix(strings.TrimLeft(line, " \t"), ">")
}

// ParseRows reads a flat, frontmatter-optional row document. recognized
// decides which fields-only bullets are rows of THIS document; everything
// else stays verbatim in place.
func ParseRows(content string, recognized func(*Row) bool) (DocFM, []Line) {
	fm, body := ParseDocFM(content)
	var lines []Line
	var open *Row
	for _, line := range BodyLines(body) {
		if r, ok := ParseRow(line); ok && recognized(r) {
			lines = append(lines, Line{Row: r})
			open = r
			continue
		}
		if open != nil && IsContinuation(line) {
			open.Sub = append(open.Sub, line)
			continue
		}
		open = nil
		lines = append(lines, Line{Raw: line})
	}
	return fm, lines
}

// SerializeRows is the fixpoint emitter for a row document.
func SerializeRows(fm DocFM, lines []Line) string {
	var out []string
	for _, ln := range lines {
		if ln.Row != nil {
			out = append(out, ln.Row.EmitLines()...)
		} else {
			out = append(out, ln.Raw)
		}
	}
	return fm.Emit(JoinLines(out))
}

// Rows collects a line list's recognized rows in order.
func Rows(lines []Line) []*Row {
	var out []*Row
	for _, ln := range lines {
		if ln.Row != nil {
			out = append(out, ln.Row)
		}
	}
	return out
}
