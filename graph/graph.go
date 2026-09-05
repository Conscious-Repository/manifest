// Package graph is the general entity/edge model (manifest P2 Phase 1): the
// recruiting network's proven relationship-claim primitive, lifted out of
// that domain so an entity of ANY kind — a task, a decision, a heuristic (the
// aion triad), an artifact, a person, an org, a paper, a repo, a property —
// can be a node, and a relationship between any two can be stated WITH its
// provenance. Nothing here was invented: Edge is recruiting's Edge with typed
// endpoints; the validator is recruiting's "no claim without a basis" rule;
// the path search is recruiting's intro-path walk. recruiting now delegates
// to this package (an adapter over its person-only vocabulary), so the two
// cannot drift.
//
// The contract an edge carries, in the order it matters:
//
//   - a CLAIM, never an assumed truth: `basis` says why we believe it and
//     `source` says who/what asserted it — an edge without either is refused
//     on every write path (Validate);
//   - fact vs inference is a STORED bit (`inferred`), never a rendering habit:
//     an overlap a scan derived must never present as a stated relationship;
//   - `confidence` is optional and bounded [0,1]; an unstated one weighs
//     UnstatedConfidence in a path so it never outranks a stated value;
//   - `evidence` points at what supports the claim (an evidence id, a URL, a
//     ledger ref); `observed` is when it was seen.
//
// Kinds are CLOSED but EXTENSIBLE: a Vocabulary names the entity kinds and
// edge kinds a graph accepts; Default() is the platform's, and a domain
// composes its own (recruiting keeps its adapter set). A kind outside the
// vocabulary is refused, not coerced.
//
// State, where this package holds any, is file-as-truth: two row documents
// (entities.md, edges.md — record.ParseRows fixpoint) written only through an
// injected write func (Store). The ledger event for an added edge is the
// server's job (it owns the ledger). UI-agnostic; stdlib + record only.
package graph

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"manifest/record"
)

// Entity kinds the platform knows (the closed default set). The aion triad
// first — tasks, decisions, heuristics are THE fundamental three — then the
// objects the other primitives already mint.
const (
	KindTask      = "task"      // composite todo id (inbox/x, aion:…, prop:slug/id)
	KindDecision  = "decision"  // aion backlog decision (aion-bl/<slug>) — P3's entity
	KindHeuristic = "heuristic" // aion heuristic id
	KindArtifact  = "artifact"  // artifacts.Artifact.ID (P1)
	KindPerson    = "person"    // aion-net/…, cand/…, ext/orcid/… (recruiting), contacts
	KindOrg       = "org"       // a company, lab, fund
	KindProject   = "project"   // a rock / initiative / property deal
	KindPaper     = "paper"     // a work: ext/openalex/W…, a DOI
	KindRepo      = "repo"      // ext/github/owner/name
	KindProperty  = "property"  // realestate property slug
)

// EntityKinds is the default closed set, in declaration order.
var EntityKinds = []string{KindTask, KindDecision, KindHeuristic, KindArtifact,
	KindPerson, KindOrg, KindProject, KindPaper, KindRepo, KindProperty}

// Edge kinds — the cross-domain vocabulary. The recruiting relationship set
// (coauthor, same_lab, …) is included verbatim so a person↔person claim means
// the same thing in both graphs; the rest name the triad's relationships:
// a task depends on a task or a decision, produced / consumes an artifact
// (the P1 binding, as edges), a heuristic supports or contradicts a decision,
// a decision blocks work, an artifact is about an entity.
const (
	EdgeDependsOn   = "depends_on"  // task → task | decision: cannot proceed until
	EdgeBlocks      = "blocks"      // decision | task → task: the reverse claim, stated directly
	EdgeProduced    = "produced"    // task | run → artifact
	EdgeConsumes    = "consumes"    // task → artifact
	EdgeSupports    = "supports"    // heuristic | artifact | paper → decision
	EdgeContradicts = "contradicts" // heuristic | artifact | paper → decision
	EdgeInforms     = "informs"     // artifact | paper | person → task | decision
	EdgeDecidedBy   = "decided_by"  // decision → person
	EdgeOwnedBy     = "owned_by"    // task | project | property → person
	EdgeAbout       = "about"       // artifact | paper | task → any
	EdgeMemberOf    = "member_of"   // person → org | project
	EdgeAuthored    = "authored"    // person → paper | repo | artifact
	EdgeReferences  = "references"  // any → any: a citation / wikilink
	EdgeRelated     = "related"     // any → any: the weakest stated tie

	// the recruiting relationship set (recruiting/sources.EdgeTypes), verbatim
	EdgeDirectKnown           = "direct_known"
	EdgeOwnerSaid             = "owner_said"
	EdgeCoauthor              = "coauthor"
	EdgeCoinventor            = "coinventor"
	EdgeCoworker              = "coworker"
	EdgeSameLab               = "same_lab"
	EdgeSameGrant             = "same_grant"
	EdgeSameRepo              = "same_repo"
	EdgeSameConference        = "same_conference"
	EdgeSameCompany           = "same_company"
	EdgeAdvisor               = "advisor"
	EdgeReferralPathCandidate = "referral_path_candidate"
	EdgeImportedExport        = "imported_export"
)

