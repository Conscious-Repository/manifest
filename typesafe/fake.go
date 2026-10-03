package typesafe

import (
	"context"
	"sync"
)

// Fake is an in-memory Evaluator for tests: it records every request and
// answers from a canned map, validated against the request exactly as the
// HTTP client validates a live response. It never touches the network.
type Fake struct {
	Answers map[string]Answer
	Model   string // "" → "jev-fake"
	Err     error

	mu       sync.Mutex
	Requests []Request
}

func (f *Fake) Evaluate(_ context.Context, req Request) (*Response, error) {
	f.mu.Lock()
	f.Requests = append(f.Requests, req)
	f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	model := f.Model
	if model == "" {
		model = "jev-fake"
	}
	res := &Response{Model: model, Answers: map[string]Answer{}}
	for id := range req.Questions {
		if a, ok := f.Answers[id]; ok {
			res.Answers[id] = a
		}
	}
	if err := Validate(req, res); err != nil {
		return nil, err
	}
	return res, nil
}

// Last is the most recent request (zero Request when none).
func (f *Fake) Last() Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Requests) == 0 {
		return Request{}
	}
	return f.Requests[len(f.Requests)-1]
}

// Helpers for building canned answers.

func ChoiceAnswer(choice string, conf float64, probs map[string]float64) Answer {
	return Answer{Type: "choice", Choice: choice, Confidence: conf, Probabilities: probs}
}

func NoulAnswer(p float64) Answer { return Answer{Type: "noul", Noul: &p} }

func ScoreAnswer(score, conf float64, probs map[string]float64, legend map[string]string) Answer {
	return Answer{Type: "score", Score: &score, Confidence: conf, Probabilities: probs, Legend: legend}
}
