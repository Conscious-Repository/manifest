package construction

import "encoding/json"

// SchemaVersion is the only document schema this build writes and edits.
// A document with a higher version opens read-only with exact export.
const SchemaVersion = 1

// CoordinateConvention names the world frame every document, compiler,
// renderer and exporter shares: right-handed, millimetres, +X along the wall,
// +Y horizontally outward from the wall toward the eave, +Z up, origin at the
// junction datum (top of the timber deck at the wall face). glTF maps
// (x, z, -y)/1000.
const CoordinateConvention = "construction-xyz-zup-mm/1"

// CompilerVersion names the geometry compiler that produced a model hash; a
// generator-only change is visible as a different version for equal input.
const CompilerVersion = "construction-compiler/1"

// NonApprovalNotice rides on every view, drawing and export.
const NonApprovalNotice = "Research/design assistance; not approved for construction; field, code, structural and manufacturer verification required."

// Document kinds (the "kind" field). (kind, id) is a document's identity.
const (
	DocProblem    = "construction.problem"
	DocAssembly   = "construction.assembly"
	DocValidation = "construction.validation"
	DocCatalog    = "construction.catalog"
	DocEvidence   = "construction.evidence"
	DocDecision   = "construction.decision"
	DocRun        = "construction.run"
	DocView       = "construction.view"
	DocDerived    = "construction.derived"
	DocReceipt    = "construction.receipt"
	DocHead       = "construction.head"
	DocSubject    = "construction.subject"
)

// Envelope is common to every immutable document. The wire revision token is
// SHA-256 of the canonical bytes — never a self-referential field.
type Envelope struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	Kind           string                     `json:"kind"`
	ID             string                     `json:"id"`
	RevisionNumber int                        `json:"revisionNumber"`
	ParentRevision string                     `json:"parentRevision,omitempty"`
	CreatedAt      string                     `json:"createdAt"`
	Actor          Actor                      `json:"actor"`
	Extensions     map[string]json.RawMessage `json:"extensions,omitempty"`
}

// Actor is who caused a document revision. The server derives it from the
// authenticated principal or the bound agent capability; a request body can
// never assert it.
type Actor struct {
	Kind       string `json:"kind"`                 // owner | agent | system
	Principal  string `json:"principal"`            // "owner", "agent:alfred", "system:construction"
	Agent      string `json:"agent,omitempty"`      // alfred | zeck (agent actors)
	Capability string `json:"capability,omitempty"` // bound capability id (agent actors)
	Run        string `json:"run,omitempty"`        // research run that produced it
}

const (
	ActorOwner  = "owner"
	ActorAgent  = "agent"
	ActorSystem = "system"
)

// OwnerActor is the trusted private-owner principal of this single-owner app.
func OwnerActor() Actor { return Actor{Kind: ActorOwner, Principal: "owner"} }

// SystemActor is the construction runtime itself (deterministic stages).
func SystemActor(run string) Actor {
	return Actor{Kind: ActorSystem, Principal: "system:construction", Run: run}
}

// AgentActor is an agent acting under a bound capability.
func AgentActor(agent, capability, run string) Actor {
	return Actor{Kind: ActorAgent, Principal: "agent:" + agent, Agent: agent, Capability: capability, Run: run}
}

// SubjectRef is the typed, immutable source the problem is bound to: an OODA
// property (id = exact property slug) or the shared Home (id = "home").
type SubjectRef struct {
	Kind string `json:"kind"` // property | home
	ID   string `json:"id"`
}

const (
	SubjectProperty = "property"
	SubjectHome     = "home"
	HomeSubjectID   = "home"
)

// ScopeRef optionally narrows a problem to existing coordination records by
// exact id: a property work-tree node (+ its task) or a shared Home task.
// Ids are never fuzzy-matched; a renamed source shows as an unresolved link.
type ScopeRef struct {
	WorkID         string `json:"workId,omitempty"`
	TaskID         string `json:"taskId,omitempty"`
	SourceRevision string `json:"sourceRevision,omitempty"`
}

// VersionRef pins one exact revision of a document or catalog entry.
type VersionRef struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// Provenance taxonomy (§3.5) — not interchangeable with source priority.
const (
	ProvVerifiedFact     = "verified-fact"
	ProvDirectGuidance   = "directly-applicable-guidance"
	ProvAdaptedPrecedent = "adapted-precedent"
	ProvInference        = "engineering-inference"
	ProvUserAssumption   = "user-assumption"
	ProvUnknown          = "unknown"
)

var provenances = map[string]bool{ProvVerifiedFact: true, ProvDirectGuidance: true, ProvAdaptedPrecedent: true,
	ProvInference: true, ProvUserAssumption: true, ProvUnknown: true}

