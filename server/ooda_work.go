package server

import (
	"sort"
	"strings"
	"time"

	"manifest/aion"
	"manifest/realestate"
)

// The WORK surface (ooda-portal plan §6.4): every open task and decision in
// the domain, grouped by the person who holds it. Four sources unified —
// the RE backlog, property rock-tree nodes, portal-created team items, and
// pending proposals — so a partner sees one list rather than four places to
// look.

type oodaWorkItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`   // task | decision
	Source    string `json:"source"` // backlog | rock | team | proposal
	Owner     string `json:"owner"`
	Container string `json:"container,omitempty"` // property / deal it belongs to
	Rock      string `json:"rock,omitempty"`
	Due       string `json:"due,omitempty"`
	Age       string `json:"age,omitempty"` // captured date, for "how long has this sat"
	// Waiting comes from a rock-tree task's [waiting:: who] field. Backlog
	// items have no such status — the aion vocabulary is exactly
	// open|in_progress|done|decided.
	Waiting bool `json:"waiting,omitempty"`
}

// oodaWorkGroup is one person's section. Empty Owner is the `— unassigned —`
// group, rendered last and highlighted: unassigned work is the finding.
type oodaWorkGroup struct {
	Owner string `json:"owner"`
	// Name is the person behind the initials. Partners do not memorize each
	// other's owner tokens, and a section headed "OS" tells them nothing.
	Name        string         `json:"name,omitempty"`
	Overdue     []oodaWorkItem `json:"overdue"`
	DueThisWeek []oodaWorkItem `json:"dueThisWeek"`
	Open        []oodaWorkItem `json:"open"`
	Decisions   []oodaWorkItem `json:"decisions"`
	Waiting     []oodaWorkItem `json:"waiting"`
}

// oodaOwnerAliases maps every way a person can be named in an [owner::] field
// back to ONE token. Real data proved this necessary: work owned as
// `olga-sobkiv` (her contractor slug) and work owned as `OS` (her initials)
// were landing in two separate groups for the same person — exactly the split
// people.md warns about ("a person who is ALSO a vendor carries
// [contractor:: <slug>] … work owned either way collects under this person").
func oodaOwnerAliases(people []*aion.Person) map[string]string {
	alias := map[string]string{}
	for _, p := range people {
		if p == nil || strings.TrimSpace(p.Initials) == "" {
			continue
		}
		ini := strings.ToUpper(strings.TrimSpace(p.Initials))
		alias[ini] = ini
		if n := strings.ToUpper(strings.TrimSpace(p.Name)); n != "" {
			alias[n] = ini
			alias[strings.ReplaceAll(n, " ", "-")] = ini
		}
		for _, f := range p.Unknown {
			if strings.EqualFold(f.Key, "contractor") {
				if slug := strings.ToUpper(strings.TrimSpace(f.Value)); slug != "" {
					alias[slug] = ini
				}
			}
		}
	}
	return alias
}

// oodaOwner normalizes one [owner::] value through the alias map. An owner
// with no registry entry (a contractor with no person record) keeps its own
// token rather than vanishing into "unassigned".
func oodaOwner(raw string, alias map[string]string) string {
	key := strings.ToUpper(strings.TrimSpace(raw))
	if key == "" {
		return ""
	}
	if ini, ok := alias[key]; ok {
		return ini
	}
	return key
}

// oodaBacklogOpen decides whether one backlog item is still outstanding.
//
// This is an ALLOW-LIST on purpose, matching the AION portal's canonical rule
// (web/portal/src/derive.js isOpen): open, in_progress, or no status at all.
// The first version of this function was a DENY-list — "not checked and not
// done" — and it failed open on every status it had not heard of. Decisions
// are not checkboxes and close with `[status:: decided]`, so 56 of 61 settled
// decisions rendered as live work. An unrecognized status means somebody
// marked the item something; the safe reading is CLOSED, not open.
func oodaBacklogOpen(it *aion.BacklogItem) bool {
	if it == nil || it.Checked {
		return false
	}
	// a decision carrying its resolution is closed whatever the status says
	if strings.TrimSpace(it.Decided) != "" || strings.TrimSpace(it.Outcome) != "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(it.Status)) {
	case "", aion.StatusOpen, aion.StatusInProgress:
		return true
	default: // done, decided, and anything new that appears later
		return false
	}
}

// oodaRockLabel turns a backlog item's `[rock::]` reference into the two
// readable halves the WORK tab shows: where the work lives, and which rock
// inside it. Backlog rocks are machine slugs — "ooda-group/operations-health",
// "4924-fountain-ave" — and rendering them raw made a partner guess which
// project a task belonged to. Rock-tree items already carry both halves.
func oodaRockLabel(rock string, shortBySlug map[string]string) (container, label string) {
	rock = strings.TrimSpace(rock)
	if rock == "" {
		return "", ""
	}
	head, tail, split := strings.Cut(rock, "/")
	human := func(s string) string {
		s = strings.TrimSpace(strings.ReplaceAll(s, "-", " "))
		if s == "" {
			return ""
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}
	name := func(slug string) string {
		if s, ok := shortBySlug[strings.ToLower(slug)]; ok {
			return s
		}
		return human(slug)
	}
	if !split {
		return name(rock), ""
	}
	return name(head), human(tail)
}

// buildOodaWork groups the domain's open work by assignee.
func buildOodaWork(snap *oodaSnapshot, today string) []oodaWorkGroup {
	if snap == nil {
		return nil
	}
	alias := oodaOwnerAliases(snap.People)
	weekOut := today
	if t, err := time.Parse("2006-01-02", today); err == nil {
		weekOut = t.AddDate(0, 0, 7).Format("2006-01-02")
	}

	// property slug → display name, so a backlog item filed against
	// "4924-fountain-ave" reads as a place rather than a slug
	shortBySlug := map[string]string{}
	for i := range snap.Properties {
		p := snap.Properties[i]
		if s := strings.TrimSpace(p.Short); s != "" {
			shortBySlug[strings.ToLower(p.Slug)] = s
		}
	}

	var items []oodaWorkItem
	// 1. the RE backlog (aion.Store over system/realestate)
	for _, it := range snap.Backlog {
		if !oodaBacklogOpen(it) {
			continue
		}
		kind := "task"
		if strings.EqualFold(it.Kind, "decision") {
			kind = "decision"
		}
		container, rock := oodaRockLabel(it.Rock, shortBySlug)
		items = append(items, oodaWorkItem{
			ID: it.ID, Title: it.Text, Kind: kind, Source: "backlog",
			Owner:     oodaOwner(it.Owner, alias),
			Container: container, Rock: rock, Due: it.Due, Age: it.Captured,
		})
	}
	// 2. property rock-tree nodes
	for i := range snap.Properties {
		p := snap.Properties[i]
		if p.Hidden {
			continue
		}
		label := p.Short
		if label == "" {
			label = p.Slug
		}
		realestate.WalkNodes(p.Work, func(st *realestate.WorkStage, n *realestate.WorkNode) {
			if n.Task == nil || n.Task.Checked {
				return
			}
			kind := "task"
			if n.Decision {
				kind = "decision"
			}
			// a rock-tree node has no due of its own — its ROCK's [done-by::]
			// is the date it is measured against, same as the cockpit shows
			items = append(items, oodaWorkItem{
				ID: "prop/" + p.Slug + "#" + n.ID, Title: n.Task.Text, Kind: kind, Source: "rock",
				Owner:     oodaOwner(n.Task.Owner, alias),
				Container: label, Rock: st.Text, Due: st.DoneBy, Age: n.Task.Added,
				Waiting: n.Task.Waiting != "",
			})
		})
	}
	// 3 + 4. portal-created team items and pending proposals live in the
	// overlay; the caller passes them through the snapshot-free path below.
	sort.SliceStable(items, func(a, b int) bool { return items[a].Title < items[b].Title })

	groups := map[string]*oodaWorkGroup{}
	for _, it := range items {
		g, ok := groups[it.Owner]
		if !ok {
			// every lane starts as an EMPTY SLICE, never nil: a nil lane
			// marshals to null and the client's .length blows up on it
			g = &oodaWorkGroup{Owner: it.Owner,
				Overdue: []oodaWorkItem{}, DueThisWeek: []oodaWorkItem{},
				Open: []oodaWorkItem{}, Decisions: []oodaWorkItem{}, Waiting: []oodaWorkItem{}}
			groups[it.Owner] = g
		}
		switch {
		case it.Waiting:
			g.Waiting = append(g.Waiting, it)
		case it.Kind == "decision":
			g.Decisions = append(g.Decisions, it)
		case it.Due != "" && it.Due < today:
			g.Overdue = append(g.Overdue, it)
		case it.Due != "" && it.Due <= weekOut:
			g.DueThisWeek = append(g.DueThisWeek, it)
		default:
			g.Open = append(g.Open, it)
		}
	}
	// put a face to each owner token: people.md first, then contractor records
	names := map[string]string{}
	for _, p := range snap.People {
		if p != nil && strings.TrimSpace(p.Initials) != "" {
			names[strings.ToUpper(p.Initials)] = p.Name
		}
	}
	for _, c := range snap.Contractors {
		if ini := contractorInitials(c.Name); ini != "" && names[ini] == "" {
			names[ini] = c.Name
		}
	}
	out := make([]oodaWorkGroup, 0, len(groups))
	for _, g := range groups {
		g.Name = names[strings.ToUpper(g.Owner)]
		out = append(out, *g)
	}
	// partners and contractors alphabetically; the unassigned group LAST,
	// because work nobody holds is the thing this tab exists to surface.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Owner == "") != (out[j].Owner == "") {
			return out[j].Owner == ""
		}
		return out[i].Owner < out[j].Owner
	})
	return out
}
