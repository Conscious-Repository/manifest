package recruiting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/recruiting/sources"
)

// Store reads and writes the private recruiting records under
// <vault>/<root>/ (root = "system/aion/recruiting"). Every write goes through
// the injected write func — main binds it to the narrow `aion-recruiting`
// vaultwriter user-action capability. This package never opens a file to
// write, and holds no other authority: it cannot reach the AION export
// contract, and there is no method here that could serve one of these records
// to the team portal.
type Store struct {
	vaultRoot string
	root      string // vault-relative, slash form
	write     func(abs string, data []byte) error
	// Owner is the person quick-adds are attributed to; it becomes the
	// candidate's [owner::] and the manual adapter's evidence source.
	owner string
	// derivedEdges yields relationship claims computed OUTSIDE this package —
	// the owner's calendar co-attendance, their notes' co-mentions — injected
	// by the server, which is the only layer that can see those stores.
	// Recomputed on every read and never saved: LoadEdges/SaveEdges only ever
	// touch the file, so a derivation cannot leak into the vault.
	derivedEdges func() []Edge
	mu           sync.Mutex
}

// UseDerivedEdges injects the outside-the-vault edge source (see the field).
func (s *Store) UseDerivedEdges(fn func() []Edge) { s.derivedEdges = fn }

// networkEdges is the graph the VIEW reads: what is on file, plus what was
// derived, with the file winning any claim they both make. Nothing else in
// this package uses it — every write path reads LoadEdges directly.
func (s *Store) networkEdges() []Edge {
	stored := s.LoadEdges().Edges()
	if s.derivedEdges == nil {
		return stored
	}
	have := map[string]bool{}
	for _, e := range stored {
		have[edgeKey(e.From, e.To, e.Kind)] = true
	}
	out := stored
	for _, e := range s.derivedEdges() {
		if ValidateEdge(e) != nil || have[edgeKey(e.From, e.To, e.Kind)] {
			continue
		}
		have[edgeKey(e.From, e.To, e.Kind)] = true
		e.Derived = true
		out = append(out, e)
	}
	return out
}

// NewStore builds the record store. A nil writer is not silently tolerated:
// every save fails loudly with the boundary it violated.
func NewStore(vaultRoot, root string, write func(string, []byte) error) *Store {
	if write == nil {
		write = func(string, []byte) error {
			return errors.New("recruiting: no vault writer injected (§A3 write boundary)")
		}
	}
	return &Store{vaultRoot: vaultRoot, root: filepath.ToSlash(root), write: write, owner: "benjamin"}
}

// UseOwner sets who quick-adds are attributed to (default "benjamin").
func (s *Store) UseOwner(owner string) {
	if strings.TrimSpace(owner) != "" {
		s.owner = strings.TrimSpace(owner)
	}
}

// Root returns the vault-relative record root ("system/aion/recruiting").
func (s *Store) Root() string { return s.root }

// Owner returns who quick-adds and manual-source runs are attributed to.
func (s *Store) Owner() string { return s.owner }

// Rel returns the vault-relative slash path of a record.
func (s *Store) Rel(name string) string { return s.root + "/" + name }

// Path returns the absolute path of a record.
func (s *Store) Path(name string) string {
	return filepath.Join(s.vaultRoot, filepath.FromSlash(s.root), filepath.FromSlash(name))
}

func (s *Store) raw(name string) string {
	b, _ := os.ReadFile(s.Path(name))
	return string(b)
}

func (s *Store) save(name, content string) error {
	return s.write(s.Path(name), []byte(content))
}

// Ensure writes the write-once seeds for anything absent. It never overwrites
// an existing record — the vault is the source of truth, not this file.
func (s *Store) Ensure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range SeedOrder {
		if _, err := os.Stat(s.Path(name)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := s.save(name, SeedFiles[name]); err != nil {
			return fmt.Errorf("seeding %s: %w", name, err)
		}
	}
	return nil
}

// ---- typed load/save pairs ----

