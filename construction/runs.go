package construction

// Research run lifecycle (§6). A ResearchRun is a domain workflow
// checkpoint, not a scheduler: the document records the plan, one attempt
// per stage try (with its parent, epoch, input/result hashes, error class and
// native receipt identity) and a durable event sequence. Every transition is
// its own commit, so progress is read only from commits, a restart between
// stages loses nothing, and a stage that was running when the process died
// is reported disconnected — never silently re-run or re-sent.
//
// Fencing: a stage result changes the problem only when the run's epoch
// still equals the attempt's epoch and no stop was requested. A late result
// after cancellation is retained as an unselected artifact ("fenced").
// Retry and resume start a new epoch; retry also allows a new native request
// after an uncertain outcome, resume does not.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Stage names in order.
const (
	StageDecompose  = "decompose"
	StagePlan       = "plan"
	StageAcquire    = "acquire"
	StageExtract    = "extract"
	StageSynthesize = "synthesize"
	StageCompile    = "compile"
	StageValidate   = "validate"
	StagePublish    = "publish"
)

// Stage/attempt states. "fenced" marks a late attempt result kept unselected.
const (
	StagePending      = "pending"
	StageRunning      = "running"
	StageCompleted    = "completed"
	StageFailed       = "failed"
	StageCancelled    = "cancelled"
	StageDisconnected = "disconnected"
	StageWaiting      = "waiting-input"
	StageFenced       = "fenced"
)

var stageStates = map[string]bool{StagePending: true, StageRunning: true, StageCompleted: true, StageFailed: true,
	StageCancelled: true, StageDisconnected: true, StageWaiting: true, StageFenced: true}

const maxRunEvents = 400

// RunRequest is the body of POST …/problems/{id}/research-runs.
type RunRequest struct {
	SchemaVersion           int         `json:"schemaVersion"`
	RequestID               string      `json:"requestId"`
	ExpectedProblemRevision string      `json:"expectedProblemRevision"`
	Questions               []string    `json:"questions,omitempty"`
	SourceBudget            int         `json:"sourceBudget,omitempty"`
	Agent                   AgentChoice `json:"agent"`
	BaseAssembly            string      `json:"baseAssembly,omitempty"`
	// Start false records a planned run without queueing it (open/assign
	// never dispatches).
	Start *bool `json:"start,omitempty"`
}

var runModes = map[string]bool{"local-only": true, "native": true}

// ParseRunRequest strictly decodes a run request and returns its payload hash.
func ParseRunRequest(raw []byte) (*RunRequest, string, error) {
	var req RunRequest
	if err := decodeRequest(raw, &req); err != nil {
		return nil, "", err
	}
	var out []string
	if req.SchemaVersion != SchemaVersion {
		out = append(out, "schemaVersion must be 1")
	}
	if !ValidRequestID(req.RequestID) {
		out = append(out, "requestId must be 8–128 characters of [A-Za-z0-9_-]")
	}
	if !ValidToken(req.ExpectedProblemRevision) {
		out = append(out, "expectedProblemRevision is required")
	}
	if len(req.Questions) > 20 {
		out = append(out, "at most 20 questions")
	}
	for i, q := range req.Questions {
		out = append(out, checkText(fmt.Sprintf("questions[%d]", i), q, 500, true)...)
	}
	if req.SourceBudget < 0 || req.SourceBudget > MaxDocumentsPerRun {
		out = append(out, fmt.Sprintf("sourceBudget must be 1–%d", MaxDocumentsPerRun))
	}
	out = append(out, checkAgentChoice(req.Agent)...)
	if req.BaseAssembly != "" && !ValidID(KindAssembly, req.BaseAssembly) {
		out = append(out, "baseAssembly must be an asm- id")
	}
	if len(out) > 0 {
		return nil, "", Invalid(out...)
	}
	canon, err := CanonicalizeJSON(raw)
	if err != nil {
		return nil, "", Invalid(err.Error())
	}
	return &req, Token(canon), nil
}

func checkAgentChoice(a AgentChoice) []string {
	var out []string
	if a.Agent != "" && !stewards[a.Agent] {
		out = append(out, "agent must be alfred or zeck")
	}
	if a.Mode != "" && !runModes[a.Mode] {
		out = append(out, "mode must be local-only or native")
	}
	for f, v := range map[string]string{"requestedProvider": a.RequestedProvider, "requestedModel": a.RequestedModel, "effort": a.Effort, "profile": a.Profile} {
		out = append(out, checkText(f, v, 120, false)...)
		if strings.ContainsAny(v, " \t\n/\\") {
			out = append(out, f+" must be a single token")
		}
	}
	return out
}

