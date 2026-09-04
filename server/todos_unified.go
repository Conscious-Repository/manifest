package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/agentchat"
	"manifest/aion"
	"manifest/realestate"
	"manifest/record"
	"manifest/tasks"
)

// ---- the unified todo substrate (redesign stage 4, Revision 3) ----
//
// TODOS is a PROJECTION over three files — personal `tasks.md` lines, each
// property's `## todos` section, and the aion backlog's open tasks — with one
// id-keyed model. Composite ids route every write back to the owning file:
//
//	prop:<slug>/<line-id>  → system/realestate/properties/<slug>.md ## todos
//	aion:<hex>             → system/aion/backlog.md
//	anything else          → tasks.md
//
// Unassigned means mine; assigned-to-someone-else never reaches the TODOS
// rows — it lives on the property page and under Outstanding.

// UseOwner sets the initials that mean "me" in owner comparisons.
func (s *Server) UseOwner(initials string) { s.ownerInitials = initials }

func (s *Server) isMine(owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" || record.OwnerIsMe(owner) {
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
func (s *Server) unifiedRows(doc *tasks.Doc, now time.Time) []unifiedRow {
	var rows []unifiedRow
	if doc != nil {
		for _, dom := range doc.Domains {
			c := unifiedContainer{Kind: "domain", Name: dom.Name}
			dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) {
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
				for _, t := range p.Tasks {
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

// unifiedView is the stage-4 addition to the /api/tasks payload.
func (s *Server) unifiedView(doc *tasks.Doc) map[string]any {
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
		"counts":      map[string]int{"tasks": len(mine), "outstanding": outstandingTotal},
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
			// Aliases are other owner keys that mean this same assignee — a
			// partner who is also a vendor carries [contractor:: <slug>] on her
			// people.md row, so work owned as the person AND work owned as the
			// contractor collect under one entry (owner call 2026-08-19).
			Aliases []string `json:"aliases,omitempty"`
		}
		var list []ctr
		claimed := map[string]bool{} // contractor slugs a person already speaks for
		// the curated RE people registry — <reRoot>/people.md, aion-people
		// grammar, kept SEPARATE from the aion roster (owner call 2026-08-09)
		if s.index != nil && s.realestateRoot != "" {
			raw, err := os.ReadFile(filepath.Join(s.index.VaultRoot(), filepath.FromSlash(s.realestateRoot), "people.md"))
			if err == nil {
				for _, p := range aion.ParsePeople(string(raw)).People() {
					row := ctr{Slug: p.Initials, Name: p.Name, Trade: p.Role}
					for _, f := range p.Unknown {
						if strings.EqualFold(f.Key, "contractor") {
							if v := strings.TrimSpace(f.Value); v != "" {
								row.Aliases = append(row.Aliases, v)
								claimed[strings.ToLower(v)] = true
							}
						}
					}
					list = append(list, row)
				}
			}
		}
		for _, e := range s.realestate.Contractors() {
			if claimed[strings.ToLower(e.Slug)] {
				continue // the person row above already speaks for this vendor
			}
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
func (s *Server) containerList(doc *tasks.Doc) []unifiedContainer {
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

// propTaskMutate loads a property's rock tree, applies fn, writes the section
// back under the realestate capability, and reindexes. fn returns false when
// the line is missing.
func (s *Server) propTaskMutate(w http.ResponseWriter, slug string, fn func(list *realestate.PropertyTaskList) (bool, error)) bool {
	if s.realestate == nil || s.vault == nil {
		http.Error(w, "properties not available", http.StatusServiceUnavailable)
		return false
	}
	list, rel, ok := s.realestate.LoadTasks(slug)
	if !ok {
		http.Error(w, "property not found", http.StatusNotFound)
		return false
	}
	found, err := fn(list)
	if err != nil {
		httpError(w, err)
		return false
	}
	if !found {
		http.Error(w, "todo not found", http.StatusNotFound)
		return false
	}
	if err := s.vault.ReplaceSectionCap("realestate", rel, list.Section, list.Emit()); err != nil {
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
//
// Agent-chat plan (2026-09-04, Q2/Q5): the Hermes do-bot is addressed as
// `agent:alfred` (display "Alfred"; `agent:hermes` stays a resolving alias
// for existing data), every non-default Hermes profile is `agent:<profile>`,
// and the team-surface agents (kairos, zeck) are addressable from the
// personal board too — listed after Alfred, never the default.
func (s *Server) rosterFor(surface string) []map[string]any {
	out := []map[string]any{}
	var intents []string
	for k, p := range s.personas() {
		if p.Enabled {
			intents = append(intents, k)
		}
	}
	sort.Strings(intents)
	row := func(id, name, harness string) map[string]any {
		r := map[string]any{"id": id, "name": name, "harness": harness}
		if len(intents) > 0 {
			r["personas"] = intents
		}
		return r
	}
	hs := s.eachHarness()
	var others []map[string]any // team-surface agents, personal-board tail (Q5)
	for i, h := range hs {
		if i == 0 { // the primary runs the house; delegation targets are the rest
			continue
		}
		name := h.Name
		if name == "" {
			continue
		}
		display := strings.ToUpper(name[:1]) + name[1:]
		if strings.EqualFold(name, "hermes") {
			if surface == "" {
				out = append(out, row("agent:"+alfredAgent, "Alfred", "hermes"))
			}
			continue
		}
		switch {
		case h.Surface == surface:
			out = append(out, row("agent:"+name, display, name))
		case surface == "":
			others = append(others, row("agent:"+name, display, name))
		}
	}
	// the virtual Hermes: the runner-backed do-bot identity, offered on the
	// personal surface even with no `hermes` harness tree (Phase 1c retired it).
	if surface == "" && s.hermesEnabled() && !s.hermesRealHarness() {
		out = append(out, row("agent:"+alfredAgent, "Alfred", "hermes"))
	}
	if surface == "" {
		for _, p := range s.hermesProfileNames() {
			out = append(out, row("agent:"+p, strings.ToUpper(p[:1])+p[1:], "hermes"))
		}
		out = append(out, others...)
	}
	return out
}

// hermesProfileNames lists the non-default Hermes profiles (cached 30s in the
// agent-chat layer). Empty when the runner or the chat store is not wired —
// the roster then carries Alfred alone.
func (s *Server) hermesProfileNames() []string {
	if s.agentChat == nil || !s.hermesEnabled() {
		return nil
	}
	profiles, _ := s.hermesProfilesCached(context.Background())
	var out []string
	for _, p := range profiles {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if name == "" || name == "default" || name == alfredAgent || name == "hermes" {
			continue
		}
		if !agentchat.ValidAgent(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// hermesProfileOf maps an agent token to the Hermes profile its turns run
// under: `agent:<profile>` → the profile; alfred/hermes → "" (the default).
func (s *Server) hermesProfileOf(token string) string {
	want := strings.TrimPrefix(token, "agent:")
	if want == alfredAgent || want == "hermes" {
		return ""
	}
	for _, p := range s.hermesProfileNames() {
		if p == want {
			return p
		}
	}
	return ""
}

// defaultAgentToken is the "hey agent" default: Alfred when the do-bot
// resolves, else the first roster entry ("" when nobody is assignable).
func (s *Server) defaultAgentToken() string {
	if s.agentHarness("agent:"+alfredAgent) != "" {
		return "agent:" + alfredAgent
	}
	for _, r := range s.agentRoster() {
		if id, _ := r["id"].(string); id != "" {
			return id
		}
	}
	return ""
}

// agentDisplayName renders an agent token for humans: alfred/hermes → Alfred,
// anything else capitalized.
func agentDisplayName(token string) string {
	name := strings.TrimPrefix(token, "agent:")
	if name == "" {
		return ""
	}
	if name == alfredAgent || name == "hermes" {
		return "Alfred"
	}
	return strings.ToUpper(name[:1]) + name[1:]
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
	if want == "" || strings.Contains(want, "::") {
		return ""
	}
	// Alfred is the do-bot (alias of hermes); profiles run on the same runner.
	if want == alfredAgent || s.hermesProfileOf(owner) != "" {
		want = "hermes"
	}
	hs := s.eachHarness()
	for i := range hs {
		if i > 0 && hs[i].Name == want {
			return want
		}
	}
	// the virtual Hermes resolves even without a harness tree (Phase 1c).
	if s.hermesEnabled() && want == "hermes" {
		return "hermes"
	}
	return ""
}

// propTaskPin freezes a property line's within-file id as an explicit
// [todo:: id] pin (plan D1 — the non-HTTP sibling of propTaskMutate). The
// composite `prop:<slug>/<lineID>` then survives rewording. Idempotent.
func (s *Server) propTaskPin(slug, lineID string) bool {
	if s.realestate == nil || s.vault == nil {
		return false
	}
	list, rel, ok := s.realestate.LoadTasks(slug)
	if !ok {
		return false
	}
	n := list.Find(lineID)
	if n == nil {
		return false
	}
	if n.Task.ExplicitID() != "" {
		return true // already pinned — no write
	}
	n.Task.PinID(lineID)
	if err := s.vault.ReplaceSectionCap("realestate", rel, list.Section, list.Emit()); err != nil {
		return false
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	return true
}

// propTaskCheck completes/reopens a property task. The task IS a tree node
// (overhaul §6) — checking it moves the rock's Ready/Recognized state
// directly; the Rev-3 dual-stamp is gone because there is only one line.
func (s *Server) propTaskCheck(w http.ResponseWriter, id string, checked bool) {
	slug, lineID := splitPropID(id)
	if slug == "" {
		httpError(w, errBadRequest("malformed property todo id"))
		return
	}
	ok := s.propTaskMutate(w, slug, func(list *realestate.PropertyTaskList) (bool, error) {
		n := list.Find(lineID)
		if n == nil {
			return false, nil
		}
		n.Task.Checked = checked
		if checked {
			n.Task.Done = time.Now().Format("2006-01-02")
		} else {
			n.Task.Done = ""
		}
		return true, nil
	})
	if !ok {
		return
	}
	writeJSON(w, s.tasksView())
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

func (s *Server) backlogTaskCheck(w http.ResponseWriter, id string, checked bool) {
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
	writeJSON(w, s.tasksView())
}

// ---- drag-to-rank: the full projected order in, ONE write per touched file ----

func (s *Server) handleTasksRank(w http.ResponseWriter, r *http.Request) {
	if !s.tasksOK(w) {
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
		doc, err := s.tasksStore.Load()
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
			if err := s.tasksStore.Save(doc); err != nil {
				httpError(w, err)
				return
			}
		}
	}
	for slug, ranks := range propRanks {
		list, rel, ok := s.realestate.LoadTasks(slug)
		if !ok {
			continue
		}
		changed := false
		for lineID, rank := range ranks {
			if n := list.Find(lineID); n != nil && n.Task.Rank != rank {
				n.Task.Rank = rank
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := s.vault.ReplaceSectionCap("realestate", rel, list.Section, list.Emit()); err != nil {
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
	writeJSON(w, s.tasksView())
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
