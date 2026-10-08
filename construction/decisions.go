package construction

// Design decisions (§12.7, P7). An agent (or the owner) may PROPOSE a
// decision that binds one exact assembly revision and retains that
// revision's unresolved issues. Only the owner can accept it for the
// project — binding the exact decision revision they reviewed — or reject
// it. Acceptance is a project decision, never approval for construction.
// Any later change to the bound assembly makes the acceptance stale; nothing
// follows a head silently.

import (
	"strings"
	"time"
)

func init() {
	registerOp("ProposeDecision", TargetDecision, false, func() Operation { return &ProposeDecision{} })
	registerOp("ApproveDecision", TargetDecision, true, func() Operation { return &ApproveDecision{} })
	registerOp("RejectDecision", TargetDecision, true, func() Operation { return &RejectDecision{} })
	docValidators[DocDecision] = func(st *State, key string, doc any) error {
		d := doc.(*Decision)
		pid := ""
		if st.Problem != nil {
			pid = st.Problem.ID
		}
		out := checkEnvelope(d.Envelope, DocDecision, KindDecision)
		if d.ProblemID != pid {
			out = append(out, "decision belongs to its problem")
		}
		out = append(out, checkText("title", d.Title, MaxTitle, true)...)
		out = append(out, checkText("proposal", d.Proposal, MaxText, true)...)
		out = append(out, checkText("rationale", d.Rationale, MaxText, false)...)
		if !ValidID(KindAssembly, d.Assembly.ID) || !ValidToken(d.Assembly.Revision) {
			out = append(out, "a decision binds an exact assembly revision")
		}
		switch d.Status {
		case DecisionProposed, DecisionRejected, DecisionSuperseded:
		case DecisionAccepted:
			if d.ReviewedBy == nil || d.ReviewedBy.Kind != ActorOwner {
				out = append(out, "only the owner accepts a decision")
			}
		default:
			out = append(out, "decision status is not recognised")
		}
		if d.ReviewedBy != nil && d.ReviewedBy.Kind != ActorOwner {
			out = append(out, "only the owner reviews a decision")
		}
		if len(out) > 0 {
			return Invalid(out...)
		}
		return nil
	}
}

// ProposeDecision records a proposal bound to an exact assembly revision
// ("" = the current head), keeping the unresolved issues of that revision.
type ProposeDecision struct {
	Op               string       `json:"op"`
	DecisionID       string       `json:"decisionId"`
	AssemblyID       string       `json:"assemblyId"`
	AssemblyRevision string       `json:"assemblyRevision,omitempty"`
	Title            string       `json:"title"`
	Proposal         string       `json:"proposal"`
	Rationale        string       `json:"rationale,omitempty"`
	Supporting       []string     `json:"supporting,omitempty"`
	Contradicting    []string     `json:"contradicting,omitempty"`
	OpenQuestions    []string     `json:"openQuestions,omitempty"`
	Alternatives     []VersionRef `json:"alternatives,omitempty"`
}

