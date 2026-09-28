package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/graph"
	"manifest/ledger"
	"manifest/recruiting"
)

// NETWORK — the top-level tab where the owner builds the people around AION:
// future hires, advisors, experts to consult, connectors (owner decisions
// 2026-09-27). List-first, because building a network is scanning, tagging
// and noting; the graph is a second view of the same people.
//
// Four registries meet here and NOTHING is inferred between them
// (ARCHITECTURE §9 — contacts are firewalled from business records and join
// only by an explicit link):
//
//	kept rows    network/people.md — the only EDITABLE tier; a row absorbs a
//	             contact by its `ref` and a team member by its `team` link
//	contacts     the vault's person notes, read-only, drawn as they are
//	investors    fundraising PersonRefs — already contact keys, so an investor
//	             lands on their contact (or the row that refs it)
//	team         system/aion/people.md, by initials
//
// Two people with the same name in two registries stay TWO rows until the
// owner links them. A wrong merge writes one person's history onto another;
// a missed merge costs one click.

// NetPerson is one resolved human as the Network list draws them.
type NetPerson struct {
	ID          string   `json:"id"` // aion-net/… (editable) · contact/<key> · team/<initials>
	Name        string   `json:"name"`
	Kind        string   `json:"kind,omitempty"`
	Org         string   `json:"org,omitempty"`
	Title       string   `json:"title,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Suggest     []string `json:"suggest,omitempty"` // source topics not yet tags
	// Topics are what a SOURCE said the person knows (kept from a sweep) —
	// shown as their experience, marked as the source's, beside your tags.
	Topics []string `json:"topics,omitempty"`
	// Pending says the person is NOT one of your people yet, and where they
	// came from: "recruiting" (saved from a sweep), "fundraising" (an
	// investor or registry name), "notes" (a name from meeting notes). ONE
	// rule decides it: your people have a contact note, or are on the team.
	// "make them a contact" (the note) is the only way in. "" = your people.
	Pending string `json:"pending,omitempty"`
	origin  string // the contacts layer's origin for a note-less contact
	Note        string   `json:"note,omitempty"`
	LastContact string   `json:"lastContact,omitempty"` // the owner's own date
	LastMet     string   `json:"lastMet,omitempty"`     // the calendar's, when a contact is linked
	// the Contacts signals (contacts.Contact), copied on read for linked contacts
	LastMentioned string   `json:"lastMentioned,omitempty"`
	HasNote       bool     `json:"hasNote,omitempty"`
	Location      string   `json:"location,omitempty"`
	Upcoming      string   `json:"upcoming,omitempty"`
	OpenLoops     int      `json:"openLoops,omitempty"`
	Cold          bool     `json:"cold,omitempty"`
	DaysSince     int      `json:"daysSince,omitempty"`
	MedianGap     int      `json:"medianGap,omitempty"`
	NeglectBasis  string   `json:"neglectBasis,omitempty"`
	Sources       []string `json:"sources"`         // kept · contact · investor · team
	Firms         []string `json:"firms,omitempty"` // fundraising opportunities they sit on
	Role          string   `json:"role,omitempty"`  // their AION team role
	Editable      bool     `json:"editable"`        // a kept row exists
	ContactKey    string   `json:"contactKey,omitempty"`
	NotePath      string   `json:"notePath,omitempty"`
	TeamLink      string   `json:"team,omitempty"`
	Consent       string   `json:"consent,omitempty"` // "owner" = someone you'd ask (an intro origin)
	Source        string   `json:"source,omitempty"`  // where a kept row came from
	SourceRef     string   `json:"sourceRef,omitempty"`
	GitHub        string   `json:"github,omitempty"`
	LinkedIn      string   `json:"linkedin,omitempty"`
	ORCID         string   `json:"orcid,omitempty"`
	Archived      string   `json:"archived,omitempty"`
	// SameName are the OTHER rows with exactly this name that an explicit
	// link could join — offered for the owner to confirm, never merged
	// (§9: a wrong merge writes one person's history onto another).
	SameName []NetAlias `json:"sameName,omitempty"`
}

// NetAlias is one same-name row, and the id to open it by.
type NetAlias struct {
	ID      string   `json:"id"`
	Sources []string `json:"sources"`
}

func addSource(p *NetPerson, s string) {
	for _, have := range p.Sources {
		if have == s {
			return
		}
	}
	p.Sources = append(p.Sources, s)
}

// networkPeople resolves the four registries into one row per human.
func (s *Server) networkPeople(now time.Time) []*NetPerson {
	var out []*NetPerson
	byContact := map[string]*NetPerson{} // lowercased contact key → row
	byTeam := map[string]*NetPerson{}    // uppercased initials → row

	// 1 — kept rows: the editable tier, and the owner of every explicit link
	var kept []recruiting.NetworkPerson
	if s.recruiting != nil {
		kept = s.recruiting.Connectors()
	}
	for _, p := range kept {
		np := &NetPerson{
			ID: p.ID, Name: p.Name, Kind: p.Type, Org: p.Org, Title: p.Title,
			Tags: p.Tags, Suggest: untagged(p.Topics, p.Tags), Topics: p.Topics, Note: p.Note, LastContact: p.LastContact, Editable: true,
			ContactKey: p.Ref, TeamLink: p.Team, Consent: p.Consent, Source: p.Source,
			SourceRef: p.SourceRef, GitHub: p.GitHub, LinkedIn: p.LinkedIn, ORCID: p.ORCID,
			Archived: p.Archived, Sources: []string{"kept"},
		}
		out = append(out, np)
		if k := strings.ToLower(strings.TrimSpace(p.Ref)); k != "" {
			byContact[k] = np
		}
		if t := strings.ToUpper(strings.TrimSpace(p.Team)); t != "" {
			byTeam[t] = np
		}
	}

	// 2 — contacts (read-only unless a row links them)
	if s.contacts != nil {
		if list, err := s.contacts.List(now); err == nil {
			for _, c := range list {
				k := strings.ToLower(strings.TrimSpace(c.Key))
				if k == "" {
					continue
				}
				np, ok := byContact[k]
				if !ok {
					np = &NetPerson{ID: "contact/" + c.Key, Name: c.Display, Sources: []string{}}
					byContact[k] = np
					out = append(out, np)
				}
				addSource(np, "contact")
				np.ContactKey, np.NotePath, np.LastMet = c.Key, c.NotePath, c.LastMet
				// the Contacts signals, as Contacts computes them (read-only)
				np.LastMentioned, np.HasNote, np.Location = c.LastMentioned, c.HasNote, c.Location
				np.Upcoming, np.OpenLoops, np.Cold = c.Upcoming, c.OpenLoops, c.Cold
				np.DaysSince, np.MedianGap, np.NeglectBasis = c.DaysSince, c.MedianGap, c.NeglectBasis
				np.origin = c.Origin
			}
		}
	}

	// 3 — investors: fundraising people are contact keys already
	if s.fundraising != nil {
		if opps, err := s.fundraising.List(); err == nil {
			for _, o := range opps {
				for _, pr := range o.People {
					k := strings.ToLower(strings.TrimSpace(pr.Key))
					if k == "" {
						continue
					}
					np, ok := byContact[k]
					if !ok {
						np = &NetPerson{ID: "contact/" + pr.Key, Name: pr.Display, ContactKey: pr.Key,
							NotePath: pr.NotePath, HasNote: pr.NotePath != "", Sources: []string{}}
						byContact[k] = np
						out = append(out, np)
					}
					addSource(np, "investor")
					if f := strings.TrimSpace(o.Firm); f != "" {
						np.Firms = appendUnique(np.Firms, f)
					}
				}
			}
		}
	}

	// 4 — the AION team, by initials
	if s.aion != nil {
		for _, tp := range s.aion.LoadPeople().People() {
			ini := strings.ToUpper(strings.TrimSpace(tp.Initials))
			if ini == "" {
				continue
			}
			np, ok := byTeam[ini]
			if !ok {
				np = &NetPerson{ID: "team/" + ini, Name: tp.Name, Sources: []string{}}
				byTeam[ini] = np
				out = append(out, np)
			}
			addSource(np, "team")
			np.Role, np.TeamLink = tp.Role, ini
		}
	}

	sameNameSuggestions(out)
	for _, p := range out {
		p.Pending = pendingOf(p)
	}

	// recency first: whichever date is newer, the owner's or the calendar's;
	// an undated person sorts last rather than pretending to be old
	last := func(p *NetPerson) string {
		out := p.LastMet
		for _, d := range []string{p.LastContact, p.LastMentioned} {
			if d > out {
				out = d
			}
		}
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := last(out[i]), last(out[j])
		if (a == "") != (b == "") {
			return a != ""
		}
		if a != b {
			return a > b
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// untagged is the topics the owner has not already made tags, by TopicID.
func untagged(topics, tags []string) []string {
	have := map[string]bool{}
	for _, t := range tags {
		have[recruiting.TopicID(t)] = true
	}
	var out []string
	for _, t := range topics {
		if id := recruiting.TopicID(t); id != "" && !have[id] {
			have[id] = true
			out = append(out, t)
		}
	}
	return out
}

// networkVocabulary is what the tag box offers: the owner's tags first
// (most used), then the topics the sources have named — the graph's topic
// nodes and every kept row's suggestions — so a tag and a source topic for
// the same idea converge on one spelling instead of drifting apart.
func (s *Server) networkVocabulary(people []*NetPerson, tagText map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if id := recruiting.TopicID(t); id != "" && !seen[id] && len(out) < 600 {
			seen[id] = true
			out = append(out, strings.TrimSpace(t))
		}
	}
	for _, t := range tagText {
		add(t)
	}
	for _, p := range people {
		for _, t := range p.Suggest {
			add(t)
		}
	}
	if s.graphStore != nil {
		var topics []string
		for _, ent := range s.graphStore.LoadEntities().Entities() {
			if ent.Kind == graph.KindTopic && ent.Title != "" {
				topics = append(topics, ent.Title)
			}
		}
		sort.Strings(topics)
		for _, t := range topics {
			add(t)
		}
	}
	return out
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return xs
		}
	}
	return append(xs, s)
}

// GET /api/network — every resolved person, the kinds, and the tags in use.
func (s *Server) handleNetwork(w http.ResponseWriter, _ *http.Request) {
	people := s.networkPeople(time.Now())
	tagCount := map[string]int{}
	tagText := map[string]string{}
	for _, p := range people {
		// your tags and the source's topics are one experience vocabulary:
		// a filter on "RF coils" finds both your tagged contacts and the
		// saved people a paper said know it
		seen := map[string]bool{}
		for _, t := range append(append([]string{}, p.Tags...), p.Topics...) {
			id := recruiting.TopicID(t)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			tagCount[id]++
			if tagText[id] == "" {
				tagText[id] = t
			}
		}
	}
	type tag struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	tags := []tag{}
	for id, n := range tagCount {
		tags = append(tags, tag{tagText[id], n})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return tags[i].Tag < tags[j].Tag
	})
	writeJSON(w, map[string]any{
		"people": people, "kinds": recruiting.PersonTypes, "tags": tags, "editable": s.recruiting != nil,
		"vocabulary": s.networkVocabulary(people, tagText),
		"contacts":   s.contacts != nil, "fundraising": s.fundraising != nil, "team": s.aion != nil,
	})
}

// POST /api/network/keep {run, draft, kind} — keep one swept person as a
// network row. The ONE path from a sweep into the Network tab.
func (s *Server) handleNetworkKeep(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	var b struct {
		Run   string `json:"run"`
		Draft string `json:"draft"`
		Kind  string `json:"kind"`
		// Also are the same person's drafts in other sweeps (a merged row in
		// the ranked people view): each is marked kept onto the one row.
		Also []struct {
			Run   string `json:"run"`
			Draft string `json:"draft"`
		} `json:"also"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Run) == "" || strings.TrimSpace(b.Draft) == "" {
		httpError(w, errBadRequest("keep needs a run and a draft"))
		return
	}
	run, p, err := s.recruitingRuns.Keep(b.Run, b.Draft, b.Kind, time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	var alsoErrs []string
	for _, a := range b.Also {
		if a.Run == b.Run && a.Draft == b.Draft {
			continue
		}
		if err := s.recruitingRuns.KeepAlso(a.Run, a.Draft, p.ID, time.Now()); err != nil {
			alsoErrs = append(alsoErrs, a.Run+"/"+a.Draft+": "+err.Error())
		}
	}
	s.ledger(ledger.Entry{Source: "recruiting", Kind: "recruiting.person.kept", Actor: "owner",
		Object: ledger.Object{Kind: "person", ID: p.ID},
		Text:   ledger.Snip(p.Name+" kept in the network from "+run.Source+keptAs(p.Type), 280),
		Meta:   map[string]any{"run": b.Run, "draft": b.Draft, "kind": p.Type, "sourceRef": p.SourceRef}})
	out := s.runsPayload(true)
	out["run"] = run
	out["person"] = p
	if len(alsoErrs) > 0 {
		out["alsoErrors"] = alsoErrs
	}
	writeJSON(w, out)
}

