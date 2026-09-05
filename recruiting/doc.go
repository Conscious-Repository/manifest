package recruiting

import (
	"strings"

	"manifest/record"
)

// The flat row-document machinery (DocFM / Row / Line / ParseRows /
// SerializeRows) was promoted to the kernel as record/rows.go once `graph`
// became its third caller (the promotion this file's TODO scheduled). This
// package keeps the aliases so every record type and call site reads as
// before, and holds only the SECTIONED shape (roles/*.md, candidates/*.md)
// the kernel does not need yet.

// Row is one fields-only bullet held as its ordered field stream
// (record.Row) — the field-order fixpoint.
type Row = record.Row

// Line is one body line: a recognized Row, or a verbatim passthrough.
type Line = record.Line

func parseDocFM(content string) (DocFM, string) { return record.ParseDocFM(content) }

func bodyLines(body string) []string  { return record.BodyLines(body) }
func joinLines(lines []string) string { return record.JoinLines(lines) }

// parseRow reads one line as a fields-only bullet; ok=false when the line is
// anything else (then the caller preserves it verbatim).
func parseRow(line string) (*Row, bool) { return record.ParseRow(line) }

// newRow builds a row from ordered key/value pairs.
func newRow(pairs ...string) *Row { return record.NewRow(pairs...) }

func isContinuation(line string) bool { return record.IsContinuation(line) }

// parseRows reads a flat, frontmatter-optional row document (seeds.md,
// network/people.md, network/edges.md).
func parseRows(content string, recognized func(*Row) bool) (DocFM, []Line) {
	return record.ParseRows(content, recognized)
}

func serializeRows(fm DocFM, lines []Line) string { return record.SerializeRows(fm, lines) }

// Section is one `## heading` block; Heading == "" is the implicit preamble.
type Section struct {
	Heading string
	Lines   []Line
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
				out = append(out, ln.Row.EmitLines()...)
			} else {
				out = append(out, ln.Raw)
			}
		}
	}
	return fm.Emit(joinLines(out))
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
	return record.Rows(s.Lines)
}
