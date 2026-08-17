package aion

import (
	"strings"

	"manifest/record"
)

// ParsePeople reads people.md: bullet rows carrying an [initials::] field
// become Person entries; every other line is preserved verbatim in place.
func ParsePeople(content string) *PeopleDoc {
	d := &PeopleDoc{}
	var body string
	d.DocFM, body = parseDocFM(content)
	for _, line := range bodyLines(body) {
		if p, ok := parsePersonLine(line); ok {
			d.Lines = append(d.Lines, PeopleLine{Person: p})
		} else {
			d.Lines = append(d.Lines, PeopleLine{Raw: line})
		}
	}
	return d
}

func parsePersonLine(line string) (*Person, bool) {
	content, ok := bulletContent(line)
	if !ok {
		return nil, false
	}
	text, fields := record.ParseFields(content)
	if text != "" {
		return nil, false // a person row is fields-only
	}
	p := &Person{}
	for _, f := range fields {
		switch strings.ToLower(f.Key) {
		case "initials":
			p.Initials = f.Value
		case "name":
			p.Name = f.Value
		case "role":
			p.Role = f.Value
		case "email":
			p.Email = f.Value
		default:
			p.Unknown = append(p.Unknown, f)
		}
	}
	if p.Initials == "" {
		return nil, false
	}
	return p, true
}

// SerializePeople is the fixpoint emitter for people.md.
func SerializePeople(d *PeopleDoc) string {
	var lines []string
	for _, ln := range d.Lines {
		if ln.Person != nil {
			lines = append(lines, emitPersonLine(ln.Person))
		} else {
			lines = append(lines, ln.Raw)
		}
	}
	return d.emit(joinLines(lines))
}

func emitPersonLine(p *Person) string {
	var fields []string
	add := func(key, val string) {
		if val != "" {
			fields = append(fields, record.EmitField(key, record.StripBracket(val, false)))
		}
	}
	add("initials", p.Initials)
	add("name", p.Name)
	add("role", p.Role)
	add("email", p.Email)
	for _, f := range p.Unknown {
		fields = append(fields, record.EmitField(f.Key, f.Value))
	}
	return composeLine("- ", "", fields)
}

// ReplacePeople swaps the registry rows for a new list, preserving verbatim
// lines in place: existing person slots are rewritten in order, extra new
// people append after the last person line (or at the end), and surplus old
// person lines are dropped.
func ReplacePeople(d *PeopleDoc, people []*Person) {
	var out []PeopleLine
	next := 0
	lastPerson := -1
	for _, ln := range d.Lines {
		if ln.Person == nil {
			out = append(out, ln)
			continue
		}
		if next < len(people) {
			out = append(out, PeopleLine{Person: people[next]})
			next++
			lastPerson = len(out) - 1
		}
		// surplus old rows dropped
	}
	rest := make([]PeopleLine, 0, len(people)-next)
	for ; next < len(people); next++ {
		rest = append(rest, PeopleLine{Person: people[next]})
	}
	if len(rest) > 0 {
		at := len(out)
		if lastPerson >= 0 {
			at = lastPerson + 1
		}
		out = append(out[:at], append(rest, out[at:]...)...)
	}
	d.Lines = out
}
