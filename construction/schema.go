package construction

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Resource budgets (§3.3). Ceilings to validate, not performance promises:
// an edit that would exceed one is refused with the prior assembly intact.
const (
	MaxComponents       = 500
	MaxTriangles        = 100000
	MaxOpsPerRequest    = 256
	MaxDependencyDepth  = 16
	MaxAssemblyBytes    = 2 << 20
	MaxDocumentBytes    = 4 << 20
	MaxInputBytes       = 20 << 20
	MaxAgentInputs      = 8
	MaxAgentInputBytes  = 40 << 20
	MaxDocumentsPerRun  = 25
	MaxPagesPerDocument = 100
	MaxCommandBytes     = 1 << 20
	MaxTitle            = 200
	MaxText             = 8000
	MaxFacts            = 100
	MaxInputs           = 64
)

// Error is a domain error with the HTTP status the API answers with.
type Error struct {
	Status   int               `json:"-"`
	Kind     string            `json:"kind"`
	Message  string            `json:"error"`
	Problems []string          `json:"problems,omitempty"`
	Current  map[string]string `json:"current,omitempty"`
}

func (e *Error) Error() string {
	if len(e.Problems) > 0 {
		return e.Message + ": " + strings.Join(e.Problems, "; ")
	}
	return e.Message
}

func newErr(status int, kind, msg string) *Error {
	return &Error{Status: status, Kind: kind, Message: msg}
}

// Sentinel kinds callers can test with errors.Is.
var (
	ErrNotFound    = newErr(http.StatusNotFound, "not-found", "not found")
	ErrForbidden   = newErr(http.StatusForbidden, "forbidden", "forbidden")
	ErrConflict    = newErr(http.StatusConflict, "conflict", "the record changed; review the latest revision")
	ErrTooLarge    = newErr(http.StatusRequestEntityTooLarge, "too-large", "request too large")
	ErrInvalid     = newErr(http.StatusUnprocessableEntity, "invalid", "invalid request")
	ErrUnavailable = newErr(http.StatusServiceUnavailable, "unavailable", "capability unavailable")
	ErrReadOnly    = newErr(http.StatusConflict, "read-only", "this record uses a newer schema and opens read-only")
	ErrCorrupt     = newErr(http.StatusInternalServerError, "corrupt", "stored construction record is corrupt; it was not reset")
	ErrWriterBusy  = newErr(http.StatusServiceUnavailable, "writer-busy", "another process owns construction writes")
)

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Kind == e.Kind
}

// Invalid builds a 422 with every problem listed.
func Invalid(problems ...string) error {
	return &Error{Status: http.StatusUnprocessableEntity, Kind: "invalid", Message: "invalid request", Problems: problems}
}

// Conflict builds a 409 naming the current revisions.
func Conflict(msg string, current map[string]string) error {
	return &Error{Status: http.StatusConflict, Kind: "conflict", Message: msg, Current: current}
}

// Unavailable builds a 503 for a capability this build/host lacks.
func Unavailable(msg string) error {
	return &Error{Status: http.StatusServiceUnavailable, Kind: "unavailable", Message: msg}
}

// Forbidden builds a 403.
func Forbidden(msg string) error {
	return &Error{Status: http.StatusForbidden, Kind: "forbidden", Message: msg}
}

// NotFound builds a 404 (also used for cross-project lookups, so a problem's
// existence never leaks across subjects).
func NotFound(msg string) error {
	return &Error{Status: http.StatusNotFound, Kind: "not-found", Message: msg}
}

func corrupt(format string, a ...any) error {
	return &Error{Status: http.StatusInternalServerError, Kind: "corrupt", Message: "stored construction record is corrupt; it was not reset: " + fmt.Sprintf(format, a...)}
}

// StatusOf maps any error to an HTTP status (500 for unknown errors).
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return http.StatusInternalServerError
}

// ---- strict decoding -------------------------------------------------------------

// versionProbe reads only the envelope fields needed to route a document.
type versionProbe struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	ID            string `json:"id"`
}

// ErrNewerSchema marks a document written by a newer schema; callers open it
// read-only and export its exact bytes.
var ErrNewerSchema = errors.New("construction: newer schema version")

// DecodeStrict decodes a stored document of the expected kind. Unknown fields
// outside "extensions" are corruption (a v1 writer never emits them); unknown
// extension entries are preserved by the Extensions map.
func DecodeStrict(raw []byte, kind string, v any) error {
	var probe versionProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return corrupt("%s: %v", kind, err)
	}
	if probe.Kind != kind {
		return corrupt("expected %s, found %q", kind, probe.Kind)
	}
	if probe.SchemaVersion > SchemaVersion {
		return ErrNewerSchema
	}
	if probe.SchemaVersion != SchemaVersion {
		return corrupt("%s %s: schema version %d", kind, probe.ID, probe.SchemaVersion)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return corrupt("%s %s: %v", kind, probe.ID, err)
	}
	return nil
}