func (s *Store) LoadSeeds() *SeedsDoc        { return ParseSeeds(s.raw("seeds.md")) }
func (s *Store) SaveSeeds(d *SeedsDoc) error { return s.save("seeds.md", SerializeSeeds(d)) }
func (s *Store) LoadNetworkPeople() *PeopleDoc {
	return ParseNetworkPeople(s.raw("network/people.md"))
}
func (s *Store) SaveNetworkPeople(d *PeopleDoc) error {
	return s.save("network/people.md", SerializeNetworkPeople(d))
}
func (s *Store) LoadEdges() *EdgesDoc { return ParseEdges(s.raw("network/edges.md")) }

// SaveEdges validates before it writes: this package cannot persist an edge
// without a basis, a source, or a kind in the closed set.
func (s *Store) SaveEdges(d *EdgesDoc) error {
	if err := d.Validate(); err != nil {
		return err
	}
	return s.save("network/edges.md", SerializeEdges(d))
}

func (s *Store) LoadRole(slug string) *RoleDoc {
	d := ParseRole(s.raw("roles/" + slug + ".md"))
	d.Slug = slug
	return d
}
func (s *Store) SaveRole(slug string, d *RoleDoc) error {
	return s.save("roles/"+slug+".md", SerializeRole(d))
}

func (s *Store) LoadCandidate(slug string) *CandidateDoc {
	d := ParseCandidate(s.raw("candidates/" + slug + ".md"))
	d.Slug = slug
	return d
}
func (s *Store) SaveCandidate(slug string, d *CandidateDoc) error {
	return s.save("candidates/"+slug+".md", SerializeCandidate(d))
}

// slugsIn lists the record slugs of a subdirectory, sorted. A missing
// directory is an empty list, never an error — the board reads before the
// first seed lands.
func (s *Store) slugsIn(dir string) []string {
	ents, err := os.ReadDir(filepath.Join(s.vaultRoot, filepath.FromSlash(s.root), dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), path.Ext(e.Name())))
	}
	sort.Strings(out)
	return out
}

func (s *Store) RoleSlugs() []string      { return s.slugsIn("roles") }
func (s *Store) CandidateSlugs() []string { return s.slugsIn("candidates") }

// ---- read projection ----

// View is everything the private recruiting board renders from. It is
// deliberately one payload: the rail counts, the board and the inspector all
// derive from the SAME candidate list, so a badge and the page it labels
// cannot disagree.
type View struct {
	Roles            []Role      `json:"roles"`
	Candidates       []Candidate `json:"candidates"`
	Seeds            []Seed      `json:"seeds"`
	Network          NetworkView `json:"network"`
	Stages           []string    `json:"stages"`
	SeedClasses      []string    `json:"seedClasses"`
	CriterionClasses []string    `json:"criterionClasses"`
	Owner            string      `json:"owner"`
}

// View loads the whole board. Roles sort pinned-first then by title; pinned
// is a SORT KEY only, never a filter (D2 — all four lanes are first-class).
func (s *Store) View() View {
	v := View{
		Stages: Stages, SeedClasses: SeedClasses, CriterionClasses: CriterionClasses,
		Owner: s.owner, Roles: []Role{}, Candidates: []Candidate{}, Seeds: []Seed{},
	}
	roleDocs := map[string]*RoleDoc{}
	for _, slug := range s.RoleSlugs() {
		d := s.LoadRole(slug)
		roleDocs[slug] = d
		if id := d.Get("id"); id != "" {
			roleDocs[id] = d
		}
	}
	// The network is loaded ONCE and every candidate's intro paths derive
	// from that same graph — the inspector cannot show a route the network
	// pane does not.
	v.Network = NetworkView{People: s.LoadNetworkPeople().People(), Edges: s.networkEdges()}
	for _, slug := range s.CandidateSlugs() {
		d := s.LoadCandidate(slug)
		c := d.View(slug, roleDocs[roleKey(d.Get("role"))])
		v.Candidates = append(v.Candidates, withDerivedPaths(c, v.Network))
	}
	sortCandidates(v.Candidates)

	// ONE derivation of "open": the same predicate the board's active filter
	// uses. Two derivations is how a rail badge reads 154 against 17 real
	// items (92-aion.js:38).
	open := map[string]int{}
	for _, c := range v.Candidates {
		if IsActive(c) {
			open[roleKey(c.Role)]++
		}
	}
	for _, slug := range s.RoleSlugs() {
		d := roleDocs[slug]
		count := open[slug]
		if id := d.Get("id"); id != "" {
			count = open[roleKey(id)]
		}
		v.Roles = append(v.Roles, d.View(slug, count))
	}
	sort.SliceStable(v.Roles, func(i, j int) bool {
		if v.Roles[i].Pinned != v.Roles[j].Pinned {
			return v.Roles[i].Pinned
		}
		return strings.ToLower(v.Roles[i].Title) < strings.ToLower(v.Roles[j].Title)
	})

	v.Seeds = s.LoadSeeds().Seeds()
	return v
}

