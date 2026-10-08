package construction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Command is the one mutation envelope shared by inspector controls and agent
// proposals (§3.4). The actor is never part of it: the server derives the
// actor from the authenticated principal or the bound agent capability.
type Command struct {
	SchemaVersion            int               `json:"schemaVersion"`
	RequestID                string            `json:"requestId"`
	ProblemID                string            `json:"problemId"`
	AssemblyID               string            `json:"assemblyId,omitempty"`
	ExpectedProblemRevision  string            `json:"expectedProblemRevision,omitempty"`
	ExpectedAssemblyRevision string            `json:"expectedAssemblyRevision,omitempty"`
	Operations               []json.RawMessage `json:"operations"`
}

// Operation is one typed, allowlisted mutation.
type Operation interface {
	Name() string
	// Check validates the operation's own fields (no state).
	Check() []string
	// Apply mutates tx.Next. It returns a domain error to refuse.
	Apply(tx *Tx, c *ApplyContext) error
}

// Target says which document family an operation mutates; it drives the
// compare-and-swap requirement and the agent permission rule.
type Target string

const (
	TargetProblem  Target = "problem"
	TargetAssembly Target = "assembly"
	TargetView     Target = "view"
	TargetDecision Target = "decision"
	TargetCatalog  Target = "catalog"
	TargetEvidence Target = "evidence"
)

type opSpec struct {
	target    Target
	ownerOnly bool // agents are forbidden (approval, steward, problem facts…)
	make      func() Operation
}

var opRegistry = map[string]opSpec{}

func registerOp(name string, target Target, ownerOnly bool, mk func() Operation) {
	if _, dup := opRegistry[name]; dup {
		panic("construction: duplicate operation " + name)
	}
	opRegistry[name] = opSpec{target: target, ownerOnly: ownerOnly, make: mk}
}

