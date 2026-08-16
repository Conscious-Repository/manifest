package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/aion"
	"manifest/realestate"
	"manifest/todos"
)

// ---- the unified todo substrate (redesign stage 4, Revision 3) ----
//
// TODOS is a PROJECTION over three files — personal `to do.md` lines, each
// property's `## todos` section, and the aion backlog's open tasks — with one
// id-keyed model. Composite ids route every write back to the owning file:
//
//	prop:<slug>/<line-id>  → system/realestate/properties/<slug>.md ## todos
//	aion:<hex>             → system/aion/backlog.md
//	anything else          → to do.md (backward compatible)
//
// Unassigned means mine; assigned-to-someone-else never reaches the TODOS
// rows — it lives on the property page and under Outstanding.

// UseOwner sets the initials that mean "me" in owner comparisons.
func (s *Server) UseOwner(initials string) { s.ownerInitials = initials }

func (s *Server) isMine(owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" || strings.EqualFold(owner, "me") {
		return true
	}
	// agent-assigned stays MINE (todo-panel plan D5): the agent works it, the
	// human owns it — the row keeps its place, the delegation chip carries state
	if strings.HasPrefix(owner, "agent:") {
		return true
	}
	me := strings.ToUpper(strings.TrimSpace(s.ownerInitials))
	if me == "" {
		return false
	}
	for _, tok := range strings.Split(owner, "/") {
		if strings.ToUpper(strings.TrimSpace(tok)) == me {
			return true
		}
	}
	return false
}

type unifiedContainer struct {
	Kind string `json:"kind"` // domain | property | aion
	Slug string `json:"slug,omitempty"`
	Name string `json:"name"`
}

type unifiedRow struct {
	ID        string           `json:"id"`
	Text      string           `json:"text"`
	Owner     string           `json:"owner,omitempty"`
	Rank      int              `json:"rank,omitempty"`
	Added     string           `json:"added,omitempty"`
	Rock      string           `json:"rock,omitempty"`
	Source    string           `json:"source"` // personal | property | aion
	Container unifiedContainer `json:"container"`
	AgeDays   int              `json:"ageDays,omitempty"`
	Waiting   string           `json:"waiting,omitempty"` // [waiting:: who] (personal) — the board's Waiting column
	// Delegation (Phase 6): derived from harness traces carrying [todo:: id].
	Delegation *delegationView `json:"delegation,omitempty"`
}

type outstandingGroup struct {
	Container unifiedContainer `json:"container"`
	Count     int              `json:"count"`
	Items     []unifiedRow     `json:"items"`
}

// unifiedRows collects every OPEN item across the three sources, unfiltered —
// callers split it into the me-projection and the Outstanding grouping.
func (s *Server) unifiedRows(doc *todos.Doc, now time.Time) []unifiedRow {
	var rows []unifiedRow
	if doc != nil {
		for _, dom := range doc.Domains {
			c := unifiedContainer{Kind: "domain", Name: dom.Name}
			dom.AllTodos(func(_ *todos.Bucket, t *todos.Todo) {
				if t.Checked {
					return
				}
				rows = append(rows, unifiedRow{
					ID: t.ID, Text: t.Text, Owner: t.Owner, Rank: t.RankN(),
					Added: t.Added, Rock: t.Rock, Source: "personal",
					Container: c, AgeDays: t.AgeDays(now), Waiting: t.Waiting,
				})
			})
		}
	}
	if s.realestate != nil {
		if props, err := s.realestate.Properties(); err == nil {
			for _, p := range props {
				c := unifiedContainer{Kind: "property", Slug: p.Slug, Name: orStr(p.Short, p.Name)}
				for _, t := range p.Todos {
					if t.Checked {
						continue
					}
					rows = append(rows, unifiedRow{
						ID: "prop:" + p.Slug + "/" + t.ID, Text: t.Text, Owner: t.Owner,
						Rank: t.RankN(), Added: t.Added, Source: "property",
						Container: c, AgeDays: t.AgeDays(now),
					})
				}
			}
		}
	}
	// the two DOMAIN backlogs — aion and its RE mirror — project identically
	// (owner report 2026-08-15: RE tasks were missing from the board; the RE
	// domain build shipped the store but never this projection)
	backlogs := []struct {
		store  *aion.Store
		prefix string
		c      unifiedContainer
		source string
	}{
		{s.aion, "aion:", unifiedContainer{Kind: "aion", Name: "Aion"}, "aion"},
		{s.re, "re:", unifiedContainer{Kind: "re", Name: "Real Estate"}, "realestate"},
	}
	for _, b := range backlogs {
		if b.store == nil {
			continue
		}
		for _, it := range b.store.LoadBacklog().Items() {
			if it.Kind != aion.KindTask || it.Checked ||
				(it.Status != aion.StatusOpen && it.Status != aion.StatusInProgress && it.Status != "") {
				continue
			}
			rank, _ := strconv.Atoi(it.Rank)
			rows = append(rows, unifiedRow{
				ID: b.prefix + it.ID, Text: it.Text, Owner: it.Owner, Rank: rank,
				Added: it.Captured, Rock: it.Rock, Source: b.source, Container: b.c,
			})
		}
	}
	// Phase 6: attach the delegation projection (one trace scan per request)
	deleg := s.delegationIndex()
	for i := range rows {
		if d, ok := deleg[rows[i].ID]; ok {
			rows[i].Delegation = &d
		}
	}
	return rows
}

