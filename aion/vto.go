package aion

import (
	"strings"

	"manifest/record"
)

// ParseVTO reads vto.md: `## …` headings open sections; bullets inside a
// section become generic entries (clean text + inline fields); everything
// else is preserved verbatim in place.
func ParseVTO(content string) *VTODoc {
	d := &VTODoc{}
	var body string
	d.DocFM, body = parseDocFM(content)
	var cur *VTOSection
	for _, line := range bodyLines(body) {
		switch {
		case strings.HasPrefix(line, "## "):
			cur = &VTOSection{Heading: strings.TrimPrefix(line, "## ")}
			d.Sections = append(d.Sections, cur)
		case cur == nil:
			d.Preamble = append(d.Preamble, line)
		default:
			if content, ok := bulletContent(line); ok {
				text, fields := record.ParseFields(content)
				cur.Lines = append(cur.Lines, VTOLine{Entry: &VTOEntry{Text: text, Fields: fields}})
			} else {
				cur.Lines = append(cur.Lines, VTOLine{Raw: line})
			}
		}
	}
	return d
}

// SerializeVTO is the fixpoint emitter for vto.md.
func SerializeVTO(d *VTODoc) string {
	var lines []string
	lines = append(lines, d.Preamble...)
	for _, s := range d.Sections {
		lines = append(lines, "## "+s.Heading)
		for _, ln := range s.Lines {
			if ln.Entry != nil {
				var fields []string
				for _, f := range ln.Entry.Fields {
					fields = append(fields, record.EmitField(f.Key, f.Value))
				}
				lines = append(lines, composeLine("- ", ln.Entry.Text, fields))
			} else {
				lines = append(lines, ln.Raw)
			}
		}
	}
	return d.emit(joinLines(lines))
}

// Section returns the section whose heading starts with the given "NN"
// number prefix (e.g. "01"), or nil.
func (d *VTODoc) Section(num string) *VTOSection {
	for _, s := range d.Sections {
		if strings.HasPrefix(s.Heading, num) {
			return s
		}
	}
	return nil
}

// EntryTexts returns the section's plain bullet texts (entries without
// fields) — the list form used for core values, uniques, measurables.
func (s *VTOSection) EntryTexts() []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, ln := range s.Lines {
		if ln.Entry != nil && len(ln.Entry.Fields) == 0 && ln.Entry.Text != "" {
			out = append(out, ln.Entry.Text)
		}
	}
	return out
}

// FieldValue returns the first value of the named inline field across the
// section's entries ("" when absent).
func (s *VTOSection) FieldValue(key string) string {
	if s == nil {
		return ""
	}
	for _, ln := range s.Lines {
		if ln.Entry == nil {
			continue
		}
		for _, f := range ln.Entry.Fields {
			if strings.EqualFold(f.Key, key) {
				return f.Value
			}
		}
	}
	return ""
}

// FieldValues returns every value of the named field across the section.
func (s *VTOSection) FieldValues(key string) []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, ln := range s.Lines {
		if ln.Entry == nil {
			continue
		}
		for _, f := range ln.Entry.Fields {
			if strings.EqualFold(f.Key, key) {
				out = append(out, f.Value)
			}
		}
	}
	return out
}
