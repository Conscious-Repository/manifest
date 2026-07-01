package goals

import (
	"regexp"
	"strings"
)

var (
	areaHeadingRe = regexp.MustCompile(`^##[ \t]+(.*\S)\s*$`)
	horizonRe     = regexp.MustCompile(`^###[ \t]+(.*\S)\s*$`)
	// goalLineRe captures leading indentation (m[1]), the checkbox state (m[2]),
	// and the rest of the line (m[3]). Indentation drives nesting depth.
	goalLineRe = regexp.MustCompile(`^([ \t]*)[-*]\s*\[([ xX])\]\s?(.*)$`)
	yearRe     = regexp.MustCompile(`\b(\d{4})\b`)
)

func isAreaHeading(line string) bool { return areaHeadingRe.MatchString(line) }

func areaName(line string) string {
	if m := areaHeadingRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

type section int

const (
	sectionNone   section = iota
	sectionAnnual         // "### 1-year"
	sectionRocks          // "### Rocks (90-day)" or legacy "### 90-day"
	sectionOther          // any other "### ..." — not a goals section
)

// classifyHorizon maps a "### ..." heading to a section, extracting the year label
// from a "### 1-year — 2026" style heading.
func classifyHorizon(line string) (section, string) {
	m := horizonRe.FindStringSubmatch(line)
	if m == nil {
		return sectionNone, ""
	}
	h := strings.TrimSpace(m[1])
	condensed := strings.ReplaceAll(strings.ToLower(h), " ", "")
	switch {
	case strings.HasPrefix(condensed, "1-year") || strings.HasPrefix(condensed, "1year"):
		year := ""
		if ym := yearRe.FindStringSubmatch(h); ym != nil {
			year = ym[1]
		}
		return sectionAnnual, year
	case strings.HasPrefix(condensed, "rocks") || strings.HasPrefix(condensed, "90"):
		return sectionRocks, ""
	default:
		return sectionOther, ""
	}
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

// indentWidth measures leading whitespace in columns (tab = 4), so 2-space,
// 4-space, or tab nesting all round-trip predictably.
func indentWidth(s string) int {
	w := 0
	for _, r := range s {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}

type frame struct {
	indent int
	goal   *Goal
}

// Parse turns goals.md content into a Doc. It never hard-errors: anything it
// doesn't recognize is preserved as area "extra" prose or the doc preamble.
// Checkbox lines under "### 1-year" nest into Annuals; under "### Rocks (90-day)"
// (or the legacy "### 90-day") they nest into Rocks (Rock → stage → task by
// indentation). Other "### " sections drop their heading and preserve any goals
// beneath as "extra" so nothing is lost.
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
		a := &Area{Name: areaName(lines[i])}
		i++
		var stack []frame
		var root *[]*Goal // nil = not currently in a goals section
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
			case horizonRe.MatchString(line):
				sec, year := classifyHorizon(line)
				stack = stack[:0]
				switch sec {
				case sectionAnnual:
					a.hasAnnual = true
					if year != "" {
						a.yearLabel = year
					}
					root = &a.Annuals
				case sectionRocks:
					a.hasRocks = true
					root = &a.Rocks
				default: // other heading: drop it, goals beneath become extra
					root = nil
				}
			default:
				if m := goalLineRe.FindStringSubmatch(line); m != nil {
					if root == nil {
						a.extra = append(a.extra, line)
						continue
					}
					w := indentWidth(m[1])
					g := parseGoal(m)
					for len(stack) > 0 && stack[len(stack)-1].indent >= w {
						stack = stack[:len(stack)-1]
					}
					if len(stack) == 0 {
						*root = append(*root, g)
					} else {
						p := stack[len(stack)-1].goal
						p.Children = append(p.Children, g)
					}
					stack = append(stack, frame{indent: w, goal: g})
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

func parseGoal(m []string) *Goal {
	text, fields := parseFields(m[3])
	g := &Goal{
		Text:    text,
		Checked: strings.EqualFold(strings.TrimSpace(m[2]), "x"),
		Fields:  fields,
	}
	for _, f := range fields {
		switch strings.ToLower(f.Key) {
		case "owner":
			g.Owner = strings.TrimSpace(f.Value)
		case "quarter":
			g.Quarter = strings.TrimSpace(f.Value)
		case "serves":
			g.Serves = strings.TrimSpace(f.Value)
		case "status":
			g.Status = strings.TrimSpace(f.Value)
		case "rolled-from":
			g.RolledFrom = strings.TrimSpace(f.Value)
			// `due` is intentionally ignored (retired).
		}
	}
	return g
}