// Quantity is a technical value with its unit and where it came from. An
// unknown value is null — never zero — with a reason; geometry that needs a
// number uses Placeholder and every drawing labels it unresolved.
type Quantity struct {
	Value        *float64 `json:"value"`
	Unit         string   `json:"unit"`
	State        string   `json:"state"` // known | assumed | unknown
	Provenance   string   `json:"provenance"`
	Illustrative bool     `json:"illustrative,omitempty"` // synthetic/test-only default
	Placeholder  *float64 `json:"placeholder,omitempty"`  // geometry stand-in while Value is unknown
	Note         string   `json:"note,omitempty"`
	EvidenceIDs  []string `json:"evidenceIds,omitempty"`
}

const (
	StateKnown   = "known"
	StateAssumed = "assumed"
	StateUnknown = "unknown"
)

// Effective is the number geometry uses: Value, else Placeholder.
func (q Quantity) Effective() (float64, bool) {
	if q.Value != nil {
		return *q.Value, true
	}
	if q.Placeholder != nil {
		return *q.Placeholder, true
	}
	return 0, false
}

// Label says how a drawing must present this value.
func (q Quantity) Label() string {
	switch {
	case q.Value == nil:
		return "unresolved"
	case q.Illustrative || q.State == StateAssumed:
		return "illustrative"
	default:
		return ""
	}
}

// ---- ConstructionProblem ------------------------------------------------------

type Problem struct {
	Envelope
	SubjectRef       SubjectRef        `json:"subjectRef"`
	ProjectRef       SubjectRef        `json:"projectRef"`
	PropertyRef      *SubjectRef       `json:"propertyRef"`
	ScopeRef         *ScopeRef         `json:"scopeRef"`
	Title            string            `json:"title"`
	Narrative        string            `json:"narrative"`
	Inputs           []InputRef        `json:"inputs"`
	Existing         []Fact            `json:"existing"`
	Proposed         []Fact            `json:"proposed"`
	Location         ContextFact       `json:"location"`
	Jurisdiction     ContextFact       `json:"jurisdiction"`
	Climate          ContextFact       `json:"climate"`
	Lifecycle        string            `json:"lifecycle"`
	Steward          Steward           `json:"steward"`
	Conversations    []ConversationRef `json:"conversations"`
	Alternatives     []string          `json:"alternatives"`
	ActiveAssembly   string            `json:"activeAssembly,omitempty"`
	SelectedAssembly *VersionRef       `json:"selectedAssembly"`
	Decisions        []string          `json:"decisions"`
	Links            Links             `json:"links"`
	LatestRun        string            `json:"latestRun,omitempty"`
	LatestView       string            `json:"latestView,omitempty"`
	UpdatedAt        string            `json:"updatedAt"`
}

const (
	LifecycleDraft         = "draft"
	LifecycleInvestigating = "investigating"
	LifecycleAlternatives  = "alternatives"
	LifecycleOwnerSelected = "owner-selected"
	LifecycleArchived      = "archived"
)

var problemLifecycles = map[string]bool{LifecycleDraft: true, LifecycleInvestigating: true,
	LifecycleAlternatives: true, LifecycleOwnerSelected: true, LifecycleArchived: true}

// InputRef is one retained owner input (photo, drawing, document). The bytes
// are an immutable artifact; this per-problem reference holds the owner's
// name, role and verification label — never the blob.
type InputRef struct {
	ArtifactID   string `json:"artifactId"`
	Revision     string `json:"revision"`
	Name         string `json:"name"`
	Mime         string `json:"mime"`
	Size         int64  `json:"size"`
	Role         string `json:"role"` // photo | drawing | document | other
	Label        string `json:"label,omitempty"`
	Verification string `json:"verification"` // not-field-verified | owner-confirmed | unknown
	RetainedAt   string `json:"retainedAt"`
	Actor        Actor  `json:"actor"`
}

var inputRoles = map[string]bool{"photo": true, "drawing": true, "document": true, "other": true}
var inputVerifications = map[string]bool{"not-field-verified": true, "owner-confirmed": true, "unknown": true}

// Fact is an owner-stated existing or proposed condition (a claim authored by
// the owner; provenance defaults to user-assumption).
type Fact struct {
	ID         string `json:"id"` // clm-
	Text       string `json:"text"`
	Provenance string `json:"provenance"`
}

// ContextFact is location, jurisdiction or climate with its provenance.
// Unknown is a first-class value; nothing is inferred from an address.
type ContextFact struct {
	Text       string `json:"text"`
	State      string `json:"state"` // known | assumed | unknown
	Provenance string `json:"provenance"`
}

// Steward: Alfred by default; Zeck only when explicitly selected. Agent
// identity is separate from provider/model.
type Steward struct {
	Agent    string `json:"agent"` // alfred | zeck
	Explicit bool   `json:"explicit"`
}

