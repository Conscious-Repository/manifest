// Property tasks live IN the `## rocks` tree (realestate-overhaul §6): the
// property record is the single home for property tasks/decisions. This file
// is the task-surface facade over that tree — the unified todo board, the
// task panel, and the composer all mutate tree nodes through it. The retired
// `## tasks` section still READS tolerantly (pre-migration files), but every
// write lands in the tree.
package realestate

import (
	"os"
	"path/filepath"
	"strings"

	"manifest/mdfm"
	"manifest/tasks"
)

// PropertyTaskList is the task-surface view of one property's rock tree,
// loaded fresh for a mutation (parse → mutate → EmitWork → section replace).
type PropertyTaskList struct {
	Stages  []WorkStage
	Section string       // the heading the tree lives under: "rocks" | legacy "work"
	Legacy  []LegacyLine // remaining `## tasks` lines (read-only; pre-migration)
}

// LegacyLine is one un-migrated `## tasks` section line.
type LegacyLine struct {
	Task *tasks.Task
	Raw  string
}

// ParsePropertyTasks reads legacy `## tasks` section body lines (tolerant
// read for pre-migration files + the migration itself). Checkbox lines parse
// through the shared grammar; everything else rides verbatim. Ids: explicit
// [todo:: id] pin wins, else the text slug with -2/-3 collision suffixes.
func ParsePropertyTasks(lines []string) []LegacyLine {
	var out []LegacyLine
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
		m := workLineRe.FindStringSubmatch(ln)
		if m == nil {
			out = append(out, LegacyLine{Raw: ln})
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
		out = append(out, LegacyLine{Task: t})
	}
	return out
}

// Find returns the tree node whose task id (or work id) matches, or nil.
func (l *PropertyTaskList) Find(id string) *WorkNode {
	_, n := FindWorkNode(l.Stages, id)
	return n
}

// Tasks returns the task-surface projection of the tree: every non-milestone,
// non-decision node (all depths), as shared-grammar tasks whose ID is the
// node's task id. Legacy `## tasks` lines (pre-migration) append after.
// Projections are copies — mutate through the tree, not these.
func (l *PropertyTaskList) Tasks() []*tasks.Task {
	var out []*tasks.Task
	WalkNodes(l.Stages, func(_ *WorkStage, n *WorkNode) {
		if n.Milestone || n.Decision {
			return
		}
		t := *n.Task
		t.ID = n.TaskID()
		out = append(out, &t)
	})
	for _, ln := range l.Legacy {
		if ln.Task != nil {
			out = append(out, ln.Task)
		}
	}
	return out
}

// Append adds a task/decision to the tree: under the rock (or milestone)
// named by parent — matched by id first, then case-insensitive rock text —
// else loose under the current rock; with no rocks at all an "Inbox" rock is
// created. Returns the new node.
func (l *PropertyTaskList) Append(t *tasks.Task, parent string) *WorkNode {
	node := &WorkNode{Task: t}
	parent = strings.TrimSpace(parent)
	if parent != "" {
		if _, p := FindWorkNode(l.Stages, parent); p != nil {
			p.Children = append(p.Children, node)
			return node
		}
		for i := range l.Stages {
			if l.Stages[i].ID == parent || strings.EqualFold(l.Stages[i].Text, parent) {
				l.Stages[i].Tasks = append(l.Stages[i].Tasks, node)
				return node
			}
		}
	}
	for i := range l.Stages {
		if !l.Stages[i].Checked {
			l.Stages[i].Tasks = append(l.Stages[i].Tasks, node)
			return node
		}
	}
	l.Stages = append(l.Stages, WorkStage{Text: "Inbox", Tasks: []*WorkNode{node}})
	return node
}

// Remove deletes the node with the given task id from the tree. Returns
// whether a node was removed.
func (l *PropertyTaskList) Remove(id string) bool {
	var prune func(nodes []*WorkNode) ([]*WorkNode, bool)
	prune = func(nodes []*WorkNode) ([]*WorkNode, bool) {
		for i, n := range nodes {
			if n.ID == id || n.TaskID() == id {
				return append(nodes[:i], nodes[i+1:]...), true
			}
			if next, ok := prune(n.Children); ok {
				n.Children = next
				return nodes, true
			}
		}
		return nodes, false
	}
	for i := range l.Stages {
		if next, ok := prune(l.Stages[i].Tasks); ok {
			l.Stages[i].Tasks = next
			return true
		}
	}
	return false
}

// Emit renders the tree back to its section body (fixpoint with the parse).
func (l *PropertyTaskList) Emit() string { return EmitWork(l.Stages) }

// LoadTasks re-reads a property's file and parses its rock tree (plus any
// legacy `## tasks` remainder). Returns the list, the vault-relative path
// (for the section write), and ok.
func (s *Service) LoadTasks(slug string) (*PropertyTaskList, string, bool) {
	p, ok := s.Get(slug)
	if !ok {
		return nil, "", false
	}
	raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(p.Path)))
	if err != nil {
		return nil, "", false
	}
	_, body := mdfm.Split(string(raw))
	secs := parseSections(body)
	list := &PropertyTaskList{Section: "rocks"}
	sec, okSec := secs["rocks"]
	if !okSec {
		if legacy, okW := secs["work"]; okW {
			sec, list.Section = legacy, "work"
		}
	}
	list.Stages = ParseWork(sec)
	list.Legacy = ParsePropertyTasks(secs["tasks"])
	return list, p.Path, true
}