// decodeRequest decodes a client body strictly (unknown fields refused).
func decodeRequest(raw []byte, v any) error {
	if len(raw) > MaxCommandBytes {
		return ErrTooLarge
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return Invalid("body: " + err.Error())
	}
	if d.More() {
		return Invalid("body: trailing data")
	}
	return nil
}

// DecodeRequest is decodeRequest for the server package.
func DecodeRequest(raw []byte, v any) error { return decodeRequest(raw, v) }

// ---- field validation helpers ---------------------------------------------------------

var slugRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,199}$`)

// ValidSubject checks a subject reference's shape. Property ids are exact
// slugs (no path separators); the shared Home is the single id "home".
func ValidSubject(s SubjectRef) error {
	switch s.Kind {
	case SubjectProperty:
		if !slugRE.MatchString(s.ID) || strings.Contains(s.ID, "..") {
			return Invalid("subject: property slug is not a valid record key")
		}
	case SubjectHome:
		if s.ID != HomeSubjectID {
			return Invalid(`subject: the shared Home subject id is "home"`)
		}
	default:
		return Invalid("subject: kind must be property or home")
	}
	return nil
}

var scopeIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:/._-]{0,159}$`)

func checkText(field, s string, max int, required bool) []string {
	var out []string
	if required && strings.TrimSpace(s) == "" {
		out = append(out, field+" is required")
	}
	if !utf8.ValidString(s) {
		out = append(out, field+" must be valid UTF-8")
	}
	if utf8.RuneCountInString(s) > max {
		out = append(out, fmt.Sprintf("%s exceeds %d characters", field, max))
	}
	if strings.ContainsRune(s, 0) {
		out = append(out, field+" contains a NUL byte")
	}
	return out
}

func checkTime(field, s string) []string {
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return []string{field + " must be an RFC 3339 time"}
	}
	return nil
}

func checkActor(field string, a Actor) []string {
	switch a.Kind {
	case ActorOwner:
		if a.Principal != "owner" {
			return []string{field + ": owner principal must be \"owner\""}
		}
	case ActorAgent:
		if !stewards[a.Agent] || a.Principal != "agent:"+a.Agent {
			return []string{field + ": agent actor must name alfred or zeck"}
		}
	case ActorSystem:
		if a.Principal != "system:construction" {
			return []string{field + ": system principal must be system:construction"}
		}
	default:
		return []string{field + ": actor kind must be owner, agent or system"}
	}
	return nil
}

func checkEnvelope(e Envelope, kind, idKind string) []string {
	var out []string
	if e.SchemaVersion != SchemaVersion {
		out = append(out, "schemaVersion must be 1")
	}
	if e.Kind != kind {
		out = append(out, "kind must be "+kind)
	}
	if idKind != "" && !ValidID(idKind, e.ID) {
		out = append(out, "id must be "+idKind+"-<32 hex>")
	}
	if e.RevisionNumber < 1 {
		out = append(out, "revisionNumber must be ≥ 1")
	}
	if e.RevisionNumber > 1 && !ValidToken(e.ParentRevision) {
		out = append(out, "parentRevision must name the previous revision token")
	}
	if e.RevisionNumber == 1 && e.ParentRevision != "" {
		out = append(out, "the first revision has no parent")
	}
	out = append(out, checkTime("createdAt", e.CreatedAt)...)
	out = append(out, checkActor("actor", e.Actor)...)
	for k := range e.Extensions {
		if !extensionKeyRE.MatchString(k) {
			out = append(out, "extension keys must be namespaced (e.g. vendor.name)")
		}
	}
	return out
}

