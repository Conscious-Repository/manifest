package domainextract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	"manifest/hermes"
	"manifest/secrets"
)

// SparksCapability preserves the explicitly supplied discovery bytes by hash.
// Discovery is not tokenizer or chat-template verification.
type SparksCapability struct {
	Provider     string `json:"provider"`
	Endpoint     string `json:"endpoint"`
	Model        string `json:"model"`
	ModelsSHA256 string `json:"modelsSha256"`
	MaxModelLen  int    `json:"maxModelLen"`
}

func LoadSparksCapability(c AionSuccessorConfig, raw []byte) (SparksCapability, error) {
	var cap SparksCapability
	if e := c.validate(); e != nil {
		return cap, e
	}
	var models struct {
		Data []struct {
			ID          string `json:"id"`
			OwnedBy     string `json:"owned_by"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	var checked json.RawMessage
	if len(raw) > 1<<20 || strict(raw, &checked) != nil || json.Unmarshal(raw, &models) != nil {
		return cap, errors.New("invalid-model-discovery")
	}
	matches := 0
	for _, m := range models.Data {
		if m.ID == c.Model {
			matches++
			cap = SparksCapability{c.Provider, c.Endpoint, c.Model, byteHash(raw), m.MaxModelLen}
			if m.OwnedBy != "vllm" {
				return cap, errors.New("model-owner-mismatch")
			}
		}
	}
	if matches != 1 || cap.MaxModelLen < 1 || cap.MaxModelLen > 16<<20 {
		return SparksCapability{}, errors.New("model-capacity-unavailable")
	}
	return cap, nil
}

// SparksAccounting is externally reviewed bounded-probe evidence, not an estimate.
// The independently supplied digest pins the entire attestation. Both token
// counts must be measured using the identified deployed tokenizer/template.
type SparksAccounting struct {
	RequestSHA256        string `json:"requestSha256"`
	CapabilitySHA256     string `json:"capabilitySha256"`
	TokenizerSHA256      string `json:"tokenizerSha256"`
	TemplateSHA256       string `json:"templateSha256"`
	ProbeSHA256          string `json:"probeSha256"`
	SerializedTokens     int    `json:"serializedTokens"`
	PromptTokens         int    `json:"promptTokens"`
	TokenMargin          int    `json:"tokenMargin"`
	ByteMargin           int    `json:"byteMargin"`
	VerifiedRequestBytes int    `json:"verifiedRequestBytes"`
	ReservedOutput       int    `json:"reservedOutput"`
}

func DecodeSparksAccounting(raw []byte, pin string) (SparksAccounting, error) {
	var a SparksAccounting
	if len(raw) > 8192 || !evidenceHash.MatchString(pin) || byteHash(raw) != pin || strict(raw, &a) != nil {
		return a, errors.New("accounting-evidence-unverified")
	}
	return a, nil
}

type sparksMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type sparksRequest struct {
	Model       string          `json:"model"`
	Messages    []sparksMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Stream      bool            `json:"stream"`
	Temperature int             `json:"temperature"`
}

// SparksRequest constructs the complete request without authorizing network I/O.
func (s AionInput) SparksRequest(c AionSuccessorConfig, reserve int) ([]byte, error) {
	if e := c.validate(); e != nil {
		return nil, e
	}
	if s.digest == "" || byteHash(s.serialized) != s.digest || reserve < 1 || reserve > 64000 {
		return nil, errors.New("invalid-request-input")
	}
	// Include the original manifest and all original bytes, not only the legacy Input.
	prompt := s.input.promptUnchecked() + "\nComplete immutable input and context manifest:\n" + string(s.serialized)
	return json.Marshal(sparksRequest{c.Model, []sparksMessage{{"user", prompt}}, reserve, false, 0})
}

type SparksUsage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
}
type SparksReceipt struct {
	Version          int          `json:"version"`
	Provider         string       `json:"provider"`
	Endpoint         string       `json:"endpoint"`
	Model            string       `json:"model"`
	InputSHA256      string       `json:"inputSha256"`
	ConfigSHA256     string       `json:"configSha256"`
	CapabilitySHA256 string       `json:"capabilitySha256"`
	AccountingSHA256 string       `json:"accountingSha256"`
	RequestSHA256    string       `json:"requestSha256"`
	ResponseSHA256   string       `json:"responseSha256,omitempty"`
	Usage            *SparksUsage `json:"usage,omitempty"`
	Finish           string       `json:"finish"`
	Error            string       `json:"error,omitempty"`
	Invoked          bool         `json:"invoked"`
	State            string       `json:"state"`
	Candidates       int          `json:"candidates"`
	ReceiptSHA256    string       `json:"receiptSha256"`
}

func (s AionInput) RunSparksCanary(ctx context.Context, c AionSuccessorConfig, cap SparksCapability, a SparksAccounting) SparksReceipt {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	return s.runSparks(ctx, c, cap, a, transport, 120*time.Second)
}
func (s AionInput) runSparks(ctx context.Context, c AionSuccessorConfig, cap SparksCapability, a SparksAccounting, transport http.RoundTripper, timeout time.Duration) (r SparksReceipt) {
	r = SparksReceipt{Version: 1, Provider: "lab-sparks", Endpoint: hermes.LocalEndpoint, Model: "deepseek-v4.1-flash", InputSHA256: s.digest, ConfigSHA256: reducerDigest(c), CapabilitySHA256: reducerDigest(cap), AccountingSHA256: reducerDigest(a), State: "comparison-unrun"}
	defer func() { r.ReceiptSHA256 = reducerDigest(r) }()
	fail := func(code string) SparksReceipt { r.Error = code; return r }
	raw, e := s.SparksRequest(c, a.ReservedOutput)
	if e != nil {
		return fail(e.Error())
	}
	r.RequestSHA256 = byteHash(raw)
	if cap.Provider != c.Provider || cap.Endpoint != c.Endpoint || cap.Model != c.Model || !evidenceHash.MatchString(cap.ModelsSHA256) || cap.MaxModelLen < 1 || cap.MaxModelLen > 16<<20 {
		return fail("provider-capacity-unverified")
	}
	for _, h := range []string{a.TokenizerSHA256, a.TemplateSHA256, a.ProbeSHA256} {
		if !evidenceHash.MatchString(h) {
			return fail("tokenizer-template-accounting-required")
		}
	}
	if a.RequestSHA256 != r.RequestSHA256 || a.CapabilitySHA256 != r.CapabilitySHA256 {
		return fail("accounting-binding-mismatch")
	}
	// Separate byte admission limit: verified probe bound, reduced by an explicit
	// byte margin, and conservatively capped at one byte per available context token.
	// This is only an additional limit; bytes NEVER establish token capacity.
	available := cap.MaxModelLen - a.ReservedOutput - a.TokenMargin
	budget := a.VerifiedRequestBytes - a.ByteMargin
	if a.TokenMargin < 128 || a.TokenMargin > cap.MaxModelLen || a.ByteMargin > 16<<20 || a.ByteMargin < 1024 || a.VerifiedRequestBytes < 1 || a.VerifiedRequestBytes > 16<<20 || a.SerializedTokens < 1 || a.PromptTokens < 1 || available < 1 || a.SerializedTokens > available || a.PromptTokens > available || len(raw) > budget || len(raw) > available {
		return fail("complete-request-capacity-refused")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/chat/completions", bytes.NewReader(raw))
	if e != nil {
		return fail("request-refused")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r.Invoked = true
	resp, e := client.Do(req)
	if e != nil {
		return fail("transport-failed-or-timeout")
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if e != nil || len(body) > 1<<20 {
		return fail("response-read-refused")
	}
	r.ResponseSHA256 = byteHash(body)
	if resp.StatusCode != 200 {
		return fail("provider-http-error")
	}
	if !utf8.Valid(body) || len(secrets.Scan(string(body))) > 0 {
		return fail("response-content-refused")
	}
	var reply struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role             string            `json:"role"`
				Content          string            `json:"content"`
				ReasoningContent *string           `json:"reasoning_content,omitempty"`
				ToolCalls        []json.RawMessage `json:"tool_calls,omitempty"`
			} `json:"message"`
			Finish   string          `json:"finish_reason"`
			Logprobs json.RawMessage `json:"logprobs,omitempty"`
		} `json:"choices"`
		Usage *SparksUsage `json:"usage"`
	}
	if strict(body, &reply) != nil || reply.Model != c.Model || reply.Object != "chat.completion" || len(reply.Choices) != 1 {
		return fail("response-envelope-refused")
	}
	choice := reply.Choices[0]
	if choice.Message.ReasoningContent != nil && len(secrets.Scan(*choice.Message.ReasoningContent)) > 0 {
		return fail("response-content-refused")
	}
	switch choice.Finish {
	case "stop", "length", "tool_calls", "content_filter":
		r.Finish = choice.Finish
	}
	if choice.Finish != "stop" || choice.Message.Role != "assistant" || len(choice.Message.ToolCalls) > 0 {
		return fail("response-finish-or-tools-refused")
	}
	r.Finish = choice.Finish
	u := reply.Usage
	if u == nil || u.Prompt != a.PromptTokens || u.Completion < 0 || u.Completion > a.ReservedOutput || u.Total != u.Prompt+u.Completion {
		return fail("response-usage-refused")
	}
	r.Usage = u
	ps, e := validateReplyEvidence(s.input, choice.Message.Content)
	if e != nil {
		return fail("candidate-contract-refused")
	}
	r.Candidates = len(ps)
	return r
}

// SparksMeasurement supplies hashes for an external tokenizer probe; it does not
// certify any token count and never emits private request bytes.
func SparksMeasurement(input AionReadinessReport, cap SparksCapability, request []byte) any {
	return struct {
		Input            AionReadinessReport `json:"input"`
		Capability       SparksCapability    `json:"capability"`
		CapabilitySHA256 string              `json:"capabilitySha256"`
		RequestSHA256    string              `json:"requestSha256"`
		RequestBytes     int                 `json:"requestBytes"`
		State            string              `json:"state"`
	}{input, cap, reducerDigest(cap), byteHash(request), len(request), "tokenizer-template-accounting-required"}
}