// sortRows: ranked first (rank asc), then oldest-added first, stable.
func sortRows(rows []unifiedRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := rows[i].Rank, rows[j].Rank
		if (ri > 0) != (rj > 0) {
			return ri > 0
		}
		if ri > 0 && rj > 0 && ri != rj {
			return ri < rj
		}
		ai, aj := rows[i].Added, rows[j].Added
		if ai != aj {
			if ai == "" {
				return false
			}
			if aj == "" {
				return true
			}
			return ai < aj
		}
		return false
	})
}

// unifiedView is the stage-4 addition to the /api/todos payload.
func (s *Server) unifiedView(doc *todos.Doc) map[string]any {
	now := time.Now()
	all := s.unifiedRows(doc, now)
	var mine []unifiedRow
	groups := map[string]*outstandingGroup{}
	var groupOrder []string
	for _, r := range all {
		if s.isMine(r.Owner) {
			mine = append(mine, r)
			continue
		}
		key := r.Container.Kind + ":" + r.Container.Slug + ":" + r.Container.Name
		g, ok := groups[key]
		if !ok {
			g = &outstandingGroup{Container: r.Container}
			groups[key] = g
			groupOrder = append(groupOrder, key)
		}
		g.Items = append(g.Items, r)
		g.Count++
	}
	sortRows(mine)
	outstanding := []outstandingGroup{}
	outstandingTotal := 0
	for _, key := range groupOrder {
		outstanding = append(outstanding, *groups[key])
		outstandingTotal += groups[key].Count
	}
	if mine == nil {
		mine = []unifiedRow{}
	}
	return map[string]any{
		"me":          s.ownerInitials,
		"rows":        mine,
		"outstanding": outstanding,
		"counts":      map[string]int{"todos": len(mine), "outstanding": outstandingTotal},
		"assignees":   s.assigneeLists(),
		"containers":  s.containerList(doc),
		// delegation state keyed by todo id — so a DONE todo (absent from the
		// open rows) can still show its "view result" chip (Phase 6 result
		// visibility, owner ask 2026-08-11)
		"delegations": s.delegationIndex(),
	}
}

// assigneeLists: real identities only (redesign Rev 3) — the aion people
// registry and the real-estate contractor records. Never free-text.
func (s *Server) assigneeLists() map[string]any {
	out := map[string]any{}
	if s.aion != nil {
		type person struct {
			Initials string `json:"initials"`
			Name     string `json:"name"`
		}
		var people []person
		for _, p := range s.aion.LoadPeople().People() {
			people = append(people, person{Initials: p.Initials, Name: p.Name})
		}
		out["aion"] = people
	}
	if s.realestate != nil {
		type ctr struct {
			Slug  string `json:"slug"`
			Name  string `json:"name"`
			Trade string `json:"trade,omitempty"`
		}
		var list []ctr
		// the curated RE people registry — <reRoot>/people.md, aion-people
		// grammar, kept SEPARATE from the aion roster (owner call 2026-08-09)
		if s.index != nil && s.realestateRoot != "" {
			raw, err := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(s.realestateRoot), "people.md"))
			if err == nil {
				for _, p := range aion.ParsePeople(string(raw)).People() {
					list = append(list, ctr{Slug: p.Initials, Name: p.Name, Trade: p.Role})
				}
			}
		}
		for _, e := range s.realestate.Contractors() {
			list = append(list, ctr{Slug: e.Slug, Name: e.Name, Trade: e.Trade})
		}
		out["realestate"] = list
	}
	if agents := s.agentRoster(); len(agents) > 0 {
		out["agents"] = agents
	}
	return out
}