var extensionKeyRE = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z0-9.-]{1,64}$`)

func checkContextFact(field string, c ContextFact) []string {
	out := checkText(field+".text", c.Text, 500, false)
	if c.State != StateKnown && c.State != StateAssumed && c.State != StateUnknown {
		out = append(out, field+".state must be known, assumed or unknown")
	}
	if !provenances[c.Provenance] {
		out = append(out, field+".provenance is not in the provenance taxonomy")
	}
	if c.State == StateUnknown && c.Provenance != ProvUnknown {
		out = append(out, field+": an unknown value has provenance unknown")
	}
	return out
}

// ValidateProblem checks a problem document's structure.
func ValidateProblem(p *Problem) error {
	out := checkEnvelope(p.Envelope, DocProblem, KindProblem)
	if err := ValidSubject(p.SubjectRef); err != nil {
		out = append(out, err.(*Error).Problems...)
	}
	if p.ProjectRef != p.SubjectRef {
		out = append(out, "projectRef must equal subjectRef in schema v1")
	}
	if p.SubjectRef.Kind == SubjectProperty {
		if p.PropertyRef == nil || *p.PropertyRef != p.SubjectRef {
			out = append(out, "propertyRef must carry the exact property slug")
		}
	} else if p.PropertyRef != nil {
		out = append(out, "propertyRef is only set for property subjects")
	}
	if s := p.ScopeRef; s != nil {
		if s.WorkID == "" && s.TaskID == "" {
			out = append(out, "scopeRef needs a workId or taskId (or null)")
		}
		for _, id := range []string{s.WorkID, s.TaskID} {
			if id != "" && !scopeIDRE.MatchString(id) {
				out = append(out, "scopeRef ids must be exact record ids")
			}
		}
		if p.SubjectRef.Kind == SubjectHome && s.WorkID != "" {
			out = append(out, "Home scope links a shared Home task, not a property work node")
		}
	}
	out = append(out, checkText("title", p.Title, MaxTitle, true)...)
	out = append(out, checkText("narrative", p.Narrative, MaxText, false)...)
	if len(p.Inputs) > MaxInputs {
		out = append(out, fmt.Sprintf("at most %d inputs", MaxInputs))
	}
	seenInput := map[string]bool{}
	for i, in := range p.Inputs {
		f := fmt.Sprintf("inputs[%d]", i)
		if !ValidToken(in.Revision) || len(in.ArtifactID) != 16 {
			out = append(out, f+": needs the native artifact id and content revision")
		}
		if seenInput[in.ArtifactID+in.Revision+in.Role] {
			out = append(out, f+": duplicate input")
		}
		seenInput[in.ArtifactID+in.Revision+in.Role] = true
		if !inputRoles[in.Role] {
			out = append(out, f+".role must be photo, drawing, document or other")
		}
		if !inputVerifications[in.Verification] {
			out = append(out, f+".verification is not recognised")
		}
		out = append(out, checkText(f+".name", in.Name, 200, true)...)
		out = append(out, checkText(f+".label", in.Label, 300, false)...)
		if in.Size <= 0 || in.Size > MaxInputBytes {
			out = append(out, f+".size out of range")
		}
		out = append(out, checkActor(f+".actor", in.Actor)...)
	}
	if len(p.Existing)+len(p.Proposed) > MaxFacts {
		out = append(out, fmt.Sprintf("at most %d facts", MaxFacts))
	}
	seen := map[string]bool{}
	for i, f := range append(append([]Fact{}, p.Existing...), p.Proposed...) {
		field := fmt.Sprintf("facts[%d]", i)
		if !ValidID(KindClaim, f.ID) || seen[f.ID] {
			out = append(out, field+": needs a unique clm- id")
		}
		seen[f.ID] = true
		out = append(out, checkText(field+".text", f.Text, 1000, true)...)
		if !provenances[f.Provenance] {
			out = append(out, field+".provenance is not in the provenance taxonomy")
		}
	}
	out = append(out, checkContextFact("location", p.Location)...)
	out = append(out, checkContextFact("jurisdiction", p.Jurisdiction)...)
	out = append(out, checkContextFact("climate", p.Climate)...)
	if !problemLifecycles[p.Lifecycle] {
		out = append(out, "lifecycle is not recognised")
	}
	if !stewards[p.Steward.Agent] || (p.Steward.Agent == "zeck" && !p.Steward.Explicit) {
		out = append(out, "steward is alfred by default; zeck only when explicitly selected")
	}
	for i, id := range p.Alternatives {
		if !ValidID(KindAssembly, id) {
			out = append(out, fmt.Sprintf("alternatives[%d] must be an asm- id", i))
		}
	}
	if p.ActiveAssembly != "" && !contains(p.Alternatives, p.ActiveAssembly) {
		out = append(out, "activeAssembly must be one of the alternatives")
	}
	if s := p.SelectedAssembly; s != nil && (!contains(p.Alternatives, s.ID) || !ValidToken(s.Revision)) {
		out = append(out, "selectedAssembly must pin an alternative's exact revision")
	}
	for _, id := range p.Decisions {
		if !ValidID(KindDecision, id) {
			out = append(out, "decisions must be dec- ids")
		}
	}
	for _, r := range append(append([]ExternalRef{}, p.Links.Tasks...), p.Links.Decisions...) {
		if !scopeIDRE.MatchString(r.ID) {
			out = append(out, "links must carry exact record ids")
		}
	}
	for _, c := range p.Conversations {
		if !stewards[c.Agent] || c.Session == "" {
			out = append(out, "conversation refs name the agent and native session")
		}
	}
	out = append(out, checkTime("updatedAt", p.UpdatedAt)...)
	if len(out) > 0 {
		return Invalid(out...)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