// OperationNames lists every accepted operation kind (sorted).
func OperationNames() []string {
	out := make([]string, 0, len(opRegistry))
	for k := range opRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ApplyContext carries capabilities the domain needs from the server without
// knowing the server: source resolvers (read-only) and the geometry compiler.
type ApplyContext struct {
	Command *Command
	// ResolveScope validates a scope against the bound source record and
	// returns it with its sourceRevision. nil: scope edits are unavailable.
	ResolveScope func(ScopeRef) (ScopeRef, error)
}

// ParsedCommand is a validated envelope with decoded operations.
type ParsedCommand struct {
	Command     Command
	Ops         []Operation
	Targets     map[Target]bool
	PayloadHash string
}

// ParseCommand strictly decodes the envelope and every operation. Unknown
// operation kinds, unknown fields, non-finite numbers and over-budget
// requests are refused before any state is read.
func ParseCommand(raw []byte) (*ParsedCommand, error) {
	var cmd Command
	if err := decodeRequest(raw, &cmd); err != nil {
		return nil, err
	}
	var problems []string
	if cmd.SchemaVersion != SchemaVersion {
		problems = append(problems, "schemaVersion must be 1")
	}
	if !ValidRequestID(cmd.RequestID) {
		problems = append(problems, "requestId must be 8–128 characters of [A-Za-z0-9_-]")
	}
	if !ValidID(KindProblem, cmd.ProblemID) {
		problems = append(problems, "problemId must be a cp- id")
	}
	if cmd.AssemblyID != "" && !ValidID(KindAssembly, cmd.AssemblyID) {
		problems = append(problems, "assemblyId must be an asm- id")
	}
	if cmd.ExpectedProblemRevision != "" && !ValidToken(cmd.ExpectedProblemRevision) {
		problems = append(problems, "expectedProblemRevision must be a revision token")
	}
	if cmd.ExpectedAssemblyRevision != "" && !ValidToken(cmd.ExpectedAssemblyRevision) {
		problems = append(problems, "expectedAssemblyRevision must be a revision token")
	}
	if len(cmd.Operations) == 0 {
		problems = append(problems, "operations must not be empty")
	}
	if len(cmd.Operations) > MaxOpsPerRequest {
		problems = append(problems, fmt.Sprintf("at most %d operations per request", MaxOpsPerRequest))
	}
	if len(problems) > 0 {
		return nil, Invalid(problems...)
	}
	pc := &ParsedCommand{Command: cmd, Targets: map[Target]bool{}}
	for i, rawOp := range cmd.Operations {
		var probe struct {
			Op string `json:"op"`
		}
		if err := json.Unmarshal(rawOp, &probe); err != nil {
			return nil, Invalid(fmt.Sprintf("operations[%d]: %v", i, err))
		}
		spec, ok := opRegistry[probe.Op]
		if !ok {
			return nil, Invalid(fmt.Sprintf("operations[%d]: unknown operation kind %q", i, probe.Op))
		}
		op := spec.make()
		d := json.NewDecoder(bytes.NewReader(rawOp))
		d.DisallowUnknownFields()
		if err := d.Decode(op); err != nil {
			return nil, Invalid(fmt.Sprintf("operations[%d] (%s): %v", i, probe.Op, err))
		}
		if errs := op.Check(); len(errs) > 0 {
			for j := range errs {
				errs[j] = fmt.Sprintf("operations[%d] (%s): %s", i, probe.Op, errs[j])
			}
			return nil, Invalid(errs...)
		}
		pc.Ops = append(pc.Ops, op)
		pc.Targets[spec.target] = true
	}
	if pc.Targets[TargetAssembly] && cmd.AssemblyID == "" {
		return nil, Invalid("assembly operations need assemblyId and expectedAssemblyRevision")
	}
	canon, err := CanonicalizeJSON(raw)
	if err != nil {
		return nil, Invalid("body: " + err.Error())
	}
	pc.PayloadHash = Token(canon)
	return pc, nil
}

// checkAuthority applies the actor rule per operation: the owner may do
// everything; an agent may only edit draft/proposed assemblies, views,
// annotations and evidence links, and propose decisions. It can never
// approve or reject a decision, change the steward or problem facts.
func checkAuthority(pc *ParsedCommand, actor Actor) error {
	if actor.Kind == ActorOwner {
		return nil
	}
	for _, op := range pc.Ops {
		if opRegistry[op.Name()].ownerOnly {
			return Forbidden(op.Name() + " requires the owner; an agent cannot perform it")
		}
	}
	return nil
}

// ApplyCommand applies a parsed command inside a transaction after checking
// authority and the expected revisions of every targeted document.
func ApplyCommand(tx *Tx, c *ApplyContext, pc *ParsedCommand) error {
	if err := checkAuthority(pc, tx.Actor); err != nil {
		return err
	}
	cmd := pc.Command
	if pc.Targets[TargetProblem] {
		if cmd.ExpectedProblemRevision == "" {
			return Invalid("problem operations need expectedProblemRevision")
		}
		if cur := tx.Base.Revision("problem"); cur != cmd.ExpectedProblemRevision {
			return Conflict("the problem changed since it was loaded", map[string]string{"problem": cur})
		}
	}
	if cmd.AssemblyID != "" {
		cur := tx.Base.Revision("assembly:" + cmd.AssemblyID)
		if cur == "" {
			return NotFound("no such assembly in this problem")
		}
		if cmd.ExpectedAssemblyRevision == "" {
			return Invalid("assembly operations need expectedAssemblyRevision")
		}
		if cur != cmd.ExpectedAssemblyRevision {
			return Conflict("the assembly changed since it was loaded", map[string]string{"assembly:" + cmd.AssemblyID: cur})
		}
		if tx.Actor.Kind == ActorAgent {
			if a := tx.Base.Assemblies[cmd.AssemblyID]; a.Lifecycle != AssemblyDraft && a.Lifecycle != AssemblyProposed {
				return Forbidden("an agent may only edit draft or proposed assemblies")
			}
		}
	}
	c.Command = &cmd
	viewOnly := true
	for _, op := range pc.Ops {
		if opRegistry[op.Name()].target != TargetView {
			viewOnly = false
		}
		if err := op.Apply(tx, c); err != nil {
			return err
		}
	}
	if viewOnly {
		tx.ViewOnly()
	}
	names := make([]string, 0, len(pc.Ops))
	for _, op := range pc.Ops {
		names = append(names, op.Name())
	}
	tx.Event("construction.command", strings.Join(names, ", "))
	return nil
}

// ---- problem-level operations ---------------------------------------------------------

func init() {
	registerOp("SetProblemText", TargetProblem, true, func() Operation { return &SetProblemText{} })
	registerOp("SetContext", TargetProblem, true, func() Operation { return &SetContext{} })
	registerOp("AddFact", TargetProblem, true, func() Operation { return &AddFact{} })
	registerOp("RemoveFact", TargetProblem, true, func() Operation { return &RemoveFact{} })
	registerOp("SetSteward", TargetProblem, true, func() Operation { return &SetSteward{} })
	registerOp("SetScope", TargetProblem, true, func() Operation { return &SetScope{} })
	registerOp("SetInputMeta", TargetProblem, true, func() Operation { return &SetInputMeta{} })
	registerOp("SetLifecycle", TargetProblem, true, func() Operation { return &SetLifecycle{} })
	registerOp("LinkExternal", TargetProblem, true, func() Operation { return &LinkExternal{} })
}

// SetProblemText edits the title and/or narrative.
type SetProblemText struct {
	Op        string  `json:"op"`
	Title     *string `json:"title,omitempty"`
	Narrative *string `json:"narrative,omitempty"`
}

func (o *SetProblemText) Name() string { return "SetProblemText" }
func (o *SetProblemText) Check() []string {
	var out []string
	if o.Title == nil && o.Narrative == nil {
		out = append(out, "set title and/or narrative")
	}
	if o.Title != nil {
		out = append(out, checkText("title", *o.Title, MaxTitle, true)...)
	}
	if o.Narrative != nil {
		out = append(out, checkText("narrative", *o.Narrative, MaxText, false)...)
	}
	return out
}
func (o *SetProblemText) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	if o.Title != nil {
		tx.Record(o.Name(), "problem.title", p.Title, strings.TrimSpace(*o.Title))
		p.Title = strings.TrimSpace(*o.Title)
	}
	if o.Narrative != nil {
		tx.Record(o.Name(), "problem.narrative", p.Narrative, *o.Narrative)
		p.Narrative = *o.Narrative
	}
	return nil
}

