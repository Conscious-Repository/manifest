package goals

import "strings"

// Serialize renders a Doc back to canonical goals.md. The output is a fixpoint:
// re-parsing and re-serializing yields identical bytes, so the app only ever
// produces minimal diffs. Each area emits "### 1-year" (annuals) then
// "### Rocks (90-day)" (Rock → stage → task, four spaces per level). Canonical
// fields are role-aware (§1); unknown fields and unrecognized prose are preserved.
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
		if a.hasAnnual || len(a.Annuals) > 0 {
			out = append(out, "", "### 1-year"+yearSuffix(a.yearLabel))
			for _, g := range a.Annuals {
				emitGoal(&out, g, 0, roleAnnual)
			}
		}
		if a.hasRocks || len(a.Rocks) > 0 {
			out = append(out, "", "### Rocks (90-day)")
			for _, g := range a.Rocks {
				emitGoal(&out, g, 0, roleRock)
			}
		}
		out = append(out, a.extra...)
	}
	return strings.Join(out, "\n") + "\n"
}

func yearSuffix(year string) string {
	if year == "" {
		return ""
	}
	return " — " + year
}

// emitGoal writes a goal line at depth, then its children. A top-level node emits
// with its role (annual/rock); everything below it is a stage/task.
func emitGoal(out *[]string, g *Goal, depth int, role fieldRole) {
	*out = append(*out, strings.Repeat("    ", depth)+goalLine(g, role))
	for _, c := range g.Children {
		emitGoal(out, c, depth+1, roleStageTask)
	}
}

func goalLine(g *Goal, role fieldRole) string {
	box := " "
	if g.Checked {
		box = "x"
	}
	var b strings.Builder
	b.WriteString("- [")
	b.WriteString(box)
	b.WriteString("] ")
	b.WriteString(g.Text)
	for _, f := range canonicalFields(g, role) {
		b.WriteString(" [")
		b.WriteString(f.Key)
		b.WriteString(":: ")
		b.WriteString(f.Value)
		b.WriteString("]")
	}
	return b.String()
}
