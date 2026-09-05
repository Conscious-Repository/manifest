// Package sources is the recruiting source-adapter layer (plan §4.9).
//
// An adapter is one source of candidates and relationship claims. Every method
// is read-only against the outside world and write-only into DTOs: an adapter
// holds NO vault writer, no store and no os write path, so it structurally
// cannot persist anything. `recruiting` imports this package and converts
// accepted drafts into records; this package must never import `recruiting`.
// One direction only — the same discipline that makes the portal unable to
// serve a private item.
package sources

import (
	"context"
	"strings"
	"time"
)

// Kind labels what family a source belongs to (the UI's adapter rail).
type Kind string

const (
	KindScholarly Kind = "scholarly"
	KindCode      Kind = "code"
	KindGrant     Kind = "grant"
	KindATS       Kind = "ats"
	KindWeb       Kind = "web"
	KindManual    Kind = "manual"
	KindImport    Kind = "import"
)

// Trust ranks a citation: a primary record beats an aggregator beats loose
// page text. It is stored, never inferred at render time.
type Trust string

const (
	TrustHigh   Trust = "high"
	TrustMedium Trust = "medium"
	TrustLow    Trust = "low"
)

// EdgeType is the closed relationship-claim vocabulary (plan §4.9). Each
// value says what kind of claim it is; every edge carries the source that
// supports it.
type EdgeType string

const (
	EdgeDirectKnown           EdgeType = "direct_known"
	EdgeOwnerSaid             EdgeType = "owner_said"
	EdgeCoauthor              EdgeType = "coauthor"
	EdgeCoinventor            EdgeType = "coinventor"
	EdgeCoworker              EdgeType = "coworker"
	EdgeSameLab               EdgeType = "same_lab"
	EdgeSameGrant             EdgeType = "same_grant"
	EdgeSameRepo              EdgeType = "same_repo"
	EdgeSameConference        EdgeType = "same_conference"
	EdgeSameCompany           EdgeType = "same_company"
	EdgeAdvisor               EdgeType = "advisor"
	EdgeReferralPathCandidate EdgeType = "referral_path_candidate"
	EdgeImportedExport        EdgeType = "imported_export"

	// Two kinds added 2026-09-05 for the derivations that read the owner's own
	// life rather than a public index. Both are deliberate widenings of a
	// closed set, and both exist because the alternative was to file them
	// under a label that means something else — `owner_said` would claim the
	// owner asserted a relationship he never asserted.
	//
	// EdgeSameMeeting: both people were on the same calendar invite, on a
	// date. It claims they were in one room; it does NOT claim either would
	// take the other's call.
	EdgeSameMeeting EdgeType = "same_meeting"
	// EdgeCoMentioned: one note links both people. The weakest claim in the
	// set — a planning note naming two people is not a relationship between
	// them — and the reason every edge built from it is inferred, low
	// confidence, and names the note it came from.
	EdgeCoMentioned EdgeType = "co_mentioned"
)

// EdgeTypes is the closed set, in the plan's declaration order.
var EdgeTypes = []EdgeType{
	EdgeDirectKnown, EdgeOwnerSaid, EdgeCoauthor, EdgeCoinventor, EdgeCoworker,
	EdgeSameLab, EdgeSameGrant, EdgeSameRepo, EdgeSameConference, EdgeSameCompany,
	EdgeAdvisor, EdgeReferralPathCandidate, EdgeImportedExport,
	EdgeSameMeeting, EdgeCoMentioned,
}

func ValidEdgeType(t EdgeType) bool {
	for _, v := range EdgeTypes {
		if v == t {
			return true
		}
	}
	return false
}

// OwnerConfidence is the confidence-table entry for a relationship the owner
// states outright (direct_known / owner_said) — the strongest edge in the
// system, and the only one asserting a real human relationship.
const OwnerConfidence = 0.95

// Evidence kinds. `contact_published` is how a published address enters the
// system (D15): as a citation, never as a profile field.
const (
	EvidencePublication      = "publication"
	EvidenceRepo             = "repo"
	EvidenceGrant            = "grant"
	EvidenceAffiliation      = "affiliation"
	EvidencePage             = "page"
	EvidenceConference       = "conference"
	EvidenceATSRecord        = "ats_record"
	EvidenceContactPublished = "contact_published"
	EvidenceOwnerNote        = "owner_note"
)

