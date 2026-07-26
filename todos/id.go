package todos

import (
	"regexp"
	"strconv"
	"strings"
)

var slugStripRe = regexp.MustCompile(`[^a-z0-9]+`)

// slug lowercases and hyphenates text into a stable id fragment (goals rules).
func slug(s string) string {
	s = strings.ToLower(s)
	s = slugStripRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

// explicitID returns the value of an explicit [todo:: id] field, or "".
func (t *Todo) explicitID() string {
	for _, f := range t.Fields {
		if strings.EqualFold(f.Key, "todo") {
			return f.Value
		}
	}
	return ""
}

// assignIDs gives every todo a stable id: an explicit [todo:: id] wins, else
// domain-slug/text-slug; collisions get -2/-3 suffixes.
func (d *Doc) assignIDs() {
	seen := map[string]bool{}
	for _, dom := range d.Domains {
		base := slug(dom.Name)
		if base == "" {
			base = "domain"
		}
		for _, t := range dom.Todos {
			id := t.explicitID()
			if id == "" {
				ts := slug(t.Text)
				if ts == "" {
					ts = "todo"
				}
				id = base + "/" + ts
			}
			root, n := id, 2
			for seen[id] {
				id = root + "-" + strconv.Itoa(n)
				n++
			}
			seen[id] = true
			t.ID = id
		}
	}
}

// Promote pins the derived id as an explicit [todo:: id] so the daily-note
// backlink survives rewording (the goals Promote contract). Returns false when
// the id is unknown.
func (d *Doc) Promote(id string) bool {
	_, t := d.Find(id)
	if t == nil {
		return false
	}
	if t.explicitID() == "" {
		t.Fields = append(t.Fields, Field{Key: "todo", Value: t.ID})
	}
	return true
}
