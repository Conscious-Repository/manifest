// The property `## todos` section (redesign stage 4 — Revision 3): a flat
// list of action lines in the SHARED to-do line grammar (tasks.ParseLine /
// EmitLine — one grammar, one file, kernel doctrine §3). A property todo
// carries text · an [owner::] assignee ("" = the owner's) · optionally a
// [work:: id] back-tether to the `## work` line it was migrated from (the
// accounting dual-stamp key) and a [rank:: n] for the unified board order.
//
// `## work` stays untouched by this section: it remains the budget/spent/
// schedule source; this list is the property's ACTION surface.
package realestate

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/mdfm"
	"manifest/tasks"
)

// PropertyTaskLine is one section body line: a parsed todo or a verbatim
// line (comments, blanks — preserved in place for the fixpoint).
type PropertyTaskLine struct {
	Task *tasks.Task
	Raw  string
}

// PropertyTaskList is the parsed `## todos` section body.
type PropertyTaskList []PropertyTaskLine

// ParsePropertyTasks reads section body lines. Checkbox lines parse through
// the shared grammar; everything else rides verbatim. Ids: explicit
// [todo:: id] pin wins, else the text slug with -2/-3 collision suffixes.
func ParsePropertyTasks(lines []string) PropertyTaskList {
	var out PropertyTaskList
	seen := map[string]bool{}
	uniq := func(id string) string {
		root, n := id, 2
		for seen[id] {
			id = root + "-" + itoaWork(n)
			n++
		}
		seen[id] = true
		return id
	}
	for _, ln := range lines {
		m := todoLineRe.FindStringSubmatch(ln)
		if m == nil {
			out = append(out, PropertyTaskLine{Raw: ln})
			continue
		}
		t := tasks.ParseLine(m[2] != " ", m[3])
		id := t.ExplicitID()
		if id == "" {
			ts := tasks.LineSlug(t.Text)
			if ts == "" {
				ts = "todo"
			}
			id = ts
		}
		t.ID = uniq(id)
		out = append(out, PropertyTaskLine{Task: t})
	}
	return out
}

var todoLineRe = workLineRe // the kernel checkbox grammar (one regex, one file)

// EmitPropertyTasks renders the section body back (fixpoint with parse).
func EmitPropertyTasks(list PropertyTaskList) string {
	var b strings.Builder
	for _, ln := range list {
		if ln.Task != nil {
			b.WriteString(tasks.EmitLine(ln.Task) + "\n")
		} else {
			b.WriteString(ln.Raw + "\n")
		}
	}
	return b.String()
}

// Find returns the todo with id, or nil.
func (l PropertyTaskList) Find(id string) *tasks.Task {
	for _, ln := range l {
		if ln.Task != nil && ln.Task.ID == id {
			return ln.Task
		}
	}
	return nil
}

// Tasks returns just the parsed todo lines, in order.
func (l PropertyTaskList) Tasks() []*tasks.Task {
	var out []*tasks.Task
	for _, ln := range l {
		if ln.Task != nil {
			out = append(out, ln.Task)
		}
	}
	return out
}

// Append adds a new todo line at the end of the section.
func (l PropertyTaskList) Append(t *tasks.Task) PropertyTaskList {
	return append(l, PropertyTaskLine{Task: t})
}

// LoadTasks re-reads a property's file and parses its `## todos` section.
// Returns the list, the vault-relative path (for the section write), and ok.
func (s *Service) LoadTasks(slug string) (PropertyTaskList, string, bool) {
	p, ok := s.Get(slug)
	if !ok {
		return nil, "", false
	}
	raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(p.Path)))
	if err != nil {
		return nil, "", false
	}
	_, body := mdfm.Split(string(raw))
	return ParsePropertyTasks(parseSections(body)["tasks"]), p.Path, true
}

// MigrateWorkTasks previews (or builds) the one-time copy of a property's OPEN
// `## work` todos into `## todos`, each carrying its [work:: id] back-tether
// and an [added::] stamp. `## work` bytes are untouched — it stays the budget
// source; the tether is what keeps the money model truthful on completion.
// Already-tethered lines are skipped (idempotent).
func MigrateWorkTasks(p Property, list PropertyTaskList, now time.Time) (PropertyTaskList, []string) {
	tethered := map[string]bool{}
	for _, ln := range list {
		if ln.Task != nil {
			if wid := ln.Task.FieldValue("work"); wid != "" {
				tethered[wid] = true
			}
		}
	}
	var added []string
	for _, st := range p.Work {
		for _, td := range st.Tasks {
			if td.Checked || tethered[td.ID] {
				continue
			}
			t := &tasks.Task{
				Text:  td.Text,
				Added: now.Format("2006-01-02"),
				Fields: []tasks.Field{
					{Key: "work", Value: td.ID},
				},
			}
			list = list.Append(t)
			added = append(added, td.Text)
		}
	}
	return list, added
}