// ScopeField declares one input the UI must collect before a run.
type ScopeField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Scope is one run's explicit, visible blast radius: which role, what query,
// how many results at most, and whether anything may be written at all.
type Scope struct {
	Role   string            `json:"role"`
	Query  string            `json:"query"`
	Max    int               `json:"max"`
	DryRun bool              `json:"dryRun"`
	Fields map[string]string `json:"fields,omitempty"`
}

// Evidence is a citation an adapter produced. Snippet is VERBATIM, never
// paraphrased.
type Evidence struct {
	SourceID    string    `json:"sourceId"`
	URLOrFile   string    `json:"urlOrFile"`
	RetrievedAt time.Time `json:"retrievedAt"`
	Snippet     string    `json:"snippet"`
	Kind        string    `json:"kind"`
	Trust       Trust     `json:"trust"`
}

// Cited reports whether this evidence can be pointed at later. A URL or file
// is the normal citation; an owner note's citation is the owner's own words,
// which is why a snippet satisfies it and nothing else does.
func (e Evidence) Cited() bool {
	if strings.TrimSpace(e.URLOrFile) != "" {
		return true
	}
	return e.Kind == EvidenceOwnerNote && strings.TrimSpace(e.Snippet) != ""
}

// EdgeClaim is a relationship claim with the basis that supports it. An edge
// with an empty Basis or SourceID cannot be serialized.
type EdgeClaim struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	Type       EdgeType `json:"type"`
	SourceID   string   `json:"sourceId"`
	Basis      string   `json:"basis"`
	Confidence float64  `json:"confidence"`
	Inferred   bool     `json:"inferred"`
}

// DedupeHint records whether a draft matched an existing vault candidate, and
// why — so the review queue can say "duplicate" with a reason.
type DedupeHint struct {
	CandidateID string `json:"candidateId,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// CandidateDraft is what an adapter emits. It is NOT a record: nothing here
// has touched the vault, and nothing does until the owner accepts it.
type CandidateDraft struct {
	SourceID   string   `json:"sourceId"`
	ExternalID string   `json:"externalId,omitempty"`
	Name       string   `json:"name"`
	Org        string   `json:"org,omitempty"`
	Title      string   `json:"title,omitempty"`
	Location   string   `json:"location,omitempty"`
	Links      []string `json:"links,omitempty"` // profile / repo / scholar URLs — never a guessed mailto
	Role       string   `json:"role,omitempty"`
	Note       string   `json:"note,omitempty"`

	// Classified presence (enrichment Phase 1, O3). Each is ONE URL taken
	// verbatim from Links, sorted there by host (links.go), never derived
	// from a name. Links stays the raw union; these are the labelled view a
	// card can print. Site is the fallback: a real page that is not a
	// platform's and that this code will not call a homepage. All omitempty,
	// so a queue written before they existed reads back unchanged.
	Homepage string `json:"homepage,omitempty"`
	LinkedIn string `json:"linkedin,omitempty"`
	Github   string `json:"github,omitempty"`
	Orcid    string `json:"orcid,omitempty"`
	Site     string `json:"site,omitempty"`

	// Topics are the person's knowledge chips, carried AS the provider said
	// them (O1) from their OWN author record only (O4) — never inferred from
	// what a co-author works on. Empty means the source named none, not that
	// the person has none. The `topics:` evidence snippet is the provenance.
	Topics []string `json:"topics,omitempty"`

	// Contact exists so D15 is TESTABLE rather than merely unimplemented: an
	// adapter that sets an email or phone here has it DROPPED by the
	// converter. A published address must arrive as an Evidence row of kind
	// contact_published instead, and be promoted onto the profile by hand.
	Contact map[string]string `json:"contact,omitempty"`

	Evidence []Evidence  `json:"evidence,omitempty"`
	Edges    []EdgeClaim `json:"edges,omitempty"`
	Dedupe   DedupeHint  `json:"dedupe,omitempty"`
}

// SourceRun is one run's trace. It lives in dataDir, NOT the vault (D14).
type SourceRun struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Scope     Scope     `json:"scope"`
	StartedAt time.Time `json:"startedAt"`
	Counts    struct {
		Fetched, New, Duplicate, Accepted, Rejected int
	} `json:"counts"`
	Cursor    string    `json:"cursor,omitempty"`
	Pinned    bool      `json:"pinned"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Adapter is one source of candidates and relationship claims.
type Adapter interface {
	ID() string
	Kind() Kind
	Scope() []ScopeField
	Search(ctx context.Context, s Scope) ([]CandidateDraft, error)
	Enrich(ctx context.Context, d CandidateDraft) (CandidateDraft, error)
	GraphEdges(ctx context.Context, d CandidateDraft) ([]EdgeClaim, error)
}
