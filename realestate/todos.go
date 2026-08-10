// The property `## todos` section (redesign stage 4 — Revision 3): a flat
// list of action lines in the SHARED to-do line grammar (todos.ParseLine /
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
	"manifest/todos"
)

// PropertyTodoLine is one section body line: a parsed todo or a verbatim
// line (comments, blanks — preserved in place for the fixpoint).
type PropertyTodoLine struct {
	Todo *todos.Todo
	Raw  string
}

// PropertyTodoList is the parsed `## todos` section body.
type PropertyTodoList []PropertyTodoLine

// ParsePropertyTodos reads section body lines. Checkbox lines parse through
// the shared grammar; everything else rides verbatim. Ids: explicit
// [todo:: id] pin wins, else the text slug with -2/-3 collision suffixes.
func ParsePropertyTodos(lines []string) PropertyTodoList {
	var out PropertyTodoList
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
			out = append(out, PropertyTodoLine{Raw: ln})
			continue
		}
		t := todos.ParseLine(m[2] != " ", m[3])
		id := t.ExplicitID()
		if id == "" {
			ts := todos.LineSlug(t.Text)
			if ts == "" {
				ts = "todo"
			}
			id = ts
		}
		t.ID = uniq(id)
		out = append(out, PropertyTodoLine{Todo: t})
	}
	return out
}

var todoLineRe = workLineRe // the kernel checkbox grammar (one regex, one file)

// EmitPropertyTodos renders the section body back (fixpoint with parse).
func EmitPropertyTodos(list PropertyTodoList) string {
	var b strings.Builder
	for _, ln := range list {
		if ln.Todo != nil {
			b.WriteString(todos.EmitLine(ln.Todo) + "\n")
		} else {
			b.WriteString(ln.Raw + "\n")
		}
	}
	return b.String()
}

// Find returns the todo with id, or nil.
func (l PropertyTodoList) Find(id string) *todos.Todo {
	for _, ln := range l {
		if ln.Todo != nil && ln.Todo.ID == id {
			return ln.Todo
		}
	}
	return nil
}

// Todos returns just the parsed todo lines, in order.
func (l PropertyTodoList) Todos() []*todos.Todo {
	var out []*todos.Todo
	for _, ln := range l {
		if ln.Todo != nil {
			out = append(out, ln.Todo)
		}
	}
	return out
}

// Append adds a new todo line at the end of the section.
func (l PropertyTodoList) Append(t *todos.Todo) PropertyTodoList {
	return append(l, PropertyTodoLine{Todo: t})
}

// LoadTodos re-reads a property's file and parses its `## todos` section.
// Returns the list, the vault-relative path (for the section write), and ok.
func (s *Service) LoadTodos(slug string) (PropertyTodoList, string, bool) {
	p, ok := s.Get(slug)
	if !ok {
		return nil, "", false
	}
	raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(p.Path)))
	if err != nil {
		return nil, "", false
	}
	_, body := mdfm.Split(string(raw))
	return ParsePropertyTodos(parseSections(body)["todos"]), p.Path, true
}

// MigrateWorkTodos previews (or builds) the one-time copy of a property's OPEN
// `## work` todos into `## todos`, each carrying its [work:: id] back-tether
// and an [added::] stamp. `## work` bytes are untouched — it stays the budget
// source; the tether is what keeps the money model truthful on completion.
// Already-tethered lines are skipped (idempotent).
func MigrateWorkTodos(p Property, list PropertyTodoList, now time.Time) (PropertyTodoList, []string) {
	tethered := map[string]bool{}
	for _, ln := range list {
		if ln.Todo != nil {
			if wid := ln.Todo.FieldValue("work"); wid != "" {
				tethered[wid] = true
			}
		}
	}
	var added []string
	for _, st := range p.Work {
		for _, td := range st.Todos {
			if td.Checked || tethered[td.ID] {
				continue
			}
			t := &todos.Todo{
				Text:  td.Text,
				Added: now.Format("2006-01-02"),
				Fields: []todos.Field{
					{Key: "work", Value: td.ID},
				},
			}
			list = list.Append(t)
			added = append(added, td.Text)
		}
	}
	return list, added
}