var stewards = map[string]bool{"alfred": true, "zeck": true}

// ConversationRef points at a native conversation (never a copy of it).
type ConversationRef struct {
	Agent        string `json:"agent"`
	Conversation string `json:"conversation"` // native descriptor key
	Session      string `json:"session"`      // native session id
	Purpose      string `json:"purpose"`      // research | steward
	CreatedAt    string `json:"createdAt"`
}

// Links are read-only references to existing coordination records. They are
// links, never dual writes; a source change is shown, not followed.
type Links struct {
	Tasks     []ExternalRef `json:"tasks"`
	Decisions []ExternalRef `json:"decisions"`
}

type ExternalRef struct {
	Kind string `json:"kind"` // property-task | home-task | property-work | re-decision
	ID   string `json:"id"`
	Note string `json:"note,omitempty"`
}

// ---- Assembly ---------------------------------------------------------------------

type Assembly struct {
	Envelope
	ProblemID       string              `json:"problemId"`
	Name            string              `json:"name"`
	Summary         string              `json:"summary"`
	Template        string              `json:"template"`
	DerivedFrom     *VersionRef         `json:"derivedFrom"`
	Correspondence  map[string]string   `json:"correspondence,omitempty"` // this component id → source component id
	Units           string              `json:"units"`
	Convention      string              `json:"coordinateConvention"`
	Junction        Junction            `json:"junction"`
	Parameters      map[string]Quantity `json:"parameters"`
	Components      []Component         `json:"components"`
	Tombstones      []Tombstone         `json:"tombstones"`
	Relationships   []Relationship      `json:"relationships"`
	Assumptions     []Assumption        `json:"assumptions"`
	EvidenceLinks   []EvidenceLink      `json:"evidenceLinks"`
	IssueStates     map[string]IssueAck `json:"issueStates"`
	Lifecycle       string              `json:"lifecycle"`
	Applicability   Applicability       `json:"applicability"`
	Research        *VersionRef         `json:"research"`
	CompilerVersion string              `json:"compilerVersion"`
	ModelHash       string              `json:"modelHash"`
}

const (
	AssemblyDraft         = "draft"
	AssemblyProposed      = "proposed"
	AssemblyOwnerSelected = "owner-selected" // never "approved for construction"
	AssemblySuperseded    = "superseded"
)

var assemblyLifecycles = map[string]bool{AssemblyDraft: true, AssemblyProposed: true,
	AssemblyOwnerSelected: true, AssemblySuperseded: true}

// Junction is the typed connection the assembly details.
type Junction struct {
	ID                  string        `json:"id"` // jct-
	Type                string        `json:"type"`
	Orientation         string        `json:"orientation"` // unresolved | headwall | sidewall
	WallCondition       WallCondition `json:"wallCondition"`
	Strategy            string        `json:"strategy"`
	Components          []string      `json:"components"`
	Transitions         []Transition  `json:"transitions"`
	AttachmentPath      []string      `json:"attachmentPath"`
	SupportedStrategies []string      `json:"supportedStrategies"`
}

// WallCondition is verified or assumed, never inferred from wythe count.
type WallCondition struct {
	Value      string `json:"value"` // unknown | solid-bonded | cavity | other
	Provenance string `json:"provenance"`
	Note       string `json:"note,omitempty"`
}

var wallConditions = map[string]bool{"unknown": true, "solid-bonded": true, "cavity": true, "other": true}
var orientations = map[string]bool{"unresolved": true, "headwall": true, "sidewall": true}

// Transition is an air/water/vapour continuity requirement between layers.
type Transition struct {
	Kind     string   `json:"kind"` // air | water | vapor
	From     string   `json:"from"` // component id
	To       string   `json:"to"`   // component id
	Via      []string `json:"via"`  // component ids that make the transition
	Required bool     `json:"required"`
	Note     string   `json:"note,omitempty"`
}

// Component is one semantic part. Identity, geometry parameters, material,
// product and appearance are separate fields; no vertex data, no scripts.
type Component struct {
	ID            string     `json:"id"` // cmp-
	Type          string     `json:"type"`
	Role          string     `json:"role"`
	Name          string     `json:"name"`
	Layer         *LayerSpec `json:"layer"`
	Shape         Shape      `json:"shape"`
	Transform     Transform  `json:"transform"`
	HostID        string     `json:"hostId,omitempty"`
	Material      *PinRef    `json:"material"`
	Product       *PinRef    `json:"product"`
	Appearance    string     `json:"appearance,omitempty"`
	Attachments   []string   `json:"attachments"`
	Notes         string     `json:"notes,omitempty"`
	Applicability string     `json:"applicability"` // applicable | conditional | inapplicable
}

