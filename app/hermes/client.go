// Package hermes is a thin client for a self-hosted Hermes Agent server (Nous
// Research), reached over Tailscale via its OpenAI-compatible API. The dashboard's
// Go backend is the ONLY holder of the API key: the browser talks to same-origin
// /api/hermes/* handlers, which proxy here. Pinned to Hermes v0.17.0 — response
// schemas are treated as a versioned contract (unknown fields ignored, list
// endpoints unwrapped from {object,data}); anything shape-sensitive fails gracefully.
package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned when no base URL / API key is available. Like the
// calendar client, an unconfigured Hermes leaves the rest of the app fully usable.
var ErrNotConfigured = errors.New("hermes is not configured (set base URL + API key)")

// Config is the resolved connection info. APIKey is loaded server-side only (env
// or a 0600 file) and never serialized to the browser.
type Config struct {
	BaseURL string
	Model   string
	APIKey  string
}

// Configured reports whether we have enough to talk to Hermes.
func (c Config) Configured() bool { return c.BaseURL != "" && c.APIKey != "" }

// Client talks to the Hermes API server. Safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient builds a client from resolved config. Non-streaming calls get a
// bounded timeout; StreamChat opens its own request bound to the caller's context
// (a stream can outlive any fixed timeout).
func NewClient(cfg Config) *Client {
	if cfg.Model == "" {
		cfg.Model = "hermes-agent"
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// Configured exposes whether the client can reach Hermes.
func (c *Client) Configured() bool { return c.cfg.Configured() }

// Model is the default model id used for chat.
func (c *Client) Model() string { return c.cfg.Model }

// BaseURL is the Hermes endpoint (safe to surface — no secret).
func (c *Client) BaseURL() string { return c.cfg.BaseURL }

// List is the Hermes list-envelope: endpoints return {"object":"list","data":[…]},
// not a bare array (the documented response-shape footgun).
type List[T any] struct {
	Object string `json:"object"`
	Data   []T    `json:"data"`
}

// Skill is a tolerant view of a /v1/skills entry — only the fields we render;
// everything else in the (versioned) payload is ignored on purpose.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Message is one OpenAI-style chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest carries the conversation plus optional per-call overrides applied
// server-side from a profile. The browser only ever supplies Messages; System and
// Model are set by the backend (from the resolved profile), never by the client.
type ChatRequest struct {
	Messages []Message `json:"messages"`
	System   string    `json:"-"` // profile brief → prepended as a system message
	Model    string    `json:"-"` // profile model tier → overrides the default
}

// messages returns the wire messages, prepending the profile's system brief if set.
func (in ChatRequest) messages() []Message {
	if strings.TrimSpace(in.System) == "" {
		return in.Messages
	}
	return append([]Message{{Role: "system", Content: in.System}}, in.Messages...)
}

// model resolves the effective model: the per-call override, else the client default.
func (c *Client) model(in ChatRequest) string {
	if strings.TrimSpace(in.Model) != "" {
		return in.Model
	}
	return c.cfg.Model
}

// Health pings the server's /health (200 == up). The key isn't strictly required
// but we send it so a misconfigured gateway still authenticates.
func (c *Client) Health(ctx context.Context) error {
	if c.cfg.BaseURL == "" {
		return ErrNotConfigured
	}
	req, err := c.newRequest(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hermes health: unexpected status %s", resp.Status)
	}
	return nil
}

// ListSkills fetches /v1/skills and unwraps the {object,data} envelope.
func (c *Client) ListSkills(ctx context.Context) ([]Skill, error) {
	if !c.cfg.Configured() {
		return nil, ErrNotConfigured
	}
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/skills", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, statusError("list skills", resp)
	}
	var out List[Skill]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode skills: %w", err)
	}
	return out.Data, nil
}

// Toolset is a /v1/toolsets entry — a named group of tools the profile picker offers.
type Toolset struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Configured  bool     `json:"configured"`
	Tools       []string `json:"tools"`
}

// ListToolsets fetches /v1/toolsets (the richer, grouped tool source for profiles).
func (c *Client) ListToolsets(ctx context.Context) ([]Toolset, error) {
	if !c.cfg.Configured() {
		return nil, ErrNotConfigured
	}
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/toolsets", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return nil, statusError("list toolsets", resp)
	}
	var out List[Toolset]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode toolsets: %w", err)
	}
	return out.Data, nil
}

// RunOnce performs a NON-streaming chat completion and returns the assistant's full
// text. Used by the feed/approvals materializer (which needs the complete structured
// output, not a stream). The caller's context bounds the run (agent runs can be slow).
func (c *Client) RunOnce(ctx context.Context, in ChatRequest) (string, error) {
	if !c.cfg.Configured() {
		return "", ErrNotConfigured
	}
	body, err := json.Marshal(map[string]any{
		"model":    c.model(in),
		"stream":   false,
		"messages": in.messages(),
	})
	if err != nil {
		return "", err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// No fixed client timeout — an agent run (web search + reasoning) can take minutes;
	// the context is the cancellation authority.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", err
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return "", statusError("run", resp)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode run: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", nil
	}
	return out.Choices[0].Message.Content, nil
}

// StreamChat POSTs to /v1/chat/completions with stream:true and copies the raw
// SSE bytes to w (flushing per chunk via flush). We deliberately do NOT parse the
// SSE here — passing bytes straight through keeps the proxy dumb and resilient to
// schema drift; the browser reconstructs text per the documented contract.
func (c *Client) StreamChat(ctx context.Context, in ChatRequest, w io.Writer, flush func()) error {
	if !c.cfg.Configured() {
		return ErrNotConfigured
	}
	// Force stream server-side; model + system come from the resolved profile.
	body := map[string]any{
		"model":    c.model(in),
		"stream":   true,
		"messages": in.messages(),
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	// A stream can run long; don't let the client's fixed timeout kill it. The
	// caller's context is the cancellation authority.
	streamer := &http.Client{}
	resp, err := streamer.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError("chat", resp)
	}
	// Stream chunks through as they arrive.
	sc := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(sc)
		if n > 0 {
			if _, werr := w.Write(sc[:n]); werr != nil {
				return werr
			}
			if flush != nil {
				flush()
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			// Context cancellation (client closed the tab) is a clean stop.
			if ctx.Err() != nil {
				return nil
			}
			return rerr
		}
	}
}

// newRequest builds a request to path with the bearer key attached.
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	return req, nil
}

// statusError reads a bounded slice of the error body for a useful message.
func statusError(op string, resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := bytes.TrimSpace(b)
	if len(msg) == 0 {
		return fmt.Errorf("hermes %s: %s", op, resp.Status)
	}
	return fmt.Errorf("hermes %s: %s: %s", op, resp.Status, msg)
}

// drain closes a response body after discarding any remainder (connection reuse).
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