// IsActive is THE "on the board" predicate: an archived candidate is retained
// but excluded from active views and counts (D7).
func IsActive(c Candidate) bool { return c.Stage != StageArchived }

// roleKey reduces a role id or slug to the one lookup key both forms share.
func roleKey(role string) string { return strings.TrimPrefix(strings.TrimSpace(role), "role/") }

// ---- semantic actions (the invariants live here, not in the handlers) ----

// QuickAdd is the board's `＋ candidate url, name, or note`. It runs through
// the manual source adapter rather than around it: the adapter emits a draft
// with its own provenance, and the converter decides what may become a
// record. That is the same path a network adapter's accepted draft will take
// in Phase 3, proven now while the only source is a human typing.
type QuickAdd struct {
	Text     string `json:"text"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Org      string `json:"org"`
	Title    string `json:"title"`
	Location string `json:"location"`
	Known    bool   `json:"known"`
	KnownVia string `json:"knownVia"`
}

// AddCandidate promotes one owner-typed entry into a candidate record.
func (s *Store) AddCandidate(q QuickAdd, now time.Time) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if q.Role != "" && !s.roleExists(q.Role) {
		return Candidate{}, errf("unknown role %q", q.Role)
	}
	adapter := sources.Manual{Owner: s.owner}
	draft, err := adapter.Draft(sources.Entry{
		Text: q.Text, Name: q.Name, Role: q.Role, Org: q.Org, Title: q.Title,
		Location: q.Location, Owner: s.owner, Known: q.Known, KnownVia: q.KnownVia, Now: now,
	})
	if err != nil {
		return Candidate{}, err
	}
	edges, err := adapter.GraphEdges(context.Background(), draft)
	if err != nil {
		return Candidate{}, err
	}
	draft.Edges = edges
	return s.acceptDraft(draft, now)
}

// AcceptDraft converts ONE adapter draft into a vault record. It is
// deliberately per-draft: a route that accepted a whole run would quietly
// turn a search result into vault PII.
func (s *Store) AcceptDraft(d sources.CandidateDraft, now time.Time) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptDraft(d, now)
}

func (s *Store) acceptDraft(d sources.CandidateDraft, now time.Time) (Candidate, error) {
	d = SanitizeDraft(d)
	if err := ValidateDraft(d); err != nil {
		return Candidate{}, err
	}
	taken := map[string]bool{}
	for _, slug := range s.CandidateSlugs() {
		taken[slug] = true
		if have := s.LoadCandidate(slug); strings.EqualFold(strings.TrimSpace(have.Get("name")), strings.TrimSpace(d.Name)) {
			return Candidate{}, errf("already on the board: %s", d.Name)
		}
	}
	slug := NewCandidateSlug(d.Name, taken)
	doc := newCandidateDoc(slug, d, now)

	for _, ev := range d.Evidence {
		if _, err := doc.AddEvidence(Evidence{
			URL:       ev.URLOrFile,
			Collected: ev.RetrievedAt.UTC().Format("2006-01-02"),
			Kind:      ev.Kind,
			Source:    ev.SourceID,
			Snippet:   ev.Snippet,
		}); err != nil {
			return Candidate{}, err
		}
	}
	// the RECORD lands first: edges pointing at a candidate whose file failed
	// to write are orphans, and the graph would carry a person who does not
	// exist. Edges are recoverable (re-accept re-derives them); a half-written
	// graph is not.
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	if err := s.saveDraftEdges(d, doc.Get("id")); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// saveDraftEdges appends the draft's relationship claims, filling in the `to`
// endpoint (the candidate that did not exist when the adapter ran), resolving
// external keys onto records that already exist, repointing edges that named
// THIS person by an external key, and refusing a claim already on file.
func (s *Store) saveDraftEdges(d sources.CandidateDraft, candidateID string) error {
	mine := extKeysOfDraft(d)
	if len(d.Edges) == 0 && len(mine) == 0 {
		return nil
	}
	edges := s.LoadEdges()
	// the person just accepted is no longer external: every edge that named
	// them by an ORCID or an OpenAlex id now names their record
	changed := repointEdges(edges, mine, candidateID) > 0

	if len(d.Edges) > 0 {
		index := s.extIndex()
		for _, k := range mine {
			index[k] = candidateID
		}
		have := map[string]bool{}
		for _, e := range edges.Edges() {
			have[edgeKey(e.From, e.To, e.Kind)] = true
		}
		for _, e := range d.Edges {
			from := strings.TrimSpace(e.From)
			if id, ok := index[from]; ok && id != "" {
				from = id
			}
			to := strings.TrimSpace(e.To)
			if to == "" {
				to = candidateID
			} else if id, ok := index[to]; ok && id != "" {
				to = id
			}
			if from == to || have[edgeKey(from, to, string(e.Type))] {
				continue // the same claim, already on file
			}
			if _, err := edges.Add(Edge{
				From: from, To: to, Kind: string(e.Type), Basis: e.Basis,
				Confidence: FormatConfidence(e.Confidence), Inferred: e.Inferred,
				Source: e.SourceID,
			}); err != nil {
				return err
			}
			have[edgeKey(from, to, string(e.Type))] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.SaveEdges(edges)
}

// AddNetworkPerson appends one curated node to network/people.md ("＋ someone
// I know" — the MY PEOPLE core the derived intro paths route through).
func (s *Store) AddNetworkPerson(p NetworkPerson) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadNetworkPeople()
	if _, err := doc.Add(p); err != nil {
		return err
	}
	return s.SaveNetworkPeople(doc)
}

// AdoptAshbyApplicant lands one Ashby-side applicant on the board (owner
// decision 2026-09-03: Manifest is the single management surface; Ashby is
// the distribution channel that collects applicants). An existing UNLINKED
// record with the same email or name is linked rather than duplicated — the
// scouted person who then applied — keeping their owner-authored profile;
// otherwise a fresh record lands straight in the `ashby` column with the ATS
// stage on `ashby_stage`. Contact details are copied on create and onto
// EMPTY slots on adopt: the D15 empty-slot rule exists because adapters
// scrape, and the owner's own ATS is first-party — the applicant typed that
// address to us. Returns the record id and whether it was newly created.
func (s *Store) AdoptAshbyApplicant(c AshbyCandidate, appID, ashbyStage, role string, now time.Time) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return "", false, errf("ashby candidate %s carries no name", c.ID)
	}
	emails := map[string]bool{}
	for _, e := range append([]string{c.PrimaryEmail}, c.Emails...) {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			emails[e] = true
		}
	}
	taken := map[string]bool{}
	var matchSlug string
	var match *CandidateDoc
	for _, slug := range s.CandidateSlugs() {
		taken[slug] = true
		d := s.LoadCandidate(slug)
		if strings.TrimSpace(d.Get("ashby_candidate_id")) == c.ID {
			return d.Get("id"), false, nil // already ours
		}
		// only unlinked records are adoption candidates — a record linked to
		// a DIFFERENT Ashby person is someone else with the same name
		if match != nil || strings.TrimSpace(d.Get("ashby_candidate_id")) != "" {
			continue
		}
		if emails[strings.ToLower(strings.TrimSpace(d.Profile()["email"]))] ||
			strings.EqualFold(strings.TrimSpace(d.Get("name")), name) {
			matchSlug, match = slug, d
		}
	}
	if match != nil {
		match.Set("ashby_candidate_id", c.ID)
		if appID != "" {
			match.Set("ashby_application_id", appID)
		}
		match.Set("ashby_stage", ashbyStage)
		match.Set("ashby_synced", now.UTC().Format("2006-01-02"))
		// an applicant needs triage even when we already scouted them —
		// `inbound` + stage `ashby` is the untriaged queue the board renders
		match.Set("inbound", now.UTC().Format("2006-01-02"))
		if match.Get("stage") != StageArchived {
			match.Set("stage", StageAshby)
		}
		p := match.Profile()
		if strings.TrimSpace(p["email"]) == "" && strings.TrimSpace(c.PrimaryEmail) != "" {
			if err := match.SetProfile("email", strings.TrimSpace(c.PrimaryEmail)); err != nil {
				return "", false, err
			}
		}
		if strings.TrimSpace(p["phone"]) == "" && strings.TrimSpace(c.PrimaryPhone) != "" {
			if err := match.SetProfile("phone", strings.TrimSpace(c.PrimaryPhone)); err != nil {
				return "", false, err
			}
		}
		if err := s.SaveCandidate(matchSlug, match); err != nil {
			return "", false, err
		}
		return match.Get("id"), false, nil
	}

	slug := NewCandidateSlug(name, taken)
	doc := &CandidateDoc{Slug: slug}
	doc.FMBlank = true
	for _, kv := range [][2]string{
		{"id", CandidateID(slug)}, {"name", name}, {"role", role},
		{"stage", StageAshby}, {"owner", ""}, {"pii", "true"},
		{"ashby_candidate_id", c.ID}, {"ashby_application_id", appID},
		{"ashby_stage", ashbyStage}, {"ashby_synced", now.UTC().Format("2006-01-02")},
		{"inbound", now.UTC().Format("2006-01-02")},
		{"created", now.UTC().Format("2006-01-02")}, {"archived", ""},
	} {
		doc.Set(kv[0], kv[1])
	}
	profile := ensureSection(&doc.Sections, "profile")
	appendRow(profile, newRow("title", strings.TrimSpace(c.Position), "org", strings.TrimSpace(c.Company), "location", strings.TrimSpace(c.Location)))
	links := newRow()
	for _, kv := range [][2]string{{"linkedin", c.LinkedInURL}, {"github", c.GitHubURL}, {"website", c.Website}} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			links.Fields = append(links.Fields, Field{Key: kv[0], Value: v})
		}
	}
	if len(links.Fields) > 0 {
		appendRow(profile, links)
	}
	appendRow(profile, newRow("email", strings.TrimSpace(c.PrimaryEmail), "phone", strings.TrimSpace(c.PrimaryPhone)))
	ensureSection(&doc.Sections, "fit")
	ensureSection(&doc.Sections, "evidence")
	ensureSection(&doc.Sections, "network")
	ensureSection(&doc.Sections, "next")
	if err := s.SaveCandidate(slug, doc); err != nil {
		return "", false, err
	}
	return CandidateID(slug), true, nil
}

// newCandidateDoc builds the record shell for an accepted draft. `pii: true`
// is stamped because it is: these records carry a real person's details, and
// the file should say so to anything that reads it.
func newCandidateDoc(slug string, d sources.CandidateDraft, now time.Time) *CandidateDoc {
	doc := &CandidateDoc{Slug: slug}
	doc.FMBlank = true
	for _, kv := range [][2]string{
		{"id", CandidateID(slug)}, {"name", d.Name}, {"role", d.Role},
		{"stage", StageNew}, {"owner", ""}, {"pii", "true"},
		{"ashby_candidate_id", ""}, {"ashby_application_id", ""},
		{"created", now.UTC().Format("2006-01-02")}, {"archived", ""},
	} {
		doc.Set(kv[0], kv[1])
	}
	// the adapter's own id for this person, so a later run of the same
	// source dedupes against the record instead of the name
	if ref := SourceRef(d); ref != "" {
		doc.Set("source_ref", ref)
	}
	profile := ensureSection(&doc.Sections, "profile")
	appendRow(profile, newRow("title", d.Title, "org", d.Org, "location", d.Location))
	links := newRow()
	for _, l := range d.Links {
		links.Fields = append(links.Fields, Field{Key: linkKey(l), Value: l})
	}
	if len(links.Fields) > 0 {
		appendRow(profile, links)
	}
	// The contact slots are written EMPTY on purpose: D15 makes contact
	// discovery manual, so the record shows where an address goes without
	// anything ever having guessed one.
	appendRow(profile, newRow("email", "", "phone", ""))
	ensureSection(&doc.Sections, "fit")
	ensureSection(&doc.Sections, "evidence")
	ensureSection(&doc.Sections, "network")
	ensureSection(&doc.Sections, "next")
	return doc
}

// linkKey routes a profile link onto the field it belongs in.
func linkKey(url string) string {
	switch {
	case strings.Contains(url, "linkedin."):
		return "linkedin"
	case strings.Contains(url, "github."):
		return "github"
	default:
		return "website"
	}
}

// SanitizeDraft is the D15 half of the converter: whatever contact detail an
// adapter set is DROPPED before anything is written. The draft's Contact map
// is cleared and a mailto:/tel: link is removed from Links. A published
// address survives only as an Evidence row of kind contact_published — a
// citation the owner may promote onto the profile by hand, never a field.
func SanitizeDraft(d sources.CandidateDraft) sources.CandidateDraft {
	d.Contact = nil
	if len(d.Links) > 0 {
		kept := make([]string, 0, len(d.Links))
		for _, l := range d.Links {
			low := strings.ToLower(strings.TrimSpace(l))
			if strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") || strings.HasPrefix(low, "sms:") {
				continue
			}
			kept = append(kept, l)
		}
		d.Links = kept
	}
	return d
}

// ValidateDraft is the converter gate (plan §4.9): no fact without a source,
// and no contact detail from a machine.
func ValidateDraft(d sources.CandidateDraft) error {
	if strings.TrimSpace(d.Name) == "" {
		return errf("a candidate draft needs a name")
	}
	if strings.TrimSpace(d.SourceID) == "" {
		return errf("a candidate draft needs the source that produced it")
	}
	if len(d.Evidence) == 0 {
		return errf("a candidate draft with no evidence cannot become a record")
	}
	for _, ev := range d.Evidence {
		if strings.TrimSpace(ev.SourceID) == "" {
			return errf("evidence needs the source that produced it")
		}
		if ev.RetrievedAt.IsZero() {
			return errf("evidence needs a retrieval date")
		}
		if strings.TrimSpace(ev.Kind) == "" {
			return errf("evidence needs a kind")
		}
		if !ev.Cited() {
			return errf("evidence needs a citable url or file")
		}
	}
	for _, e := range d.Edges {
		if strings.TrimSpace(e.From) == "" {
			return errf("an edge claim needs a from endpoint")
		}
		if !sources.ValidEdgeType(e.Type) {
			return errf("edge type %q is not in the closed set", e.Type)
		}
		if strings.TrimSpace(e.Basis) == "" {
			return errf("an edge claim needs a basis")
		}
		if strings.TrimSpace(e.SourceID) == "" {
			return errf("an edge claim needs a source")
		}
	}
	return nil
}

// resolve loads a candidate by id or slug.
func (s *Store) resolve(id string) (string, *CandidateDoc, error) {
	slug := CandidateSlug(id)
	if slug == "" {
		return "", nil, errf("a candidate id is required")
	}
	if strings.ContainsAny(slug, "/\\") {
		return "", nil, errf("candidate %q not found", id)
	}
	if _, err := os.Stat(s.Path("candidates/" + slug + ".md")); err != nil {
		return "", nil, errf("candidate %q not found", id)
	}
	return slug, s.LoadCandidate(slug), nil
}

func (s *Store) roleExists(role string) bool {
	key := roleKey(role)
	if key == "" || strings.ContainsAny(key, "/\\") {
		return false
	}
	_, err := os.Stat(s.Path("roles/" + key + ".md"))
	return err == nil
}

func (s *Store) roleDocFor(role string) *RoleDoc {
	if !s.roleExists(role) {
		return nil
	}
	return s.LoadRole(roleKey(role))
}

// candidateView is the post-write projection every mutating action returns.
// It carries the same derived intro paths the board view does, so a
// post-write inspector never differs from the next full load.
func (s *Store) candidateView(slug string, doc *CandidateDoc) Candidate {
	c := doc.View(slug, s.roleDocFor(doc.Get("role")))
	net := NetworkView{People: s.LoadNetworkPeople().People(), Edges: s.LoadEdges().Edges()}
	return withDerivedPaths(c, net)
}

// withDerivedPaths merges the Phase 4 derived intro paths (DerivePaths, seeded
// from the owner-consent people) after the record's hand-authored `## network`
// rows. Hand-written rows are owner evidence and pass through untouched.
func withDerivedPaths(c Candidate, net NetworkView) Candidate {
	derived := DerivePaths(net.People, PathEdges(net.Edges), c.ID, nil, DefaultTopPaths)
	c.Paths = MergePaths(c.Paths, derived)
	return c
}

// SetStage moves a candidate between board columns. The stage set is closed,
// and `archived` is NOT reachable here — archiving is a disposition with a
// date, not a column you drag into (D7).
func (s *Store) SetStage(id, stage string) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidStage(stage) {
		return Candidate{}, errf("stage must be one of %s", strings.Join(Stages, ", "))
	}
	if stage == StageArchived {
		return Candidate{}, errf("archive a candidate through archive, so the date is recorded")
	}
	slug, doc, err := s.resolve(id)
	if err != nil {
		return Candidate{}, err
	}
	if doc.Get("stage") == StageArchived {
		return Candidate{}, errf("restore %s before moving it on the board", id)
	}
	doc.Set("stage", stage)
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// Archive is the D7 disposition: the record is RETAINED and excluded from
// active views and counts. It is not deletion — the vault is a git repo, and
// the two are genuinely different operations. Restoring returns a candidate
// to `reviewing`, the stage a second look starts from.
func (s *Store) Archive(id string, archived bool, now time.Time) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return Candidate{}, err
	}
	if archived {
		doc.Set("stage", StageArchived)
		doc.Set("archived", now.UTC().Format("2006-01-02"))
	} else {
		doc.Set("stage", StageReviewing)
		doc.Set("archived", "")
	}
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// UpdateCandidate edits the name, the role tether, and the profile fields.
// `email` and `phone` are editable HERE and via the Ashby import only:
// contact discovery from SCOUTED sources stays manual (D15) — the import is
// the one machine writer, and only with first-party applicant data the
// person typed into the owner's own ATS (decision 2026-09-03).
func (s *Store) UpdateCandidate(id string, set map[string]string) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return Candidate{}, err
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic write order, so two identical edits agree
	for _, key := range keys {
		val := set[key]
		switch key {
		case "name":
			if strings.TrimSpace(val) == "" {
				return Candidate{}, errf("a candidate needs a name")
			}
			doc.Set("name", val)
		case "role":
			if val != "" && !s.roleExists(val) {
				return Candidate{}, errf("unknown role %q", val)
			}
			doc.Set("role", val)
		case "owner":
			doc.Set("owner", val)
		case "ashby_candidate_id", "ashby_application_id":
			doc.Set(key, val)
		default:
			if err := doc.SetProfile(key, val); err != nil {
				return Candidate{}, err
			}
		}
	}
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// AddEvidence captures one citation by hand. The URL, the quote and the date
// are preserved exactly as supplied — a citation is the thing that outlives
// every cache and every adapter.
func (s *Store) AddEvidence(id string, ev Evidence, now time.Time) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return Candidate{}, err
	}
	if strings.TrimSpace(ev.Kind) == "" {
		ev.Kind = sources.EvidenceOwnerNote
	}
	if strings.TrimSpace(ev.Collected) == "" {
		ev.Collected = now.UTC().Format("2006-01-02")
	}
	if strings.TrimSpace(ev.Source) == "" {
		ev.Source = "owner"
	}
	if _, err := doc.AddEvidence(ev); err != nil {
		return Candidate{}, err
	}
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// ScoreFit records one criterion's judgment against the candidate.
func (s *Store) ScoreFit(id, criterion, score string, evidence []string, present bool) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return Candidate{}, err
	}
	if err := doc.Score(criterion, score, evidence, present); err != nil {
		return Candidate{}, err
	}
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// SetOverride records — or clears — the D6 gate override on the record.
func (s *Store) SetOverride(id, by, reason string, now time.Time) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return Candidate{}, err
	}
	o := Override{By: strings.TrimSpace(by), Reason: strings.TrimSpace(reason)}
	if o.Present() {
		o.At = now.UTC().Format("2006-01-02")
	}
	if err := doc.SetOverride(o); err != nil {
		return Candidate{}, err
	}
	if err := s.SaveCandidate(slug, doc); err != nil {
		return Candidate{}, err
	}
	return s.candidateView(slug, doc), nil
}