func newRunStages() []Stage {
	out := make([]Stage, len(StageNames))
	for i, n := range StageNames {
		out[i] = Stage{Name: n, State: StagePending, Attempts: []Attempt{}}
	}
	return out
}

func (r *ResearchRun) stage(name string) *Stage {
	for i := range r.Stages {
		if r.Stages[i].Name == name {
			return &r.Stages[i]
		}
	}
	return nil
}

// LastAttempt is a stage's most recent attempt (nil when never tried).
func (s *Stage) LastAttempt() *Attempt {
	if len(s.Attempts) == 0 {
		return nil
	}
	return &s.Attempts[len(s.Attempts)-1]
}

// Completed is the stage's last completed attempt (nil when none).
func (s *Stage) Completed() *Attempt {
	for i := len(s.Attempts) - 1; i >= 0; i-- {
		if s.Attempts[i].State == StageCompleted {
			return &s.Attempts[i]
		}
	}
	return nil
}

func (r *ResearchRun) attempt(id string) (*Stage, *Attempt) {
	for i := range r.Stages {
		for j := range r.Stages[i].Attempts {
			if r.Stages[i].Attempts[j].ID == id {
				return &r.Stages[i], &r.Stages[i].Attempts[j]
			}
		}
	}
	return nil, nil
}

func (r *ResearchRun) event(now time.Time, stage, attempt, state, msg string) {
	r.Sequence++
	r.Events = append(r.Events, RunEvent{Seq: r.Sequence, At: now.Format(time.RFC3339Nano), Stage: stage, Attempt: attempt, State: state, Message: truncate(msg, 500)})
	if len(r.Events) > maxRunEvents {
		r.Events = append([]RunEvent{}, r.Events[len(r.Events)-maxRunEvents:]...)
	}
}

// Terminal reports a run that will not continue without retry/resume.
func (r *ResearchRun) Terminal() bool {
	switch r.State {
	case RunCompleted, RunCancelled, RunFailed, RunDisconnected, RunWaitingInput:
		return true
	}
	return false
}

// runIDFromReceipt finds the run a commit created.
func runIDFromReceipt(rc *Receipt) string {
	for _, c := range rc.Changes {
		if strings.HasPrefix(c.Key, "run:") && c.From == "" {
			return strings.TrimPrefix(c.Key, "run:")
		}
	}
	for _, op := range rc.Operations {
		if op.Op == "CreateRun" {
			return strings.TrimPrefix(op.Target, "run:")
		}
	}
	return ""
}

