package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"manifest/artifacts"
	"manifest/daily"
	"manifest/goals"
	"manifest/mdfm"
	"manifest/record"
	"manifest/tasks"
)

// Record context is a reviewed projection of existing records, never a second
// task/goal store. A reference does not assign a task or grant shared access.
type chatContextRecord struct {
	Kind             string `json:"kind"`
	ID               string `json:"id"`
	Title            string `json:"title"`
	Detail           string `json:"detail"`
	Route            string `json:"route"`
	schedule         *daily.ScheduleRow
	contextError     string
	candidateSource  string
	candidateText    string
	aliases          string
	profileNotePath  string
	ambiguousProfile bool
	task             *unifiedRow
	goal             *goalContextBranch
	project          *projectContextRecord
}
type goalContextBranch struct {
	Area      string
	NorthStar string
	Year      string
	Section   string
	Parents   []goals.GoalView
	Selected  goals.GoalView
}

func (s *Server) chatContextRecords(kind string, q string) ([]chatContextRecord, error) {
	out := []chatContextRecord{}
	switch kind {
	case "note":
		if s.index == nil {
			return nil, fmt.Errorf("note index unavailable")
		}
		notes, err := s.index.ContextNotes(q, 50)
		if err != nil {
			return nil, err
		}
		for _, n := range notes {
			out = append(out, chatContextRecord{Kind: kind, ID: n.Path, Title: n.Name, Detail: n.Path, Route: "#/note/" + url.PathEscape(n.Path)})
		}
	case "schedule":
		return s.chatScheduleRecords(q)
	case "organization":
		return s.chatOrganizationRecords()
	case "candidate":
		return s.chatCandidateRecords()
	case "project":
		return s.chatProjectRecords()
	case "person":
		return s.chatPersonRecords()
	case "task":
		if s.tasksStore == nil && s.realestate == nil && s.aion == nil && s.re == nil {
			return nil, fmt.Errorf("task records unavailable")
		}
		var doc *tasks.Doc
		if s.tasksStore != nil {
			var err error
			doc, err = s.tasksStore.Load()
			if err != nil {
				return nil, err
			}
		}
		// Use the same composite identities and coordination projection as Tasks.
		rows, err := s.unifiedRowsChecked(doc, time.Now())
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			row := row
			out = append(out, chatContextRecord{Kind: kind, ID: row.ID, Title: row.Text, Detail: row.Container.Name + " · " + row.ID, Route: "#/tasks/" + url.PathEscape(row.ID), task: &row})
		}
	case "goal":
		if s.goals == nil {
			return nil, fmt.Errorf("goal records unavailable")
		}
		for _, area := range s.goals.Load().View().Areas {
			var walk func([]goals.GoalView, string, []goals.GoalView)
			walk = func(nodes []goals.GoalView, section string, parents []goals.GoalView) {
				for _, g := range nodes {
					branch := &goalContextBranch{area.Name, area.NorthStar, area.Year, section, append([]goals.GoalView(nil), parents...), g}
					ancestry := []string{area.Name, section}
					for _, p := range parents {
						ancestry = append(ancestry, p.Text)
					}
					out = append(out, chatContextRecord{Kind: kind, ID: g.ID, Title: g.Text, Detail: strings.Join(ancestry, " › ") + " · " + g.ID, Route: "#/goals/" + url.PathEscape(g.ID), aliases: strings.Join(g.Aliases, " "), goal: branch})
					walk(g.Children, section, append(append([]goals.GoalView(nil), parents...), g))
				}
			}
			walk(area.Annuals, "Annual", nil)
			walk(area.Rocks, "Rock", nil)
		}
	default:
		return nil, errBadRequest("choose note, task, goal, person or project")
	}
	return out, nil
}

func (s *Server) handleChatRecordSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len(q) > 256 {
		http.Error(w, "query is too long", 400)
		return
	}
	rows, err := s.chatContextRecords(r.URL.Query().Get("kind"), q)
	if err != nil {
		httpError(w, err)
		return
	}
	found := []chatContextRecord{}
	for _, row := range rows {
		if row.Kind == "note" || strings.Contains(strings.ToLower(row.Title+" "+row.Detail+" "+row.aliases), q) {
			found = append(found, row)
		}
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := strings.ToLower(found[i].Title), strings.ToLower(found[j].Title)
		if a == b {
			return found[i].ID < found[j].ID
		}
		return a < b
	})
	if len(found) > 50 {
		found = found[:50]
	}
	writeJSON(w, map[string]any{"records": found, "limit": 50})
}