// SetRoleCriteria replaces a role's must/nice/disqualifier bar.
func (s *Store) SetRoleCriteria(slug string, crits []Criterion) (Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.roleExists(slug) {
		return Role{}, errf("unknown role %q", slug)
	}
	slug = roleKey(slug)
	doc := s.LoadRole(slug)
	if err := doc.SetCriteria(crits); err != nil {
		return Role{}, err
	}
	if err := s.SaveRole(slug, doc); err != nil {
		return Role{}, err
	}
	return doc.View(slug, 0), nil
}

// AddSeed adds one entry to the high-signal seed set (D11).
func (s *Store) AddSeed(seed Seed, now time.Time) (Seed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.LoadSeeds()
	if strings.TrimSpace(seed.Added) == "" {
		seed.Added = now.UTC().Format("2006-01-02")
	}
	if strings.TrimSpace(seed.Source) == "" {
		seed.Source = "owner"
	}
	if seed.Class == SeedPerson && strings.TrimSpace(seed.Consent) == "" {
		seed.Consent = "owner"
	}
	stored, err := doc.Add(seed)
	if err != nil {
		return Seed{}, err
	}
	if err := s.SaveSeeds(doc); err != nil {
		return Seed{}, err
	}
	return stored, nil
}

// PersonIdentity is one candidate as the graph needs to name them: the record
// id, the display name, and the address that might match a calendar invite.
type PersonIdentity struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// Identities is the cheap read behind edge derivation: who is on the board and
// what they answer to.
//
// ⚠ It must NEVER call View(). View derives paths from networkEdges(), which
// asks the injected derivation, which asks who is on the board — so a View()
// here is an infinite recursion that takes the process down on the first page
// load. This reads the records directly, which is also all the caller needs.
func (s *Store) Identities() []PersonIdentity {
	out := []PersonIdentity{}
	for _, slug := range s.CandidateSlugs() {
		d := s.LoadCandidate(slug)
		id := d.Get("id")
		if id == "" {
			id = CandidateID(slug)
		}
		out = append(out, PersonIdentity{ID: id, Name: d.Get("name"), Email: d.Profile()["email"]})
	}
	return out
}

// Connectors is the same cheap read for network/people.md — the rows a path
// can start from — without deriving anything.
func (s *Store) Connectors() []NetworkPerson { return s.LoadNetworkPeople().People() }
