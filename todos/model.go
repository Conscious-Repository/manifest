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

	"manifest/record"
)

// Field is the kernel's inline-field pair (unrecognized keys round-trip).
type Field = record.Field

// Todo is one action line under a domain heading (loose or inside a bucket).
type Todo struct {
	ID      string `json:"id"` // explicit [todo:: id] else derived domain/text slug
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Added   string `json:"added,omitempty"`   // [added:: YYYY-MM-DD] — the age anchor
	Done    string `json:"done,omitempty"`    // [done:: YYYY-MM-DD] — stamped at check (sweep key)
	Waiting string `json:"waiting,omitempty"` // [waiting:: who] — free text or [[person]]
	Since   string `json:"since,omitempty"`   // [since:: YYYY-MM-DD] — waiting-since
	// task-substrate tethers: bucket is WHERE it belongs, tether is WHAT it advances
	Rock  string `json:"rock,omitempty"`  // [rock:: rock-id] — services a Rock
	Issue string `json:"issue,omitempty"` // [issue:: issue-id] — works an issue
	Stage string `json:"stage,omitempty"` // [stage:: name] — then-current stage, stamped at completion
	// assignment model (redesign Rev 3): unassigned means the owner's
	Owner  string  `json:"owner,omitempty"`  // [owner:: initials|name] — who owes this ("" = me)
	Rank   string  `json:"rank,omitempty"`   // [rank:: n] — drag-to-rank position (raw, round-trips)
	Fields []Field `json:"fields,omitempty"` // unrecognized fields, verbatim
}

// Bucket is a standing task grouping that is NOT a goal (partnerships,
// properties, long-lived containers). Heading links bind records: a linked
// property page renders the bucket's open todos.
type Bucket struct {
	Name   string   `json:"name"`
	Slug   string   `json:"slug"` // kernel slug; explicit [bucket:: slug] pin wins
	Links  []string `json:"links,omitempty"`
	Todos  []*Todo  `json:"todos"`
	pinned bool
}

// Issue is a decision/blocker under `### issues` — its own type: never aged,
// never in the todo signal path. Resolving = checking with a one-line note.
type Issue struct {
	ID         string  `json:"id"` // [issue:: domain/slug], auto-assigned
	Text       string  `json:"text"`
	Checked    bool    `json:"checked"`
	Added      string  `json:"added,omitempty"`
	Done       string  `json:"done,omitempty"`
	Resolution string  `json:"resolution,omitempty"` // [resolution:: note]
	Fields     []Field `json:"fields,omitempty"`
	explicit   bool    // carried an explicit [issue:: id]
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

// Domain is one ## section, in canonical order: loose todos → buckets →
// ### issues → ### backlog. Inbox is the special undomained capture heading.
type Domain struct {
	Name    string    `json:"name"`
	Todos   []*Todo   `json:"todos"` // loose (unbucketed)
	Buckets []*Bucket `json:"buckets,omitempty"`
	Issues  []*Issue  `json:"issues,omitempty"`
	Backlog []string  `json:"backlog,omitempty"` // plain idea bullets, verbatim, never aged
	extra   []string  // verbatim non-todo lines (preserved, unrendered)
}

// AllTodos iterates loose + bucket todos (the aging/signal/find surface).
func (dom *Domain) AllTodos(fn func(b *Bucket, t *Todo)) {
	for _, t := range dom.Todos {
		fn(nil, t)
	}
	for _, b := range dom.Buckets {
		for _, t := range b.Todos {
			fn(b, t)
		}
	}
}

// Doc is the whole file.
type Doc struct {
	preamble []string // verbatim through the first ## heading
	Domains  []*Domain
}

// InboxName is the capture heading (case-insensitive match on parse).
const InboxName = "Inbox"

// Find returns the todo with id (explicit or derived) and its domain —
// searching loose AND bucket todos.
func (d *Doc) Find(id string) (*Domain, *Todo) {
	for _, dom := range d.Domains {
		var hit *Todo
		dom.AllTodos(func(_ *Bucket, t *Todo) {
			if hit == nil && (t.ID == id || t.explicitID() == id) {
				hit = t
			}
		})
		if hit != nil {
			return dom, hit
		}
	}
	return nil, nil
}

// FindIssue returns the issue with id and its domain.
func (d *Doc) FindIssue(id string) (*Domain, *Issue) {
	for _, dom := range d.Domains {
		for _, is := range dom.Issues {
			if is.ID == id {
				return dom, is
			}
		}
	}
	return nil, nil
}

// Rename changes a bucket's display name, pinning its ORIGINAL slug first so
// every reference (record links, board state, tethers) survives the rename —
// the kernel identity rule.
func (b *Bucket) Rename(name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == b.Name {
		return
	}
	b.pinned = true // keep the old slug as the durable identity
	b.Name = name
}

// EnsureBucket returns the named bucket in a domain, creating it.
func (dom *Domain) EnsureBucket(name string) *Bucket {
	for _, b := range dom.Buckets {
		if strings.EqualFold(b.Name, name) || strings.EqualFold(b.Slug, slug(name)) {
			return b
		}
	}
	b := &Bucket{Name: strings.TrimSpace(name), Slug: slug(name)}
	dom.Buckets = append(dom.Buckets, b)
	return b
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
	Name    string       `json:"name"`
	Todos   []TodoView   `json:"todos"` // loose
	Buckets []BucketView `json:"buckets,omitempty"`
	Issues  []IssueView  `json:"issues,omitempty"`
	Backlog []string     `json:"backlog,omitempty"`
}

type BucketView struct {
	Name  string     `json:"name"`
	Slug  string     `json:"slug"`
	Links []string   `json:"links,omitempty"`
	Todos []TodoView `json:"todos"`
}

type IssueView struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	Checked    bool   `json:"checked"`
	Resolution string `json:"resolution,omitempty"`
	OpenTasks  int    `json:"openTasks"` // open todos tethered to this issue — worked, not parked
}

