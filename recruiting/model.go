// Package recruiting is the PRIVATE AION scout domain (plan
// 2026-09-01_220154-aion-scout-codebase-audit.md): vault-backed records under
// <vault>/system/aion/recruiting/ holding open roles, an evidence-backed
// candidate board, the high-signal seed set, and the owner-asserted network.
//
// It MUST NOT import `manifest/aion`. That package is not a private domain —
// it is the PUBLIC export contract (nine files served open-read on the team
// portal and copied into the kairos pack). Recruiting records carry candidate
// PII; keeping the import edge absent is what makes serving one through the
// portal something the code cannot express rather than something it declines
// to do. `aion` must not import `recruiting` either.
//
// Every byte written to the vault goes through the injected write func, bound
// in main to the narrow `aion-recruiting` vaultwriter capability (§A3 write
// boundary). This package never opens a vault file to write. The one direct
// write path it holds is the source-run cache (runs.go), rooted under
// dataDir — outside the vault by construction, and refused inside it.
package recruiting

import (
	"strings"

	"manifest/record"
	"manifest/recruiting/sources"
)

// Field is the kernel's inline-field pair.
type Field = record.Field

// DocFM is the shared frontmatter carrier: raw block lines (nil = no block)
// plus whether a blank line separated the fence from the body — preserved so
// parse→emit stays byte-identical.
type DocFM struct {
	FM      []string `json:"-"`
	FMBlank bool     `json:"-"`
}

// ---- closed sets ----

// Stages are the candidate board columns (plan §4.4). Closed: a stage outside
// this list is refused by the store, not coerced.
const (
	StageNew       = "new"
	StageReviewing = "reviewing"
	StageShortlist = "shortlist"
	StageIntro     = "intro"
	StageOutreach  = "outreach"
	StageReplied   = "replied"
	StageAshby     = "ashby"
	StageArchived  = "archived"
)

// Stages is the board order — the same order the columns paint in.
var Stages = []string{StageNew, StageReviewing, StageShortlist, StageIntro,
	StageOutreach, StageReplied, StageAshby, StageArchived}

// ValidStage reports whether s is a member of the closed stage set.
func ValidStage(s string) bool { return inSet(Stages, s) }

// Criterion classes (plan §4.4). `weight` is ignored for disqualifiers.
const (
	ClassMust         = "must"
	ClassNice         = "nice"
	ClassDisqualifier = "disqualifier"
)

var CriterionClasses = []string{ClassMust, ClassNice, ClassDisqualifier}

func ValidCriterionClass(s string) bool { return inSet(CriterionClasses, s) }

// Seed classes — the four D11 seed classes, with lab and company split
// because they resolve through different adapters.
const (
	SeedPerson  = "person"
	SeedCompany = "company"
	SeedLab     = "lab"
	SeedWork    = "work"
	SeedRepo    = "repo"
)

var SeedClasses = []string{SeedPerson, SeedCompany, SeedLab, SeedWork, SeedRepo}

func ValidSeedClass(s string) bool { return inSet(SeedClasses, s) }

// PersonTypes / ConsentKinds are the network/people.md closed sets. There is
// deliberately no `visibility` field: everything here is private by
// construction, and a field that says "private" invites one that says
// otherwise.
var PersonTypes = []string{"founder", "employee", "advisor", "investor",
	"collaborator", "candidate", "external"}

var ConsentKinds = []string{"owner", "public_record", "owner_import", "manual"}

func ValidPersonType(s string) bool { return inSet(PersonTypes, s) }
func ValidConsent(s string) bool    { return inSet(ConsentKinds, s) }

// ValidEdgeKind delegates to the adapter layer's closed EdgeType set — the
// edge vocabulary is declared once, where the adapters that emit it live.
func ValidEdgeKind(s string) bool { return sources.ValidEdgeType(sources.EdgeType(s)) }

// ScoreUnknown is the literal a fit row carries when the criterion has not
// been judged. Any other value must be an integer 0–5.
const ScoreUnknown = "unknown"

// GateMinScore is the D6 bar: a `must` criterion needs at least this score,
// backed by at least one resolvable evidence id.
const GateMinScore = 3

