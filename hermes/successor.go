package hermes

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed successor.py
var successorScript string

// This is a fixed implementation, not a configurable executable or a caller's
// sandbox assertion. The unexported seam is replaced only by package tests.
var successorCommand = func(ctx context.Context, usage string) *exec.Cmd {
	return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", successorScript, usage)
}

func (r *Runner) runSuccessor(ctx context.Context, req Request, a DutyAuthority) (Result, error) {
	if !r.cfg.Enabled {
		return Result{}, refuse("successor runner disabled")
	}
	if req.Profile != "" || req.Skills != "" {
		return Result{}, refuse("successor profile or skills not permitted")
	}
	if strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > 64000 {
		return Result{}, refuse("invalid successor input size")
	}
	timeout := time.Duration(a.TimeoutSeconds) * time.Second
	if req.TimeoutSeconds > 0 && req.TimeoutSeconds < a.TimeoutSeconds {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	path, err := createUsageFile(os.TempDir(), "")
	if err != nil {
		return Result{}, refuse("usage evidence unavailable")
	}
	defer os.Remove(path)
	packet, err := json.Marshal(map[string]any{"authority": a, "prompt": req.Prompt})
	if err != nil {
		return Result{}, refuse("invalid successor authority")
	}
	cmd := successorCommand(ctx, path)
	cmd.Env = []string{"LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(packet)
	var output limitedOutput
	cmd.Stdout = &output
	var diagnostic limitedOutput
	cmd.Stderr = &diagnostic
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, refuse("successor timeout or cancellation")
		}
		reason := map[string]string{
			"isolation":      "successor OS isolation unavailable",
			"cost":           "cost bound exceeded",
			"model":          "model drift",
			"provider_drift": "provider drift",
			"completion":     "incomplete successor completion",
			"usage":          "missing usage evidence",
		}[diagnostic.String()]
		if reason == "" {
			reason = "bounded successor execution failed"
		}
		return Result{}, refuse(reason)
	}
	if ctx.Err() != nil {
		return Result{}, refuse("successor timeout or cancellation")
	}
	res, err := VerifyDutyUsageFile(a, Result{Reply: output.String()}, path)
	if err != nil {
		return Result{}, err
	}
	b, readErr := os.ReadFile(path)
	var report struct {
		Steps *int `json:"steps"`
	}
	if readErr != nil || json.Unmarshal(b, &report) != nil || report.Steps == nil || *report.Steps != 1 || *report.Steps > a.MaxSteps {
		return Result{}, refuse("invalid successor step evidence")
	}
	res.dutyVerified = true
	return res, nil
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64000 {
		return 0, io.ErrShortBuffer
	}
	return b.Buffer.Write(p)
}

// DutyEvidence exposes only reviewed, valid authority metadata, never input or
// profile/environment values. Unknown/unsupported contracts have no authority.
func (r *Runner) DutyEvidence(duty string) map[string]any {
	if r == nil {
		return nil
	}
	a, err := r.dutyAuthority(Request{MigratedDuty: duty})
	if err != nil {
		return nil
	}
	return map[string]any{"provider": a.Provider, "model": a.Model, "tools": []string{"none"}, "mcp": a.MCP, "timeoutSeconds": a.TimeoutSeconds, "maxSteps": a.MaxSteps, "enforcedMaxSteps": 1, "ceilingUsd": 0, "fallback": false, "isolation": "landlock+seccomp"}
}
