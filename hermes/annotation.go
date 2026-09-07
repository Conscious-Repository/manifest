package hermes

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

//go:embed annotation.py
var annotationScript string

type AnnotationResult struct {
	Reply        string `json:"reply"`
	Model        string `json:"model"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
}

// AnnotationError contains only a bounded machine-readable failure code.
// Provider response bodies and request context never become error messages.
type AnnotationError struct{ Code string }

func (e *AnnotationError) Error() string { return "writing completion: " + e.Code }
func (e *AnnotationError) UserMessage() string {
	switch e.Code {
	case "timeout":
		return "The model took too long to answer. Please retry."
	case "token_limit":
		return "The model reached its response limit. Try a narrower question."
	case "http_429", "http_502", "http_503", "http_504":
		return "The model is busy or temporarily unavailable. Please retry."
	case "connection":
		return "Could not reach the model. Please retry."
	case "empty_response":
		return "The model returned no answer. Please retry."
	default:
		return "The model could not complete this request. Please retry."
	}
}
func annotationFailure(code string) error {
	switch code {
	case "timeout", "connection", "token_limit", "empty_response", "invalid_response", "unsupported_provider", "unexpected_tool_call", "response_too_large", "http_400", "http_401", "http_403", "http_404", "http_408", "http_413", "http_429", "http_500", "http_502", "http_503", "http_504":
	default:
		code = "provider_error"
	}
	return &AnnotationError{Code: code}
}

func (r *Runner) annotationPython() string {
	if r.cfg.AnnotationPython != "" {
		return r.cfg.AnnotationPython
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", "hermes-agent", "venv", "bin", "python")
}
func (r *Runner) AnnotationEnabled() bool {
	if !r.Enabled() {
		return false
	}
	_, err := os.Stat(r.annotationPython())
	return err == nil
}

// Annotate makes a completion through the installed provider resolver, never
// through `hermes -z` (which loads tools and auto-approves them). The fixed
// helper has no dispatch loop: even a returned tool call cannot execute.
func (r *Runner) Annotate(ctx context.Context, packet any) (AnnotationResult, error) {
	var result AnnotationResult
	if !r.AnnotationEnabled() {
		return result, ErrNotEnabled
	}
	input, err := json.Marshal(packet)
	if err != nil || len(input) > 100000 {
		return result, errors.New("writing context too large")
	}
	ctx, cancel := context.WithTimeout(ctx, 160*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.annotationPython(), "-c", annotationScript)
	cmd.Stdin = bytes.NewReader(input)
	var output, diagnostic bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &diagnostic
	cmd.WaitDelay = pipeGrace
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return result, annotationFailure("timeout")
		}
		var failure struct {
			Code string `json:"code"`
		}
		if diagnostic.Len() < 4096 {
			_ = json.Unmarshal(diagnostic.Bytes(), &failure)
		}
		return result, annotationFailure(failure.Code)
	}
	if output.Len() > 100000 {
		return result, annotationFailure("response_too_large")
	}
	if err = json.Unmarshal(output.Bytes(), &result); err != nil || result.Reply == "" || len(result.Reply) > 24000 {
		return result, annotationFailure("invalid_response")
	}
	return result, nil
}