// PinRef pins a catalog entry version; it never follows catalog head.
type PinRef struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// LayerSpec places a component in the ordered roof stack.
type LayerSpec struct {
	Order int    `json:"order"`
	Role  string `json:"role"`
}

// Shape is an allowlisted primitive kind with typed parameters.
type Shape struct {
	Kind   string              `json:"kind"`
	Params map[string]Quantity `json:"params"`
}

// Transform is a supported rigid offset: translation (mm) and a unit
// quaternion (x, y, z, w). No scale, shear or mirroring exists to set.
type Transform struct {
	Translation [3]float64 `json:"translation"`
	Rotation    [4]float64 `json:"rotation"`
}

// IdentityTransform is no offset.
func IdentityTransform() Transform { return Transform{Rotation: [4]float64{0, 0, 0, 1}} }

// Tombstone records a removed component; its id is never reused.
type Tombstone struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	RemovedAt string `json:"removedAt"`
	RemovedBy Actor  `json:"removedBy"`
	Revision  int    `json:"revision"` // assembly revision number that removed it
}

type Relationship struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // hosts | laps-over | attaches-to | seals | drains-to | bears-on
}

var relationshipKinds = map[string]bool{"hosts": true, "laps-over": true, "attaches-to": true,
	"seals": true, "drains-to": true, "bears-on": true}

type Assumption struct {
	Key        string `json:"key"`
	Text       string `json:"text"`
	Provenance string `json:"provenance"`
	Status     string `json:"status"` // open | confirmed | withdrawn
}

// EvidenceLink ties evidence to a component, junction, parameter or the
// assembly. Evidence stays on surviving ids across revisions.
type EvidenceLink struct {
	Target     string `json:"target"` // component id, junction id, "param:<name>" or the assembly id
	EvidenceID string `json:"evidenceId"`
	Relation   string `json:"relation"` // supports | contradicts | informs
}

var evidenceRelations = map[string]bool{"supports": true, "contradicts": true, "informs": true}

