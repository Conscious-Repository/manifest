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

// explicitID returns the value of an explicit [goal:: id] field, or "".
func (g *Goal) explicitID() string {
	for _, f := range g.Fields {
		if strings.EqualFold(f.Key, "goal") {
			return f.Value
		}
	}
	return ""
}

// identity is the durable slug to emit for a Rock or annual: an explicit
// [goal:: id] wins, otherwise the derived id (assigned by assignIDs).
func (g *Goal) identity() string {
	if id := g.explicitID(); id != "" {
		return id
	}
	return g.ID
}

// assignIDs gives every goal a stable, hierarchical id: an explicit [goal:: id]
// wins, otherwise the id is the parent's id (the area slug for a Rock/annual root)
// plus the goal's own text slug — e.g. "aion/series-a-15m" for a Rock,
// "aion/series-a-15m/term-sheet" for its stage. Collisions get -2/-3 suffixes.
func (d *Doc) assignIDs() {
	seen := map[string]bool{}
	for _, a := range d.Areas {
		base := slug(a.Name)
		if base == "" {
			base = "area"
		}
		assignChildren(base, a.Annuals, seen)
		assignChildren(base, a.Rocks, seen)
	}
}

func assignChildren(prefix string, gs []*Goal, seen map[string]bool) {
	for _, g := range gs {
		id := g.explicitID()
		if id == "" {
			t := slug(g.Text)
			if t == "" {
				t = "goal"
			}
			id = prefix + "/" + t
		}
		base, n := id, 2
		for seen[id] {
			id = base + "-" + strconv.Itoa(n)
			n++
		}
		seen[id] = true
		g.ID = id
		assignChildren(g.ID, g.Children, seen)
	}
}
