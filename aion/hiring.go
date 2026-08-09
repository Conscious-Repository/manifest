package aion

import (
	"strings"

	"manifest/record"
)

// ParseHiring reads hiring.md: fields-only bullet rows carrying at least a
// [role::] or [candidate::] field become pipeline items; every other line is
// preserved verbatim in place.
func ParseHiring(content string) *HiringDoc {
	d := &HiringDoc{}
	var body string
	d.DocFM, body = parseDocFM(content)
	for _, line := range bodyLines(body) {
		if it, ok := parseHiringLine(line); ok {
			d.Lines = append(d.Lines, HiringLine{Item: it})
		} else {
			d.Lines = append(d.Lines, HiringLine{Raw: line})
		}
	}
	return d
}

func parseHiringLine(line string) (*HiringItem, bool) {
	content, ok := bulletContent(line)
	if !ok {
		return nil, false
	}
	text, fields := record.ParseFields(content)
	if text != "" {
		return nil, false
	}
	it := &HiringItem{}
	for _, f := range fields {
		switch strings.ToLower(f.Key) {
		case "role":
			it.Role = f.Value
		case "candidate":
			it.Candidate = f.Value
		case "stage":
			it.Stage = f.Value
		case "next":
			it.Next = f.Value
		case "priority":
			it.Priority = f.Value
		default:
			it.Unknown = append(it.Unknown, f)
		}
	}
	if it.Role == "" && it.Candidate == "" {
		return nil, false
	}
	return it, true
}

// ReplaceHiring swaps the pipeline rows for a new list, preserving verbatim
// lines in place (the ReplacePeople pattern).
func ReplaceHiring(d *HiringDoc, items []*HiringItem) {
	var out []HiringLine
	next := 0
	lastItem := -1
	for _, ln := range d.Lines {
		if ln.Item == nil {
			out = append(out, ln)
			continue
		}
		if next < len(items) {
			out = append(out, HiringLine{Item: items[next]})
			next++
			lastItem = len(out) - 1
		}
	}
	rest := make([]HiringLine, 0)
	for ; next < len(items); next++ {
		rest = append(rest, HiringLine{Item: items[next]})
	}
	if len(rest) > 0 {
		at := len(out)
		if lastItem >= 0 {
			at = lastItem + 1
		}
		out = append(out[:at], append(rest, out[at:]...)...)
	}
	d.Lines = out
}

// SerializeHiring is the fixpoint emitter for hiring.md.
func SerializeHiring(d *HiringDoc) string {
	var lines []string
	for _, ln := range d.Lines {
		if ln.Item == nil {
			lines = append(lines, ln.Raw)
			continue
		}
		it := ln.Item
		var fields []string
		add := func(key, val string) {
			if val != "" {
				fields = append(fields, record.EmitField(key, record.StripBracket(val, false)))
			}
		}
		add("role", it.Role)
		add("candidate", it.Candidate)
		add("stage", it.Stage)
		add("next", it.Next)
		add("priority", it.Priority)
		for _, f := range it.Unknown {
			fields = append(fields, record.EmitField(f.Key, f.Value))
		}
		lines = append(lines, composeLine("- ", "", fields))
	}
	return d.emit(joinLines(lines))
}