type TodoView struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	State   string `json:"state"`
	Added   string `json:"added,omitempty"`
	Waiting string `json:"waiting,omitempty"`
	Since   string `json:"since,omitempty"`
	Rock    string `json:"rock,omitempty"`
	Issue   string `json:"issue,omitempty"`
	Bucket  string `json:"bucket,omitempty"` // bucket slug when inside one
	Owner   string `json:"owner,omitempty"`  // assignee ("" = me)
	Rank    int    `json:"rank,omitempty"`   // parsed [rank:: n] (0 = unranked)
	AgeDays int    `json:"ageDays"`
}

// View projects the doc for the client (done items included until the sweep
// removes them — the board strikes them through).
func (d *Doc) View(now time.Time) View {
	v := View{Domains: []DomainView{}}
	// open tethered-task counts per issue, across the whole doc
	openByIssue := map[string]int{}
	for _, dom := range d.Domains {
		dom.AllTodos(func(_ *Bucket, t *Todo) {
			if t.Issue != "" && !t.Checked {
				openByIssue[t.Issue]++
			}
		})
	}
	tv := func(t *Todo, bucket string) TodoView {
		return TodoView{
			ID: t.ID, Text: t.Text, State: t.State(),
			Added: t.Added, Waiting: t.Waiting, Since: t.Since,
			Rock: t.Rock, Issue: t.Issue, Bucket: bucket,
			Owner: t.Owner, Rank: t.RankN(),
			AgeDays: t.AgeDays(now),
		}
	}
	for _, dom := range d.Domains {
		dv := DomainView{Name: dom.Name, Todos: []TodoView{}}
		for _, t := range dom.Todos {
			dv.Todos = append(dv.Todos, tv(t, ""))
		}
		for _, b := range dom.Buckets {
			bv := BucketView{Name: b.Name, Slug: b.Slug, Links: b.Links, Todos: []TodoView{}}
			for _, t := range b.Todos {
				bv.Todos = append(bv.Todos, tv(t, b.Slug))
			}
			dv.Buckets = append(dv.Buckets, bv)
		}
		for _, is := range dom.Issues {
			dv.Issues = append(dv.Issues, IssueView{
				ID: is.ID, Text: is.Text, Checked: is.Checked,
				Resolution: is.Resolution, OpenTasks: openByIssue[is.ID],
			})
		}
		dv.Backlog = dom.Backlog
		v.Domains = append(v.Domains, dv)
	}
	return v
}

// SplitDone reports whether the one-time task-substrate split has run (a
// [split:: date] marker line in the preamble — task-substrate §7).
func (d *Doc) SplitDone() bool {
	for _, ln := range d.preamble {
		if strings.Contains(ln, "[split::") {
			return true
		}
	}
	return false
}

// MarkSplitDone stamps the split marker (idempotent).
func (d *Doc) MarkSplitDone(date string) {
	if !d.SplitDone() {
		d.preamble = append(d.preamble, "", "[split:: "+date+"]")
	}
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
