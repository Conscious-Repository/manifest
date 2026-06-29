package goals

import "strings"

// Serialize renders a Doc back to canonical goals.md. The output is stable:
// re-parsing and re-serializing yields identical bytes (a fixpoint), so the app
// only ever produces minimal diffs. Unknown inline fields and unrecognized prose
// are preserved; whitespace/field-ordering are normalized to canonical form.
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
		if a.has90 || len(a.Goals90) > 0 {
			out = append(out, "", "### 90-day")
			for _, g := range a.Goals90 {
				out = append(out, goalLine(g))
			}
		}
		if a.has30 || len(a.Goals30) > 0 {
			out = append(out, "", "### 30-day")
			for _, g := range a.Goals30 {
				out = append(out, goalLine(g))
			}
		}
		for _, g := range a.Loose {
			out = append(out, goalLine(g))
		}
		out = append(out, a.extra...)
	}
	return strings.Join(out, "\n") + "\n"
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
