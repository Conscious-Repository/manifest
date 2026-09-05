package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// THE PEOPLE YOU ALREADY KNOW.
//
// `who I'd ask` held two rows — the owner and his cofounder — because it was a
// hand-typed list, and every derived intro path starts from someone on it. So
// DerivePaths returned nothing for all 71 candidates and the NETWORK view has
// never had anything to show.
//
// Meanwhile the app already knew 227 people: the vault's own person notes,
// with calendar-verified last-met dates, sitting behind the CONTACTS tab where
// recruiting could not see them. This file is the seam between the two —
// deliberately in `server`, because `recruiting` is a vault-record package
// that must not import the index or the calendar, and the ONE layer that can
// see both stores is this one.
//
// Two things live here:
//
//	the picker   — the contacts you have not marked yet, so marking someone is
//	               a click on a name you already know rather than retyping it
//	the derivation — edges from the owner's own calendar: two people in the
//	               same meeting have met, which is a fact, dated, and the
//	               strongest honest signal this system can get for free
//
// Nothing here is stored. The connector row a mark writes IS stored (it is the
// owner's judgment about who they would ask); the edges are recomputed on
// every read, because a derivation that gets written becomes a fact nobody can
// correct at the source.

// coAttendanceCap is the most attendees a meeting may have before it stops
// being evidence of a relationship. A standup with fourteen people on it says
// nothing about any two of them; the same guardrail the coauthor derivation
// uses on a crowded paper, for the same reason.
const coAttendanceCap = 8

// coAttendanceConfidence — a shared meeting is a stated, dated fact about the
// owner's own calendar, which puts it above an unstated 0.5 and below the
// owner saying outright that they know someone (0.95).
const coAttendanceConfidence = 0.7

// coMentionCap / coMentionConfidence — a note that links more people than the
// cap is a roster, not a room. Being written about together is the weakest
// signal here and says so in its confidence.
const (
	coMentionCap        = 6
	coMentionConfidence = 0.4
)

// KnownPerson is one row of the picker: somebody the vault knows, and whether
// they are already someone you would ask.
type KnownPerson struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	NotePath string `json:"notePath,omitempty"`
	LastMet  string `json:"lastMet,omitempty"`
	Marked   bool   `json:"marked"`
	PersonID string `json:"personId,omitempty"` // the connector row, when marked
}

// GET /api/aion/recruiting/people/known?q= — the vault's people, marked first,
// then by how recently you actually met them. This is a READ of the contacts
// layer: nothing here writes to a person note, and a name the owner has not
// marked stays where it is.
func (s *Server) handleRecruitingKnownPeople(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	if s.contacts == nil {
		writeJSON(w, map[string]any{"people": []KnownPerson{}, "available": false,
			"note": "the contacts layer is not wired here — mark people by name instead"})
		return
	}
	list, err := s.contacts.List(time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	marked := map[string]recruiting.NetworkPerson{}
	for _, p := range s.recruiting.View().Network.People {
		if k := strings.ToLower(strings.TrimSpace(p.Ref)); k != "" {
			marked[k] = p
		}
		marked[strings.ToLower(strings.TrimSpace(p.Name))] = p
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := []KnownPerson{}
	for _, c := range list {
		if q != "" && !strings.Contains(strings.ToLower(c.Display+" "+c.Key), q) {
			continue
		}
		row := KnownPerson{Key: c.Key, Name: c.Display, NotePath: c.NotePath, LastMet: c.LastMet}
		if p, ok := marked[strings.ToLower(c.Key)]; ok {
			row.Marked, row.PersonID = true, p.ID
		} else if p, ok := marked[strings.ToLower(c.Display)]; ok {
			row.Marked, row.PersonID = true, p.ID
		}
		out = append(out, row)
	}
	// marked first, then whoever you saw most recently — an undated contact
	// sorts last rather than pretending to be old
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Marked != out[j].Marked {
			return out[i].Marked
		}
		if (out[i].LastMet == "") != (out[j].LastMet == "") {
			return out[i].LastMet != ""
		}
		if out[i].LastMet != out[j].LastMet {
			return out[i].LastMet > out[j].LastMet
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, map[string]any{"people": out, "available": true, "count": len(out)})
}

// POST /api/aion/recruiting/network/mark {key} — mark a vault contact as
// someone you would ask. This is the ONLY thing that creates a path origin,
// and it is a judgment, so it is stored: the row carries `consent: owner` and
// the contact key it came from, which is what stops the same human becoming a
// second record here.
func (s *Server) handleRecruitingMarkKnown(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("say who"))
		return
	}
	key, name := strings.TrimSpace(b.Key), strings.TrimSpace(b.Name)
	if key == "" && name == "" {
		httpError(w, errBadRequest("say who"))
		return
	}
	// already marked? saying so beats a duplicate row
	for _, p := range s.recruiting.View().Network.People {
		if (key != "" && strings.EqualFold(p.Ref, key)) || (name != "" && strings.EqualFold(p.Name, name)) {
			writeJSON(w, map[string]any{"view": s.recruiting.View(), "already": true, "id": p.ID})
			return
		}
	}
	if name == "" && s.contacts != nil {
		if list, err := s.contacts.List(time.Now()); err == nil {
			for _, c := range list {
				if strings.EqualFold(c.Key, key) {
					name = c.Display
					break
				}
			}
		}
	}
	if name == "" {
		name = key
	}
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{
		Name: name, Ref: key, Source: "owner", Consent: "owner",
		Added: time.Now().UTC().Format("2006-01-02"),
	}); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"view": s.recruiting.View(), "marked": name})
}