func contextField(out *strings.Builder, name, value string) {
	if value != "" {
		fmt.Fprintf(out, "%s: %s\n", name, value)
	}
}

// Read optional narratives without the legacy readPlanRecord's missing/error
// fallback. A slug collision must not attach another task's private description.
func (s *Server) taskContextNarrative(id string) (planRecord, error) {
	var rec planRecord
	if s.todoPlans != nil && s.vault != nil {
		raw, err := s.vault.ReadVaultFile(s.todoPlans.rel(id))
		if err != nil && !os.IsNotExist(err) {
			return rec, err
		}
		if err == nil {
			fm, body := mdfm.Split(string(raw))
			if record.Unquote(fm["todo"]) != id {
				return rec, errBadRequest("task plan identity mismatch")
			}
			rec.Description = planRecordSection(body, "description")
			rec.Plan = planRecordSection(body, "plan")
			rec.Assignee = record.Unquote(fm["assignee"])
		}
	}
	if path := s.plannerNotesPath(id); path != "" {
		raw, err := os.ReadFile(filepath.Join(path, "description.md"))
		if err != nil && !os.IsNotExist(err) {
			return rec, err
		}
		if err == nil {
			rec.Description = string(raw)
		}
	}
	return rec, nil
}

func (s *Server) chatContextRecordPreview(kind, id string) (chatContextRecord, []byte, error) {
	if kind == "note" {
		b, err := s.contextNoteBytes(id)
		return chatContextRecord{Kind: kind, ID: id, Title: id, Detail: id, Route: "#/note/" + url.PathEscape(id)}, b, err
	}
	query := ""
	if kind == "schedule" {
		query = strings.SplitN(id, "/", 2)[0]
	}
	rows, err := s.chatContextRecords(kind, query)
	if err != nil {
		return chatContextRecord{}, nil, err
	}
	var selected *chatContextRecord
	for i := range rows {
		if rows[i].ID == id {
			if selected != nil {
				return chatContextRecord{}, nil, errBadRequest("record identity is ambiguous")
			}
			selected = &rows[i]
		}
	}
	if selected == nil {
		return chatContextRecord{}, nil, errBadRequest("record no longer available")
	}
	if selected.contextError != "" {
		return *selected, nil, errBadRequest(selected.contextError)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\nRecord type: %s\nRecord ID: %s\n", selected.Title, kind, id)
	if selected.schedule != nil {
		contextField(&out, "Date", strings.SplitN(id, "/", 2)[0])
		contextField(&out, "Time slot", selected.schedule.Time)
		contextField(&out, "Saved label", selected.schedule.Label)
		fmt.Fprintf(&out, "Focused: %t\n\nSource: saved Day schedule only. Calendar events, journal, tasks and adjacent slots are excluded. Selection does not reschedule anything.\n", selected.schedule.Focused)
	} else if selected.Kind == "organization" {
		if err := s.renderOrganizationContext(&out, *selected); err != nil {
			return *selected, nil, err
		}
	} else if selected.Kind == "candidate" {
		contextField(&out, "Source record", selected.candidateSource)
		out.WriteString("\n## Exact candidate record\n\n" + selected.candidateText + "\n\nLinked evidence files, outreach logs and role records are references only; their contents are excluded. Selecting this record does not approve outreach or change candidate state.\n")
	} else if selected.project != nil {
		s.renderProjectContext(&out, *selected)
	} else if selected.Kind == "person" {
		if err := s.renderPersonContext(&out, *selected); err != nil {
			return *selected, nil, err
		}
	} else if t := selected.task; t != nil {
		contextField(&out, "Source", t.Source)
		contextField(&out, "Container", t.Container.Name)
		contextField(&out, "Owner", t.Owner)
		contextField(&out, "State", t.State)
		contextField(&out, "Priority", t.Priority)
		contextField(&out, "Added", t.Added)
		contextField(&out, "Waiting", t.Waiting)
		contextField(&out, "Linked goal", t.Rock)
		for _, f := range []struct {
			name   string
			values []string
		}{{"Depends on", t.Depends}, {"Blocked by", t.BlockedBy}, {"Unresolved dependencies", t.Unresolved}, {"Dependents", t.Dependents}, {"Input artifact IDs", t.Inputs}, {"Output artifact IDs", t.Outputs}} {
			contextField(&out, f.name, strings.Join(f.values, ", "))
		}
		rec, err := s.taskContextNarrative(id)
		if err != nil {
			return *selected, nil, err
		}
		contextField(&out, "Plan assignee", rec.Assignee)
		if rec.Description != "" {
			fmt.Fprintf(&out, "\n## Description\n\n%s\n", rec.Description)
		}
		if rec.Plan != "" {
			fmt.Fprintf(&out, "\n## Plan\n\n%s\n", rec.Plan)
		}
	} else if g := selected.goal; g != nil {
		contextField(&out, "Area", g.Area)
		contextField(&out, "North star", g.NorthStar)
		contextField(&out, "Year", g.Year)
		contextField(&out, "Section", g.Section)
		for _, p := range g.Parents {
			contextField(&out, "Ancestor", p.Text+" ["+p.ID+"]")
		}
		out.WriteString("\n## Selected branch\n\n")
		var render func(goals.GoalView, int)
		render = func(node goals.GoalView, depth int) {
			indent := strings.Repeat("  ", depth)
			checked := " "
			if node.Checked {
				checked = "x"
			}
			fmt.Fprintf(&out, "%s- [%s] %s\n", indent, checked, node.Text)
			for _, f := range []struct{ name, value string }{{"ID", node.ID}, {"Owner", node.Owner}, {"Quarter", node.Quarter}, {"Start", node.Start}, {"Due", node.Due}, {"Status", node.Status}, {"Serves", strings.Join(node.Serves, ", ")}, {"Aliases", strings.Join(node.Aliases, ", ")}, {"Moved", node.Moved}} {
				if f.value != "" {
					fmt.Fprintf(&out, "%s  %s: %s\n", indent, f.name, f.value)
				}
			}
			for _, child := range node.Children {
				render(child, depth+1)
			}
			for _, line := range node.Frozen {
				fmt.Fprintf(&out, "%s  Historical line: %s\n", indent, line)
			}
		}
		render(g.Selected, 0)
	}
	b := []byte(out.String())
	if len(b) > 64000 || !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 || describeArtifactPreview(artifacts.Hash(b), b).Kind != "text" {
		return *selected, nil, errBadRequest("record snapshot exceeds supported text limits")
	}
	return *selected, b, nil
}

func (s *Server) handleChatRecordPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	row, b, err := s.chatContextRecordPreview(r.URL.Query().Get("kind"), r.URL.Query().Get("id"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"record": row, "content": string(b), "revision": artifacts.Hash(b)})
}
func (s *Server) handleChatRecordRetain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.artifactsOK(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var req struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Revision string `json:"revision"`
	}
	if err := decode(r, &req); err != nil || !artifacts.ValidHash(req.Revision) {
		http.Error(w, "record identity and reviewed revision required", 400)
		return
	}
	row, b, err := s.chatContextRecordPreview(req.Kind, req.ID)
	if err != nil {
		httpError(w, err)
		return
	}
	if artifacts.Hash(b) != req.Revision {
		http.Error(w, "record changed; review its current snapshot before selecting it", 409)
		return
	}
	source, harness := req.Kind+"-context", "manifest"
	if req.Kind == "note" {
		source, harness = "knowledge-context", "vault"
	}
	title := row.Title
	if title != row.ID {
		title += " · " + row.ID
	}
	ref, err := s.retainContextSnapshot(source, harness, row.ID, title, b)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, ref)
}