// SetContext sets location, jurisdiction or climate with provenance.
type SetContext struct {
	Op         string `json:"op"`
	Field      string `json:"field"` // location | jurisdiction | climate
	Text       string `json:"text"`
	State      string `json:"state"`
	Provenance string `json:"provenance"`
}

func (o *SetContext) Name() string { return "SetContext" }
func (o *SetContext) Check() []string {
	out := checkContextFact(o.Field, ContextFact{Text: o.Text, State: o.State, Provenance: o.Provenance})
	if o.Field != "location" && o.Field != "jurisdiction" && o.Field != "climate" {
		out = append(out, "field must be location, jurisdiction or climate")
	}
	return out
}
func (o *SetContext) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	f := map[string]*ContextFact{"location": &p.Location, "jurisdiction": &p.Jurisdiction, "climate": &p.Climate}[o.Field]
	next := ContextFact{Text: strings.TrimSpace(o.Text), State: o.State, Provenance: o.Provenance}
	tx.Record(o.Name(), "problem."+o.Field, *f, next)
	*f = next
	return nil
}

// AddFact records an owner-stated existing or proposed condition.
type AddFact struct {
	Op         string `json:"op"`
	ID         string `json:"id"`
	List       string `json:"list"` // existing | proposed
	Text       string `json:"text"`
	Provenance string `json:"provenance"`
}