// POST /api/network/person/{id...} {set} — edit one person. A contact or a
// team member has no row to edit, so the first edit CREATES one, linked by
// the explicit key it came from (never by name) — that is how a contact gets
// tags and a note without anything touching their vault note.
func (s *Server) handleNetworkPerson(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var b struct {
		Set map[string]string `json:"set"`
	}
	if err := decode(r, &b); err != nil || len(b.Set) == 0 {
		httpError(w, errBadRequest("say what to change"))
		return
	}
	if !strings.HasPrefix(id, "aion-net/") {
		rowID, err := s.networkAdopt(id)
		if err != nil {
			httpError(w, err)
			return
		}
		id = rowID
	}
	p, err := s.recruiting.UpdateNetworkPerson(id, b.Set)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"person": p})
}

// networkAdopt gives a contact or a team member their editable row, linked by
// the key they are listed under. Idempotent: an existing link is reused.
func (s *Server) networkAdopt(id string) (string, error) {
	var np *NetPerson
	for _, p := range s.networkPeople(time.Now()) {
		if p.ID == id {
			np = p
			break
		}
	}
	if np == nil {
		return "", errBadRequest("no such person " + id)
	}
	if np.Editable {
		return np.ID, nil
	}
	row := recruiting.NetworkPerson{Name: np.Name, Source: "owner", Added: time.Now().UTC().Format("2006-01-02")}
	switch {
	case strings.HasPrefix(id, "contact/"):
		row.Ref = np.ContactKey
	case strings.HasPrefix(id, "team/"):
		row.Team = np.TeamLink
	default:
		return "", errBadRequest("cannot edit " + id)
	}
	if err := s.recruiting.AddNetworkPerson(row); err != nil {
		return "", err
	}
	for _, p := range s.recruiting.Connectors() {
		if (row.Ref != "" && strings.EqualFold(p.Ref, row.Ref)) || (row.Team != "" && strings.EqualFold(p.Team, row.Team)) {
			return p.ID, nil
		}
	}
	return "", errBadRequest("the new row did not land")
}

