package goals

import (
	"regexp"
	"strconv"
	"strings"
)

var slugStripRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug lowercases and hyphenates text into a stable id fragment.
func slug(s string) string {
	s = strings.ToLower(s)
	s = slugStripRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

// deriveID builds a stable id from an area name + goal text, e.g.
// "aion/draft-murugan-picard-contract".
func deriveID(area, text string) string {
	a, t := slug(area), slug(text)
	if a == "" {
		a = "area"
	}
	if t == "" {
		t = "goal"
	}
	return a + "/" + t
}

// explicitID returns the value of an explicit [goal:: id] field, or "".
func (g *Goal) explicitID() string {
	for _, f := range g.Fields {
		if strings.EqualFold(f.Key, "goal") {
			return f.Value
		}
	}
	return ""
}

// assignIDs gives every goal a stable id: an explicit [goal:: id] wins, else a
// derived area/text slug, deduped within the document with -2/-3 suffixes.
func (d *Doc) assignIDs() {
	seen := map[string]bool{}
	for _, a := range d.Areas {
		for _, g := range a.allGoals() {
			id := g.explicitID()
			if id == "" {
				id = deriveID(a.Name, g.Text)
			}
			base, n := id, 2
			for seen[id] {
				id = base + "-" + strconv.Itoa(n)
				n++
			}
			seen[id] = true
			g.ID = id
		}
	}
}
