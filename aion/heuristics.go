package aion

import (
	"strings"

	"manifest/record"
)

// ParseHeuristics reads heuristics.md: top-level plain bullets are
// heuristic statements; indented bullets whose text is a [[wikilink]] are
// reinforcements (`- [[note]] [date:: …]`); a `## retired` heading opens the
// retired zone (same grammar, provenance survives pruning). Unclaimed lines
// are preserved verbatim — indented ones under their heuristic, top-level
// ones in place.
func ParseHeuristics(content string) *HeuristicsDoc {
	d := &HeuristicsDoc{}
	var body string
	d.DocFM, body = parseDocFM(content)
	zone := &d.Live
	started := false
	var cur *Heuristic
	for _, line := range bodyLines(body) {
		if strings.HasPrefix(line, "## ") && strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(line, "## ")), "retired") {
			d.RetiredHeading = line
			zone = &d.Retired
			started = true
			cur = nil
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		indent := record.IndentWidth(line[:len(line)-len(trimmed)])
		if strings.HasPrefix(trimmed, "- ") && indent == 0 {
			cur = parseHeuristicLine(trimmed[2:])
			*zone = append(*zone, HeuristicLine{Heuristic: cur})
			started = true
			continue
		}
		if cur != nil && indent > 0 && strings.TrimSpace(line) != "" {
			if r, ok := parseReinforcementLine(trimmed); ok {
				if cur.ChildIndent == "" {
					// remember the verbatim indent (the corpus uses tabs)
					cur.ChildIndent = line[:len(line)-len(trimmed)]
				}
				cur.Sources = append(cur.Sources, r)
			} else {
				cur.Raw = append(cur.Raw, line)
			}
			continue
		}
		if !started {
			d.Preamble = append(d.Preamble, line)
		} else {
			*zone = append(*zone, HeuristicLine{Raw: line})
			if strings.TrimSpace(line) != "" {
				cur = nil
			}
		}
	}
	return d
}

func parseHeuristicLine(content string) *Heuristic {
	text, fields := record.ParseFields(content)
	h := &Heuristic{Text: text}
	for _, f := range fields {
		if strings.EqualFold(f.Key, "first") {
			h.First = f.Value
		} else {
			h.Unknown = append(h.Unknown, f)
		}
	}
	h.ID = HeuristicID(h.Text)
	return h
}

func parseReinforcementLine(trimmed string) (Reinforcement, bool) {
	if !strings.HasPrefix(trimmed, "- ") {
		return Reinforcement{}, false
	}
	text, fields := record.ParseFields(trimmed[2:])
	if !strings.HasPrefix(text, "[[") || !strings.HasSuffix(text, "]]") {
		return Reinforcement{}, false
	}
	r := Reinforcement{Note: strings.TrimSuffix(strings.TrimPrefix(text, "[["), "]]")}
	for _, f := range fields {
		if strings.EqualFold(f.Key, "date") {
			r.Date = f.Value
		}
	}
	return r, true
}

// SerializeHeuristics is the fixpoint emitter for heuristics.md.
func SerializeHeuristics(d *HeuristicsDoc) string {
	var lines []string
	lines = append(lines, d.Preamble...)
	emitZone := func(zone []HeuristicLine) {
		for _, ln := range zone {
			if ln.Heuristic == nil {
				lines = append(lines, ln.Raw)
				continue
			}
			h := ln.Heuristic
			var fields []string
			if h.First != "" {
				fields = append(fields, record.EmitField("first", h.First))
			}
			for _, f := range h.Unknown {
				fields = append(fields, record.EmitField(f.Key, f.Value))
			}
			lines = append(lines, composeLine("- ", h.Text, fields))
			indent := h.ChildIndent
			if indent == "" {
				indent = record.IndentUnit
			}
			for _, r := range h.Sources {
				l := indent + "- [[" + r.Note + "]]"
				if r.Date != "" {
					l += " " + record.EmitField("date", r.Date)
				}
				lines = append(lines, l)
			}
			lines = append(lines, h.Raw...)
		}
	}
	emitZone(d.Live)
	if d.RetiredHeading != "" {
		lines = append(lines, d.RetiredHeading)
		emitZone(d.Retired)
	}
	return d.emit(joinLines(lines))
}