// The kind and exact source ID are independent from task ownership/provenance.
func contextSnapshotSource(a artifacts.Artifact) (kind, id, route string) {
	if path := knowledgeContextPath(a); path != "" {
		return "note", path, "#/note/" + url.PathEscape(path)
	}
	if a.Harness != "manifest" || (a.Provenance.Source != "task-context" && a.Provenance.Source != "goal-context" && a.Provenance.Source != "person-context" && a.Provenance.Source != "project-context" && a.Provenance.Source != "candidate-context" && a.Provenance.Source != "organization-context" && a.Provenance.Source != "schedule-context") {
		return "", "", ""
	}
	i := strings.LastIndex(a.Ref, "#context-")
	if i < 1 || !artifacts.ValidHash(a.Ref[i+9:]) {
		return "", "", ""
	}
	kind = strings.TrimSuffix(a.Provenance.Source, "-context")
	id = a.Ref[:i]
	route = "#/" + kind + "s/" + url.PathEscape(id)
	if kind == "schedule" {
		route = scheduleContextRoute(id)
	}
	if kind == "candidate" {
		route = candidateContextRoute(id)
	}
	if kind == "project" {
		route = "#/chat/project/" + url.PathEscape(id)
	}
	if kind == "person" || kind == "organization" {
		route = "#/contacts/" + url.PathEscape(id)
	}
	return
}