func inSet(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

// ---- role ----

// RoleDoc is one roles/<slug>.md record.
type RoleDoc struct {
	DocFM
	Slug     string
	Sections []*Section
}

// Criterion is one `## criteria` row: the must/nice/disqualifier bar the fit
// gate reads.
type Criterion struct {
	Criterion string  `json:"criterion"`
	Class     string  `json:"class"`
	Weight    int     `json:"weight,omitempty"`
	Unknown   []Field `json:"unknown,omitempty"`
}

// Role is the read projection of a role record.
type Role struct {
	Slug           string      `json:"slug"`
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Status         string      `json:"status"`
	Location       string      `json:"location"`
	Employment     string      `json:"employment"`
	HandoffMode    string      `json:"handoffMode"`
	AshbyJobID     string      `json:"ashbyJobId"`
	AshbyPostingID string      `json:"ashbyPostingId"`
	AshbyProjectID string      `json:"ashbyProjectId"`
	Pinned         bool        `json:"pinned"`
	Source         string      `json:"source"`
	Synced         string      `json:"synced"`
	Criteria       []Criterion `json:"criteria"`
	Terms          []string    `json:"terms"`
	Posting        string      `json:"posting"`
	OpenCount      int         `json:"openCount"`
}

// ---- candidate ----

// CandidateDoc is one candidates/<slug>.md record.
type CandidateDoc struct {
	DocFM
	Slug     string
	Sections []*Section
}

// FitEntry is one `## fit` row: a criterion judged against this candidate.
type FitEntry struct {
	Criterion string   `json:"criterion"`
	Score     string   `json:"score"` // "0".."5" or ScoreUnknown
	Evidence  []string `json:"evidence,omitempty"`
	Present   bool     `json:"present,omitempty"` // disqualifier confirmed present
}

// Evidence is one `## evidence` row plus its verbatim snippet lines. The
// citation (url/file, kind, date) is what survives a source-run cache sweep.
type Evidence struct {
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
	File      string `json:"file,omitempty"`
	Collected string `json:"collected"`
	Kind      string `json:"kind"`
	Source    string `json:"source,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
}

// PathClaim is one ranked intro path: either a hand-authored `## network` row
// (owner evidence, kind as written) or one DerivePaths computed from the
// network graph (Kind == PathKindDerived). Derived paths are never stored on
// the record — they are recomputed on every view from the edges that exist.
type PathClaim struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence,omitempty"`
	Inferred   bool   `json:"inferred"`
}

// NextAction is one `## next` row.
type NextAction struct {
	Action string `json:"action"`
	Due    string `json:"due,omitempty"`
	Owner  string `json:"owner,omitempty"`
}

// OutreachRef is one `## outreach` row — a pointer at the Phase 5 log, never
// the message bytes.
type OutreachRef struct {
	Log    string `json:"log"`
	Last   string `json:"last,omitempty"`
	Status string `json:"status,omitempty"`
	// MessageID / ThreadID are the Gmail ids of the last send (Phase 5) —
	// the join a later reply sync matches on. Never the message bytes.
	MessageID string `json:"messageId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
}

// Override is the recorded D6 gate override. It is written ONTO the record so
// the judgment is auditable rather than invisible.
type Override struct {
	By     string `json:"by,omitempty"`
	Reason string `json:"reason,omitempty"`
	At     string `json:"at,omitempty"`
}

func (o Override) Present() bool { return strings.TrimSpace(o.By) != "" }

// Candidate is the read projection of a candidate record.
type Candidate struct {
	Slug               string            `json:"slug"`
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Role               string            `json:"role"`
	Stage              string            `json:"stage"`
	Owner              string            `json:"owner"`
	PII                bool              `json:"pii"`
	AshbyCandidateID   string            `json:"ashbyCandidateId,omitempty"`
	AshbyApplicationID string            `json:"ashbyApplicationId,omitempty"`
	SourceRef          string            `json:"sourceRef,omitempty"`
	AshbyStage         string            `json:"ashbyStage,omitempty"` // Ashby-authoritative official stage
	Inbound            string            `json:"inbound,omitempty"`    // date this record arrived AS AN APPLICANT (sync-back import); untriaged while stage is still `ashby`
	Created            string            `json:"created"`
	Archived           string            `json:"archived,omitempty"`
	Profile            map[string]string `json:"profile"`
	Fit                []FitEntry        `json:"fit"`
	Evidence           []Evidence        `json:"evidence"`
	Paths              []PathClaim       `json:"paths"`
	Outreach           []OutreachRef     `json:"outreach"`
	Next               []NextAction      `json:"next"`
	Override           Override          `json:"override"`
	Gate               GateState         `json:"gate"`
	// Resume is the applicant's own file as the record REFERENCES it: the
	// artifact-pool hash and their filename. The bytes never enter the vault.
	Resume ResumeRef `json:"resume"`
}

// ResumeRef is a stored resume by reference.
type ResumeRef struct {
	Hash string `json:"hash,omitempty"`
	Name string `json:"name,omitempty"`
}

// ProfileKeys is the closed profile vocabulary, in emit order. `email` and
// `phone` are here because the OWNER may type them by hand (D15) — no adapter
// and no converter ever fills them in.
var ProfileKeys = []string{"title", "org", "location", "linkedin", "github",
	"website", "email", "phone"}

// ContactKeys are the profile fields no adapter may ever set (D15). The
// draft converter drops them; a published address arrives as evidence.
var ContactKeys = []string{"email", "phone"}

// ---- seeds ----

// Seed is one seeds.md row: what a source run is scoped FROM. A seed is not
// itself a candidate.
type Seed struct {
	ID      string  `json:"id"`
	Class   string  `json:"class"`
	Name    string  `json:"name"`
	Org     string  `json:"org,omitempty"`
	URL     string  `json:"url,omitempty"`
	Added   string  `json:"added,omitempty"`
	Source  string  `json:"source,omitempty"`
	Consent string  `json:"consent,omitempty"`
	Unknown []Field `json:"unknown,omitempty"`
}

// SeedsDoc is seeds.md.
type SeedsDoc struct {
	DocFM
	Lines []Line
}

// ---- network ----

// NetworkPerson is one network/people.md row.
type NetworkPerson struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type,omitempty"`
	Email    string  `json:"email,omitempty"`
	LinkedIn string  `json:"linkedin,omitempty"`
	GitHub   string  `json:"github,omitempty"`
	Org      string  `json:"org,omitempty"`
	Title    string  `json:"title,omitempty"`
	Source   string  `json:"source,omitempty"`
	Consent  string  `json:"consent,omitempty"`
	Added    string  `json:"added,omitempty"`
	Unknown  []Field `json:"unknown,omitempty"`
}

// PeopleDoc is network/people.md.
type PeopleDoc struct {
	DocFM
	Lines []Line
}

// Edge is one network/edges.md row — a relationship CLAIM, never an assumed
// truth. `inferred` is mandatory and stored, because the UI's visual
// distinction has to key off a fact rather than a rendering habit.
type Edge struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Kind       string  `json:"kind"`
	Basis      string  `json:"basis"`
	Confidence string  `json:"confidence,omitempty"`
	Inferred   bool    `json:"inferred"`
	Source     string  `json:"source,omitempty"`
	Evidence   string  `json:"evidence,omitempty"`
	Observed   string  `json:"observed,omitempty"`
	Unknown    []Field `json:"unknown,omitempty"`
}

// EdgesDoc is network/edges.md.
type EdgesDoc struct {
	DocFM
	Lines []Line
}

// NetworkView is the network summary the board carries.
type NetworkView struct {
	People []NetworkPerson `json:"people"`
	Edges  []Edge          `json:"edges"`
}

// ---- helpers shared by the parsers ----

func boolField(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "y", "1":
		return true
	}
	return false
}

func emitBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// unknownFields collects the fields of a row outside a recognized vocabulary,
// so a hand-added `[foo:: bar]` survives a load → mutate → save.
func unknownFields(r *Row, known ...string) []Field {
	var out []Field
	for _, f := range r.Fields {
		if !inSetFold(known, f.Key) {
			out = append(out, f)
		}
	}
	return out
}

func inSetFold(set []string, s string) bool {
	for _, v := range set {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
