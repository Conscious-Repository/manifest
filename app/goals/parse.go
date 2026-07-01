package goals

import (
	"regexp"
	"strings"
	"time"
)

var (
	areaHeadingRe = regexp.MustCompile(`^##[ \t]+(.*\S)\s*$`)
	horizonRe     = regexp.MustCompile(`^###[ \t]+(.*\S)\s*$`)
	// goalLineRe captures leading indentation (m[1]), the checkbox state (m[2]),
	// and the rest of the line (m[3]). Indentation drives cascade nesting.
	goalLineRe = regexp.MustCompile(`^([ \t]*)[-*]\s*\[([ xX])\]\s?(.*)$`)
)

func isAreaHeading(line string) bool { return areaHeadingRe.MatchString(line) }

func areaName(line string) string {
	if m := areaHeadingRe.FindStringSubmatch(line); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// horizonOf returns whether a "### ..." heading is the 90-day cascade section.
func is90Heading(line string) bool {
	m := horizonRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	h := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(m[1])), " ", "")
	return strings.HasPrefix(h, "90")
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
// doesn't recognize is preserved as area "extra" prose or the doc preamble. Under
// "### 90-day", checkbox lines nest by indentation into the cascade (90-day →
// 30-day → tasks). Legacy "### 30-day" (and other "###") sections are not part of
// the cascade — their heading is dropped and any goals beneath are preserved
// verbatim as "extra" so nothing is lost.
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
		inCascade := false
		var stack []frame
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
				if is90Heading(line) {
					inCascade = true
					a.has90 = true
					stack = stack[:0]
				} else {
					inCascade = false // legacy 30-day / unknown: drop heading, preserve goals as extra
				}
			default:
				if m := goalLineRe.FindStringSubmatch(line); m != nil {
					if !inCascade {
						a.extra = append(a.extra, line)
						continue
					}
					w := indentWidth(m[1])
					g := parseGoal(m)
					for len(stack) > 0 && stack[len(stack)-1].indent >= w {
						stack = stack[:len(stack)-1]
					}
					if len(stack) == 0 {
						a.Goals = append(a.Goals, g)
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
		switch {
		case strings.EqualFold(f.Key, "owner"):
			g.Owner = strings.TrimSpace(f.Value)
		case strings.EqualFold(f.Key, "due"):
			g.Due = normalizeDue(f.Value)
		}
	}
	return g
}