// EdgeKinds is the default closed set, in declaration order.
var EdgeKinds = []string{
	EdgeDependsOn, EdgeBlocks, EdgeProduced, EdgeConsumes, EdgeSupports, EdgeContradicts,
	EdgeInforms, EdgeDecidedBy, EdgeOwnedBy, EdgeAbout, EdgeMemberOf, EdgeAuthored,
	EdgeReferences, EdgeRelated,
	EdgeDirectKnown, EdgeOwnerSaid, EdgeCoauthor, EdgeCoinventor, EdgeCoworker,
	EdgeSameLab, EdgeSameGrant, EdgeSameRepo, EdgeSameConference, EdgeSameCompany,
	EdgeAdvisor, EdgeReferralPathCandidate, EdgeImportedExport,
}

// DependencyKinds are the edge kinds Upstream/Downstream follow by default:
// "what does this depend on" / "what depends on this".
var DependencyKinds = []string{EdgeDependsOn, EdgeConsumes}

// UnstatedConfidence is the weight of an edge whose row carries no
// [confidence::]. Deliberately below any owner-asserted value so a claim with
// no stated strength never outranks one that has it (recruiting's constant).
const UnstatedConfidence = 0.5

// ---- vocabulary: closed, extensible ----

// Vocabulary is the closed kind set one graph accepts. Closed: Validate
// refuses a kind outside it. Extensible: a domain composes its own with
// Extend, or from scratch — recruiting's is its adapter EdgeType set over the
// single entity kind `person`.
type Vocabulary struct {
	EntityKinds []string `json:"entityKinds"`
	EdgeKinds   []string `json:"edgeKinds"`
}

// Default is the platform vocabulary.
func Default() Vocabulary {
	return Vocabulary{EntityKinds: append([]string(nil), EntityKinds...), EdgeKinds: append([]string(nil), EdgeKinds...)}
}

// Extend returns a copy with the given kinds added (deduped, order kept).
func (v Vocabulary) Extend(entityKinds, edgeKinds []string) Vocabulary {
	return Vocabulary{EntityKinds: union(v.EntityKinds, entityKinds), EdgeKinds: union(v.EdgeKinds, edgeKinds)}
}

// ValidEntityKind reports membership.
func (v Vocabulary) ValidEntityKind(k string) bool { return inSet(v.EntityKinds, k) }

// ValidEdgeKind reports membership.
func (v Vocabulary) ValidEdgeKind(k string) bool { return inSet(v.EdgeKinds, k) }

