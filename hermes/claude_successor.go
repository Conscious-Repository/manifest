package hermes

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed claude_successor.py
var claudeSuccessorScript string

func claudeDutyAllowed(duty string, a DutyAuthority) bool {
	switch duty {
	case "extractor/aion", "extractor/real-estate", "extractor/ooda-email":
	default:
		return false
	}
	ceiling := 4.0
	if duty == "extractor/ooda-email" {
		ceiling = 2
	}
	return a.Provider == "claude-sub" && a.Model == "claude-sonnet-5" && a.CeilingUSD != nil && *a.CeilingUSD > 0 && *a.CeilingUSD <= ceiling && a.MaxSteps == 1 && a.TimeoutSeconds <= 120 && len(a.Tools) == 1 && a.Tools[0] == "none" && a.MCP == "no_mcp"
}

// runClaudeSuccessor keeps the existing model but grants it no engine casts.
// Authentication is copied privately; the child cannot read the vault, harness,
// caller HOME or plugins. Only its disposable scratch directory is writable.
func (r *Runner) runClaudeSuccessor(ctx context.Context, req Request, a DutyAuthority) (Result, error) {
	if !r.cfg.Enabled || !claudeDutyAllowed(req.MigratedDuty, a) || req.Profile != "" || req.Skills != "" || len(req.Prompt) > 64000 || strings.TrimSpace(req.Prompt) == "" {
		return Result{}, refuse("invalid subscription duty")
	}
	binary, e := exec.LookPath("claude")
	if e != nil {
		return Result{}, refuse("subscription executable unavailable")
	}
	binary, e = filepath.EvalSymlinks(binary)
	if e != nil {
		return Result{}, refuse("subscription executable unavailable")
	}
	scratch, e := os.MkdirTemp("", "manifest-extraction-*")
	if e != nil {
		return Result{}, refuse("subscription isolation unavailable")
	}
	defer os.RemoveAll(scratch)
	auth := filepath.Join(scratch, ".claude")
	if e = os.Mkdir(auth, 0700); e != nil {
		return Result{}, refuse("subscription isolation unavailable")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return Result{}, refuse("subscription auth unavailable")
	}
	// Only the OAuth credential file crosses. User settings/plugins/project memory
	// and API credentials never enter the child environment or filesystem grant.
	secret, e := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if e != nil {
		return Result{}, refuse("subscription auth unavailable")
	}
	if e = os.WriteFile(filepath.Join(auth, ".credentials.json"), secret, 0600); e != nil {
		return Result{}, refuse("subscription isolation unavailable")
	}
	timeout := a.TimeoutSeconds
	if req.TimeoutSeconds > 0 && req.TimeoutSeconds < timeout {
		timeout = req.TimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", claudeSuccessorScript, binary, scratch, strconv.FormatFloat(*a.CeilingUSD, 'f', -1, 64), a.Model)
	cmd.Env = []string{"HOME=" + scratch, "CLAUDE_CONFIG_DIR=" + auth, "TMPDIR=" + scratch, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "CLAUDE_CODE_SAFE_MODE=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1"}
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.WaitDelay = time.Second
	var output subscriptionOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if e = cmd.Run(); e != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return Result{}, refuse("bounded subscription timeout or cancellation")
		}
		return Result{}, refuse("bounded subscription execution failed")
	}
	return parseSubscriptionResult(output.Bytes(), a)
}

type subscriptionOutput struct{ bytes.Buffer }

func (b *subscriptionOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2<<20 {
		return 0, io.ErrShortBuffer
	}
	return b.Buffer.Write(p)
}

// The init record and assistant message must independently corroborate actual
// model and empty tool/MCP authority. A result alone cannot certify a run.
func parseSubscriptionResult(raw []byte, a DutyAuthority) (Result, error) {
	fail := func() (Result, error) { return Result{}, refuse("subscription execution evidence invalid") }
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	initSeen, assistantSeen, resultSeen := false, false, false
	var result Result
	for scanner.Scan() {
		fields, ok := strictUsageFields(scanner.Bytes())
		if !ok {
			return fail()
		}
		var kind, subtype string
		json.Unmarshal(fields["type"], &kind)
		json.Unmarshal(fields["subtype"], &subtype)
		switch kind {
		case "system":
			if subtype != "init" {
				return fail()
			}
			if initSeen {
				return fail()
			}
			initSeen = true
			var model string
			var tools, servers []json.RawMessage
			if json.Unmarshal(fields["model"], &model) != nil || model != a.Model || string(fields["tools"]) != "[]" || json.Unmarshal(fields["tools"], &tools) != nil || string(fields["mcp_servers"]) != "[]" || json.Unmarshal(fields["mcp_servers"], &servers) != nil {
				return fail()
			}
		case "assistant":
			var m struct {
				Model   string `json:"model"`
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			}
			if json.Unmarshal(fields["message"], &m) != nil || m.Model != a.Model {
				return fail()
			}
			for _, c := range m.Content {
				if c.Type != "text" && c.Type != "thinking" {
					return fail()
				}
			}
			assistantSeen = true
		case "result":
			if resultSeen {
				return fail()
			}
			resultSeen = true
			var report struct {
				Result string                     `json:"result"`
				Error  *bool                      `json:"is_error"`
				Cost   *float64                   `json:"total_cost_usd"`
				Turns  *int                       `json:"num_turns"`
				Models map[string]json.RawMessage `json:"modelUsage"`
				Usage  *struct {
					Input  int64 `json:"input_tokens"`
					Output int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(scanner.Bytes(), &report) != nil || subtype != "success" || report.Error == nil || *report.Error || report.Cost == nil || math.IsNaN(*report.Cost) || *report.Cost < 0 || a.CeilingUSD == nil || *report.Cost > *a.CeilingUSD || report.Turns == nil || *report.Turns != 1 || report.Usage == nil || report.Usage.Input < 0 || report.Usage.Output <= 0 || strings.TrimSpace(report.Result) == "" || len(report.Models) != 1 || report.Models[a.Model] == nil {
				return fail()
			}
			result = Result{Reply: report.Result, Model: a.Model, SpentUSD: *report.Cost, Usage: &TokenUsage{PromptTokens: report.Usage.Input, CompletionTokens: report.Usage.Output, TotalTokens: report.Usage.Input + report.Usage.Output}, dutyVerified: true}
		default:
			return fail()
		}
	}
	if scanner.Err() != nil || !initSeen || !assistantSeen || !resultSeen {
		return fail()
	}
	return result, nil
}

// ValidateExtractionDuty refuses a route whose declared authority would change
// the existing model or fall back to a different executor.
func (r *Runner) ValidateExtractionDuty(ritual string) error {
	if r == nil || !r.cfg.Enabled {
		return refuse("successor disabled")
	}
	duty := "extractor/" + ritual
	a, e := r.dutyAuthority(Request{MigratedDuty: duty})
	if e != nil {
		return e
	}
	if !claudeDutyAllowed(duty, a) {
		return refuse("extraction requires its existing subscription model")
	}
	return nil
}
