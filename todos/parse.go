package todos

import (
	"regexp"
	"strings"
)

var (
	headingRe     = regexp.MustCompile(`^##[ \t]+(.*\S)\s*$`)
	todoLineRe    = regexp.MustCompile(`^[ \t]*[-*]\s*\[([ xX])\]\s?(.*)$`)
	inlineFieldRe = regexp.MustCompile(`\[([A-Za-z][\w-]*)\s*::\s*([^\]]*)\]`)
	// waiting values may BE a wikilink — [waiting:: [[Josh]]] — which the
	// generic field regex can't hold (]] inside the value); match it first
	waitingLinkRe = regexp.MustCompile(`\[waiting::\s*(\[\[[^\]]+\]\])\s*\]`)
)

// Parse reads `to do.md` bytes into a Doc. Tolerant: any checkbox bullet under
// a ## heading (any indent — the surface is flat) is a todo; every other line
// round-trips verbatim (preamble before the first heading, extra lines inside
// a domain — the [[x posts]] tail survives untouched).
func Parse(raw string) *Doc {
	d := &Doc{}
	var cur *Domain
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			cur = &Domain{Name: strings.TrimSpace(m[1])}
			d.Domains = append(d.Domains, cur)
			continue
		}
		if cur == nil {
			d.preamble = append(d.preamble, line)
			continue
		}
		if m := todoLineRe.FindStringSubmatch(line); m != nil {
			cur.Todos = append(cur.Todos, parseTodo(m[1] != " ", m[2]))
			continue
		}
		if strings.TrimSpace(line) != "" {
			cur.extra = append(cur.extra, line)
		}
	}
	d.assignIDs()
	return d
}

func parseTodo(checked bool, rest string) *Todo {
	t := &Todo{Checked: checked}
	rest = waitingLinkRe.ReplaceAllStringFunc(rest, func(m string) string {
		t.Waiting = strings.TrimSpace(waitingLinkRe.FindStringSubmatch(m)[1])
		return ""
	})
	var unknown []Field
	clean := inlineFieldRe.ReplaceAllStringFunc(rest, func(m string) string {
		sm := inlineFieldRe.FindStringSubmatch(m)
		key, val := strings.TrimSpace(sm[1]), strings.TrimSpace(sm[2])
		switch strings.ToLower(key) {
		case "todo":
			unknown = append(unknown, Field{Key: "todo", Value: val}) // identity, kept in Fields for explicitID
		case "added":
			t.Added = val
		case "done":
			t.Done = val
		case "waiting":
			t.Waiting = val
			return "" // waiting values may carry [[links]] — strip only the field shell
		case "since":
			t.Since = val
		default:
			unknown = append(unknown, Field{Key: sm[1], Value: val})
		}
		return ""
	})
	t.Fields = unknown
	t.Text = strings.Join(strings.Fields(clean), " ")
	return t
}
