package server

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"manifest/mdfm"
	"manifest/record"
)

// The todo-panel plan record (todo-panel plan D2): one hand-editable vault
// file per ENRICHED todo — `<SystemRoot>/todo-plans/<slug>.md` — frontmatter
// (todo / assignee / state) + `## plan`. Writes are
// surgical section swaps (vaultwriter.ReplaceSectionCap under the
// `todo-plans` capability), so an Obsidian hand-edit never
// collides with an app write. The agent-plan materialization
// (Phase 4) writes the `## plan` section only, under `todo-plans-agent` —
// the §12 standing-consent lane.

type todoPlansCfg struct {
	root string // vault-relative slash path, e.g. "system/todo-plans"
}

// UseTaskPlans wires the plan-record layer (root = <SystemRoot>/todo-plans).
func (s *Server) UseTaskPlans(root string) {
	if strings.TrimSpace(root) == "" {
		return
	}
	s.todoPlans = &todoPlansCfg{root: path.Clean(filepath.ToSlash(root))}
}

// planSlug files a composite todo id (`aion:x`, `prop:a/b`, `inbox/thing`)
// as one filename; the frontmatter `todo:` field keeps the exact id.
func planSlug(id string) string {
	return record.Slug(strings.NewReplacer(":", "-", "/", "-").Replace(id), 64)
}

func (c *todoPlansCfg) rel(id string) string { return c.root + "/" + planSlug(id) + ".md" }

// planRecord is the parsed record.
type planRecord struct {
	Exists   bool   `json:"exists"`
	Plan     string `json:"plan"`
	Assignee string `json:"assignee,omitempty"`
	State    string `json:"state,omitempty"`
	Rel      string `json:"rel"` // vault-relative path (the "plan file →" link)
}

// sectionBody extracts one `## name` body from a markdown body — the read
// twin of vaultwriter.spliceSection's rules (## heading, until the next ##).
func sectionBody(body, name string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t")
		if strings.EqualFold(t, "## "+name) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start+1:end], "\n"))
}

// readPlanRecord loads a todo's record (zero value when none exists yet).
func (s *Server) readPlanRecord(id string) planRecord {
	out := planRecord{}
	if s.todoPlans == nil || s.vault == nil {
		return out
	}
	out.Rel = s.todoPlans.rel(id)
	raw, err := s.vault.ReadVaultFile(out.Rel)
	if err != nil {
		return out
	}
	fm, body := mdfm.Split(string(raw))
	out.Exists = true
	out.Plan = sectionBody(body, "plan")
	out.Assignee = fm["assignee"]
	out.State = fm["state"]
	return out
}

// ensurePlanRecord creates the record skeleton when absent (assignee may be
// ""). Frontmatter is stable field order (mdfm.Writer) for diff-friendliness.
func (s *Server) ensurePlanRecord(id, assignee string) error {
	if s.todoPlans == nil || s.vault == nil {
		return errBadRequest("todo plans not configured")
	}
	rel := s.todoPlans.rel(id)
	if _, err := s.vault.ReadVaultFile(rel); err == nil {
		return nil // already a record
	}
	w := (&mdfm.Writer{}).Set("todo", id).Set("assignee", assignee).SetRaw("state", "open")
	return s.vault.WriteCap("todo-plans", rel, []byte(w.String("## plan\n")))
}

// setPlanAssignee rewrites the record's frontmatter assignee (creating the
// record when absent), preserving both section bodies byte-for-byte.
func (s *Server) setPlanAssignee(id, assignee string) error {
	if err := s.ensurePlanRecord(id, assignee); err != nil {
		return err
	}
	rel := s.todoPlans.rel(id)
	raw, err := s.vault.ReadVaultFile(rel)
	if err != nil {
		return err
	}
	fm, body := mdfm.Split(string(raw))
	w := (&mdfm.Writer{}).Set("todo", fm["todo"]).Set("assignee", assignee).SetRaw("state", orStr(fm["state"], "open"))
	return s.vault.WriteCap("todo-plans", rel, []byte(w.String(strings.TrimLeft(body, "\n"))))
}

// writePlanSection swaps one section under the given capability.
func (s *Server) writePlanSection(capName, id, section, body string) error {
	if err := s.ensurePlanRecord(id, ""); err != nil {
		return err
	}
	return s.vault.ReplaceSectionCap(capName, s.todoPlans.rel(id), section, body)
}

// --- endpoints ---------------------------------------------------------------

// handleTaskPanel serves everything the panel needs in one payload:
// the plan record + the thread + the delegation state.
func (s *Server) handleTaskPanel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	out := map[string]any{
		"id":         id,
		"record":     s.readPlanRecord(id),
		"thread":     s.listThread(id),
		"threadKind": s.threadKind(id),
	}
	if d, ok := s.delegationIndex()[id]; ok {
		out["delegation"] = d
	}
	writeJSON(w, out)
}

// handleTaskPlan — the owner's direct section edit.
// The FIRST panel artifact pins the todo's identity (plan D1).
func (s *Server) handleTaskPlan(w http.ResponseWriter, r *http.Request) {
	s.handlePlanSectionWrite(w, r, "plan")
}

func (s *Server) handlePlanSectionWrite(w http.ResponseWriter, r *http.Request, section string) {
	var b struct{ ID, Text string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	id, ok := s.pinTaskID(strings.TrimSpace(b.ID))
	if !ok {
		http.Error(w, "todo not found", http.StatusNotFound)
		return
	}
	if err := s.writePlanSection("todo-plans", id, section, strings.TrimRight(b.Text, "\n")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "record": s.readPlanRecord(id)})
}