// containerList feeds the add-picker: personal domains, aion, every property.
func (s *Server) containerList(doc *todos.Doc) []unifiedContainer {
	var out []unifiedContainer
	if doc != nil {
		for _, dom := range doc.Domains {
			out = append(out, unifiedContainer{Kind: "domain", Name: dom.Name})
		}
	}
	if s.aion != nil {
		out = append(out, unifiedContainer{Kind: "aion", Name: "Aion"})
	}
	if s.re != nil {
		out = append(out, unifiedContainer{Kind: "re", Name: "Real Estate"})
	}
	if s.realestate != nil {
		if props, err := s.realestate.Properties(); err == nil {
			for _, p := range props {
				out = append(out, unifiedContainer{Kind: "property", Slug: p.Slug, Name: orStr(p.Short, p.Name)})
			}
		}
	}
	return out
}

func orStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// ---- composite-id write routing ----

// splitPropID parses "prop:<slug>/<line-id>" ("", "" when malformed).
func splitPropID(id string) (slug, lineID string) {
	rest := strings.TrimPrefix(id, "prop:")
	i := strings.Index(rest, "/")
	if i <= 0 || i == len(rest)-1 {
		return "", ""
	}
	return rest[:i], rest[i+1:]
}

// propTodoMutate loads a property's `## todos`, applies fn, writes the section
// back under the realestate capability, and reindexes. fn returns false when
// the line is missing.
func (s *Server) propTodoMutate(w http.ResponseWriter, slug string, fn func(list realestate.PropertyTodoList) (realestate.PropertyTodoList, bool, error)) bool {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return false
	}
	list, rel, ok := s.realestate.LoadTodos(slug)
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return false
	}
	next, found, err := fn(list)
	if err != nil {
		httpError(w, err)
		return false
	}
	if !found {
		http.Error(w, "todo not found", http.StatusNotFound)
		return false
	}
	if err := s.vault.ReplaceSectionCap("realestate", rel, "todos", realestate.EmitPropertyTodos(next)); err != nil {
		httpError(w, err)
		return false
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	return true
}

// rosterFor — the assignable agents (todo-panel plan D5): every configured
// non-primary harness matching the surface, ids carrying the hard `agent:`
// prefix so the raw markdown token `[owner:: agent:hermes]` is visually
// unambiguous. Each row carries the enabled persona intents (persona plan
// Phase 1) so the mention typeahead can offer `agent:hermes::brief` variants.
func (s *Server) rosterFor(surface string) []map[string]any {
	out := []map[string]any{}
	var intents []string
	for k, p := range s.personas() {
		if p.Enabled {
			intents = append(intents, k)
		}
	}
	sort.Strings(intents)
	hs := s.eachHarness()
	for i, h := range hs {
		if i == 0 { // the primary runs the house; delegation targets are the rest
			continue
		}
		if h.Surface != surface {
			continue
		}
		name := h.Name
		if name == "" {
			continue
		}
		display := strings.ToUpper(name[:1]) + name[1:]
		row := map[string]any{"id": "agent:" + name, "name": display, "harness": name}
		if len(intents) > 0 {
			row["personas"] = intents
		}
		out = append(out, row)
	}
	return out
}

// agentRoster is the PERSONAL surface's roster (team-surface harnesses like
// kairos never appear in dashboard typeaheads — kairos plan).
func (s *Server) agentRoster() []map[string]any { return s.rosterFor("") }

// teamAgentRoster is the AION portal's roster (surface "team").
func (s *Server) teamAgentRoster() []map[string]any { return s.rosterFor("team") }

// agentHarness resolves a BARE `agent:` owner token to its harness name
// ("" = not a known agent; intent-suffixed tokens are deliberately not
// accepted — split them with splitAgentToken first). Resolution spans ALL
// non-primary harnesses regardless of surface — relays, ingestion and portal
// assigns must recognize team-scoped agents too.
func (s *Server) agentHarness(owner string) string {
	if !strings.HasPrefix(owner, "agent:") {
		return ""
	}
	want := strings.TrimPrefix(owner, "agent:")
	hs := s.eachHarness()
	for i := range hs {
		if i > 0 && hs[i].Name == want {
			return want
		}
	}
	return ""
}

