package construction

import (
	"fmt"
	"strings"
)

// DecisionPoint is one question the owner must settle to narrow in on an
// approach: "is the wall solid or a cavity?", "galvanized or aluminum?".
// Agents and the owner raise them; the owner answers or drops them. Every
// change is a problem revision like any other, so a wrong answer is undone by
// restoring or re-answering, never by rewriting history.
type DecisionPoint struct {
	ID         string   `json:"id"` // dq-
	Text       string   `json:"text"`
	Why        string   `json:"why,omitempty"`     // what it changes
	Options    []string `json:"options,omitempty"` // suggested answers
	Stage      string   `json:"stage"`             // approach | specifics
	State      string   `json:"state"`             // open | answered | dropped
	Answer     string   `json:"answer,omitempty"`
	Note       string   `json:"note,omitempty"`
	Provenance string   `json:"provenance,omitempty"` // of the answer
	RaisedBy   string   `json:"raisedBy"`             // owner | alfred | zeck
}

const MaxQuestions = 60

var questionStages = map[string]bool{"approach": true, "specifics": true}
var questionStates = map[string]bool{"open": true, "answered": true, "dropped": true}

func checkQuestion(f string, q DecisionPoint) []string {
	var out []string
	if !ValidID(KindQuestion, q.ID) {
		out = append(out, f+": needs a dq- id")
	}
	out = append(out, checkText(f+".text", q.Text, 500, true)...)
	out = append(out, checkText(f+".why", q.Why, 1000, false)...)
	if len(q.Options) > 8 {
		out = append(out, f+": at most 8 options")
	}
	for i, o := range q.Options {
		out = append(out, checkText(fmt.Sprintf("%s.options[%d]", f, i), o, 200, true)...)
	}
	if !questionStages[q.Stage] {
		out = append(out, f+".stage must be approach or specifics")
	}
	if !questionStates[q.State] {
		out = append(out, f+".state must be open, answered or dropped")
	}
	out = append(out, checkText(f+".answer", q.Answer, 1000, q.State == "answered")...)
	out = append(out, checkText(f+".note", q.Note, 1000, false)...)
	if q.Provenance != "" && !provenances[q.Provenance] {
		out = append(out, f+".provenance is not in the taxonomy")
	}
	if q.RaisedBy != "owner" && !stewards[q.RaisedBy] {
		out = append(out, f+".raisedBy must be owner, alfred or zeck")
	}
	return out
}

func checkQuestions(qs []DecisionPoint) []string {
	var out []string
	if len(qs) > MaxQuestions {
		out = append(out, fmt.Sprintf("at most %d questions", MaxQuestions))
	}
	seen := map[string]bool{}
	for i, q := range qs {
		f := fmt.Sprintf("questions[%d]", i)
		if seen[q.ID] {
			out = append(out, f+": duplicate id")
		}
		seen[q.ID] = true
		out = append(out, checkQuestion(f, q)...)
	}
	return out
}

func init() {
	registerOp("AddQuestion", TargetProblem, false, func() Operation { return &AddQuestion{} })
	registerOp("AnswerQuestion", TargetProblem, true, func() Operation { return &AnswerQuestion{} })
	registerOp("SetQuestionState", TargetProblem, true, func() Operation { return &SetQuestionState{} })
}

// AddQuestion raises a decision point (owner or steward).
type AddQuestion struct {
	Op      string   `json:"op"`
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Why     string   `json:"why,omitempty"`
	Options []string `json:"options,omitempty"`
	Stage   string   `json:"stage,omitempty"` // default approach
}

func (o *AddQuestion) draft(a *Actor) DecisionPoint {
	by := "owner"
	if a != nil && a.Kind == "agent" && stewards[a.Agent] {
		by = a.Agent
	}
	opts := make([]string, 0, len(o.Options))
	for _, x := range o.Options {
		opts = append(opts, strings.TrimSpace(x))
	}
	return DecisionPoint{ID: o.ID, Text: strings.TrimSpace(o.Text), Why: strings.TrimSpace(o.Why), Options: opts,
		Stage: orDefault(o.Stage, "approach"), State: "open", RaisedBy: by}
}
func (o *AddQuestion) Name() string { return "AddQuestion" }
func (o *AddQuestion) Check() []string {
	return checkQuestion("question", o.draft(nil))
}
func (o *AddQuestion) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	for _, q := range p.Questions {
		if q.ID == o.ID {
			return Invalid("question id already exists")
		}
	}
	if len(p.Questions) >= MaxQuestions {
		return Invalid(fmt.Sprintf("at most %d questions", MaxQuestions))
	}
	q := o.draft(&tx.Actor)
	p.Questions = append(p.Questions, q)
	tx.Record(o.Name(), "problem.questions/"+q.ID, nil, q)
	tx.Summary("question raised: " + q.Text)
	return nil
}

// AnswerQuestion records the owner's answer (and may re-answer one).
type AnswerQuestion struct {
	Op         string `json:"op"`
	ID         string `json:"id"`
	Answer     string `json:"answer"`
	Note       string `json:"note,omitempty"`
	Provenance string `json:"provenance,omitempty"` // default user-assumption
}

func (o *AnswerQuestion) Name() string { return "AnswerQuestion" }
func (o *AnswerQuestion) Check() []string {
	out := checkText("answer", o.Answer, 1000, true)
	out = append(out, checkText("note", o.Note, 1000, false)...)
	if !ValidID(KindQuestion, o.ID) {
		out = append(out, "id must be a dq- id")
	}
	if o.Provenance != "" && !provenances[o.Provenance] {
		out = append(out, "provenance is not in the taxonomy")
	}
	return out
}
func (o *AnswerQuestion) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	for i := range p.Questions {
		q := &p.Questions[i]
		if q.ID != o.ID {
			continue
		}
		before := *q
		q.State, q.Answer, q.Note, q.Provenance = "answered", strings.TrimSpace(o.Answer), strings.TrimSpace(o.Note), orDefault(o.Provenance, ProvUserAssumption)
		tx.Record(o.Name(), "problem.questions/"+q.ID, before, *q)
		tx.Summary("answered: " + q.Text + " → " + q.Answer)
		return nil
	}
	return NotFound("no such question")
}

// SetQuestionState reopens (clearing the answer) or drops a question.
type SetQuestionState struct {
	Op    string `json:"op"`
	ID    string `json:"id"`
	State string `json:"state"` // open | dropped
}

func (o *SetQuestionState) Name() string { return "SetQuestionState" }
func (o *SetQuestionState) Check() []string {
	var out []string
	if !ValidID(KindQuestion, o.ID) {
		out = append(out, "id must be a dq- id")
	}
	if o.State != "open" && o.State != "dropped" {
		out = append(out, "state must be open or dropped")
	}
	return out
}
func (o *SetQuestionState) Apply(tx *Tx, c *ApplyContext) error {
	p := tx.Next.Problem
	for i := range p.Questions {
		q := &p.Questions[i]
		if q.ID != o.ID {
			continue
		}
		before := *q
		q.State = o.State
		if o.State == "open" {
			q.Answer, q.Note, q.Provenance = "", "", ""
		}
		tx.Record(o.Name(), "problem.questions/"+q.ID, before, *q)
		return nil
	}
	return NotFound("no such question")
}
