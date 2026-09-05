package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/aion"
	"manifest/daily"
	"manifest/goals"
	"manifest/teamportal"
)

// syncAionTasks mirrors day-note ticks for backlog-backed tasks (Task.TaskID
// "aion:<id>" / "re:<id>") into the owning domain backlog: done → status done
// (stamps done_on + the Rock's moved::), unticked → status open. The domain
// twin of syncTaskTasks.
func (s *Server) syncAionTasks(tasks []daily.Task) {
	now := time.Now()
	for _, t := range tasks {
		var st *aion.Store
		var id string
		var live *AionLive
		switch {
		case strings.HasPrefix(t.TaskID, "aion:") && s.aion != nil:
			st, id = s.aion, strings.TrimPrefix(t.TaskID, "aion:")
			live = s.aionLive
		case strings.HasPrefix(t.TaskID, "re:") && s.re != nil:
			st, id = s.re, strings.TrimPrefix(t.TaskID, "re:")
		default:
			continue
		}
		status := aion.StatusOpen
		if t.Done {
			status = aion.StatusDone
		}
		var err error
		if live != nil {
			err = live.ownerUpdate(id, map[string]string{"status": status}, now)
		} else {
			err = st.UpdateItem(id, map[string]string{"status": status}, now)
		}
		if err == nil && t.Done {
			if it := st.LoadBacklog().Find(id); it != nil && it.Rock != "" {
				s.stampRockMoved(it.Rock)
			}
		}
	}
}

// UseAion wires the AION tab and its live projection. The checkout arguments
// remain accepted for one transition release, but no runtime behavior uses
// them; historical receipt files are left untouched.
func (s *Server) UseAion(st *aion.Store, _ string, _ string, _ string, dataDir string) {
	s.aion = st
	s.aionDataDir = dataDir
	s.aionLive = newAionLive(s)
}

// UsePortalSync gives the portal→vault reconciler its OWN store handle, bound
// to the `aion-portal` capability rather than the cockpit's `aion`.
//
// Same file, same parser, different actor: a line the owner typed and a line a
// portal member's edit produced are both legitimate, and write-audit.log should
// be able to tell them apart. Sharing s.aion would have worked and would have
// logged every team edit as `user-action` — a small lie, told forever.
// Nil-safe: no handle, no materialization, and the portal stays overlay-only.
func (s *Server) UsePortalSync(st *aion.Store) { s.aionPortal = st }

// handleAion is the aggregate read: all seven parsed corpora + the goals
// Aion ladder (read-only) + the collaboration/live-sync state.
func (s *Server) handleAion(w http.ResponseWriter, r *http.Request) {
	if s.aion == nil {
		httpError(w, errBadRequest("aion not enabled"))
		return
	}
	people := s.aion.LoadPeople()
	backlog := s.aion.LoadBacklog()
	heur := s.aion.LoadHeuristics()
	vto := s.aion.LoadVTO()
	fin := s.aion.LoadFinances()

	finances := map[string]string{}
	for _, k := range aion.FinanceKeys {
		finances[k] = fin.Get(k)
	}

	nonNilItems := func(v []*aion.BacklogItem) []*aion.BacklogItem {
		if v == nil {
			return []*aion.BacklogItem{}
		}
		return v
	}
	nonNilHeur := func(v []*aion.Heuristic) []*aion.Heuristic {
		if v == nil {
			return []*aion.Heuristic{}
		}
		return v
	}
	effectiveBacklog := any(nonNilItems(backlog.Items()))
	var collaboration any
	var syncStatus any
	if s.aionLive != nil {
		effectiveBacklog = s.aionLive.ManifestItems()
		collaboration = s.aionLive.TeamState()
		syncStatus = s.aionLive.Status()
	}
	resp := map[string]any{
		"people":        peopleView(people),
		"vto":           vtoView(vto),
		"backlog":       effectiveBacklog,
		"collaboration": collaboration,
		"sync":          syncStatus,
		"heuristics":    nonNilHeur(heur.LiveEntries()),
		"retired":       retiredView(heur),
		"hiring":        hiringView(s.aion.LoadHiring()),
		"references":    referencesView(s.aion.LoadReferences()),
		"finances":      finances,
		"goalsArea":     s.aionGoalsArea(),
	}
	writeJSON(w, resp)
}

func peopleView(d *aion.PeopleDoc) []*aion.Person {
	out := d.People()
	if out == nil {
		out = []*aion.Person{}
	}
	return out
}

func retiredView(d *aion.HeuristicsDoc) []*aion.Heuristic {
	var out []*aion.Heuristic
	for _, ln := range d.Retired {
		if ln.Heuristic != nil {
			out = append(out, ln.Heuristic)
		}
	}
	if out == nil {
		out = []*aion.Heuristic{}
	}
	return out
}

