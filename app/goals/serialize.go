package goals

import "strings"

// Serialize renders a Doc back to canonical goals.md. The output is a fixpoint:
// re-parsing and re-serializing yields identical bytes, so the app only ever
// produces minimal diffs. The cascade nests under "### 90-day" with four spaces
// per level (90-day → 30-day → tasks). Unknown inline fields and unrecognized
// prose are preserved; whitespace/field-ordering are normalized.
func Serialize(d *Doc) string {
	pre := strings.TrimRight(d.preamble, "\n")
	if strings.TrimSpace(pre) == "" {
		pre = "# Goals"
	}
	out := []string{pre}
	for _, a := range d.Areas {
		out = append(out, "", "## "+a.Name)
		if a.NorthStar != "" {
			out = append(out, "> North Star: "+a.NorthStar)
		}
		if a.has90 || len(a.Goals) > 0 {
			out = append(out, "", "### 90-day")
			for _, g := range a.Goals {
				emitGoal(&out, g, 0)
			}
		}
		out = append(out, a.extra...)
	}
	return strings.Join(out, "\n") + "\n"
}

func emitGoal(out *[]string, g *Goal, depth int) {
	*out = append(*out, strings.Repeat("    ", depth)+goalLine(g))
	for _, c := range g.Children {
		emitGoal(out, c, depth+1)
	}
}

func goalLine(g *Goal) string {
	box := " "
	if g.Checked {
		box = "x"
	}
	var b strings.Builder
	b.WriteString("- [")
	b.WriteString(box)
	b.WriteString("] ")
	b.WriteString(g.Text)
	for _, f := range canonicalFields(g) {
		b.WriteString(" [")
		b.WriteString(f.Key)
		b.WriteString(":: ")
		b.WriteString(f.Value)
		b.WriteString("]")
	}
	return b.String()
}
