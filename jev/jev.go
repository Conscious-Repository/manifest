// Package jev holds manifest's advisory judgments built on TypeSafe's Jev
// (package typesafe). Each judgment asks Jev typed questions over a bounded
// state, keeps the raw probabilities and confidence, and applies any policy
// in plain Go where it can be read and tested.
//
// Nothing here acts. Every result is advisory: it is shown to the owner or an
// agent as an opinion with its numbers attached, and every authoritative
// decision (a transcript's tier, an approval, a run's completion) stays with
// the mechanism that already owns it.
package jev

import (
	"context"
	"errors"
	"fmt"
	"math"

	"manifest/typesafe"
)

// Judge runs the advisory judgments against one Evaluator.
type Judge struct {
	Eval  typesafe.Evaluator
	Model string // "" → typesafe.DefaultModel
}

// ErrNoEvaluator: the Judge was built without an Evaluator (treated like a
// missing key by callers).
var ErrNoEvaluator = errors.New("jev: no evaluator configured")

func (j *Judge) ask(ctx context.Context, state any, qs map[string]typesafe.Question) (*typesafe.Response, error) {
	if j == nil || j.Eval == nil {
		return nil, ErrNoEvaluator
	}
	model := j.Model
	if model == "" {
		model = typesafe.DefaultModel
	}
	res, err := j.Eval.Evaluate(ctx, typesafe.Request{State: state, Model: model, Questions: qs})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Meta is carried by every advisory result.
type Meta struct {
	Advisory bool           `json:"advisory"` // always true: nothing here is a decision
	Kind     string         `json:"kind"`
	Model    string         `json:"model"`
	Usage    typesafe.Usage `json:"usage"`
}

func meta(kind string, res *typesafe.Response) Meta {
	return Meta{Advisory: true, Kind: kind, Model: res.Model, Usage: res.Usage}
}

// ChoiceJudgment is one raw Choice answer.
type ChoiceJudgment struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// ScoreJudgment is one raw Score answer plus the nearest level, for display.
type ScoreJudgment struct {
	Score         float64            `json:"score"`
	Level         int                `json:"level"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

func choiceOf(res *typesafe.Response, id string) ChoiceJudgment {
	a := res.Answers[id]
	return ChoiceJudgment{Choice: a.Choice, Probabilities: a.Probabilities, Confidence: a.Confidence}
}

func noulOf(res *typesafe.Response, id string) float64 {
	if a := res.Answers[id]; a.Noul != nil {
		return *a.Noul
	}
	return 0
}

func scoreOf(res *typesafe.Response, id string) ScoreJudgment {
	a := res.Answers[id]
	s := 0.0
	if a.Score != nil {
		s = *a.Score
	}
	return ScoreJudgment{Score: s, Level: int(math.Round(s)), Probabilities: a.Probabilities, Confidence: a.Confidence, Legend: a.Legend}
}

// options turns ordered (key, rubric) pairs into a Choice criteria map and the
// key order (kept for validation and documentation; the wire map is unordered).
func options(pairs ...[2]string) (map[string]any, []string) {
	m := make(map[string]any, len(pairs))
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		m[p[0]] = p[1]
		keys = append(keys, p[0])
	}
	return m, keys
}

// pct renders a probability for policy reasons ("0.82").
func pct(p float64) string { return fmt.Sprintf("%.2f", p) }

// riskLevels is the shared 0–4 risk rubric (clarify gate, approval risk).
var riskLevels = []any{
	"0 — none: reading, looking things up, or answering; nothing changes anywhere",
	"1 — low: a small local change that is easy to undo (edit a draft, a scratch file, a local note)",
	"2 — moderate: a change others may see or that takes effort to undo (a commit, a shared record, a task reassignment)",
	"3 — high: leaves the machine or reaches people (push, deploy, restart a live service, send a message or email, spend money)",
	"4 — severe: irreversible or damaging if wrong (deleting data, rewriting history, exposing a secret, legal or financial commitment)",
}
