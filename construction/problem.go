package construction

import (
	"strings"
	"time"
)

// CreateRequest is the body of POST …/construction/problems.
type CreateRequest struct {
	SchemaVersion int       `json:"schemaVersion"`
	RequestID     string    `json:"requestId"`
	Title         string    `json:"title"`
	Narrative     string    `json:"narrative,omitempty"`
	Steward       string    `json:"steward,omitempty"` // "" → alfred (default)
	Scope         *ScopeRef `json:"scope,omitempty"`
	// Template seeds the first draft assembly ("" = none). The only family in
	// schema v1 is TemplateRoofMasonry; its numbers are illustrative.
	Template string `json:"template,omitempty"`
}

// ParseCreate strictly decodes a creation body and returns its payload hash.
func ParseCreate(raw []byte) (*CreateRequest, string, error) {
	var req CreateRequest
	if err := decodeRequest(raw, &req); err != nil {
		return nil, "", err
	}
	var problems []string
	if req.SchemaVersion != SchemaVersion {
		problems = append(problems, "schemaVersion must be 1")
	}
	if !ValidRequestID(req.RequestID) {
		problems = append(problems, "requestId must be 8–128 characters of [A-Za-z0-9_-]")
	}
	problems = append(problems, checkText("title", req.Title, MaxTitle, true)...)
	problems = append(problems, checkText("narrative", req.Narrative, MaxText, false)...)
	if req.Steward != "" && !stewards[req.Steward] {
		problems = append(problems, "steward must be alfred or zeck")
	}
	if req.Template != "" && !templates[req.Template] {
		problems = append(problems, "template is not recognised")
	}
	if s := req.Scope; s != nil {
		if s.SourceRevision != "" {
			problems = append(problems, "scope.sourceRevision is recorded by the server")
		}
		for _, id := range []string{s.WorkID, s.TaskID} {
			if id != "" && !scopeIDRE.MatchString(id) {
				problems = append(problems, "scope ids must be exact record ids")
			}
		}
	}
	if len(problems) > 0 {
		return nil, "", Invalid(problems...)
	}
	canon, err := CanonicalizeJSON(raw)
	if err != nil {
		return nil, "", Invalid(err.Error())
	}
	return &req, Token(canon), nil
}

// templates registered by the assembly phase (assembly.go).
var templates = map[string]bool{}

// templateSeeders build a template's first assembly inside the creating tx.
var templateSeeders = map[string]func(tx *Tx) error{}

func unknownContext() ContextFact {
	return ContextFact{Text: "", State: StateUnknown, Provenance: ProvUnknown}
}

// NewProblem builds a problem's first document. Location, jurisdiction and
// climate start unknown; nothing is inferred from an address or a title.
func NewProblem(id string, sub SubjectRef, req *CreateRequest, scope *ScopeRef, now time.Time) *Problem {
	steward := Steward{Agent: "alfred"}
	if req.Steward == "zeck" {
		steward = Steward{Agent: "zeck", Explicit: true}
	}
	p := &Problem{
		Envelope:      Envelope{SchemaVersion: SchemaVersion, Kind: DocProblem, ID: id},
		SubjectRef:    sub,
		ProjectRef:    sub,
		ScopeRef:      scope,
		Title:         strings.TrimSpace(req.Title),
		Narrative:     req.Narrative,
		Inputs:        []InputRef{},
		Existing:      []Fact{},
		Proposed:      []Fact{},
		Location:      unknownContext(),
		Jurisdiction:  unknownContext(),
		Climate:       unknownContext(),
		Lifecycle:     LifecycleDraft,
		Steward:       steward,
		Conversations: []ConversationRef{},
		Alternatives:  []string{},
		Decisions:     []string{},
		Links:         Links{Tasks: []ExternalRef{}, Decisions: []ExternalRef{}},
		UpdatedAt:     now.UTC().Format(time.RFC3339Nano),
	}
	if sub.Kind == SubjectProperty {
		ref := sub
		p.PropertyRef = &ref
	}
	return p
}

// CreateProblem creates a problem (and its empty catalog/evidence/derived
// bundles, and a template draft assembly when asked) as one commit. The
// scope, when given, must already be resolved by the server's read-only
// source resolver.
func (s *Store) CreateProblem(sub SubjectRef, req *CreateRequest, payloadHash string, actor Actor, scope *ScopeRef) (*State, *Receipt, error) {
	return s.Create(sub, CommitRequest{RequestID: req.RequestID, PayloadHash: payloadHash, Actor: actor}, func(tx *Tx) error {
		id := tx.Base.Head.ProblemID
		tx.Next.Problem = NewProblem(id, sub, req, scope, tx.Now)
		tx.Next.Catalog = newCatalog(id)
		tx.Next.Evidence = &EvidenceBundle{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocEvidence, ID: id},
			ProblemID: id, Sources: []Source{}, Claims: []Claim{}, Evidence: []Evidence{}, Relations: []Relation{}}
		tx.Next.Derived = &DerivedBundle{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocDerived, ID: id},
			ProblemID: id, Artifacts: []DerivedRecord{}}
		tx.Record("CreateProblem", "problem", nil, map[string]any{"title": tx.Next.Problem.Title, "subject": sub})
		tx.Summary("created problem")
		tx.Event("construction.problem.created", tx.Next.Problem.Title)
		if req.Template != "" {
			seed := templateSeeders[req.Template]
			if seed == nil {
				return Invalid("template is not available in this build")
			}
			if err := seed(tx); err != nil {
				return err
			}
		}
		return nil
	})
}