type vtoSectionView struct {
	Heading string          `json:"heading"`
	Entries []aion.VTOEntry `json:"entries"`
	Raw     []string        `json:"raw"`
}

func vtoView(d *aion.VTODoc) []vtoSectionView {
	out := make([]vtoSectionView, 0, len(d.Sections))
	for _, sec := range d.Sections {
		v := vtoSectionView{Heading: sec.Heading, Entries: []aion.VTOEntry{}, Raw: []string{}}
		for _, ln := range sec.Lines {
			if ln.Entry != nil {
				v.Entries = append(v.Entries, *ln.Entry)
			} else if strings.TrimSpace(ln.Raw) != "" {
				v.Raw = append(v.Raw, ln.Raw)
			}
		}
		out = append(out, v)
	}
	return out
}

func hiringView(d *aion.HiringDoc) []*aion.HiringItem {
	var out []*aion.HiringItem
	for _, ln := range d.Lines {
		if ln.Item != nil {
			out = append(out, ln.Item)
		}
	}
	if out == nil {
		out = []*aion.HiringItem{}
	}
	return out
}

func referencesView(d *aion.ReferencesDoc) []*aion.Reference {
	var out []*aion.Reference
	for _, ln := range d.Lines {
		if ln.Ref != nil {
			out = append(out, ln.Ref)
		}
	}
	if out == nil {
		out = []*aion.Reference{}
	}
	return out
}

// aionGoalsArea projects the goals.md `## Aion` area (read-only here — the
// GOALS tab is the edit surface).
func (s *Server) aionGoalsArea() *goals.AreaView { return s.goalsAreaByName("Aion") }

// goalsAreaByName is the area↔domain coupling point (aion: "Aion";
// real estate: "Real Estate") — read-only; the GOALS tab owns edits.
func (s *Server) goalsAreaByName(name string) *goals.AreaView {
	if s.goals == nil {
		return nil
	}
	view := s.goals.Load().View()
	for i := range view.Areas {
		if strings.EqualFold(view.Areas[i].Name, name) {
			return &view.Areas[i]
		}
	}
	return nil
}

// ---- corpus writes (user-action capability) ----