func keptAs(kind string) string {
	if kind == "" {
		return ""
	}
	return " as " + kind
}

// ---- the graph as a view of Network: the SAME renderer and walk as
// Recruiting's, re-kinded by what the owner has decided each person is.

// The Network lens colours people by the group they are in — no taxonomy you
// have to fill: your team, investors, contacts ("known"), and the people you
// saved from recruiting. Recruiting's own statuses (applicants, swept
// people, strangers) can be switched on to see where your graph reaches.
func networkStatusDefault() map[string]bool {
	return map[string]bool{"team": true, "investor": true, "known": true, "saved": true}
}

type networkLens struct {
	alias map[string]string // any id a person is known by → their resolved row id
	kind  map[string]string // resolved row id → network kind
	names map[string]string
	order []string // every resolved person, so an unconnected one can still be drawn
}

func (s *Server) networkLens(now time.Time) *networkLens {
	l := &networkLens{alias: map[string]string{}, kind: map[string]string{}, names: map[string]string{}}
	for _, p := range s.networkPeople(now) {
		if p.Archived != "" {
			continue
		}
		l.order = append(l.order, p.ID)
		l.names[p.ID] = p.Name
		if k := strings.ToLower(strings.TrimSpace(p.ContactKey)); k != "" {
			l.alias["contact/"+k] = p.ID
		}
		if t := strings.ToUpper(strings.TrimSpace(p.TeamLink)); t != "" {
			l.alias["team/"+t] = p.ID
		}
		k := "known"
		for _, src := range p.Sources {
			if src == "investor" {
				k = "investor"
			}
		}
		switch {
		case p.TeamLink != "":
			k = "team"
		case p.Pending != "":
			k = "saved"
		}
		l.kind[p.ID] = k
	}
	return l
}