// propTodoPin freezes a property line's within-file id as an explicit
// [todo:: id] pin (plan D1 — the non-HTTP sibling of propTodoMutate). The
// composite `prop:<slug>/<lineID>` then survives rewording. Idempotent.
func (s *Server) propTodoPin(slug, lineID string) bool {
	if s.realestate == nil || s.vault == nil {
		return false
	}
	list, rel, ok := s.realestate.LoadTodos(slug)
	if !ok {
		return false
	}
	t := list.Find(lineID)
	if t == nil {
		return false
	}
	if t.ExplicitID() != "" {
		return true // already pinned — no write
	}
	t.PinID(lineID)
	if err := s.vault.ReplaceSectionCap("realestate", rel, "todos", realestate.EmitPropertyTodos(list)); err != nil {
		return false
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	return true
}

// propTodoCheck completes/reopens a property todo. A [work:: id] back-tether
// DUAL-STAMPS the matching `## work` line so stage Ready/Recognized money
// stays truthful (the one place Rev 3 touches the accounting model).
func (s *Server) propTodoCheck(w http.ResponseWriter, id string, checked bool) {
	slug, lineID := splitPropID(id)
	if slug == "" {
		httpError(w, errBadRequest("malformed property todo id"))
		return
	}
	var workTether string
	ok := s.propTodoMutate(w, slug, func(list realestate.PropertyTodoList) (realestate.PropertyTodoList, bool, error) {
		t := list.Find(lineID)
		if t == nil {
			return list, false, nil
		}
		t.Checked = checked
		if checked {
			t.Done = time.Now().Format("2006-01-02")
		} else {
			t.Done = ""
		}
		workTether = t.FieldValue("work")
		return list, true, nil
	})
	if !ok {
		return
	}
	if workTether != "" {
		if p, found := s.realestate.Get(slug); found {
			stages := p.Work
			changed := false
			for i := range stages {
				for j := range stages[i].Todos {
					if stages[i].Todos[j].ID == workTether && stages[i].Todos[j].Checked != checked {
						stages[i].Todos[j].Checked = checked
						changed = true
					}
				}
			}
			if changed {
				if err := s.vault.ReplaceSectionCap("realestate", p.Path, "work", realestate.EmitWork(stages)); err != nil {
					httpError(w, err)
					return
				}
				if s.index != nil {
					_ = s.index.ReindexPaths([]string{p.Path})
				}
			}
		}
	}
	writeJSON(w, s.todosView())
}

// backlogStoreFor maps a composite backlog id (aion:<id> / re:<id>) to its
// store — the RE domain is an AION mirror, so one router serves both.
func (s *Server) backlogStoreFor(id string) (*aion.Store, string, bool) {
	switch {
	case strings.HasPrefix(id, "aion:"):
		return s.aion, strings.TrimPrefix(id, "aion:"), s.aion != nil
	case strings.HasPrefix(id, "re:"):
		return s.re, strings.TrimPrefix(id, "re:"), s.re != nil
	}
	return nil, id, false
}

func (s *Server) backlogTodoCheck(w http.ResponseWriter, id string, checked bool) {
	store, bare, ok := s.backlogStoreFor(id)
	if !ok {
		http.Error(w, "backlog not available", http.StatusServiceUnavailable)
		return
	}
	status := aion.StatusOpen
	if checked {
		status = aion.StatusDone
	}
	if err := store.UpdateItem(bare, map[string]string{"status": status}, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.todosView())
}

// ---- drag-to-rank: the full projected order in, ONE write per touched file ----

func (s *Server) handleTodosRank(w http.ResponseWriter, r *http.Request) {
	if !s.todosOK(w) {
		return
	}
	var b struct {
		Order []string `json:"order"`
	}
	if err := decode(r, &b); err != nil || len(b.Order) == 0 {
		httpError(w, errBadRequest("order is required"))
		return
	}
	personal := map[string]string{}
	aionRanks := map[string]string{}
	reRanks := map[string]string{}
	propRanks := map[string]map[string]string{} // slug → lineID → rank
	for i, id := range b.Order {
		rank := strconv.Itoa(i + 1)
		switch {
		case strings.HasPrefix(id, "prop:"):
			slug, lineID := splitPropID(id)
			if slug == "" {
				continue
			}
			if propRanks[slug] == nil {
				propRanks[slug] = map[string]string{}
			}
			propRanks[slug][lineID] = rank
		case strings.HasPrefix(id, "aion:"):
			aionRanks[strings.TrimPrefix(id, "aion:")] = rank
		case strings.HasPrefix(id, "re:"):
			reRanks[strings.TrimPrefix(id, "re:")] = rank
		default:
			personal[id] = rank
		}
	}
	if len(personal) > 0 {
		doc, err := s.todosStore.Load()
		if err != nil {
			httpError(w, err)
			return
		}
		changed := false
		for id, rank := range personal {
			if _, t := doc.Find(id); t != nil && t.Rank != rank {
				t.Rank = rank
				changed = true
			}
		}
		if changed {
			if err := s.todosStore.Save(doc); err != nil {
				httpError(w, err)
				return
			}
		}
	}
	for slug, ranks := range propRanks {
		list, rel, ok := s.realestate.LoadTodos(slug)
		if !ok {
			continue
		}
		changed := false
		for lineID, rank := range ranks {
			if t := list.Find(lineID); t != nil && t.Rank != rank {
				t.Rank = rank
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := s.vault.ReplaceSectionCap("realestate", rel, "todos", realestate.EmitPropertyTodos(list)); err != nil {
			httpError(w, err)
			return
		}
		if s.index != nil {
			_ = s.index.ReindexPaths([]string{rel})
		}
	}
	if s.re != nil && len(reRanks) > 0 {
		if err := s.re.SetRanks(reRanks); err != nil {
			httpError(w, err)
			return
		}
	}
	if s.aion != nil && len(aionRanks) > 0 {
		if err := s.aion.SetRanks(aionRanks); err != nil {
			httpError(w, err)
			return
		}
	}
	writeJSON(w, s.todosView())
}

// ---- RE people registry: <reRoot>/people.md, aion-people grammar ----
// The Settings page's PEOPLE table — the curated assignee roster for property
// todos, kept separate from the aion roster. Full-row replace like the aion
// registries; verbatim non-person lines survive (ReplacePeople contract).

func (s *Server) rePeopleRel() string {
	return filepath.ToSlash(filepath.Join(s.realestateRoot, "people.md"))
}

func (s *Server) handleRePeopleGet(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.index == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	raw, _ := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(s.rePeopleRel())))
	people := aion.ParsePeople(string(raw)).People()
	if people == nil {
		people = []*aion.Person{}
	}
	writeJSON(w, map[string]any{"people": people, "rel": s.rePeopleRel()})
}

func (s *Server) handleRePeopleSave(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.index == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
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
	rel := s.rePeopleRel()
	raw, _ := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(rel)))
	doc := aion.ParsePeople(string(raw))
	aion.ReplacePeople(doc, b.People)
	if err := s.vault.WriteCap("realestate", rel, []byte(aion.SerializePeople(doc))); err != nil {
		httpError(w, err)
		return
	}
	_ = s.index.ReindexPaths([]string{rel})
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- one-time migration: open `## work` todos → `## todos` lines ----
// GET previews per property; POST commits. `## work` bytes untouched; each
// copied line carries its [work:: id] back-tether (idempotent — tethered
// work ids are skipped on re-run).

