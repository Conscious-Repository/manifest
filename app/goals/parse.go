package goals

import (
	"regexp"
	"strings"
	"time"
)

var (
	areaHeadingRe = regexp.MustCompile(`^##[ \t]+(.*\S)\s*$`)
	horizonRe     = regexp.MustCompile(`^###[ \t]+(.*\S)\s*$`)
	goalLineRe    = regexp.MustCompile(`^\s*[-*]\s*\[([ xX])\]\s?(.*)$`)
)

func isAreaHeading(line string) bool { return areaHeadingRe.MatchString(line) }

func areaName(line string) string {
	if m := areaHeadingRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// horizonOf returns the recognized horizon for a "### ..." heading. Unknown
// "###" headings are not horizons (they are preserved as extra prose).
func horizonOf(line string) (Horizon, bool) {
	m := horizonRe.FindStringSubmatch(line)
	if m == nil {
		return HNone, false
	}
	h := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(m[1])), " ", "")
	switch {
	case strings.HasPrefix(h, "90"):
		return H90, true
	case strings.HasPrefix(h, "30"):
		return H30, true
	}
	return HNone, false
}

func isNorthStar(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), ">") }

func northStarText(line string) string {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
	if strings.HasPrefix(strings.ToLower(t), "north star:") {
		t = strings.TrimSpace(t[len("north star:"):])
	}
	return t
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

func normalizeDue(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

// Parse turns goals.md content into a Doc. It never hard-errors: anything it
// doesn't recognize is preserved as area "extra" prose or the doc preamble.
func Parse(content string) *Doc {
	doc := &Doc{}
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1] // drop the empty element from a trailing newline
		}
	}

	i := 0
	var pre []string
	for i < len(lines) && !isAreaHeading(lines[i]) {
		pre = append(pre, lines[i])
		i++
	}
	doc.preamble = strings.Join(pre, "\n")

	for i < len(lines) {
		a := &Area{Name: areaName(lines[i]), headingRaw: lines[i]}
		i++
		cur := HNone
		for i < len(lines) && !isAreaHeading(lines[i]) {
			line := lines[i]
			i++
			switch {
			case isBlank(line):
				continue
			case isNorthStar(line):
				if a.NorthStar == "" && a.nsRaw == "" {
					a.nsRaw = line
					a.NorthStar = northStarText(line)
				} else {
					a.extra = append(a.extra, line)
				}
			default:
				if h, ok := horizonOf(line); ok {
					cur = h
					switch h {
					case H90:
						a.has90 = true
					case H30:
						a.has30 = true
					}
					continue
				}
				if m := goalLineRe.FindStringSubmatch(line); m != nil {
					g := parseGoal(m, cur)
					b := a.bucket(cur)
					*b = append(*b, g)
					continue
				}
				a.extra = append(a.extra, line)
			}
		}
		doc.Areas = append(doc.Areas, a)
	}
	doc.assignIDs()
	return doc
}

func parseGoal(m []string, h Horizon) *Goal {
	text, fields := parseFields(m[2])
	g := &Goal{
		Text:    text,
		Checked: strings.EqualFold(strings.TrimSpace(m[1]), "x"),
		Horizon: h,
		Fields:  fields,
	}
	for _, f := range fields {
		switch {
		case strings.EqualFold(f.Key, "owner"):
			g.Owner = strings.TrimSpace(f.Value)
		case strings.EqualFold(f.Key, "due"):
			g.Due = normalizeDue(f.Value)
		}
	}
	return g
}