// newCatalog seeds the problem-local catalog (generic families; see
// catalog.go). Before the catalog phase it is empty.
var newCatalog = func(problemID string) *Catalog {
	return &Catalog{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocCatalog, ID: problemID},
		ProblemID: problemID, Materials: []Material{}, Products: []Product{}}
}

// InputUpload is one bounded owner upload to retain as an immutable input.
type InputUpload struct {
	RequestID               string
	ExpectedProblemRevision string
	Name                    string
	Mime                    string
	Role                    string
	Label                   string
	Verification            string
	Content                 []byte
}

// RetainInput stores an input's exact bytes as a private artifact owned by
// this problem and references it from the problem. Replaying the same
// upload (lost ACK) yields the same single input.
func (s *Store) RetainInput(sub SubjectRef, id string, up InputUpload, actor Actor) (*State, *Receipt, error) {
	var problems []string
	if !ValidRequestID(up.RequestID) {
		problems = append(problems, "requestId must be 8–128 characters of [A-Za-z0-9_-]")
	}
	if !ValidToken(up.ExpectedProblemRevision) {
		problems = append(problems, "expectedProblemRevision is required")
	}
	if up.Verification == "" {
		up.Verification = "not-field-verified"
	}
	if !inputRoles[up.Role] {
		problems = append(problems, "role must be photo, drawing, document or other")
	}
	if !inputVerifications[up.Verification] {
		problems = append(problems, "verification is not recognised")
	}
	problems = append(problems, checkText("name", up.Name, 200, true)...)
	problems = append(problems, checkText("label", up.Label, 300, false)...)
	if len(up.Content) == 0 {
		problems = append(problems, "empty file")
	}
	if len(problems) > 0 {
		return nil, nil, Invalid(problems...)
	}
	if len(up.Content) > MaxInputBytes {
		return nil, nil, ErrTooLarge
	}
	hash := Token(up.Content)
	payload := map[string]any{"op": "RetainInput", "problemId": id, "expected": up.ExpectedProblemRevision,
		"name": up.Name, "mime": up.Mime, "role": up.Role, "label": up.Label, "verification": up.Verification, "content": hash}
	_, payloadHash, err := TokenOf(payload)
	if err != nil {
		return nil, nil, err
	}
	return s.Commit(sub, id, CommitRequest{RequestID: up.RequestID, PayloadHash: payloadHash, Actor: actor}, func(tx *Tx) error {
		if cur := tx.Base.Revision("problem"); cur != up.ExpectedProblemRevision {
			return Conflict("the problem changed since it was loaded", map[string]string{"problem": cur})
		}
		if len(tx.Next.Problem.Inputs) >= MaxInputs {
			return Invalid("input limit reached")
		}
		artifactID, rev, err := tx.Retain("construction-input", up.Name, up.Content)
		if err != nil {
			return err
		}
		for _, in := range tx.Next.Problem.Inputs {
			if in.ArtifactID == artifactID && in.Revision == rev && in.Role == up.Role {
				return Conflict("this file is already retained in that role", nil)
			}
		}
		in := InputRef{ArtifactID: artifactID, Revision: rev, Name: up.Name, Mime: up.Mime, Size: int64(len(up.Content)),
			Role: up.Role, Label: up.Label, Verification: up.Verification, RetainedAt: tx.Now.Format(time.RFC3339Nano), Actor: tx.Actor}
		tx.Next.Problem.Inputs = append(tx.Next.Problem.Inputs, in)
		tx.Record("RetainInput", "input/"+artifactID, nil, in)
		tx.Input(rev)
		tx.Summary("retained " + up.Role + " input")
		tx.Event("construction.input.retained", up.Role+": "+up.Name)
		return nil
	})
}

// ExecuteCommand parses nothing: it applies an already parsed command as one
// commit under the given actor.
func (s *Store) ExecuteCommand(sub SubjectRef, pc *ParsedCommand, actor Actor, c *ApplyContext) (*State, *Receipt, error) {
	if c == nil {
		c = &ApplyContext{}
	}
	return s.Commit(sub, pc.Command.ProblemID, CommitRequest{RequestID: pc.Command.RequestID, PayloadHash: pc.PayloadHash, Actor: actor}, func(tx *Tx) error {
		return ApplyCommand(tx, c, pc)
	})
}