func (o *AddFact) Name() string { return "AddFact" }
func (o *AddFact) Check() []string {
	out := checkText("text", o.Text, 1000, true)
	if !ValidID(KindClaim, o.ID) {
		out = append(out, "id must be a new clm- id chosen by the caller")
	}
	if o.List != "existing" && o.List != "proposed" {
		out = append(out, "list must be existing or proposed")
	}
	if !provenances[o.Provenance] {
		out = append(out, "provenance is not in the taxonomy")
	}
	return out
}
func (o *AddFact) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	for _, f := range append(append([]Fact{}, p.Existing...), p.Proposed...) {
		if f.ID == o.ID {
			return Invalid("fact id already exists")
		}
	}
	f := Fact{ID: o.ID, Text: strings.TrimSpace(o.Text), Provenance: o.Provenance}
	if o.List == "existing" {
		p.Existing = append(p.Existing, f)
	} else {
		p.Proposed = append(p.Proposed, f)
	}
	tx.Record(o.Name(), "problem."+o.List+"/"+o.ID, nil, f)
	return nil
}

// RemoveFact removes a fact by id.
type RemoveFact struct {
	Op string `json:"op"`
	ID string `json:"id"`
}

func (o *RemoveFact) Name() string { return "RemoveFact" }
func (o *RemoveFact) Check() []string {
	if !ValidID(KindClaim, o.ID) {
		return []string{"id must be a clm- id"}
	}
	return nil
}
func (o *RemoveFact) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	for _, list := range []*[]Fact{&p.Existing, &p.Proposed} {
		for i, f := range *list {
			if f.ID == o.ID {
				tx.Record(o.Name(), "fact/"+o.ID, f, nil)
				*list = append((*list)[:i], (*list)[i+1:]...)
				return nil
			}
		}
	}
	return NotFound("no such fact")
}

// SetSteward selects Alfred (default) or explicitly Zeck. It assigns; it
// never creates a conversation or starts a run.
type SetSteward struct {
	Op    string `json:"op"`
	Agent string `json:"agent"`
}

func (o *SetSteward) Name() string { return "SetSteward" }
func (o *SetSteward) Check() []string {
	if !stewards[o.Agent] {
		return []string{"agent must be alfred or zeck"}
	}
	return nil
}
func (o *SetSteward) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	next := Steward{Agent: o.Agent, Explicit: true} // an owner's choice is explicit
	tx.Record(o.Name(), "problem.steward", p.Steward, next)
	p.Steward = next
	return nil
}

// SetScope links (or clears) the existing work node / Home task by exact id.
type SetScope struct {
	Op     string `json:"op"`
	WorkID string `json:"workId,omitempty"`
	TaskID string `json:"taskId,omitempty"`
	Clear  bool   `json:"clear,omitempty"`
}

func (o *SetScope) Name() string { return "SetScope" }
func (o *SetScope) Check() []string {
	if o.Clear && (o.WorkID != "" || o.TaskID != "") {
		return []string{"clear takes no ids"}
	}
	if !o.Clear && o.WorkID == "" && o.TaskID == "" {
		return []string{"set workId and/or taskId, or clear"}
	}
	for _, id := range []string{o.WorkID, o.TaskID} {
		if id != "" && !scopeIDRE.MatchString(id) {
			return []string{"scope ids must be exact record ids"}
		}
	}
	return nil
}
func (o *SetScope) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	if o.Clear {
		tx.Record(o.Name(), "problem.scopeRef", p.ScopeRef, nil)
		p.ScopeRef = nil
		return nil
	}
	if c.ResolveScope == nil {
		return Unavailable("scope links cannot be resolved here")
	}
	scope, err := c.ResolveScope(ScopeRef{WorkID: o.WorkID, TaskID: o.TaskID})
	if err != nil {
		return err
	}
	tx.Record(o.Name(), "problem.scopeRef", p.ScopeRef, scope)
	p.ScopeRef = &scope
	return nil
}