// ---- the derivation ----

// wireRecruitingDerivedEdges hands the record store a source of claims it
// cannot compute itself. Called once at wiring time; the closure runs on every
// view.
func (s *Server) wireRecruitingDerivedEdges() {
	if s.recruiting == nil {
		return
	}
	s.recruiting.UseDerivedEdges(func() []recruiting.Edge {
		return append(s.coAttendanceEdges(), s.coMentionEdges()...)
	})
}

// personIndex maps every way a person can be named — vault contact key, email,
// display name — onto the node id this graph should use for them, preferring
// a record that already exists here (a candidate, then a connector) over a
// bare contact. ONE resolution, so the calendar and the notes cannot disagree
// about who someone is.
type personIndex struct {
	byEmail map[string]string // email-lower → node id
	byKey   map[string]string // vault contact key → node id
	name    map[string]string // node id → display name
}

// ⚠ personIndex reads the RECORDS, never View(): View derives paths from the
// edges, the edges come from this derivation, and this derivation asks who is
// on the board — a View() call here is an infinite recursion that takes the
// process down on the first page load.
func (s *Server) personIndex() personIndex {
	idx := personIndex{byEmail: map[string]string{}, byKey: map[string]string{}, name: map[string]string{}}

	// vault people first: the fallback identity, `contact/<key>`
	if s.index != nil {
		if byEmail, err := s.index.PeopleByEmail(); err == nil {
			for email, key := range byEmail {
				idx.byEmail[email] = "contact/" + key
				idx.byKey[key] = "contact/" + key
			}
		}
		if people, err := s.index.PeopleNotes(); err == nil {
			for _, p := range people {
				id := "contact/" + p.Key
				idx.byKey[p.Key] = id
				idx.name[id] = p.Display
			}
		}
	}
	// a candidate outranks a contact: if the person is on the board, the edge
	// should land on the record that carries their evidence
	for _, c := range s.recruiting.Identities() {
		if em := strings.ToLower(strings.TrimSpace(c.Email)); em != "" {
			idx.byEmail[em] = c.ID
		}
		idx.name[c.ID] = c.Name
	}
	// a connector outranks both — it is the owner's own judgment, and it is
	// where every path starts
	for _, p := range s.recruiting.Connectors() {
		if p.Archived != "" {
			continue
		}
		if em := strings.ToLower(strings.TrimSpace(p.Email)); em != "" {
			idx.byEmail[em] = p.ID
		}
		if ref := strings.TrimSpace(p.Ref); ref != "" {
			idx.byKey[ref] = p.ID
		}
		idx.name[p.ID] = p.Name
	}
	return idx
}

