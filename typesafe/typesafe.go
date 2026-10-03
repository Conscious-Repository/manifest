// Package typesafe is manifest's client for TypeSafe's System One endpoint
// (Jev). Jev returns typed judgments — Choice, Noul, Score — with their
// probabilities; it never takes an action. Callers own the control flow and
// decide what a judgment is allowed to change (in manifest: nothing, yet —
// every consumer is advisory).
//
// The API key is read per request through a KeyFunc, never stored on the
// client, never logged, never echoed in an error. An empty key is
// ErrDisabled: the caller degrades explicitly, it does not guess.
//
// Wire contract (docs.typesafe.ai/api, read 2026-10-03):
//
//	POST {base}/v1/systemone  Authorization: Bearer <key>
//	{state, model, questions:{id:{type, instructions, criteria}}}
//	→ {model, answers:{id:{type, choice|noul|score, probabilities, confidence, legend}}, usage}
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
	// DefaultTimeout bounds one evaluation including retries. Jev answers in
	// well under a second normally; a stuck call must not hold a handler.
	DefaultTimeout = 30 * time.Second

	EnvKey   = "TYPESAFE_API_KEY"
	EnvURL   = "TYPESAFE_API_URL"
	EnvModel = "TYPESAFE_MODEL"
)

// ErrDisabled: no API key is configured. Every advisory surface reports this
// as "disabled", never as a judgment.
var ErrDisabled = errors.New(EnvKey + " is not set")

// Question is one typed question. Instructions and Criteria take any JSON
// shape the API accepts (string, object, array).
type Question struct {
	Type         string `json:"type"` // choice | noul | score
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Choice builds a Choice question; options maps option → rubric. Option order
// in the wire map is the JSON encoder's (sorted), which is fine: the docs
// recommend checking order sensitivity, not relying on it.
func Choice(instructions any, options map[string]any) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: options}
}

// Noul builds a yes/no question; yes/no describe the two ends (either may be "").
func Noul(instructions any, yes, no string) Question {
	q := Question{Type: "noul", Instructions: instructions}
	if yes != "" || no != "" {
		c := map[string]string{}
		if yes != "" {
			c["true"] = yes
		}
		if no != "" {
			c["false"] = no
		}
		q.Criteria = c
	}
	return q
}

// Score builds a Score question over ordered levels (2–10).
func Score(instructions any, levels ...any) Question {
	return Question{Type: "score", Instructions: instructions, Criteria: levels}
}

// Request is one evaluation: one state, many questions answered in parallel.
type Request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Answer is one typed answer. Noul is a pointer so a missing value is
// distinguishable from a real 0.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Evaluator is the seam every consumer depends on; tests substitute a fake
// so no test ever reaches the network.
type Evaluator interface {
	Evaluate(ctx context.Context, req Request) (*Response, error)
}

// KeyFunc supplies the API key at call time ("" = not configured).
type KeyFunc func() string

// EnvKeyFunc reads TYPESAFE_API_KEY on each call.
func EnvKeyFunc() string { return strings.TrimSpace(os.Getenv(EnvKey)) }

// Client is the HTTP Evaluator.
type Client struct {
	Key     KeyFunc
	BaseURL string // "" → TYPESAFE_API_URL or DefaultBaseURL
	Model   string // default model when a Request leaves Model empty
	HTTP    *http.Client
	// Backoff before the retries of a 429/529 (one entry per retry). nil →
	// 500ms, 1500ms. Tests set it to zero durations.
	Backoff []time.Duration
}

// NewFromEnv builds a Client configured from the environment: key, optional
// base URL, optional model. The key is still read per call.
func NewFromEnv() *Client {
	return &Client{
		Key:     EnvKeyFunc,
		BaseURL: strings.TrimSpace(os.Getenv(EnvURL)),
		Model:   strings.TrimSpace(os.Getenv(EnvModel)),
	}
}

// ModelName is the model a request with no explicit model will use.
func (c *Client) ModelName() string {
	if c.Model != "" {
		return c.Model
	}
	return DefaultModel
}

// Enabled reports whether a key is configured right now.
func (c *Client) Enabled() bool { return c.Key != nil && strings.TrimSpace(c.Key()) != "" }

// APIError is a non-200 answer. Body is a truncated excerpt of the service's
// own error text; the request (and so the key) is never part of it.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("typesafe: HTTP %d", e.Status)
	}
	return fmt.Sprintf("typesafe: HTTP %d: %s", e.Status, e.Body)
}

func (c *Client) Evaluate(ctx context.Context, req Request) (*Response, error) {
	key := ""
	if c.Key != nil {
		key = strings.TrimSpace(c.Key())
	}
	if key == "" {
		return nil, ErrDisabled
	}
	if len(req.Questions) == 0 {
		return nil, errors.New("typesafe: no questions")
	}
	if req.Model == "" {
		req.Model = c.ModelName()
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("typesafe: encode request: %w", err)
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	url := strings.TrimRight(base, "/") + "/v1/systemone"
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: DefaultTimeout}
	}
	backoff := c.Backoff
	if backoff == nil {
		backoff = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}
	}
	for attempt := 0; ; attempt++ {
		res, err := c.do(ctx, hc, url, key, body)
		var ae *APIError
		if err == nil || !errors.As(err, &ae) || (ae.Status != http.StatusTooManyRequests && ae.Status != 529) || attempt >= len(backoff) {
			if err != nil {
				return nil, err
			}
			if err := res.check(req); err != nil {
				return nil, err
			}
			return res, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff[attempt]):
		}
	}
}

func (c *Client) do(ctx context.Context, hc *http.Client, url, key string, body []byte) (*Response, error) {
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("typesafe: %w", err)
	}
	hr.Header.Set("Authorization", "Bearer "+key)
	hr.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(hr)
	if err != nil {
		// net/http errors name the URL, never headers — safe to surface
		return nil, fmt.Errorf("typesafe: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("typesafe: read response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return nil, &APIError{Status: res.StatusCode, Body: msg}
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("typesafe: decode response: %w", err)
	}
	return &out, nil
}

// check refuses a response that does not answer every question with the
// asked type — a typed interface is only useful if it is actually typed.
func (r *Response) check(req Request) error {
	for id, q := range req.Questions {
		a, ok := r.Answers[id]
		if !ok {
			return fmt.Errorf("typesafe: no answer for %q", id)
		}
		if a.Type != q.Type {
			return fmt.Errorf("typesafe: answer %q is %q, asked %q", id, a.Type, q.Type)
		}
		switch q.Type {
		case "choice":
			opts, _ := q.Criteria.(map[string]any)
			if _, ok := opts[a.Choice]; !ok {
				return fmt.Errorf("typesafe: answer %q chose %q, not an asked option", id, a.Choice)
			}
		case "noul":
			if a.Noul == nil {
				return fmt.Errorf("typesafe: answer %q has no noul", id)
			}
		case "score":
			if a.Score == nil {
				return fmt.Errorf("typesafe: answer %q has no score", id)
			}
		}
	}
	return nil
}

// Validate checks a response against the request it answers (exported for
// fakes and other Evaluators that want the same guarantee).
func Validate(req Request, res *Response) error {
	if res == nil {
		return errors.New("typesafe: empty response")
	}
	return res.check(req)
}
