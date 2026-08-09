package aion

import (
	"strings"

	"manifest/record"
)

// ParseReferences reads references.md: bullet rows carrying a [url::] field
// become references (optional leading text is the title); every other line
// is preserved verbatim in place.
func ParseReferences(content string) *ReferencesDoc {
	d := &ReferencesDoc{}
	var body string
	d.DocFM, body = parseDocFM(content)
	for _, line := range bodyLines(body) {
		if r, ok := parseReferenceLine(line); ok {
			d.Lines = append(d.Lines, ReferenceLine{Ref: r})
		} else {
			d.Lines = append(d.Lines, ReferenceLine{Raw: line})
		}
	}
	return d
}

func parseReferenceLine(line string) (*Reference, bool) {
	content, ok := bulletContent(line)
	if !ok {
		return nil, false
	}
	text, fields := record.ParseFields(content)
	r := &Reference{Text: text}
	for _, f := range fields {
		switch strings.ToLower(f.Key) {
		case "url":
			r.URL = f.Value
		case "source":
			r.Source = f.Value
		case "date":
			r.Date = f.Value
		default:
			r.Unknown = append(r.Unknown, f)
		}
	}
	// a reference row carries at least one recognized field (the corpus has
	// internal refs with [source:: internal] and no url)
	if r.URL == "" && r.Source == "" && r.Date == "" {
		return nil, false
	}
	return r, true
}

// ReplaceReferences swaps the rows for a new list, preserving verbatim
// lines in place (the ReplacePeople pattern).
func ReplaceReferences(d *ReferencesDoc, refs []*Reference) {
	var out []ReferenceLine
	next := 0
	lastRef := -1
	for _, ln := range d.Lines {
		if ln.Ref == nil {
			out = append(out, ln)
			continue
		}
		if next < len(refs) {
			out = append(out, ReferenceLine{Ref: refs[next]})
			next++
			lastRef = len(out) - 1
		}
	}
	rest := make([]ReferenceLine, 0)
	for ; next < len(refs); next++ {
		rest = append(rest, ReferenceLine{Ref: refs[next]})
	}
	if len(rest) > 0 {
		at := len(out)
		if lastRef >= 0 {
			at = lastRef + 1
		}
		out = append(out[:at], append(rest, out[at:]...)...)
	}
	d.Lines = out
}

// SerializeReferences is the fixpoint emitter for references.md.
func SerializeReferences(d *ReferencesDoc) string {
	var lines []string
	for _, ln := range d.Lines {
		if ln.Ref == nil {
			lines = append(lines, ln.Raw)
			continue
		}
		r := ln.Ref
		var fields []string
		add := func(key, val string) {
			if val != "" {
				fields = append(fields, record.EmitField(key, record.StripBracket(val, false)))
			}
		}
		add("url", r.URL)
		add("source", r.Source)
		add("date", r.Date)
		for _, f := range r.Unknown {
			fields = append(fields, record.EmitField(f.Key, f.Value))
		}
		lines = append(lines, composeLine("- ", r.Text, fields))
	}
	return d.emit(joinLines(lines))
}