// ownerNode is the connector row that represents the person whose calendar
// this is — the one node every meeting on it implies. "" when the owner has
// not been marked as someone to route through, in which case co-attendance
// still records who met whom, just never a route from you.
func (i personIndex) ownerNode(store *recruiting.Store) string {
	want := strings.ToLower(strings.TrimSpace(store.Owner()))
	for _, p := range store.Connectors() {
		if p.Archived != "" || p.Consent != "owner" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if want != "" && (strings.Contains(name, want) || strings.EqualFold(p.Ref, want)) {
			return p.ID
		}
	}
	return ""
}

func (i personIndex) display(id string) string {
	if n := i.name[id]; n != "" {
		return n
	}
	return strings.TrimPrefix(id, "contact/")
}

// coAttendanceEdges reads the owner's own calendar: two people on the same
// meeting have met, on a date, and that is the strongest claim this system can
// derive without asking anyone. It is `inferred` all the same — the calendar
// says they were invited to the same room, not that either would take the
// other's call.
func (s *Server) coAttendanceEdges() []recruiting.Edge {
	if s.contacts == nil {
		return nil
	}
	idx := s.personIndex()
	// ⚠ THE OWNER IS AT EVERY MEETING ON HIS OWN CALENDAR, and Google's
	// attendee list excludes self — so without this every derived edge joined
	// two other people and NOTHING started from the one node paths begin at.
	// 1,072 edges, 0 usable routes, until the obvious party was added back.
	me := idx.ownerNode(s.recruiting)
	var out []recruiting.Edge
	seen := map[string]bool{}
	for _, m := range s.contacts.PastMeetingParties(time.Now()) {
		// the owner counts as a party, so a 1:1 IS a meeting: one other
		// attendee plus you is the most common and most useful shape there is
		if len(m.Emails) < 1 || len(m.Emails) > coAttendanceCap {
			continue // fourteen people on an invite is not a relationship
		}
		ids := make([]string, 0, len(m.Emails))
		for _, em := range m.Emails {
			if id := idx.byEmail[em]; id != "" && !containsString(ids, id) {
				ids = append(ids, id)
			}
		}
		if me != "" {
			ids = append([]string{me}, ids...)
		}
		for a := 0; a < len(ids); a++ {
			for b := a + 1; b < len(ids); b++ {
				key := ids[a] + "\x00" + ids[b]
				if seen[key] {
					continue // one edge per pair; the first (newest) meeting is the basis
				}
				seen[key] = true
				where := strings.TrimSpace(m.Title)
				if where == "" {
					where = "a meeting"
				}
				out = append(out, recruiting.Edge{
					From: ids[a], To: ids[b], Kind: string(sources.EdgeSameMeeting),
					Basis: idx.display(ids[a]) + " and " + idx.display(ids[b]) +
						" were both on “" + where + "” on " + m.Date,
					Confidence: recruiting.FormatConfidence(coAttendanceConfidence),
					Inferred:   true, Source: "calendar", Observed: m.Date,
				})
			}
		}
	}
	return out
}

// coMentionEdges reads the owner's notes: two people linked from one note were
// written about together. The weakest signal here, and the one to be most
// sceptical of — a planning note naming two people is not a relationship
// between them — so it carries the lowest confidence, says which note in its
// basis, and is capped hard (vaultindex.CoMentions).
func (s *Server) coMentionEdges() []recruiting.Edge {
	if s.index == nil {
		return nil
	}
	pairs, err := s.index.CoMentions(coMentionCap)
	if err != nil {
		return nil
	}
	idx := s.personIndex()
	var out []recruiting.Edge
	seen := map[string]bool{}
	for _, m := range pairs {
		a, b := idx.byKey[m.A], idx.byKey[m.B]
		if a == "" || b == "" || a == b {
			continue
		}
		if a > b {
			a, b = b, a
		}
		key := a + "\x00" + b
		if seen[key] {
			continue
		}
		seen[key] = true
		where := strings.TrimSpace(m.Name)
		if where == "" {
			where = m.Path
		}
		out = append(out, recruiting.Edge{
			From: a, To: b, Kind: string(sources.EdgeCoMentioned),
			Basis:      idx.display(a) + " and " + idx.display(b) + " are written about together in “" + where + "”",
			Confidence: recruiting.FormatConfidence(coMentionConfidence),
			Inferred:   true, Source: "notes", Evidence: m.Path, Observed: m.Date,
		})
	}
	return out
}

func containsString(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