func (s *Server) handlePropTodosMigrate(w http.ResponseWriter, r *http.Request) {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return
	}
	props, err := s.realestate.Properties()
	if err != nil {
		httpError(w, err)
		return
	}
	type report struct {
		Slug  string   `json:"slug"`
		Name  string   `json:"name"`
		Added []string `json:"added"`
	}
	now := time.Now()
	switch r.Method {
	case http.MethodGet:
		var out []report
		for _, p := range props {
			list, _, ok := s.realestate.LoadTodos(p.Slug)
			if !ok {
				continue
			}
			if _, added := realestate.MigrateWorkTodos(p, list, now); len(added) > 0 {
				out = append(out, report{Slug: p.Slug, Name: orStr(p.Short, p.Name), Added: added})
			}
		}
		writeJSON(w, map[string]any{"properties": out})
	case http.MethodPost:
		var b struct {
			Slugs []string `json:"slugs"` // empty = every property
		}
		_ = decode(r, &b)
		want := map[string]bool{}
		for _, sl := range b.Slugs {
			want[sl] = true
		}
		var out []report
		for _, p := range props {
			if len(want) > 0 && !want[p.Slug] {
				continue
			}
			list, rel, ok := s.realestate.LoadTodos(p.Slug)
			if !ok {
				continue
			}
			next, added := realestate.MigrateWorkTodos(p, list, now)
			if len(added) == 0 {
				continue
			}
			if err := s.vault.ReplaceSectionCap("realestate", rel, "todos", realestate.EmitPropertyTodos(next)); err != nil {
				httpError(w, err)
				return
			}
			if s.index != nil {
				_ = s.index.ReindexPaths([]string{rel})
			}
			out = append(out, report{Slug: p.Slug, Name: orStr(p.Short, p.Name), Added: added})
		}
		writeJSON(w, map[string]any{"migrated": out})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
