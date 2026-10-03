package jev

import (
	"context"
	"errors"
	"strings"

	"manifest/typesafe"
)

// Agent-workflow judgments: the clarification gate (ideas list #15), evidence
// sufficiency, goal/run state, and approval risk. All four are advisory API
// surfaces only — none is wired to suppress or force an agent question,
// accept a run, or pass an approval. Existing gates stay authoritative.

// MaxAgentInput bounds the combined text of one agent-workflow judgment's
// state (well inside Jev's 32k-token state budget).
const MaxAgentInput = 48_000

// stateSize is the byte size of the text fields a judgment sends.
func stateSize(parts ...string) int {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	return n
}

func buildState(pairs ...string) (map[string]any, error) {
	state := map[string]any{}
	var texts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if v := strings.TrimSpace(pairs[i+1]); v != "" {
			state[pairs[i]] = v
			texts = append(texts, v)
		}
	}
	if n := stateSize(texts...); n > MaxAgentInput {
		return nil, ErrTooLarge{Field: "input", Limit: MaxAgentInput}
	}
	return state, nil
}

// ---------------------------------------------------------------- clarify gate

// ClarifyInput: the request an agent received, what it already knows, and
// the default it would take without asking.
type ClarifyInput struct {
	Request         string
	Context         string // flattened by the caller (JSON or prose)
	ProposedDefault string
}

type ClarifyGate struct {
	Meta
	Decision      string             `json:"decision"` // act_now | ask_user | inspect_source_first | stop_due_to_risk
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Risk          ScoreJudgment      `json:"risk"`
	// SourceRetrievable: P(the missing information can be looked up by the agent).
	SourceRetrievable float64 `json:"sourceRetrievable"`
	// Continuation: P(the request continues work already under way in context).
	Continuation   float64 `json:"continuation"`
	Recommendation string  `json:"recommendation"`
	// Cautions are code-side flags on the raw judgment (low confidence, a
	// high-risk act_now); they do not change Decision.
	Cautions []string `json:"cautions"`
}

var clarifyChoice, clarifyOrder = options(
	[2]string{"act_now", "Proceed without asking. A reasonable default exists and acting on it is safe and easy to correct. " +
		"Example: \"tidy up the README\" — fix obvious typos and formatting; example: \"keep going\" right after a plan was agreed."},
	[2]string{"ask_user", "Ask the user first. The ambiguity changes WHAT would be done — which file, which person, which of two incompatible readings — " +
		"and no lookup can settle it because the answer is the user's preference. Example: \"send the update to the team\" when two teams exist."},
	[2]string{"inspect_source_first", "Look it up before asking. The missing detail is retrievable from files, records, history, or tools the agent can read. " +
		"Example: \"fix the failing test\" — run the tests to see which one fails rather than asking which."},
	[2]string{"stop_due_to_risk", "Do not proceed without explicit instruction. Guessing wrong would be costly or irreversible — deleting data, pushing, deploying, " +
		"sending external messages, spending money, touching secrets — and the request does not clearly authorize it. Example: \"clean up the old branches\" with no list."},
)

func clarifyQuestions() map[string]typesafe.Question {
	return map[string]typesafe.Question{
		"gate": typesafe.Choice(map[string]any{
			"question": "An assistant received `request`. Given `context` and the assistant's `proposed_default` (if any), what should it do next?",
			"rules": []string{
				"Prefer act_now when the default is safe and easy to undo: unnecessary questions are a real cost.",
				"Prefer inspect_source_first over ask_user when a lookup could answer the question.",
				"Choose ask_user only when the user's own preference decides between materially different actions.",
				"Choose stop_due_to_risk when a wrong guess would be costly or irreversible and the request does not clearly authorize it.",
			},
		}, clarifyChoice),
		"risk":               typesafe.Score("How much harm could acting on `request` with the most likely default cause if the guess is wrong?", riskLevels...),
		"source_retrievable": typesafe.Noul("Could the information missing from `request` be found by reading files, records, history, or tools, without asking the user?", "", ""),
		"continuation":       typesafe.Noul("Is `request` a continuation of work already under way in `context` (for example \"keep going\" or \"do the next step\")?", "", ""),
	}
}

var clarifyAdvice = map[string]string{
	"act_now":              "Proceed with the default; mention the assumption in the reply.",
	"ask_user":             "Ask one specific question that separates the readings before acting.",
	"inspect_source_first": "Look the missing detail up first; ask only if the lookup comes back empty.",
	"stop_due_to_risk":     "Do not act; state the risk and ask for explicit instruction.",
}

