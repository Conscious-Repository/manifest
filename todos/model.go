// Package todos is the third surface's file library: a vault-root `to do.md`
// parsed and re-serialized as a FIXPOINT (goals.md's exact contract — the
// file is truth, hand-editable in Obsidian, byte-stable round trips).
//
// A todo is deliberately small: one line of text · a domain (## heading) ·
// a state (open → done, plus waiting-on carrying who + since-when). No
// projects, no subtasks, no priorities, no due dates — deep structured work
// belongs to the domains (todos-surface-scope §"The object").
package todos

import (
	"regexp"
	"strings"
	"time"
)

// Field is one inline [key:: value] pair (unrecognized keys round-trip).
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Todo is one action line under a domain heading.
type Todo struct {
	ID      string  `json:"id"` // explicit [todo:: id] else derived domain/text slug
	Text    string  `json:"text"`
	Checked bool    `json:"checked"`
	Added   string  `json:"added,omitempty"`   // [added:: YYYY-MM-DD] — the age anchor
	Done    string  `json:"done,omitempty"`    // [done:: YYYY-MM-DD] — stamped at check (sweep key)
	Waiting string  `json:"waiting,omitempty"` // [waiting:: who] — free text or [[person]]
	Since   string  `json:"since,omitempty"`   // [since:: YYYY-MM-DD] — waiting-since
	Fields  []Field `json:"fields,omitempty"`  // unrecognized fields, verbatim
}

// State reports open | waiting | done.
func (t *Todo) State() string {
	switch {
	case t.Checked:
		return "done"
	case strings.TrimSpace(t.Waiting) != "":
		return "waiting"
	default:
		return "open"
	}
}

// AgeDays is days since Added (waiting items age from Since — the fuse
// restarts when the ball moves to someone else's court).
func (t *Todo) AgeDays(now time.Time) int {
	anchor := t.Added
	if t.State() == "waiting" && t.Since != "" {
		anchor = t.Since
	}
	if anchor == "" {
		return 0
	}
	d, err := time.Parse("2006-01-02", anchor)
	if err != nil {
		return 0
	}
	days := int(now.Sub(d).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// Domain is one ## section. Inbox is the special undomained capture heading.
type Domain struct {
	Name  string  `json:"name"`
	Todos []*Todo `json:"todos"`
	extra []string // verbatim non-todo lines under the heading (preserved, unrendered)
}

// Doc is the whole file.
type Doc struct {
	preamble []string // verbatim through the first ## heading
	Domains  []*Domain
}

// InboxName is the capture heading (case-insensitive match on parse).
const InboxName = "Inbox"

// Find returns the todo with id (explicit or derived) and its domain.
func (d *Doc) Find(id string) (*Domain, *Todo) {
	for _, dom := range d.Domains {
		for _, t := range dom.Todos {
			if t.ID == id || t.explicitID() == id {
				return dom, t
			}
		}
	}
	return nil, nil
}

// Domain returns the domain by name (case-insensitive), or nil.
func (d *Doc) Domain(name string) *Domain {
	for _, dom := range d.Domains {
		if strings.EqualFold(dom.Name, name) {
			return dom
		}
	}
	return nil
}

// EnsureDomain returns the named domain, creating it (before Inbox stays
// first; new domains append at the end).
func (d *Doc) EnsureDomain(name string) *Domain {
	if dom := d.Domain(name); dom != nil {
		return dom
	}
	dom := &Domain{Name: strings.TrimSpace(name)}
	d.Domains = append(d.Domains, dom)
	return dom
}

// View is the JSON projection for the board.
type View struct {
	Domains []DomainView `json:"domains"`
}

type DomainView struct {
	Name  string     `json:"name"`
	Todos []TodoView `json:"todos"`
}

type TodoView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	State   string `json:"state"`
	Added   string `json:"added,omitempty"`
	Waiting string `json:"waiting,omitempty"`
	Since   string `json:"since,omitempty"`
	AgeDays int    `json:"ageDays"`
}

// View projects the doc for the client (done items included until the sweep
// removes them — the board strikes them through).
func (d *Doc) View(now time.Time) View {
	v := View{Domains: []DomainView{}}
	for _, dom := range d.Domains {
		dv := DomainView{Name: dom.Name, Todos: []TodoView{}}
		for _, t := range dom.Todos {
			dv.Todos = append(dv.Todos, TodoView{
				ID: t.ID, Text: t.Text, State: t.State(),
				Added: t.Added, Waiting: t.Waiting, Since: t.Since,
				AgeDays: t.AgeDays(now),
			})
		}
		v.Domains = append(v.Domains, dv)
	}
	return v
}

var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// WaitingPerson extracts the [[person]] target from a waiting-on value ("" if
// the who is free text) — the contacts page's open-loop hook.
func (t *Todo) WaitingPerson() string {
	if m := wikilinkRe.FindStringSubmatch(t.Waiting); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