// CreateRun records a new research run (queued unless Start is false). The
// capabilities are what the server observed at start; nothing is assumed.
func (s *Store) CreateRun(sub SubjectRef, problemID string, req *RunRequest, payloadHash string, actor Actor, caps RunCapabilities) (*State, *ResearchRun, *Receipt, error) {
	st, rc, err := s.Commit(sub, problemID, CommitRequest{RequestID: req.RequestID, PayloadHash: payloadHash, Actor: actor}, func(tx *Tx) error {
		if cur := tx.Base.Revision("problem"); cur != req.ExpectedProblemRevision {
			return Conflict("the problem changed since it was loaded", map[string]string{"problem": cur})
		}
		p := tx.Next.Problem
		for _, r := range tx.Next.Runs {
			if !r.Terminal() && r.State != RunPlanned {
				return Conflict("another research run is active on this problem: "+r.ID, nil)
			}
		}
		if len(tx.Next.Runs) >= 200 {
			return Invalid("run limit reached")
		}
		base := req.BaseAssembly
		if base == "" {
			base = p.ActiveAssembly
		}
		if base == "" && len(p.Alternatives) > 0 {
			base = p.Alternatives[0]
		}
		if base != "" && tx.Next.Assemblies[base] == nil {
			return NotFound("no such base assembly")
		}
		agent := req.Agent
		if agent.Agent == "" {
			agent.Agent = p.Steward.Agent
		}
		if agent.Mode == "" {
			agent.Mode = "local-only"
		}
		if agent.Mode == "native" && caps.NativeAgent != "available" {
			return Unavailable("native agent research is unavailable here: " + strings.Join(caps.Notes, "; "))
		}
		budget := req.SourceBudget
		if budget == 0 {
			budget = 12
		}
		qs := []Question{}
		for i, q := range req.Questions {
			qs = append(qs, Question{ID: fmt.Sprintf("q-owner-%d", i+1), Text: strings.TrimSpace(q), Status: "open"})
		}
		state := RunQueued
		if req.Start != nil && !*req.Start {
			state = RunPlanned
		}
		run := &ResearchRun{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocRun, ID: NewID(KindRun)}, ProblemID: p.ID,
			BaseProblemRevision: tx.Base.Revision("problem"), BaseAssembly: base,
			Plan:  RunPlan{Questions: qs, Scope: "roof-to-masonry junction", SourceBudget: budget, SourcePriority: append([]string{}, SourceClasses...)},
			Agent: agent, State: state, Epoch: 1, Stages: newRunStages(), Events: []RunEvent{}, Capabilities: caps}
		if run.Capabilities.Notes == nil {
			run.Capabilities.Notes = []string{}
		}
		run.event(tx.Now, "", "", state, "run created by "+actor.Principal+" ("+agent.Mode+", agent "+agent.Agent+")")
		tx.Next.Runs[run.ID] = run
		p.LatestRun = run.ID
		if p.Lifecycle == LifecycleDraft {
			p.Lifecycle = LifecycleInvestigating
		}
		tx.Record("CreateRun", "run:"+run.ID, nil, map[string]any{"agent": agent, "base": base, "budget": budget, "state": state})
		tx.Summary("research run created")
		tx.Event("construction.run.created", agent.Mode+" research run")
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	id := runIDFromReceipt(rc)
	if st.Runs[id] == nil {
		return st, nil, rc, corrupt("run %s missing after creation", id)
	}
	return st, st.Runs[id], rc, nil
}

func runCommitRequest(requestID string, actor Actor, payload any) (CommitRequest, error) {
	_, h, err := TokenOf(payload)
	if err != nil {
		return CommitRequest{}, err
	}
	return CommitRequest{RequestID: requestID, PayloadHash: h, Actor: actor}, nil
}

// StartRun queues a planned run.
func (s *Store) StartRun(sub SubjectRef, problemID, runID, requestID string, actor Actor) (*State, *ResearchRun, error) {
	req, err := runCommitRequest(requestID, actor, map[string]any{"op": "StartRun", "run": runID})
	if err != nil {
		return nil, nil, err
	}
	st, _, err := s.Commit(sub, problemID, req, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		if r.State != RunPlanned {
			return Conflict("run is "+r.State+", not planned", nil)
		}
		r.State = RunQueued
		r.event(tx.Now, "", "", RunQueued, "queued by "+actor.Principal)
		tx.Record("StartRun", "run:"+runID, RunPlanned, RunQueued)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return st, st.Runs[runID], nil
}

// RetainSpec is one artifact a stage keeps (source snapshot, packet).
type RetainSpec struct {
	Kind    string
	Title   string
	Content []byte
}

// ClaimStage starts an attempt: one running attempt per run, only on the
// current epoch, never after a stop request. The packet (an agent step's
// exact context) is retained in the same commit.
func (s *Store) ClaimStage(sub SubjectRef, problemID, runID string, epoch int, stage, attemptID, inputHash string, packet *RetainSpec, actor Actor) (*State, *ResearchRun, error) {
	if !ValidID(KindOperation, attemptID) || !ValidToken(inputHash) {
		return nil, nil, Invalid("attempt id and input hash are required")
	}
	req, err := runCommitRequest("claim-"+attemptID, actor, map[string]any{"run": runID, "epoch": epoch, "stage": stage, "attempt": attemptID, "input": inputHash})
	if err != nil {
		return nil, nil, err
	}
	st, _, err := s.Commit(sub, problemID, req, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		if r.Epoch != epoch {
			return Conflict(fmt.Sprintf("run epoch is %d, not %d", r.Epoch, epoch), nil)
		}
		if r.StopRequested {
			return Conflict("a stop was requested for this run", nil)
		}
		if r.State != RunQueued && r.State != RunRunning {
			return Conflict("run is "+r.State, nil)
		}
		for _, sg := range r.Stages {
			if sg.State == StageRunning {
				return Conflict("stage "+sg.Name+" is already running", nil)
			}
		}
		sg := r.stage(stage)
		if sg == nil {
			return Invalid("unknown stage " + stage)
		}
		for _, prior := range r.Stages {
			if prior.Name == stage {
				break
			}
			if prior.State != StageCompleted {
				return Conflict("stage "+prior.Name+" has not completed", nil)
			}
		}
		at := Attempt{ID: attemptID, Number: len(sg.Attempts) + 1, Epoch: epoch, State: StageRunning, StartedAt: tx.Now.Format(time.RFC3339Nano), InputHash: inputHash}
		if last := sg.LastAttempt(); last != nil {
			at.Parent = last.ID
		}
		if packet != nil {
			id, rev, err := tx.Retain(packet.Kind, packet.Title, packet.Content)
			if err != nil {
				return err
			}
			at.CheckpointHash = rev
			at.Native = &NativeRef{PacketHash: rev, RequestID: "cx-" + strings.TrimPrefix(attemptID, "op-"), State: "prepared"}
			_ = id
			tx.Output(rev)
		}
		sg.Attempts = append(sg.Attempts, at)
		sg.State = StageRunning
		r.State = RunRunning
		r.event(tx.Now, stage, attemptID, StageRunning, fmt.Sprintf("%s attempt %d started (epoch %d)", stage, at.Number, epoch))
		tx.Record("ClaimStage", "run:"+runID+"/"+stage, nil, map[string]any{"attempt": attemptID, "epoch": epoch, "input": inputHash})
		tx.Summary(stage + " started")
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return st, st.Runs[runID], nil
}

// MarkNative persists a native step's dispatch identity before the
// instruction is accepted, so a restart knows a request may exist. It is
// refused once a stop was requested: nothing is dispatched after a stop.
func (s *Store) MarkNative(sub SubjectRef, problemID, runID, attemptID string, n NativeRef) (*State, error) {
	req, err := runCommitRequest("dispatch-"+attemptID, SystemActor(runID), map[string]any{"attempt": attemptID, "native": n})
	if err != nil {
		return nil, err
	}
	st, _, err := s.Commit(sub, problemID, req, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		sg, at := r.attempt(attemptID)
		if at == nil {
			return NotFound("no such attempt")
		}
		if at.State != StageRunning || r.Epoch != at.Epoch {
			return Conflict("attempt is not running on the current epoch", nil)
		}
		if r.StopRequested {
			return Conflict("a stop was requested; nothing is dispatched", nil)
		}
		if at.Native != nil && n.PacketHash == "" {
			n.PacketHash = at.Native.PacketHash
		}
		at.Native = &n
		tx.Native(n)
		r.event(tx.Now, sg.Name, attemptID, "dispatched", "agent step dispatched to "+n.Agent+" ("+orDefault(n.RequestedModel, "model unspecified")+")")
		tx.Record("MarkNative", "run:"+runID+"/"+sg.Name, nil, n)
		return nil
	})
	return st, err
}

// StageOutcome is what an attempt produced.
type StageOutcome struct {
	State   string // completed | failed | cancelled | waiting-input
	Result  any    // retained as the stage result (canonical JSON); may be nil on failure
	Error   *StageError
	Native  *NativeRef
	Summary string
	Retain  []RetainSpec
	// Apply makes the stage's domain change (evidence at extract,
	// alternatives at publish). It runs only when the result is not fenced.
	Apply  func(tx *Tx, r *ResearchRun) error
	Counts func(c *RunCounts)
}

// FinishStage records an attempt's outcome. It reports fenced=true when the
// epoch moved or a stop was requested: the result is then retained as an
// unselected artifact and nothing else changes.
func (s *Store) FinishStage(sub SubjectRef, problemID, runID, attemptID string, out StageOutcome, actor Actor) (*State, *ResearchRun, bool, error) {
	var raw []byte
	if out.Result != nil {
		b, err := Canonical(out.Result)
		if err != nil {
			return nil, nil, false, Invalid("stage result: " + err.Error())
		}
		if len(b) > MaxDocumentBytes {
			return nil, nil, false, &Error{Status: 413, Kind: "too-large", Message: "stage result exceeds the document budget"}
		}
		raw = b
	}
	var rh []string
	for _, r := range out.Retain {
		rh = append(rh, Token(r.Content))
	}
	payload := map[string]any{"attempt": attemptID, "state": out.State, "result": Token(raw), "error": out.Error, "native": out.Native, "retain": rh}
	req, err := runCommitRequest("finish-"+attemptID, actor, payload)
	if err != nil {
		return nil, nil, false, err
	}
	fenced := false
	st, _, err := s.Commit(sub, problemID, req, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		sg, at := r.attempt(attemptID)
		if at == nil {
			return NotFound("no such attempt")
		}
		if at.State != StageRunning {
			return Conflict("attempt is "+at.State+", not running", nil)
		}
		if raw != nil {
			id, rev, err := tx.Retain("construction-stage-result", sg.Name+" result", raw)
			if err != nil {
				return err
			}
			at.Result = &VersionRef{ID: id, Revision: rev}
			at.ResultHash = rev
			tx.Output(rev)
		}
		for _, rs := range out.Retain {
			_, rev, err := tx.Retain(rs.Kind, rs.Title, rs.Content)
			if err != nil {
				return err
			}
			tx.Output(rev)
		}
		at.FinishedAt = tx.Now.Format(time.RFC3339Nano)
		at.Error = out.Error
		if out.Native != nil {
			n := *out.Native
			if at.Native != nil && n.PacketHash == "" {
				n.PacketHash = at.Native.PacketHash
			}
			at.Native = &n
			tx.Native(n)
		}
		at.Summary = truncate(out.Summary, 500)
		if r.StopRequested && r.Epoch == at.Epoch && out.State == StageCancelled {
			at.State, sg.State, r.State = StageCancelled, StageCancelled, RunCancelled
			r.event(tx.Now, sg.Name, attemptID, RunCancelled, sg.Name+" cancelled at owner request")
			tx.Record("FinishStage", "run:"+runID+"/"+sg.Name, nil, map[string]any{"attempt": attemptID, "state": StageCancelled})
			tx.Summary("research run cancelled")
			tx.Event("construction.run.cancelled", sg.Name+" cancelled")
			return nil
		}
		fenced = r.Epoch != at.Epoch || r.StopRequested
		if fenced {
			at.State = StageFenced
			if r.StopRequested && r.Epoch == at.Epoch {
				sg.State = StageCancelled
				r.State = RunCancelled
				r.event(tx.Now, sg.Name, attemptID, RunCancelled, sg.Name+" result arrived after the stop request; kept unselected, nothing applied")
			} else {
				r.event(tx.Now, sg.Name, attemptID, StageFenced, fmt.Sprintf("late %s result from epoch %d kept unselected (run is at epoch %d)", sg.Name, at.Epoch, r.Epoch))
			}
			tx.Record("FinishStage", "run:"+runID+"/"+sg.Name, nil, map[string]any{"attempt": attemptID, "fenced": true})
			return nil
		}
		at.State = out.State
		sg.State = out.State
		switch out.State {
		case StageCompleted:
			if out.Apply != nil {
				if err := out.Apply(tx, r); err != nil {
					return err
				}
			}
			if sg.Name == StagePublish {
				r.State = RunCompleted
			} else {
				r.State = RunRunning
			}
		case StageFailed:
			r.State = RunFailed
		case StageCancelled:
			r.State = RunCancelled
		case StageWaiting:
			r.State = RunWaitingInput
		default:
			return Invalid("stage outcome state " + out.State + " is not recognised")
		}
		if out.Counts != nil {
			out.Counts(&r.Counts)
		}
		msg := sg.Name + " " + out.State
		if out.Summary != "" {
			msg += ": " + out.Summary
		}
		if out.Error != nil {
			msg += " (" + out.Error.Class + ": " + out.Error.Message + ")"
		}
		r.event(tx.Now, sg.Name, attemptID, out.State, msg)
		if r.State == RunCompleted {
			r.event(tx.Now, "", "", RunCompleted, "run completed")
		}
		tx.Record("FinishStage", "run:"+runID+"/"+sg.Name, nil, map[string]any{"attempt": attemptID, "state": out.State, "result": at.ResultHash})
		tx.Summary(sg.Name + " " + out.State)
		if r.State == RunCompleted || r.State == RunFailed {
			tx.Event("construction.run."+r.State, msg)
		}
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	return st, st.Runs[runID], fenced, nil
}

// StopRun persists the stop request first. A run with nothing in flight is
// cancelled at once; a running stage becomes stop-requested and the worker
// (or the next restart's reconcile) finishes the cancellation.
func (s *Store) StopRun(sub SubjectRef, problemID, runID, requestID, payloadHash string, actor Actor) (*State, *ResearchRun, error) {
	st, _, err := s.Commit(sub, problemID, CommitRequest{RequestID: requestID, PayloadHash: payloadHash, Actor: actor}, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		switch r.State {
		case RunCompleted, RunCancelled, RunFailed:
			return Conflict("run is already "+r.State, nil)
		}
		r.StopRequested = true
		r.StopRequestedAt = tx.Now.Format(time.RFC3339Nano)
		running := false
		for _, sg := range r.Stages {
			running = running || sg.State == StageRunning
		}
		before := r.State
		if running {
			r.State = RunStopRequested
			r.event(tx.Now, "", "", RunStopRequested, "stop requested by "+actor.Principal+"; the running stage is being cancelled")
		} else {
			r.State = RunCancelled
			r.event(tx.Now, "", "", RunCancelled, "cancelled by "+actor.Principal+" (nothing was running)")
		}
		tx.Record("StopRun", "run:"+runID, before, r.State)
		tx.Summary("research run stop requested")
		tx.Event("construction.run.stop", "stop requested")
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return st, st.Runs[runID], nil
}

// RestartRun is retry (mode "retry") or resume (mode "resume"). Both start a
// new epoch and queue the run; completed stages are reused only when their
// inputs still match (the worker checks). Resume refuses a stage whose
// native outcome is uncertain — only an explicit retry may send again.
// acceptRuntime records the owner's acceptance of a requested/observed
// runtime mismatch on a waiting stage.
func (s *Store) RestartRun(sub SubjectRef, problemID, runID, mode string, acceptRuntime bool, requestID, payloadHash string, actor Actor) (*State, *ResearchRun, error) {
	if mode != "retry" && mode != "resume" {
		return nil, nil, Invalid("mode must be retry or resume")
	}
	st, _, err := s.Commit(sub, problemID, CommitRequest{RequestID: requestID, PayloadHash: payloadHash, Actor: actor}, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		allowed := map[string]bool{RunFailed: mode == "retry", RunCancelled: true, RunDisconnected: true, RunWaitingInput: true}
		if !allowed[r.State] {
			return Conflict("a "+r.State+" run cannot be "+map[bool]string{true: "retried", false: "resumed"}[mode == "retry"], nil)
		}
		for _, sg := range r.Stages {
			last := sg.LastAttempt()
			if last == nil {
				continue
			}
			if mode == "resume" && sg.State == StageDisconnected && last.Native != nil && !NativeSafeToResume(last.Native.State) {
				return Conflict("stage "+sg.Name+" had an agent request in flight whose outcome is unknown; resume never resends it — choose Retry to send a new request", nil)
			}
			if sg.State == StageWaiting && !acceptRuntime && mode == "resume" {
				return Conflict("stage "+sg.Name+" is waiting: "+last.Summary+" — accept the observed runtime to resume, or retry", nil)
			}
		}
		for i := range r.Stages {
			sg := &r.Stages[i]
			if last := sg.LastAttempt(); last != nil && sg.State == StageWaiting && acceptRuntime {
				r.AcceptedRuntime = append(r.AcceptedRuntime, last.ID)
			}
			switch sg.State {
			case StageFailed, StageCancelled, StageDisconnected, StageWaiting, StageRunning, StageFenced:
				sg.State = StagePending
			}
		}
		before := r.State
		r.Epoch++
		r.StopRequested, r.StopRequestedAt = false, ""
		r.State = RunQueued
		r.Retry = mode == "retry"
		r.event(tx.Now, "", "", RunQueued, fmt.Sprintf("%s by %s from %s (epoch %d)", mode, actor.Principal, before, r.Epoch))
		tx.Record("RestartRun", "run:"+runID, before, map[string]any{"mode": mode, "epoch": r.Epoch})
		tx.Summary("research run " + mode)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return st, st.Runs[runID], nil
}

// SetRunQuestions is the owner's correction of the decomposition. It never
// discards retained acquisition: only stages whose inputs change re-run.
func (s *Store) SetRunQuestions(sub SubjectRef, problemID, runID string, qs []Question, requestID, payloadHash string, actor Actor) (*State, *ResearchRun, error) {
	if len(qs) == 0 || len(qs) > 20 {
		return nil, nil, Invalid("give 1–20 questions")
	}
	seen := map[string]bool{}
	for i, q := range qs {
		if p := checkText(fmt.Sprintf("questions[%d]", i), q.Text, 500, true); len(p) > 0 {
			return nil, nil, Invalid(p...)
		}
		if q.ID == "" || len(q.ID) > 40 || seen[q.ID] {
			return nil, nil, Invalid("each question needs a unique id")
		}
		seen[q.ID] = true
		if q.Status != "open" && q.Status != "answered" && q.Status != "unanswerable" {
			return nil, nil, Invalid("question status must be open, answered or unanswerable")
		}
	}
	st, _, err := s.Commit(sub, problemID, CommitRequest{RequestID: requestID, PayloadHash: payloadHash, Actor: actor}, func(tx *Tx) error {
		r := tx.Next.Runs[runID]
		if r == nil {
			return NotFound("no such run")
		}
		if r.State == RunRunning || r.State == RunStopRequested || r.State == RunQueued {
			return Conflict("questions can be corrected while the run is not running", nil)
		}
		before := r.Plan.Questions
		r.Plan.Questions = append([]Question{}, qs...)
		r.Plan.Corrected = true
		r.event(tx.Now, StageDecompose, "", "corrected", "decomposition corrected by "+actor.Principal)
		tx.Record("SetRunQuestions", "run:"+runID+"/questions", before, qs)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return st, st.Runs[runID], nil
}

// ---- restart reconciliation --------------------------------------------------------------

// ProblemRef names one problem in the store.
type ProblemRef struct {
	Subject SubjectRef `json:"subject"`
	ID      string     `json:"id"`
}

// Problems lists every problem in the store (all subjects), by subject index.
func (s *Store) Problems() ([]ProblemRef, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "projects"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ProblemRef
	for _, e := range entries {
		if !e.IsDir() || !ValidToken(e.Name()) {
			continue
		}
		raw, err := readNoFollow(filepath.Join(s.root, "projects", e.Name(), "subject.json"))
		if err != nil {
			continue
		}
		var idx struct {
			Subject SubjectRef `json:"subject"`
		}
		if json.Unmarshal(raw, &idx) != nil || ProjectKey(idx.Subject) != e.Name() {
			continue
		}
		ps, err := os.ReadDir(filepath.Join(s.root, "projects", e.Name(), "problems"))
		if err != nil {
			continue
		}
		for _, p := range ps {
			if p.IsDir() && ValidID(KindProblem, p.Name()) {
				out = append(out, ProblemRef{Subject: idx.Subject, ID: p.Name()})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ReconcileRuns runs at startup, before any dispatch: a stage that was
// running when the process died has an unknown outcome — it is marked
// disconnected, never re-run or re-sent; a queued run is not started
// (refreshing never starts work); a stop request becomes cancelled.
// nativeState reports what the native store knows about an attempt's
// delivery: "not-sent", "completed", "failed" or "uncertain" ("" = unknown).
func (s *Store) ReconcileRuns(nativeState func(n NativeRef) string) ([]string, error) {
	refs, err := s.Problems()
	if err != nil {
		return nil, err
	}
	var touched []string
	var errs []string
	for _, pr := range refs {
		st, err := s.Load(pr.Subject, pr.ID)
		if err != nil || st.ReadOnly {
			continue
		}
		ids := make([]string, 0, len(st.Runs))
		for id, r := range st.Runs {
			if r.State == RunRunning || r.State == RunQueued || r.State == RunStopRequested {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			r := st.Runs[id]
			req, err := runCommitRequest("reconcile-"+strings.TrimPrefix(id, "run-")+fmt.Sprintf("-%d-%d", r.Epoch, r.Sequence), SystemActor(id), map[string]any{"reconcile": id, "seq": r.Sequence})
			if err != nil {
				return touched, err
			}
			_, _, err = s.Commit(pr.Subject, pr.ID, req, func(tx *Tx) error {
				r := tx.Next.Runs[id]
				for i := range r.Stages {
					sg := &r.Stages[i]
					if sg.State != StageRunning {
						continue
					}
					at := sg.LastAttempt()
					at.State = StageDisconnected
					at.FinishedAt = tx.Now.Format(time.RFC3339Nano)
					msg := "the server restarted while this stage was running; its outcome is unknown and it was not resumed automatically"
					if at.Native != nil && at.Native.State != "prepared" {
						ns := ""
						if nativeState != nil {
							ns = nativeState(*at.Native)
						}
						switch ns {
						case "not-sent":
							msg = "the server restarted before the agent request was dispatched; nothing was sent (resume sends a new request)"
						case "completed", "failed":
							msg = "the server restarted after the agent request " + ns + "; its reply is kept on the native receipt and resume adopts it without resending"
						default:
							ns = "disconnected"
							msg = "the server restarted while an agent request was in flight; the provider outcome is unknown and it is never resent automatically"
						}
						at.Native.State = ns
					}
					at.Error = &StageError{Class: "unknown-outcome", Message: msg}
					sg.State = StageDisconnected
					r.event(tx.Now, sg.Name, at.ID, StageDisconnected, msg)
				}
				before := r.State
				switch r.State {
				case RunStopRequested:
					r.State = RunCancelled
					r.event(tx.Now, "", "", RunCancelled, "stop request completed at restart")
				case RunQueued:
					r.State = RunDisconnected
					r.event(tx.Now, "", "", RunDisconnected, "the server restarted before this run was picked up; resume explicitly")
				default:
					r.State = RunDisconnected
				}
				tx.Record("ReconcileRun", "run:"+id, before, r.State)
				tx.Summary("research run reconciled at restart")
				tx.Event("construction.run.reconciled", before+" → "+r.State)
				return nil
			})
			if err != nil {
				errs = append(errs, id+": "+err.Error())
				continue
			}
			touched = append(touched, id)
		}
	}
	if len(errs) > 0 {
		return touched, errors.New("reconcile: " + strings.Join(errs, "; "))
	}
	return touched, nil
}

// NativeSafeToResume: a native step may be resumed (with a new attempt that
// sends nothing twice) only when nothing was sent or its reply is known.
func NativeSafeToResume(state string) bool {
	switch state {
	case "prepared", "not-sent", "completed", "failed":
		return true
	}
	return false
}

// ---- validation and publication tokens -------------------------------------------------------

func init() {
	docValidators[DocRun] = func(st *State, key string, doc any) error {
		r := doc.(*ResearchRun)
		pid := ""
		if st.Problem != nil {
			pid = st.Problem.ID
		}
		if errs := validateRun(r, pid); len(errs) > 0 {
			return Invalid(errs...)
		}
		return nil
	}
	postStampHooks = append(postStampHooks, publicationTokensHook)
}

func validateRun(r *ResearchRun, problemID string) []string {
	out := checkEnvelope(r.Envelope, DocRun, KindRun)
	if r.ProblemID != problemID {
		out = append(out, "run belongs to its problem")
	}
	if !runStates[r.State] {
		out = append(out, "run state is not recognised")
	}
	if r.Epoch < 1 {
		out = append(out, "epoch must be ≥ 1")
	}
	if len(r.Stages) != len(StageNames) {
		out = append(out, "a run has exactly the eight stages")
	}
	running := 0
	ids := map[string]bool{}
	for i, sg := range r.Stages {
		if i < len(StageNames) && sg.Name != StageNames[i] {
			out = append(out, "stages are in the fixed order")
		}
		if !stageStates[sg.State] {
			out = append(out, "stage "+sg.Name+" state is not recognised")
		}
		for _, at := range sg.Attempts {
			if !ValidID(KindOperation, at.ID) || ids[at.ID] {
				out = append(out, "attempt ids are unique op- ids")
			}
			ids[at.ID] = true
			if !stageStates[at.State] {
				out = append(out, "attempt state is not recognised")
			}
			if at.State == StageRunning {
				running++
			}
			if at.Parent != "" && !ids[at.Parent] {
				out = append(out, "an attempt's parent is an earlier attempt of the same stage")
			}
			if at.Error != nil && !errorClasses[at.Error.Class] {
				out = append(out, "attempt error class is not recognised")
			}
			if at.Epoch < 1 || at.Epoch > r.Epoch {
				out = append(out, "attempt epoch out of range")
			}
		}
	}
	if running > 1 {
		out = append(out, "at most one running attempt per run")
	}
	out = append(out, checkAgentChoice(r.Agent)...)
	if !runModes[r.Agent.Mode] || !stewards[r.Agent.Agent] {
		out = append(out, "run agent and mode are required")
	}
	last := 0
	for _, e := range r.Events {
		if e.Seq <= last {
			out = append(out, "event sequence must increase")
			break
		}
		last = e.Seq
	}
	if last > r.Sequence {
		out = append(out, "event sequence beyond the run sequence")
	}
	return out
}

// publicationTokensHook fills the exact assembly revision tokens into a
// run's publication once the publishing commit has stamped them.
func publicationTokensHook(tx *Tx, tokens map[string]string) error {
	for _, r := range tx.Next.Runs {
		if r.Publication == nil {
			continue
		}
		for i := range r.Publication.Assemblies {
			ref := &r.Publication.Assemblies[i]
			if ref.Revision == "" {
				ref.Revision = tokens["assembly:"+ref.ID]
			}
		}
	}
	return nil
}

// AttachConversation records a native conversation on the problem (a
// pointer to the native store, never a copy). Idempotent by session.
func (s *Store) AttachConversation(sub SubjectRef, problemID string, ref ConversationRef, key string) (*State, error) {
	if !stewards[ref.Agent] && ref.Agent != "" {
		return nil, Invalid("conversation agent must be alfred or zeck")
	}
	req, err := runCommitRequest("attach-"+key, SystemActor(""), map[string]any{"conversation": ref.Conversation, "session": ref.Session, "purpose": ref.Purpose})
	if err != nil {
		return nil, err
	}
	st, _, err := s.Commit(sub, problemID, req, func(tx *Tx) error {
		p := tx.Next.Problem
		for _, c := range p.Conversations {
			if c.Session == ref.Session && c.Agent == ref.Agent {
				return nil
			}
		}
		ref.CreatedAt = tx.Now.Format(time.RFC3339Nano)
		p.Conversations = append(p.Conversations, ref)
		tx.Record("AttachConversation", "problem.conversations", nil, ref)
		tx.Summary("native " + ref.Purpose + " conversation linked")
		return nil
	})
	return st, err
}

// RetainPacket retains an exact agent-step context packet as a member of the
// problem (so it can be delivered by hash and exported with the problem).
func (s *Store) RetainPacket(sub SubjectRef, problemID, requestID string, content []byte, actor Actor) (string, error) {
	if len(content) == 0 || len(content) > MaxDocumentBytes {
		return "", Invalid("packet size out of bounds")
	}
	hash := Token(content)
	req := CommitRequest{RequestID: requestID, PayloadHash: hash, Actor: actor}
	_, _, err := s.Commit(sub, problemID, req, func(tx *Tx) error {
		if _, _, err := tx.Retain("construction-context-packet", "steward packet", content); err != nil {
			return err
		}
		tx.Output(hash)
		tx.ViewOnly()
		tx.Record("RetainPacket", "packet/"+hash[:12], nil, map[string]any{"hash": hash, "bytes": len(content)})
		return nil
	})
	return hash, err
}