// SetInputMeta edits an input's role, label or verification label.
type SetInputMeta struct {
	Op           string  `json:"op"`
	ArtifactID   string  `json:"artifactId"`
	Revision     string  `json:"revision"`
	Role         *string `json:"role,omitempty"`
	Label        *string `json:"label,omitempty"`
	Verification *string `json:"verification,omitempty"`
}

func (o *SetInputMeta) Name() string { return "SetInputMeta" }
func (o *SetInputMeta) Check() []string {
	var out []string
	if len(o.ArtifactID) != 16 || !ValidToken(o.Revision) {
		out = append(out, "artifactId and revision are required")
	}
	if o.Role != nil && !inputRoles[*o.Role] {
		out = append(out, "role must be photo, drawing, document or other")
	}
	if o.Label != nil {
		out = append(out, checkText("label", *o.Label, 300, false)...)
	}
	if o.Verification != nil && !inputVerifications[*o.Verification] {
		out = append(out, "verification is not recognised")
	}
	return out
}
func (o *SetInputMeta) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	for i := range p.Inputs {
		in := &p.Inputs[i]
		if in.ArtifactID != o.ArtifactID || in.Revision != o.Revision {
			continue
		}
		before := *in
		if o.Role != nil {
			in.Role = *o.Role
		}
		if o.Label != nil {
			in.Label = *o.Label
		}
		if o.Verification != nil {
			in.Verification = *o.Verification
		}
		tx.Record(o.Name(), "input/"+o.ArtifactID, before, *in)
		return nil
	}
	return NotFound("no such input")
}

// SetLifecycle moves the problem through draft/investigating/alternatives/
// archived. owner-selected is reachable only through ApproveDecision.
type SetLifecycle struct {
	Op        string `json:"op"`
	Lifecycle string `json:"lifecycle"`
}

func (o *SetLifecycle) Name() string { return "SetLifecycle" }
func (o *SetLifecycle) Check() []string {
	if !problemLifecycles[o.Lifecycle] {
		return []string{"lifecycle is not recognised"}
	}
	if o.Lifecycle == LifecycleOwnerSelected {
		return []string{"owner-selected is set only by approving a decision"}
	}
	return nil
}
func (o *SetLifecycle) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	tx.Record(o.Name(), "problem.lifecycle", p.Lifecycle, o.Lifecycle)
	p.Lifecycle = o.Lifecycle
	return nil
}

// LinkExternal adds or removes a read-only link to an existing task or
// decision record (never a write to it).
type LinkExternal struct {
	Op     string `json:"op"`
	List   string `json:"list"` // tasks | decisions
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Remove bool   `json:"remove,omitempty"`
}

var externalKinds = map[string]bool{"property-task": true, "home-task": true, "property-work": true, "re-decision": true}

func (o *LinkExternal) Name() string { return "LinkExternal" }
func (o *LinkExternal) Check() []string {
	var out []string
	if o.List != "tasks" && o.List != "decisions" {
		out = append(out, "list must be tasks or decisions")
	}
	if !externalKinds[o.Kind] {
		out = append(out, "kind must be property-task, home-task, property-work or re-decision")
	}
	if !scopeIDRE.MatchString(o.ID) {
		out = append(out, "id must be an exact record id")
	}
	return out
}
func (o *LinkExternal) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	list := &p.Links.Tasks
	if o.List == "decisions" {
		list = &p.Links.Decisions
	}
	ref := ExternalRef{Kind: o.Kind, ID: o.ID}
	for i, r := range *list {
		if r.Kind == o.Kind && r.ID == o.ID {
			if o.Remove {
				*list = append((*list)[:i], (*list)[i+1:]...)
				tx.Record(o.Name(), "links/"+o.ID, r, nil)
			}
			return nil
		}
	}
	if o.Remove {
		return NotFound("no such link")
	}
	*list = append(*list, ref)
	tx.Record(o.Name(), "links/"+o.ID, nil, ref)
	return nil
}