func (s *Server) handleAionPeopleSave(w http.ResponseWriter, r *http.Request) {
	var b struct {
		People []*aion.Person `json:"people"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	for _, p := range b.People {
		if strings.TrimSpace(p.Initials) == "" {
			httpError(w, errBadRequest("every person needs initials"))
			return
		}
	}
	doc := s.aion.LoadPeople()
	aion.ReplacePeople(doc, b.People)
	if err := s.aion.SavePeople(doc); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAionVTOSave replaces the entries of each submitted section (matched
// by heading), keeping raw lines and unknown sections intact — the client
// round-trips the full section list from GET /api/aion.
func (s *Server) handleAionVTOSave(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Sections []struct {
			Heading string          `json:"heading"`
			Entries []aion.VTOEntry `json:"entries"`
		} `json:"sections"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	doc := s.aion.LoadVTO()
	for _, in := range b.Sections {
		sec := findVTOSection(doc, in.Heading)
		if sec == nil {
			sec = &aion.VTOSection{Heading: in.Heading}
			doc.Sections = append(doc.Sections, sec)
		}
		var kept []aion.VTOLine
		for _, ln := range sec.Lines {
			if ln.Entry == nil {
				kept = append(kept, ln)
			}
		}
		sec.Lines = nil
		for i := range in.Entries {
			e := in.Entries[i]
			sec.Lines = append(sec.Lines, aion.VTOLine{Entry: &e})
		}
		sec.Lines = append(sec.Lines, kept...)
	}
	if err := s.aion.SaveVTO(doc); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func findVTOSection(d *aion.VTODoc, heading string) *aion.VTOSection {
	for _, sec := range d.Sections {
		if sec.Heading == heading {
			return sec
		}
	}
	return nil
}

func (s *Server) handleAionHiringSave(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Items []*aion.HiringItem `json:"items"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	for _, it := range b.Items {
		if strings.TrimSpace(it.Role) == "" && strings.TrimSpace(it.Candidate) == "" {
			httpError(w, errBadRequest("every hiring row needs a role or a candidate"))
			return
		}
	}
	doc := s.aion.LoadHiring()
	aion.ReplaceHiring(doc, b.Items)
	if err := s.aion.SaveHiring(doc); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionReferencesSave(w http.ResponseWriter, r *http.Request) {
	var b struct {
		References []*aion.Reference `json:"references"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	for _, ref := range b.References {
		if strings.TrimSpace(ref.URL) == "" && strings.TrimSpace(ref.Source) == "" && strings.TrimSpace(ref.Date) == "" {
			httpError(w, errBadRequest("every reference needs a url, source or date (title-only lines stay hand-edited)"))
			return
		}
	}
	doc := s.aion.LoadReferences()
	aion.ReplaceReferences(doc, b.References)
	if err := s.aion.SaveReferences(doc); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionFinancesSave(w http.ResponseWriter, r *http.Request) {
	var b map[string]string
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.aion.SetFinances(b); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionBacklogAdd(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Owner    string `json:"owner"`
		Rock     string `json:"rock"`
		Due      string `json:"due"`
		NeededBy string `json:"needed_by"`
		Source   string `json:"source"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	it := &aion.BacklogItem{
		Kind: b.Kind, Text: strings.TrimSpace(b.Title), Owner: b.Owner,
		Rock: b.Rock, Due: b.Due, NeededBy: b.NeededBy,
		Captured: time.Now().Format("2006-01-02"),
	}
	if b.Source != "" {
		it.Sources = []string{b.Source}
	}
	if err := s.aion.AddItem(it); err != nil {
		httpError(w, err)
		return
	}
	if s.aionLive != nil {
		_ = s.aionLive.refresh(true)
	}
	writeJSON(w, map[string]any{"ok": true, "id": it.ID})
}

func (s *Server) handleAionBacklogUpdate(w http.ResponseWriter, r *http.Request) {
	var b map[string]string
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	var err error
	if s.aionLive != nil {
		err = s.aionLive.ownerUpdate(r.PathValue("id"), b, time.Now())
	} else {
		err = s.aion.UpdateItem(r.PathValue("id"), b, time.Now())
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionBacklogLegacy(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.PathValue("legacy"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	r.SetPathValue("id", parts[0])
	switch parts[1] {
	case "update":
		s.handleAionBacklogUpdate(w, r)
	case "delete":
		s.handleAionBacklogDelete(w, r)
	case "decide":
		s.handleAionBacklogDecide(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAionBacklogDelete(w http.ResponseWriter, r *http.Request) {
	if s.aion == nil {
		http.Error(w, "aion not available", http.StatusServiceUnavailable)
		return
	}
	var err error
	if s.aionLive != nil {
		err = s.aionLive.ownerDelete(r.PathValue("id"), time.Now())
	} else {
		err = s.aion.DeleteItem(r.PathValue("id"))
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionBacklogDecide(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Outcome string `json:"outcome"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	var err error
	if s.aionLive != nil {
		err = s.aionLive.ownerDecide(r.PathValue("id"), b.Outcome, time.Now())
	} else {
		err = s.aion.Decide(r.PathValue("id"), b.Outcome, time.Now())
	}
	if err != nil {
		httpError(w, err)
		return
	}
	// the backlog decision's lifecycle rides the same ledger object as the
	// decision ledger's (P3): decision.decided under decision:aion-bl/<id>
	if it := s.aion.LoadBacklog().Find(r.PathValue("id")); it != nil {
		if d, ok := it.AsDecision(); ok {
			s.decisionEvent("decision.decided", d, "owner", d.Title+" — "+d.Outcome, map[string]any{"outcome": d.Outcome})
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionProposalDecide(w http.ResponseWriter, r *http.Request) {
	if s.aionLive == nil {
		http.Error(w, "Aion live state unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	store, actor := s.aionLive.teamStore()
	if store == nil {
		http.Error(w, "Aion team store unavailable", http.StatusServiceUnavailable)
		return
	}
	p, err := store.Decide(teamportal.Identity{Email: actor.Email, Name: actor.Name}, b.ID, b.Approve, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, p)
}

func (s *Server) handleAionCollaborationActivity(w http.ResponseWriter, r *http.Request) {
	item := strings.TrimSpace(r.URL.Query().Get("item"))
	if item == "" {
		httpError(w, errBadRequest("item is required"))
		return
	}
	writeJSON(w, map[string]any{"activity": s.AionActivity(item)})
}

func (s *Server) handleAionHeuristicEdit(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Statement string `json:"statement"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.aion.EditHeuristic(r.PathValue("id"), b.Statement); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionHeuristicRetire(w http.ResponseWriter, r *http.Request) {
	if err := s.aion.RetireHeuristic(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionHeuristicsMerge(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Into string `json:"into"`
		From string `json:"from"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.aion.MergeHeuristics(b.Into, b.From); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAionHeuristicsReorder(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.aion.ReorderHeuristics(b.IDs); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