func inSet(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

func union(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := map[string]bool{}
	for _, s := range append(append([]string(nil), a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ---- refs and entities ----

// Ref names one entity: its kind plus the id that kind's own store minted
// (a task's composite id, an artifact id, a recruiting person id). Wire form
// is `kind:id` — the same shape the ledger's object ref uses, so a graph
// endpoint and a ledger object are the same string.
type Ref struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// R is the short constructor.
func R(kind, id string) Ref { return Ref{Kind: strings.TrimSpace(kind), ID: strings.TrimSpace(id)} }

// IsZero reports an unset ref.
func (r Ref) IsZero() bool { return r.Kind == "" && r.ID == "" }

// String renders kind:id ("" when unset; a kindless ref renders as its id).
func (r Ref) String() string {
	if r.Kind == "" {
		return r.ID
	}
	return r.Kind + ":" + r.ID
}

// ParseRef reads `kind:id` (the FIRST colon splits — a task id such as
// `aion:123` keeps its own colon). No colon → a kindless ref, which Validate
// refuses; a caller with a default kind fills it in.
func ParseRef(s string) Ref {
	s = strings.TrimSpace(s)
	kind, id, ok := strings.Cut(s, ":")
	if !ok {
		return Ref{ID: s}
	}
	return Ref{Kind: strings.TrimSpace(kind), ID: strings.TrimSpace(id)}
}

// Entity is one node the graph knows by name — a registration, not the
// object itself (the task, artifact, or person lives in its own store; this
// row says the graph may point at it and how to find it). An edge endpoint
// need not be registered: the ref IS the identity, and an edge naming an
// entity no store has yet is still a claim (recruiting's ext/… keys).
type Entity struct {
	ID      string         `json:"id"`
	Kind    string         `json:"kind"`
	Title   string         `json:"title,omitempty"`
	Ref     string         `json:"ref,omitempty"`    // where it lives: vault path, URL, harness ref
	Source  string         `json:"source,omitempty"` // who registered it
	Added   string         `json:"added,omitempty"`  // date
	Unknown []record.Field `json:"unknown,omitempty"`
}

// Ref is the entity's endpoint form.
func (e Entity) AsRef() Ref { return Ref{Kind: e.Kind, ID: e.ID} }

// ValidateEntity refuses an entity without an id or with a kind outside the
// vocabulary.
func ValidateEntity(e Entity, v Vocabulary) error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("an entity needs an id")
	}
	if !v.ValidEntityKind(strings.TrimSpace(e.Kind)) {
		return fmt.Errorf("entity kind %q is not in the closed set", e.Kind)
	}
	return nil
}

// ---- edges ----

// Edge is one relationship CLAIM between two entities, with what supports
// it. Field for field recruiting's Edge, endpoints typed. `inferred` is
// mandatory and stored. Confidence is kept as the string the row carries
// ("0.95"), so a round-trip never rewrites the owner's number; Weight reads
// it.
type Edge struct {
	From       Ref            `json:"from"`
	To         Ref            `json:"to"`
	Kind       string         `json:"kind"`
	Basis      string         `json:"basis"`
	Confidence string         `json:"confidence,omitempty"`
	Inferred   bool           `json:"inferred"`
	Source     string         `json:"source,omitempty"`
	Evidence   string         `json:"evidence,omitempty"`
	Observed   string         `json:"observed,omitempty"`
	Unknown    []record.Field `json:"unknown,omitempty"`
}

// Weight is the edge's confidence as a number: the stated value, or
// UnstatedConfidence when the row carries none.
func (e Edge) Weight() float64 {
	c := strings.TrimSpace(e.Confidence)
	if c == "" {
		return UnstatedConfidence
	}
	v, err := strconv.ParseFloat(c, 64)
	if err != nil {
		return UnstatedConfidence
	}
	return v
}

// Key is the identity of a claim for dedupe: the same two endpoints, in this
// direction, the same kind. A second scan of the same paper must not double
// every edge it already wrote.
func (e Edge) Key() string {
	return e.From.String() + "\x00" + e.To.String() + "\x00" + strings.TrimSpace(e.Kind)
}

// UndirectedKey is Key with the endpoints unordered — recruiting's edgeKey,
// for the relationship kinds that read the same either way (coauthor).
func (e Edge) UndirectedKey() string {
	a, b := e.From.String(), e.To.String()
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b + "\x00" + strings.TrimSpace(e.Kind)
}

// Validate is the "no claim without a basis" rule, unchanged from
// recruiting.ValidateEdge and now the ONE place it lives. An edge with no
// [basis::] is a bug: we would be asserting a relationship we cannot
// explain. The messages are recruiting's verbatim — its callers and tests
// read them.
func Validate(e Edge, v Vocabulary) error {
	if strings.TrimSpace(e.From.ID) == "" || strings.TrimSpace(e.To.ID) == "" {
		return errors.New("an edge needs both endpoints")
	}
	for _, r := range []Ref{e.From, e.To} {
		if !v.ValidEntityKind(strings.TrimSpace(r.Kind)) {
			return fmt.Errorf("entity kind %q is not in the closed set", r.Kind)
		}
	}
	if !v.ValidEdgeKind(strings.TrimSpace(e.Kind)) {
		return fmt.Errorf("edge kind %q is not in the closed set", e.Kind)
	}
	if strings.TrimSpace(e.Basis) == "" {
		return errors.New("an edge needs a basis — an unexplainable claim is not a claim")
	}
	if strings.TrimSpace(e.Source) == "" {
		return errors.New("an edge needs the source that supports it")
	}
	if c := strings.TrimSpace(e.Confidence); c != "" {
		f, err := strconv.ParseFloat(c, 64)
		if err != nil || f < 0 || f > 1 {
			return errors.New("edge confidence must be between 0 and 1")
		}
	}
	return nil
}

// FormatConfidence renders a confidence the way the records carry it (two
// decimals, so 0.95 never reads as 0.9500000000000001).
func FormatConfidence(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