// IssueAck is human state about a deterministic issue (rule key + target).
// Acknowledgement never validates unsafe engineering.
type IssueAck struct {
	Status string `json:"status"` // acknowledged
	Actor  Actor  `json:"actor"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

// Applicability of an alternative under the current known conditions.
type Applicability struct {
	Status     string   `json:"status"` // applicable | conditional | inapplicable | unknown
	Conditions []string `json:"conditions"`
	Reasons    []string `json:"reasons"`
}

var applicabilityStatuses = map[string]bool{"applicable": true, "conditional": true, "inapplicable": true, "unknown": true}

// ---- ValidationIssue / report ---------------------------------------------------

type Issue struct {
	ID               string    `json:"id"` // iss-
	RuleKey          string    `json:"ruleKey"`
	RuleVersion      int       `json:"ruleVersion"`
	Target           string    `json:"target"`
	Severity         string    `json:"severity"` // blocking | critical-unresolved | advisory | informational
	Category         string    `json:"category"`
	ComponentIDs     []string  `json:"componentIds"`
	JunctionIDs      []string  `json:"junctionIds"`
	Observed         string    `json:"observed"`
	Expected         string    `json:"expected"`
	Message          string    `json:"message"`
	EvidenceIDs      []string  `json:"evidenceIds"`
	Status           string    `json:"status"` // open | resolved | acknowledged | superseded
	Resolution       *IssueAck `json:"resolution"`
	SpecialistReview bool      `json:"specialistReview"`
	Deterministic    bool      `json:"deterministic"`
	Inputs           []string  `json:"inputs"` // the parameters/fields the rule read
}

const (
	SevBlocking = "blocking"
	SevCritical = "critical-unresolved"
	SevAdvisory = "advisory"
	SevInfo     = "informational"
)

type ValidationReport struct {
	Envelope
	AssemblyID       string  `json:"assemblyId"`
	AssemblyRevision string  `json:"assemblyRevision"`
	RuleSet          string  `json:"ruleSet"`
	GeometryHash     string  `json:"geometryHash"`
	Issues           []Issue `json:"issues"`
	Counts           Counts  `json:"counts"`
}

type Counts struct {
	Blocking      int `json:"blocking"`
	Critical      int `json:"critical"`
	Advisory      int `json:"advisory"`
	Informational int `json:"informational"`
}

// ---- Catalog: materials and products ------------------------------------------

// Catalog is the problem-local, evidence-pinned catalog (§12.6). Entries
// carry their own revision numbers; components pin (id, revision).
type Catalog struct {
	Envelope
	ProblemID string     `json:"problemId"`
	Materials []Material `json:"materials"`
	Products  []Product  `json:"products"`
}

type Material struct {
	ID            string                    `json:"id"` // mat-
	Revision      int                       `json:"revision"`
	History       []MaterialVersion         `json:"history,omitempty"`
	Family        string                    `json:"family"`
	Name          string                    `json:"name"`
	Generic       bool                      `json:"generic"`
	Properties    map[string]TechnicalValue `json:"properties"`
	Limits        []string                  `json:"limits"`
	Compatibility []CompatibilityNote       `json:"compatibility"`
	Appearance    Appearance                `json:"appearance"`
	Unknowns      []string                  `json:"unknowns"`
}

// MaterialVersion keeps earlier pinned revisions resolvable after an edit.
type MaterialVersion struct {
	Revision   int                       `json:"revision"`
	Name       string                    `json:"name"`
	Properties map[string]TechnicalValue `json:"properties"`
	Appearance Appearance                `json:"appearance"`
}

// TechnicalValue: a property with unit, test condition and provenance; null
// is unknown, never zero.
type TechnicalValue struct {
	Value         *float64 `json:"value"`
	Unit          string   `json:"unit"`
	TestCondition string   `json:"testCondition,omitempty"`
	Provenance    string   `json:"provenance"`
	EvidenceID    string   `json:"evidenceId,omitempty"`
	Note          string   `json:"note,omitempty"`
}

type CompatibilityNote struct {
	With       string `json:"with"`   // material family or id
	Status     string `json:"status"` // compatible | incompatible | unknown | conditional
	Provenance string `json:"provenance"`
	EvidenceID string `json:"evidenceId,omitempty"`
	Note       string `json:"note,omitempty"`
}

// Appearance is visual only; it never changes identity or specification.
type Appearance struct {
	Color     string  `json:"color"` // #rrggbb
	Roughness float64 `json:"roughness"`
	Metalness float64 `json:"metalness"`
	Opacity   float64 `json:"opacity"`
}

type Product struct {
	ID               string                    `json:"id"` // prd-
	Revision         int                       `json:"revision"`
	Manufacturer     string                    `json:"manufacturer"`
	Fictional        bool                      `json:"fictional"`
	Family           string                    `json:"family"`
	Model            string                    `json:"model"`
	SKU              string                    `json:"sku,omitempty"`
	Dimensions       map[string]TechnicalValue `json:"dimensions"`
	Options          []string                  `json:"options"`
	Materials        []PinRef                  `json:"materials"`
	Facts            []ProductFact             `json:"facts"`
	Documents        []ProductDocument         `json:"documents"`
	Geography        string                    `json:"geography"`
	DocumentRevision string                    `json:"documentRevision"`
	CheckedAt        string                    `json:"checkedAt"`
	Lifecycle        string                    `json:"lifecycle"` // active | stale | withdrawn | unknown
}

type ProductFact struct {
	Kind       string `json:"kind"` // dimension | compatibility | installation | limit
	Text       string `json:"text"`
	EvidenceID string `json:"evidenceId,omitempty"`
	Verified   bool   `json:"verified"`
}

type ProductDocument struct {
	SourceID string `json:"sourceId"`
	Title    string `json:"title"`
	URL      string `json:"url,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// ---- Evidence graph -------------------------------------------------------------

type EvidenceBundle struct {
	Envelope
	ProblemID string     `json:"problemId"`
	Sources   []Source   `json:"sources"`
	Claims    []Claim    `json:"claims"`
	Evidence  []Evidence `json:"evidence"`
	Relations []Relation `json:"relations"`
}

type Source struct {
	ID             string      `json:"id"` // src-
	Title          string      `json:"title"`
	Publisher      string      `json:"publisher"`
	Class          string      `json:"class"` // code | code-interpretation | manufacturer | trade-association | engineering | detail-library | secondary | owner-document
	Fictional      bool        `json:"fictional"`
	URL            string      `json:"url,omitempty"`
	Locator        string      `json:"locator,omitempty"` // authorized document locator (e.g. input:<artifactId>)
	DocumentRev    string      `json:"documentRevision,omitempty"`
	PublishedAt    string      `json:"publishedAt,omitempty"`
	RetrievedAt    string      `json:"retrievedAt,omitempty"`
	FinalURL       string      `json:"finalUrl,omitempty"`
	Mime           string      `json:"mime,omitempty"`
	Bytes          int64       `json:"bytes,omitempty"`
	ContentHash    string      `json:"contentHash,omitempty"`
	Snapshot       *VersionRef `json:"snapshot"` // retained artifact (id, revision) when allowed
	Excerpt        string      `json:"excerpt,omitempty"`
	OmissionReason string      `json:"omissionReason,omitempty"`
	Extraction     Extraction  `json:"extraction"`
	Access         string      `json:"access"` // open | licensed | restricted | unknown
	Jurisdiction   string      `json:"jurisdiction,omitempty"`
	Edition        string      `json:"edition,omitempty"`
}

// Extraction records how page text was obtained and how good its page map is.
type Extraction struct {
	Tool      string `json:"tool"`
	Version   string `json:"version"`
	PageMap   string `json:"pageMap"` // exact | approximate | unavailable
	Pages     int    `json:"pages"`
	PagesHash string `json:"pagesHash,omitempty"`
}

type Claim struct {
	ID            string       `json:"id"` // clm-
	Statement     string       `json:"statement"`
	Values        []ClaimValue `json:"values"`
	Provenance    string       `json:"provenance"`
	Applicability []string     `json:"applicability"`
	Confidence    float64      `json:"confidence"`
	Verification  string       `json:"verification"` // unverified | quote-verified | owner-reviewed | contradicted
	Supporting    []string     `json:"supporting"`   // evidence ids
	Contradicting []string     `json:"contradicting"`
	Author        Actor        `json:"author"`
	Rationale     string       `json:"rationale,omitempty"`
}

type ClaimValue struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type Evidence struct {
	ID             string   `json:"id"` // evd-
	SourceID       string   `json:"sourceId"`
	SourceRevision string   `json:"sourceRevision"` // content hash of the retained source
	Quote          string   `json:"quote"`
	Page           int      `json:"page"`      // physical PDF/page-map page (1-based; 0 unknown)
	PageLabel      string   `json:"pageLabel"` // printed label
	Figure         string   `json:"figure,omitempty"`
	Table          string   `json:"table,omitempty"`
	Section        string   `json:"section,omitempty"`
	Offsets        [2]int   `json:"offsets"`
	Method         string   `json:"method"`     // page-text-match | owner-supplied-excerpt | fixture
	QuoteMatch     string   `json:"quoteMatch"` // exact | normalized | not-found | unavailable
	Normalization  string   `json:"normalization,omitempty"`
	Supports       []string `json:"supports"` // claim/component/constraint refs
	Refutes        []string `json:"refutes"`
	Applicability  []string `json:"applicability"`
	Verification   string   `json:"verification"` // verified | unverified
	Confidence     float64  `json:"confidence"`
}

// Relation is a canonical graph edge kept inside the private snapshot; it is
// validated with graph.Validate over the construction vocabulary.
type Relation struct {
	From       string  `json:"from"` // kind:id
	To         string  `json:"to"`
	Kind       string  `json:"kind"` // supports | contradicts | informs | references
	Basis      string  `json:"basis"`
	Source     string  `json:"source"`
	Inferred   bool    `json:"inferred"`
	Confidence float64 `json:"confidence"`
}

// ---- DesignDecision -------------------------------------------------------------

type Decision struct {
	Envelope
	ProblemID        string        `json:"problemId"`
	Title            string        `json:"title"`
	Proposal         string        `json:"proposal"`
	Rationale        string        `json:"rationale"`
	Assembly         VersionRef    `json:"assembly"`
	Alternatives     []VersionRef  `json:"alternatives"`
	Supporting       []string      `json:"supporting"`
	Contradicting    []string      `json:"contradicting"`
	OpenQuestions    []string      `json:"openQuestions"`
	UnresolvedIssues []IssueBrief  `json:"unresolvedIssues"`
	ProposedBy       Actor         `json:"proposedBy"`
	Status           string        `json:"status"` // proposed | accepted-for-project | rejected | superseded
	ReviewedBy       *Actor        `json:"reviewedBy"`
	ReviewedAt       string        `json:"reviewedAt,omitempty"`
	ReviewNote       string        `json:"reviewNote,omitempty"`
	Links            []ExternalRef `json:"links"`
}

const (
	DecisionProposed   = "proposed"
	DecisionAccepted   = "accepted-for-project" // never approved-for-construction
	DecisionRejected   = "rejected"
	DecisionSuperseded = "superseded"
)

type IssueBrief struct {
	ID       string `json:"id"`
	RuleKey  string `json:"ruleKey"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// ---- ResearchRun ------------------------------------------------------------------

type ResearchRun struct {
	Envelope
	ProblemID           string          `json:"problemId"`
	BaseProblemRevision string          `json:"baseProblemRevision"`
	Plan                RunPlan         `json:"plan"`
	Agent               AgentChoice     `json:"agent"`
	State               string          `json:"state"`
	Epoch               int             `json:"epoch"`
	Stages              []Stage         `json:"stages"`
	StopRequested       bool            `json:"stopRequested"`
	StopRequestedAt     string          `json:"stopRequestedAt,omitempty"`
	Sequence            int             `json:"sequence"`
	Events              []RunEvent      `json:"events"`
	Publication         *Publication    `json:"publication"`
	Capabilities        RunCapabilities `json:"capabilities"`
	Counts              RunCounts       `json:"counts"`
}

// Run/stage states (§6).
const (
	RunPlanned       = "planned"
	RunQueued        = "queued"
	RunRunning       = "running"
	RunWaitingInput  = "waiting-input"
	RunStopRequested = "stop-requested"
	RunCancelled     = "cancelled"
	RunFailed        = "failed"
	RunDisconnected  = "disconnected"
	RunCompleted     = "completed"
)

var runStates = map[string]bool{RunPlanned: true, RunQueued: true, RunRunning: true, RunWaitingInput: true,
	RunStopRequested: true, RunCancelled: true, RunFailed: true, RunDisconnected: true, RunCompleted: true}

// Research stages in order.
var StageNames = []string{"decompose", "plan", "acquire", "extract", "synthesize", "compile", "validate", "publish"}

type RunPlan struct {
	Questions      []Question `json:"questions"`
	Scope          string     `json:"scope"`
	SourceBudget   int        `json:"sourceBudget"`
	SourcePriority []string   `json:"sourcePriority"`
}

type Question struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"` // open | answered | unanswerable
}

// AgentChoice separates agent identity from provider/model. Observed values
// live per attempt; unknown stays unknown.
type AgentChoice struct {
	Agent             string `json:"agent"` // alfred | zeck
	RequestedProvider string `json:"requestedProvider,omitempty"`
	RequestedModel    string `json:"requestedModel,omitempty"`
	Effort            string `json:"effort,omitempty"`
	Profile           string `json:"profile,omitempty"`
	Mode              string `json:"mode"` // native | local-only
}

type Stage struct {
	Name     string    `json:"name"`
	State    string    `json:"state"`
	Attempts []Attempt `json:"attempts"`
}

type Attempt struct {
	ID             string      `json:"id"` // op-
	Parent         string      `json:"parent,omitempty"`
	Number         int         `json:"number"`
	Epoch          int         `json:"epoch"`
	State          string      `json:"state"`
	StartedAt      string      `json:"startedAt,omitempty"`
	FinishedAt     string      `json:"finishedAt,omitempty"`
	InputHash      string      `json:"inputHash,omitempty"`
	ResultHash     string      `json:"resultHash,omitempty"`
	CheckpointHash string      `json:"checkpointHash,omitempty"`
	Result         *VersionRef `json:"result"`
	Error          *StageError `json:"error"`
	Native         *NativeRef  `json:"native"`
	Summary        string      `json:"summary,omitempty"`
}

type StageError struct {
	Class   string `json:"class"` // input | capability | source | rate-limit | timeout | validation | storage | unknown-outcome
	Message string `json:"message"`
}

var errorClasses = map[string]bool{"input": true, "capability": true, "source": true, "rate-limit": true,
	"timeout": true, "validation": true, "storage": true, "unknown-outcome": true}

// NativeRef is the native delivery a stage step rode on, with requested and
// observed runtime kept apart.
type NativeRef struct {
	Agent          string `json:"agent"`
	Session        string `json:"session"`
	Conversation   string `json:"conversation"`
	RequestID      string `json:"requestId"`
	State          string `json:"state"`
	RequestedModel string `json:"requestedModel,omitempty"`
	ObservedModel  string `json:"observedModel,omitempty"`
	ObservedSource string `json:"observedSource,omitempty"` // runner-report | unknown
	NativeSession  string `json:"nativeSession,omitempty"`
	ToolScope      string `json:"toolScope,omitempty"`
	PacketHash     string `json:"packetHash,omitempty"`
}

type RunEvent struct {
	Seq     int    `json:"seq"`
	At      string `json:"at"`
	Stage   string `json:"stage,omitempty"`
	Attempt string `json:"attempt,omitempty"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type Publication struct {
	Epoch           int          `json:"epoch"`
	Assemblies      []VersionRef `json:"assemblies"`
	Receipt         string       `json:"receipt"` // op- id of the publishing commit
	PublishedAt     string       `json:"publishedAt"`
	MissingResearch []string     `json:"missingResearch"`
}

// RunCapabilities records what this run could actually do, observed at start.
type RunCapabilities struct {
	AutonomousAcquisition string   `json:"autonomousAcquisition"` // unavailable | fixture | available
	NativeAgent           string   `json:"nativeAgent"`           // unavailable | available
	PDFExtraction         string   `json:"pdfExtraction"`         // unavailable | text-layer
	Notes                 []string `json:"notes"`
}

type RunCounts struct {
	SourcesConsidered int `json:"sourcesConsidered"`
	SourcesRetained   int `json:"sourcesRetained"`
	EvidenceVerified  int `json:"evidenceVerified"`
	EvidenceRejected  int `json:"evidenceRejected"`
	Alternatives      int `json:"alternatives"`
}

// ---- View / Annotation ------------------------------------------------------------

type View struct {
	Envelope
	ProblemID    string        `json:"problemId"`
	Name         string        `json:"name"`
	Assembly     VersionRef    `json:"assembly"`
	Camera       Camera        `json:"camera"`
	Bookmarks    []Bookmark    `json:"bookmarks"`
	Section      *SectionPlane `json:"section"`
	Selection    []string      `json:"selection"`
	Hidden       []string      `json:"hidden"`
	Isolated     []string      `json:"isolated"`
	Transparent  []string      `json:"transparent"`
	Exploded     float64       `json:"exploded"`
	Mode         string        `json:"mode"` // technical | realistic
	Overlays     Overlays      `json:"overlays"`
	Annotations  []Annotation  `json:"annotations"`
	Measurements []Measurement `json:"measurements"`
}

type Camera struct {
	Projection string     `json:"projection"` // perspective | orthographic
	Position   [3]float64 `json:"position"`   // world mm
	Target     [3]float64 `json:"target"`
	Up         [3]float64 `json:"up"`
	FOV        float64    `json:"fov"`
	Zoom       float64    `json:"zoom"`
}

type Bookmark struct {
	Name   string `json:"name"`
	Camera Camera `json:"camera"`
}

// SectionPlane is an arbitrary plane {originMm, normal, up}.
type SectionPlane struct {
	Origin  [3]float64 `json:"originMm"`
	Normal  [3]float64 `json:"normal"`
	Up      [3]float64 `json:"up"`
	Enabled bool       `json:"enabled"`
}

type Overlays struct {
	Water      bool `json:"water"`
	Attachment bool `json:"attachment"`
}

// Annotation anchors on a semantic id (+ optional canonical point), never on
// a renderer mesh index. A removed anchor shows as unresolved.
type Annotation struct {
	ID          string      `json:"id"` // ann-
	ComponentID string      `json:"componentId"`
	Point       *[3]float64 `json:"point"`
	Text        string      `json:"text"`
	Author      Actor       `json:"author"`
	EvidenceIDs []string    `json:"evidenceIds"`
	CreatedAt   string      `json:"createdAt"`
	State       string      `json:"state"` // active | unresolved | deleted
}

type Measurement struct {
	A        [3]float64 `json:"a"`
	B        [3]float64 `json:"b"`
	Distance float64    `json:"distanceMm"`
	Label    string     `json:"label,omitempty"`
}

// ---- Derived artifacts ---------------------------------------------------------------

type DerivedBundle struct {
	Envelope
	ProblemID string          `json:"problemId"`
	Artifacts []DerivedRecord `json:"artifacts"`
}

// DerivedRecord ties one generated file to the exact assembly revision and
// generator that produced it.
type DerivedRecord struct {
	ArtifactID       string            `json:"artifactId"`
	Revision         string            `json:"revision"` // sha256 of bytes
	Format           string            `json:"format"`   // glb | svg | pdf | png | package
	Name             string            `json:"name"`
	AssemblyID       string            `json:"assemblyId"`
	AssemblyRevision string            `json:"assemblyRevision"`
	GeometryHash     string            `json:"geometryHash"`
	Generator        string            `json:"generator"`
	Parameters       map[string]string `json:"parameters"`
	InputHashes      []string          `json:"inputHashes"`
	Size             int64             `json:"size"`
	CreatedAt        string            `json:"createdAt"`
	RequestID        string            `json:"requestId"`
}

// ---- OperationReceipt (commit record) ------------------------------------------------

type Receipt struct {
	Envelope
	RequestID        string            `json:"requestId"`
	PayloadHash      string            `json:"payloadHash"`
	ProblemID        string            `json:"problemId"`
	Subject          SubjectRef        `json:"subject"`
	BaseGeneration   int64             `json:"baseGeneration"`
	ResultGeneration int64             `json:"resultGeneration"`
	ParentCommit     string            `json:"parentCommit,omitempty"`
	Summary          string            `json:"summary"`
	Operations       []OperationRecord `json:"operations"`
	Changes          []DocChange       `json:"changes"`
	Status           string            `json:"status"` // committed
	ViewOnly         bool              `json:"viewOnly"`
	Native           []NativeRef       `json:"native"`
	InputHashes      []string          `json:"inputHashes"`
	OutputHashes     []string          `json:"outputHashes"`
	Events           []LedgerEvent     `json:"events"`
}

// OperationRecord is one applied operation with canonical before/after.
type OperationRecord struct {
	Op     string          `json:"op"`
	Target string          `json:"target"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

type DocChange struct {
	Key  string `json:"key"` // problem | assembly:<id> | …
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

// LedgerEvent is the activity projection of a commit. The ledger is never
// commit authority: events stay in the receipt until published.
type LedgerEvent struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}