// ClarifyGate judges whether a request should be acted on or clarified.
func (j *Judge) ClarifyGate(ctx context.Context, in ClarifyInput) (*ClarifyGate, error) {
	if strings.TrimSpace(in.Request) == "" {
		return nil, errors.New("request is empty")
	}
	state, err := buildState("request", in.Request, "context", in.Context, "proposed_default", in.ProposedDefault)
	if err != nil {
		return nil, err
	}
	res, err := j.ask(ctx, state, clarifyQuestions())
	if err != nil {
		return nil, err
	}
	c := choiceOf(res, "gate")
	g := &ClarifyGate{
		Meta:              meta("clarify.gate", res),
		Decision:          c.Choice,
		Probabilities:     c.Probabilities,
		Confidence:        c.Confidence,
		Risk:              scoreOf(res, "risk"),
		SourceRetrievable: noulOf(res, "source_retrievable"),
		Continuation:      noulOf(res, "continuation"),
		Recommendation:    clarifyAdvice[c.Choice],
		Cautions:          []string{},
	}
	if g.Confidence < 0.5 {
		g.Cautions = append(g.Cautions, "low confidence "+pct(g.Confidence))
	}
	if g.Decision == "act_now" && g.Risk.Score >= 3 {
		g.Cautions = append(g.Cautions, "act_now with risk "+pct(g.Risk.Score)+" ≥ 3")
	}
	if g.Decision == "ask_user" && g.SourceRetrievable >= 0.7 {
		g.Cautions = append(g.Cautions, "ask_user although the source looks retrievable ("+pct(g.SourceRetrievable)+")")
	}
	return g, nil
}

// ------------------------------------------------------------- evidence check

type EvidenceInput struct {
	Claim, Evidence, Context string
}

type EvidenceCheck struct {
	Meta
	Verdict             string             `json:"verdict"` // sufficient | partial | missing | contradicted | not_applicable
	Probabilities       map[string]float64 `json:"probabilities"`
	Confidence          float64            `json:"confidence"`
	Overclaims          float64            `json:"overclaims"`          // P(the claim says more than the evidence shows)
	MissingVerification float64            `json:"missingVerification"` // P(a check the claim needs was not run)
}

var evidenceChoice, evidenceOrder = options(
	[2]string{"sufficient", "`evidence` directly shows everything `claim` asserts (for example the test output that shows the named tests passing)."},
	[2]string{"partial", "`evidence` supports some of `claim` but not all of it — some parts are shown, others are only asserted."},
	[2]string{"missing", "There is no evidence for `claim`, or the evidence is about something else."},
	[2]string{"contradicted", "`evidence` shows something that conflicts with `claim` (a failure, an error, a different result)."},
	[2]string{"not_applicable", "`claim` is not a checkable statement of fact or completion (a question, a plan, an opinion)."},
)

func evidenceQuestions() map[string]typesafe.Question {
	return map[string]typesafe.Question{
		"verdict": typesafe.Choice("How well does `evidence` support `claim`? Judge only from `evidence` and `context`, not from how confident `claim` sounds.", evidenceChoice),
		"overclaims": typesafe.Noul("Does `claim` assert more than `evidence` shows — for example \"all tests pass\" when only some were run, or \"deployed\" when only built?",
			"The claim goes beyond the evidence", "The claim stays within the evidence"),
		"missing_verification": typesafe.Noul("Does `claim` depend on a verification step (running tests, checking the live result, reading the output) that `evidence` does not show was done?",
			"A needed check is not shown", "Every needed check is shown"),
	}
}

func (j *Judge) EvidenceCheck(ctx context.Context, in EvidenceInput) (*EvidenceCheck, error) {
	if strings.TrimSpace(in.Claim) == "" {
		return nil, errors.New("claim is empty")
	}
	state, err := buildState("claim", in.Claim, "evidence", in.Evidence, "context", in.Context)
	if err != nil {
		return nil, err
	}
	if _, ok := state["evidence"]; !ok {
		state["evidence"] = "(none provided)"
	}
	res, err := j.ask(ctx, state, evidenceQuestions())
	if err != nil {
		return nil, err
	}
	c := choiceOf(res, "verdict")
	return &EvidenceCheck{
		Meta:                meta("evidence.check", res),
		Verdict:             c.Choice,
		Probabilities:       c.Probabilities,
		Confidence:          c.Confidence,
		Overclaims:          noulOf(res, "overclaims"),
		MissingVerification: noulOf(res, "missing_verification"),
	}, nil
}

// ----------------------------------------------------------------- goal state

type GoalInput struct {
	Goal, Status, Context string
}

