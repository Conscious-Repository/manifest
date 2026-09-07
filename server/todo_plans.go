package server

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"manifest/approvals"
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
	Exists      bool   `json:"exists"`
	Description string `json:"description"` // `## description` (plan D2 — the owner's context; rides every work order)
	Plan        string `json:"plan"`
	Assignee    string `json:"assignee,omitempty"`
	State       string `json:"state,omitempty"`
	Document    string `json:"document,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Rel         string `json:"rel"` // vault-relative path (the "plan file →" link)
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
	out.Description = sectionBody(body, "description")
	out.Plan = sectionBody(body, "plan")
	out.Assignee = fm["assignee"]
	out.State = fm["state"]
	out.Document = record.Unquote(fm["document"])
	out.Mode = record.Unquote(fm["mode"])
	return out
}

// ensurePlanRecord creates the record skeleton when absent (assignee may be
// ""). Frontmatter is stable field order (mdfm.Writer) for diff-friendliness.
func (s *Server) ensurePlanRecord(id, assignee string) error {
	if s.todoPlans == nil || s.vault == nil {
		return errBadRequest("todo plans not configured")
	}
	rel := s.todoPlans.rel(id)
	if raw, err := s.vault.ReadVaultFile(rel); err == nil {
		fm, _ := mdfm.Split(string(raw))
		if record.Unquote(fm["todo"]) != id {
			return fmt.Errorf("task plan identity collision")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	w := (&mdfm.Writer{}).Set("todo", id).Set("assignee", assignee).SetRaw("state", "open")
	return s.vault.WriteCap("todo-plans", rel, []byte(w.String("## description\n\n## plan\n")))
}

// setPlanAssignee rewrites the record's frontmatter assignee (creating the
// record when absent), preserving both section bodies byte-for-byte.
func (s *Server) setPlanAssignee(id, assignee string) error {
	if err := s.ensurePlanRecord(id, assignee); err != nil {
		return err
	}
	rel := s.todoPlans.rel(id)
	return s.vault.UpdateCap("todo-plans", rel, func(raw []byte) ([]byte, error) {
		fm, _ := mdfm.Split(string(raw))
		if record.Unquote(fm["todo"]) != id {
			return nil, fmt.Errorf("task plan identity collision")
		}
		next, err := record.SetScalar(string(raw), "assignee", assignee)
		return []byte(next), err
	})
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
	rec := s.readPlanRecord(id)
	thread := s.listThread(id)
	out := map[string]any{
		"id":         id,
		"record":     rec,
		"thread":     thread,
		"threadKind": s.threadKind(id),
		"proposals":  s.taskProposals(id),
	}
	// "open in chat" (§3.4f): the conversation this task came from, else the
	// assignee's rail section
	if link := s.taskChatLink(id, thread, rec.Assignee); link != nil {
		out["chat"] = link
	}
	// coordination (P1 Phase 1): blocked-by / depends-on / dependents, derived
	// across every source on this read — the panel's "depends on · blocks" rows
	if c := s.taskCoord(id); c != nil {
		out["coord"] = c
	}
	// artifacts (P1): what this task produced / consumes — bound ids plus
	// every registered artifact whose provenance names it
	out["artifacts"] = s.taskArtifactsView(id)
	if d, ok := s.delegationIndex()[id]; ok {
		out["delegation"] = d
		// presence (agent-chat plan §3.4d): derived from the live index, never
		// stored — the thread renders "✦ Alfred is working… since 14:02 (plan)"
		if activeDelegation(d.State) {
			agent := d.Agent
			if agent == "" {
				agent = "agent:" + d.Harness
				if s.agentHarness(rec.Assignee) == d.Harness && rec.Assignee != "" {
					agent = rec.Assignee
				}
			}
			inflight := map[string]any{"agent": agent, "name": agentDisplayName(agent), "phase": orStr(d.Phase, "comment"), "state": d.State}
			if !d.Started.IsZero() {
				inflight["since"] = d.Started
			}
			out["inflight"] = inflight
		}
	}
	writeJSON(w, out)
}

// taskProposal is one pending approval filed against a todo — the panel's
// "⚑ N changes proposed — review" link deep-links to these FEED cards.
type taskProposal struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

// taskProposals lists the PENDING approvals carrying this todo's token,
// across every harness inbox (the do-bot files into the primary's). FEED
// stays the one approvals surface — this is a pointer, not a second inbox.
func (s *Server) taskProposals(id string) []taskProposal {
	out := []taskProposal{}
	seen := map[string]bool{}
	scan := func(st interface {
		List(status string) []approvals.Proposal
	}) {
		for _, p := range st.List("pending") {
			if seen[p.ID] {
				continue
			}
			if m := todoTokenRe.FindStringSubmatch(p.Action + "\n" + p.Body); m != nil && strings.TrimSpace(m[1]) == id {
				seen[p.ID] = true
				out = append(out, taskProposal{ID: p.ID, Action: strings.TrimSpace(todoTokenRe.ReplaceAllString(p.Action, ""))})
			}
		}
	}
	for _, h := range s.eachHarness() {
		if h.Approvals != nil {
			scan(h.Approvals)
		}
	}
	if s.approvals != nil {
		scan(s.approvals)
	}
	return out
}

// handleTaskPlan — the owner's direct section edit.
// The FIRST panel artifact pins the todo's identity (plan D1).
func (s *Server) handleTaskPlan(w http.ResponseWriter, r *http.Request) {
	s.handlePlanSectionWrite(w, r, "plan")
}

// handleTaskDescription — the `## description` section (plan D2, gap D): the
// owner's context for the task. Rides every work order and the plan-context
// hash, so a changed description re-plans like a changed title.
func (s *Server) handleTaskDescription(w http.ResponseWriter, r *http.Request) {
	s.handlePlanSectionWrite(w, r, "description")
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

// A workspace binding is optional and uses the task's existing record.
func (s *Server) handleWritingBinding(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || s.todoPlans == nil {
		http.Error(w, "task workspaces unavailable", 503)
		return
	}
	var b struct {
		ID       string `json:"id"`
		Document string `json:"document"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id, ok := s.pinTaskID(b.ID)
	if !ok {
		http.Error(w, "task not found", 404)
		return
	}
	if b.Document != "" {
		if _, ok = safeVaultPath(s.vault.VaultRoot(), b.Document); !ok || !s.vault.CanUserWrite(b.Document) {
			http.Error(w, "note is not writable", 400)
			return
		}
		if _, err := s.vault.ReadVaultFile(b.Document); err != nil {
			http.Error(w, "note not found", 404)
			return
		}
	}
	if err := s.ensurePlanRecord(id, ""); err != nil {
		httpError(w, err)
		return
	}
	err := s.vault.UpdateCap("todo-plans", s.todoPlans.rel(id), func(raw []byte) ([]byte, error) {
		fm, _ := mdfm.Split(string(raw))
		if record.Unquote(fm["todo"]) != id {
			return nil, fmt.Errorf("task plan identity collision")
		}
		next, err := record.SetScalar(string(raw), "document", strconv.Quote(b.Document))
		if err != nil {
			return nil, err
		}
		mode := ""
		if b.Document != "" {
			mode = "write"
		}
		next, err = record.SetScalar(next, "mode", strconv.Quote(mode))
		return []byte(next), err
	})
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"id": id, "document": b.Document})
}

// Relink only explicit task document pointers after an owner move. Other
// frontmatter and plan bytes are preserved; failures are reported after the move.
func (s *Server) relinkWritingTasks(from, to string) error {
	if s.todoPlans == nil || s.vault == nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(s.todoPlans.root)))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		rel := path.Join(s.todoPlans.root, entry.Name())
		raw, err := s.vault.ReadVaultFile(rel)
		if err != nil {
			return err
		}
		fm, _ := mdfm.Split(string(raw))
		if record.Unquote(fm["document"]) != from {
			continue
		}
		err = s.vault.UpdateCap("todo-plans", rel, func(current []byte) ([]byte, error) {
			fm, _ := mdfm.Split(string(current))
			if record.Unquote(fm["document"]) != from {
				return current, nil
			}
			next, err := record.SetScalar(string(current), "document", strconv.Quote(to))
			return []byte(next), err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