// HasReinforcement reports whether the heuristic already carries this
// note+date pair (reinforce is idempotent per pair).
func (h *Heuristic) HasReinforcement(note, date string) bool {
	for _, r := range h.Sources {
		if r.Note == note && r.Date == date {
			return true
		}
	}
	return false
}

// AppendLive adds a new heuristic at the end of the live zone, before any
// trailing blank lines (so the blank line before `## retired` stays put).
func (d *HeuristicsDoc) AppendLive(h *Heuristic) {
	h.ID = HeuristicID(h.Text)
	at := len(d.Live)
	for at > 0 && d.Live[at-1].Heuristic == nil && strings.TrimSpace(d.Live[at-1].Raw) == "" {
		at--
	}
	d.Live = append(d.Live[:at], append([]HeuristicLine{{Heuristic: h}}, d.Live[at:]...)...)
}

// Retire moves a live heuristic (by id) to the retired zone — provenance
// survives pruning; the entry is never deleted. Creates the `## retired`
// heading on first use.
func (d *HeuristicsDoc) Retire(id string) bool {
	for i, ln := range d.Live {
		if ln.Heuristic == nil || ln.Heuristic.ID != id {
			continue
		}
		d.Live = append(d.Live[:i], d.Live[i+1:]...)
		if d.RetiredHeading == "" {
			d.RetiredHeading = "## retired"
			// keep one blank line between the live zone and the heading
			if n := len(d.Live); n > 0 && strings.TrimSpace(lastRaw(d.Live)) != "" {
				d.Live = append(d.Live, HeuristicLine{Raw: ""})
			}
		}
		d.Retired = append(d.Retired, ln)
		return true
	}
	return false
}

func lastRaw(zone []HeuristicLine) string {
	if len(zone) == 0 {
		return ""
	}
	ln := zone[len(zone)-1]
	if ln.Heuristic != nil {
		return "x" // an entry line counts as content
	}
	return ln.Raw
}

// Merge combines two live entries: `from`'s reinforcements union into
// `into` (skipping duplicate note+date pairs), the earlier [first::] wins,
// and `from` is removed. Never loses a reinforcement (property-tested).
func (d *HeuristicsDoc) Merge(intoID, fromID string) bool {
	into := d.FindLive(intoID)
	from := d.FindLive(fromID)
	if into == nil || from == nil || intoID == fromID {
		return false
	}
	for _, r := range from.Sources {
		if !into.HasReinforcement(r.Note, r.Date) {
			into.Sources = append(into.Sources, r)
		}
	}
	if into.First == "" || (from.First != "" && from.First < into.First) {
		into.First = from.First
	}
	into.Raw = append(into.Raw, from.Raw...)
	for i, ln := range d.Live {
		if ln.Heuristic == from {
			d.Live = append(d.Live[:i], d.Live[i+1:]...)
			break
		}
	}
	return true
}

// Reorder rearranges the live entries to the given id order. It refuses
// (returns false) unless ids is exactly a permutation of the current live
// ids. Verbatim lines keep their slots; entries fill in the new order.
func (d *HeuristicsDoc) Reorder(ids []string) bool {
	cur := d.LiveEntries()
	if len(ids) != len(cur) {
		return false
	}
	byID := map[string]*Heuristic{}
	for _, h := range cur {
		byID[h.ID] = h
	}
	ordered := make([]*Heuristic, 0, len(ids))
	for _, id := range ids {
		h, ok := byID[id]
		if !ok {
			return false
		}
		delete(byID, id)
		ordered = append(ordered, h)
	}
	n := 0
	for i, ln := range d.Live {
		if ln.Heuristic != nil {
			d.Live[i].Heuristic = ordered[n]
			n++
		}
	}
	return true
}