func (l *networkLens) canon(id string) string {
	if to, ok := l.alias[id]; ok {
		return to
	}
	if strings.HasPrefix(id, "contact/") {
		if to, ok := l.alias["contact/"+strings.ToLower(strings.TrimPrefix(id, "contact/"))]; ok {
			return to
		}
	}
	return id
}

// fold re-addresses every edge onto the resolved row, so a person linked by
// an explicit ref is ONE node however the edge was filed, and drops the
// self-ties and duplicates the folding creates.
func (l *networkLens) fold(edges []recruiting.Edge) []recruiting.Edge {
	out := make([]recruiting.Edge, 0, len(edges))
	seen := map[string]bool{}
	for _, e := range edges {
		e.From, e.To = l.canon(e.From), l.canon(e.To)
		if e.From == e.To {
			continue
		}
		a, b := e.From, e.To
		if b < a {
			a, b = b, a
		}
		key := a + "\x00" + b + "\x00" + e.Kind
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func (l *networkLens) kinder(base func(string) string, owner string) func(string) string {
	return func(id string) string {
		if id != "" && id == owner {
			return "you"
		}
		if k, ok := l.kind[id]; ok {
			return k
		}
		if k := base(id); k != "in_touch" {
			return k
		}
		return "known"
	}
}

// GET /api/network/graph — the Recruiting graph's answer through the Network lens.
func (s *Server) handleNetworkGraph(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	s.peopleGraph(w, r, s.networkLens(time.Now()))
}

// GET /api/aion/recruiting/sources/people?limit= — every undecided person
// across the live sweeps, merged on durable identifiers and ranked with
// their reasons (recruiting/rank.go). A read; the rank is never stored.
func (s *Server) handleRecruitingSourcePeople(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingRunsReady(w) {
		return
	}
	people := s.recruitingRuns.MergedPeople(time.Now())
	total := len(people)
	limit := graphIntParam(r, "limit")
	if limit <= 0 {
		limit = 25
	}
	if limit > 500 {
		limit = 500
	}
	if len(people) > limit {
		people = people[:limit]
	}
	if people == nil {
		people = []recruiting.MergedPerson{}
	}
	writeJSON(w, map[string]any{"people": people, "total": total, "formula": recruiting.RankFormula})
}

// pendingOf is the one rule: a contact note, or the team, makes someone one
// of your people. Everyone else says where they came from.
func pendingOf(p *NetPerson) string {
	if p.HasNote || p.TeamLink != "" {
		return ""
	}
	switch {
	case p.Source != "" && p.Source != "owner":
		return "recruiting"
	case len(p.Firms) > 0 || p.origin == "fundraising":
		return "fundraising"
	case p.origin == "notes":
		return "notes"
	case p.ContactKey != "":
		return "notes"
	}
	return ""
}

// sameNameSuggestions pairs rows whose names fold to the same letters when a
// link could actually join them: one side still lacks the contact link or
// the team link the other supplies. Two kept rows are never paired — there
// is no link that merges rows, and guessing one is exactly what §9 forbids.
func sameNameSuggestions(people []*NetPerson) {
	fold := func(s string) string {
		var b strings.Builder
		for _, r := range strings.ToLower(s) {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	byName := map[string][]*NetPerson{}
	for _, p := range people {
		if k := fold(p.Name); k != "" && p.Archived == "" {
			byName[k] = append(byName[k], p)
		}
	}
	joinable := func(a, b *NetPerson) bool {
		if a.Editable && b.Editable {
			return false
		}
		return (a.ContactKey == "" && b.ContactKey != "") || (b.ContactKey == "" && a.ContactKey != "") ||
			(a.TeamLink == "" && b.TeamLink != "") || (b.TeamLink == "" && a.TeamLink != "")
	}
	for _, group := range byName {
		for _, a := range group {
			for _, b := range group {
				if a != b && joinable(a, b) {
					a.SameName = append(a.SameName, NetAlias{ID: b.ID, Sources: b.Sources})
				}
			}
		}
	}
}