func (o *ProposeDecision) Name() string { return "ProposeDecision" }
func (o *ProposeDecision) Check() []string {
	out := checkText("title", o.Title, MaxTitle, true)
	out = append(out, checkText("proposal", o.Proposal, MaxText, true)...)
	out = append(out, checkText("rationale", o.Rationale, MaxText, false)...)
	if !ValidID(KindDecision, o.DecisionID) || !ValidID(KindAssembly, o.AssemblyID) {
		out = append(out, "decisionId (new dec- id) and assemblyId are required")
	}
	if o.AssemblyRevision != "" && !ValidToken(o.AssemblyRevision) {
		out = append(out, "assemblyRevision must be a revision token")
	}
	for _, id := range append(append([]string{}, o.Supporting...), o.Contradicting...) {
		if !ValidID(KindEvidence, id) {
			out = append(out, "supporting/contradicting are evd- ids")
		}
	}
	if len(o.OpenQuestions) > 20 || len(o.Alternatives) > 10 {
		out = append(out, "too many open questions or alternatives")
	}
	for _, q := range o.OpenQuestions {
		out = append(out, checkText("openQuestion", q, 500, true)...)
	}
	return out
}
func (o *ProposeDecision) Apply(tx *Tx, c *ApplyContext) error {
	if tx.Next.Decisions[o.DecisionID] != nil {
		return Invalid("decision id already exists")
	}
	head := tx.Base.Revision("assembly:" + o.AssemblyID)
	if head == "" {
		return NotFound("no such assembly in this problem")
	}
	rev := o.AssemblyRevision
	if rev == "" {
		rev = head
	}
	if rev != head {
		if _, err := tx.assemblyAt(o.AssemblyID, rev); err != nil {
			return err
		}
	}
	for _, id := range append(append([]string{}, o.Supporting...), o.Contradicting...) {
		if _, ok := tx.Next.Evidence.evidence(id); !ok {
			return NotFound("no evidence " + id + " in this problem")
		}
	}
	for _, alt := range o.Alternatives {
		if tx.Base.Revision("assembly:"+alt.ID) == "" || !ValidToken(alt.Revision) {
			return Invalid("alternatives considered must pin this problem's assemblies")
		}
	}
	var issues []IssueBrief
	if rep := tx.Base.Validation[o.AssemblyID]; rep != nil && rep.AssemblyRevision == rev {
		issues = unresolvedIssues(rep)
	} else if tx.store != nil {
		if rep, err := tx.store.ValidationAt(tx.subject, tx.Next.Problem.ID, o.AssemblyID, rev); err == nil {
			issues = unresolvedIssues(rep)
		}
	}
	if issues == nil {
		issues = []IssueBrief{}
	}
	d := &Decision{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocDecision, ID: o.DecisionID}, ProblemID: tx.Next.Problem.ID,
		Title: strings.TrimSpace(o.Title), Proposal: o.Proposal, Rationale: o.Rationale, Assembly: VersionRef{ID: o.AssemblyID, Revision: rev},
		Alternatives: nonNilRefs(o.Alternatives), Supporting: sortedUnique(o.Supporting), Contradicting: sortedUnique(o.Contradicting),
		OpenQuestions: nonNilStrings(o.OpenQuestions), UnresolvedIssues: issues, ProposedBy: tx.Actor, Status: DecisionProposed, Links: []ExternalRef{}}
	tx.Next.Decisions[d.ID] = d
	tx.Next.Problem.Decisions = append(tx.Next.Problem.Decisions, d.ID)
	tx.Record(o.Name(), "decision:"+d.ID, nil, map[string]any{"assembly": d.Assembly, "unresolved": len(issues), "proposedBy": tx.Actor.Principal})
	tx.Summary("decision proposed: " + d.Title)
	tx.Event("construction.decision.proposed", d.Title)
	return nil
}

func unresolvedIssues(rep *ValidationReport) []IssueBrief {
	out := []IssueBrief{}
	for _, is := range rep.Issues {
		if is.Status == "resolved" || is.Severity == SevInfo {
			continue
		}
		out = append(out, IssueBrief{ID: is.ID, RuleKey: is.RuleKey, Severity: is.Severity, Message: truncate(is.Message, 300)})
	}
	return out
}

func nonNilRefs(x []VersionRef) []VersionRef {
	if x == nil {
		return []VersionRef{}
	}
	return x
}

// ApproveDecision is the owner's acceptance of one exact decision revision
// for the project. It is refused when the decision changed since it was
// reviewed, or when its assembly moved on since the proposal (propose again
// on the current revision).
type ApproveDecision struct {
	Op                       string `json:"op"`
	DecisionID               string `json:"decisionId"`
	ExpectedDecisionRevision string `json:"expectedDecisionRevision"`
	Note                     string `json:"note,omitempty"`
}