type GoalState struct {
	Meta
	State         string             `json:"state"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

var goalChoice, goalOrder = options(
	[2]string{"not_started", "No work on `goal` has begun yet."},
	[2]string{"in_progress", "Work is actively under way and nothing is blocking it."},
	[2]string{"waiting_on_process", "Waiting for a running process to finish — a build, a test run, a deploy, a long job."},
	[2]string{"waiting_on_user", "Waiting for the user to answer, approve, or provide something."},
	[2]string{"blocked_by_error", "Stopped by an error or failure that has to be fixed before work can continue."},
	[2]string{"ready_to_verify", "The change is made but has not yet been tested or checked."},
	[2]string{"ready_to_push", "The change is made and verified; it remains to commit, push, or ship it."},
	[2]string{"complete", "The goal is done, verified, and shipped or delivered; nothing remains."},
	[2]string{"stale_or_superseded", "The goal was abandoned, replaced by other work, or has not moved in a long time."},
)

func goalQuestions() map[string]typesafe.Question {
	return map[string]typesafe.Question{
		"state": typesafe.Choice("Given the latest `status` (and `context`), which state is the work on `goal` in right now? Judge from what `status` shows happened, not from what it hopes.", goalChoice),
	}
}

func (j *Judge) GoalState(ctx context.Context, in GoalInput) (*GoalState, error) {
	if strings.TrimSpace(in.Status) == "" {
		return nil, errors.New("status is empty")
	}
	state, err := buildState("goal", in.Goal, "status", in.Status, "context", in.Context)
	if err != nil {
		return nil, err
	}
	if _, ok := state["goal"]; !ok {
		state["goal"] = "(the work described in `status`)"
	}
	res, err := j.ask(ctx, state, goalQuestions())
	if err != nil {
		return nil, err
	}
	c := choiceOf(res, "state")
	return &GoalState{Meta: meta("goal.state", res), State: c.Choice, Probabilities: c.Probabilities, Confidence: c.Confidence}, nil
}

// --------------------------------------------------------------- approval risk

type ApprovalInput struct {
	Action, Context string
}

type ApprovalRisk struct {
	Meta
	Category      string             `json:"category"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Risk          ScoreJudgment      `json:"risk"`
	Irreversible  float64            `json:"irreversible"` // P(the action cannot be undone)
}

var approvalChoice, approvalOrder = options(
	[2]string{"read_only", "Only reads, searches, lists, or inspects; changes nothing."},
	[2]string{"local_file_write", "Creates or edits files on this machine without committing them."},
	[2]string{"git_commit", "Records a git commit (or amends one) locally, without pushing."},
	[2]string{"push_to_remote", "Pushes commits or branches to a remote repository."},
	[2]string{"deploy_restart", "Deploys, builds-and-ships, or restarts a live service."},
	[2]string{"send_external_message", "Sends an email, chat message, comment, or other message that a person outside this session will read."},
	[2]string{"touch_secret", "Reads, writes, prints, moves, or shares an API key, token, password, or credential file."},
	[2]string{"destructive_action", "Deletes, overwrites, resets, or force-pushes data that may not be recoverable."},
	[2]string{"standing_permission_rule", "Creates or changes a standing rule that allows future actions without asking (\"always allow …\")."},
)

func approvalQuestions() map[string]typesafe.Question {
	return map[string]typesafe.Question{
		"category": typesafe.Choice("Which kind of action is `action`? If it does several things, choose the most consequential one.", approvalChoice),
		"risk":     typesafe.Score("How much harm could `action` cause if it was a mistake?", riskLevels...),
		"irreversible": typesafe.Noul("Once `action` is done, would it be impossible or very costly to undo?",
			"Cannot practically be undone", "Can be undone"),
	}
}

func (j *Judge) ApprovalRisk(ctx context.Context, in ApprovalInput) (*ApprovalRisk, error) {
	if strings.TrimSpace(in.Action) == "" {
		return nil, errors.New("action is empty")
	}
	state, err := buildState("action", in.Action, "context", in.Context)
	if err != nil {
		return nil, err
	}
	res, err := j.ask(ctx, state, approvalQuestions())
	if err != nil {
		return nil, err
	}
	c := choiceOf(res, "category")
	return &ApprovalRisk{
		Meta:          meta("approval.risk", res),
		Category:      c.Choice,
		Probabilities: c.Probabilities,
		Confidence:    c.Confidence,
		Risk:          scoreOf(res, "risk"),
		Irreversible:  noulOf(res, "irreversible"),
	}, nil
}

// Option lists in rubric order (for UIs and tests).
func ClarifyOptions() []string  { return append([]string(nil), clarifyOrder...) }
func EvidenceOptions() []string { return append([]string(nil), evidenceOrder...) }
func GoalOptions() []string     { return append([]string(nil), goalOrder...) }
func ApprovalOptions() []string { return append([]string(nil), approvalOrder...) }
