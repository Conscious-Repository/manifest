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
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.WaitDelay = pipeGrace
	if err = cmd.Run(); err != nil {
		return result, errors.New("writing completion unavailable")
	}
	if output.Len() > 100000 {
		return result, errors.New("writing response too large")
	}
	if err = json.Unmarshal(output.Bytes(), &result); err != nil || result.Reply == "" || len(result.Reply) > 24000 {
		return result, errors.New("invalid writing response")
	}
	return result, nil
}