func (o *ApproveDecision) Name() string { return "ApproveDecision" }
func (o *ApproveDecision) Check() []string {
	out := checkText("note", o.Note, 1000, false)
	if !ValidID(KindDecision, o.DecisionID) || !ValidToken(o.ExpectedDecisionRevision) {
		out = append(out, "decisionId and the exact expectedDecisionRevision are required")
	}
	return out
}
func (o *ApproveDecision) Apply(tx *Tx, c *ApplyContext) error {
	d := tx.Next.Decisions[o.DecisionID]
	if d == nil {
		return NotFound("no such decision")
	}
	if cur := tx.Base.Revision("decision:" + d.ID); cur != o.ExpectedDecisionRevision {
		return Conflict("the decision changed since it was reviewed", map[string]string{"decision:" + d.ID: cur})
	}
	if d.Status != DecisionProposed {
		return Conflict("only a proposed decision can be accepted (it is "+d.Status+")", nil)
	}
	if head := tx.Base.Revision("assembly:" + d.Assembly.ID); head != d.Assembly.Revision {
		return Conflict("the assembly changed since this decision was proposed; propose again on the current revision", map[string]string{"assembly:" + d.Assembly.ID: head})
	}
	now := tx.Now.Format(time.RFC3339Nano)
	for id, other := range tx.Next.Decisions {
		if id != d.ID && other.Status == DecisionAccepted {
			other.Status = DecisionSuperseded
			tx.Record(o.Name(), "decision:"+id, DecisionAccepted, DecisionSuperseded)
		}
	}
	reviewer := tx.Actor
	d.Status, d.ReviewedBy, d.ReviewedAt, d.ReviewNote = DecisionAccepted, &reviewer, now, o.Note
	p := tx.Next.Problem
	sel := d.Assembly
	before := p.SelectedAssembly
	p.SelectedAssembly = &sel
	p.Lifecycle = LifecycleOwnerSelected
	tx.Record(o.Name(), "decision:"+d.ID, DecisionProposed, map[string]any{"status": DecisionAccepted, "assembly": sel, "note": o.Note})
	tx.Record(o.Name(), "problem.selectedAssembly", before, sel)
	tx.Summary("decision accepted for the project (not approved for construction): " + d.Title)
	tx.Event("construction.decision.accepted", d.Title)
	return nil
}

// RejectDecision is the owner's rejection of a proposed decision.
type RejectDecision struct {
	Op                       string `json:"op"`
	DecisionID               string `json:"decisionId"`
	ExpectedDecisionRevision string `json:"expectedDecisionRevision"`
	Note                     string `json:"note,omitempty"`
}

func (o *RejectDecision) Name() string { return "RejectDecision" }
func (o *RejectDecision) Check() []string {
	out := checkText("note", o.Note, 1000, false)
	if !ValidID(KindDecision, o.DecisionID) || !ValidToken(o.ExpectedDecisionRevision) {
		out = append(out, "decisionId and the exact expectedDecisionRevision are required")
	}
	return out
}
func (o *RejectDecision) Apply(tx *Tx, c *ApplyContext) error {
	d := tx.Next.Decisions[o.DecisionID]
	if d == nil {
		return NotFound("no such decision")
	}
	if cur := tx.Base.Revision("decision:" + d.ID); cur != o.ExpectedDecisionRevision {
		return Conflict("the decision changed since it was reviewed", map[string]string{"decision:" + d.ID: cur})
	}
	if d.Status != DecisionProposed {
		return Conflict("only a proposed decision can be rejected", nil)
	}
	reviewer := tx.Actor
	d.Status, d.ReviewedBy, d.ReviewedAt, d.ReviewNote = DecisionRejected, &reviewer, tx.Now.Format(time.RFC3339Nano), o.Note
	tx.Record(o.Name(), "decision:"+d.ID, DecisionProposed, DecisionRejected)
	tx.Summary("decision rejected: " + d.Title)
	return nil
}

// DecisionView is a decision as shown: its stored status plus whether the
// bound assembly has moved on since (stale), never silently re-bound.
type DecisionView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Stale  bool   `json:"stale"`
	Reason string `json:"reason,omitempty"`
}

// DecisionStates computes each decision's staleness against the heads.
func DecisionStates(st *State) map[string]DecisionView {
	out := map[string]DecisionView{}
	for id, d := range st.Decisions {
		v := DecisionView{ID: id, Status: d.Status}
		head := st.Revision("assembly:" + d.Assembly.ID)
		if (d.Status == DecisionAccepted || d.Status == DecisionProposed) && head != d.Assembly.Revision {
			v.Stale = true
			v.Reason = "the assembly changed after this decision bound revision " + d.Assembly.Revision[:12] + "; review the comparison and decide again"
		}
		out[id] = v
	}
	return out
}
