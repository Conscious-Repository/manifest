package tasks

import (
	"strconv"
	"strings"

	"manifest/record"
)

// slug is the kernel slug at the shared 48-char id cap (goals rules).
func slug(s string) string { return record.Slug(s, 48) }

// explicitID returns the value of an explicit [todo:: id] field, or "".
func (t *Task) explicitID() string {
	for _, f := range t.Fields {
		if strings.EqualFold(f.Key, "todo") {
			return f.Value
		}
	}
	return ""
}

// assignIDs gives every todo and issue a stable id: an explicit pin wins,
// else domain-slug/text-slug; collisions get -2/-3 suffixes. Tasks (loose +
// bucket) and issues share one id space — both are tether targets.
func (d *Doc) assignIDs() {
	seen := map[string]bool{}
	uniq := func(id string) string {
		root, n := id, 2
		for seen[id] {
			id = root + "-" + strconv.Itoa(n)
			n++
		}
		seen[id] = true
		return id
	}
	for _, dom := range d.Domains {
		base := slug(dom.Name)
		if base == "" {
			base = "domain"
		}
		assign := func(t *Task) {
			id := t.explicitID()
			if id == "" {
				ts := slug(t.Text)
				if ts == "" {
					ts = "todo"
				}
				id = base + "/" + ts
			}
			t.ID = uniq(id)
		}
		for _, t := range dom.Tasks {
			assign(t)
		}
		for _, b := range dom.Buckets {
			for _, t := range b.Tasks {
				assign(t)
			}
		}
		for _, is := range dom.Issues {
			if is.ID == "" {
				ts := slug(is.Text)
				if ts == "" {
					ts = "issue"
				}
				is.ID = uniq(base + "/" + ts)
			} else {
				seen[is.ID] = true
			}
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
