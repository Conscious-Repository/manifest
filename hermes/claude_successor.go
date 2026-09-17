package hermes

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Use the installed wrapper's exact Python runtime, without invoking its shell.
const extractionBinary = "/home/benjamin/.hermes/hermes-agent/venv/bin/python"

// Half the pinned context in bytes conservatively reserves room for runtime
// instructions and 4096 output tokens, without truncation or token-ratio guesses.
const extractionPromptLimit = 512 * 1024

//go:embed extraction.py
var extractionScript string

// Pin the sparks alias and its wire model using Hermes provider configuration.
// The installed chat path does not expand model.aliases before API submission;
// extra_body is Hermes' own provider setting that pins the canonical wire model.
// Keep this invocation's config
// private and fixed: no inherited fallback, MCP servers, skills or credentials.
const extractionConfig = `{"compression":{"enabled":false},"model":{"context_length":1048576,"aliases":{"sparks":"lab-sparks/deepseek-v4.1-flash"}},"custom_providers":[{"name":"lab-sparks","base_url":"http://192.168.87.11:8000/v1","api_key":"local","model":"deepseek-v4.1-flash","api_mode":"chat_completions","models":{"sparks":{"context_length":1048576},"deepseek-v4.1-flash":{"context_length":1048576}},"discover_models":false,"extra_body":{"model":"deepseek-v4.1-flash","tool_choice":"none"}}],"fallback_providers":[],"fallback_model":null,"mcp_servers":{}}`

// Package-private process seam; production always invokes the fixed CLI by argv.
var extractionCommand = func(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, extractionBinary, append([]string{"-I", "-S", "-c", extractionScript}, args...)...)
}

func extractionDutyAllowed(duty string, a DutyAuthority) bool {
	ceiling := 4.0
	switch duty {
	case "extractor/aion", "extractor/real-estate":
	case "extractor/ooda-email":
		ceiling = 2
	default:
		return false
	}
	return a.Validate() == nil && a.Provider == "lab-sparks" && a.Model == "deepseek-v4.1-flash" && *a.CeilingUSD <= ceiling && a.MaxSteps == 1 && a.TimeoutSeconds <= 120 && len(a.Tools) == 1 && a.Tools[0] == "none" && a.MCP == "no_mcp"
}

func defaultExtractionDuties() map[string]DutyAuthority {
	duties := make(map[string]DutyAuthority)
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		budget := 4.0
		if ritual == "ooda-email" {
			budget = 2
		}
		duties["extractor/"+ritual] = DutyAuthority{Provider: "lab-sparks", Model: "deepseek-v4.1-flash", Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 120, MaxSteps: 1, CeilingUSD: &budget}
	}
	return duties
}

// runExtractionSuccessor uses Hermes' safe, tool-free single-query CLI. The
// model returns candidate JSON only; domainextract validates it and publishes
// proposals through the existing approval store. Local compute has no marginal
// API charge; zero cost here is policy, not a claim of measured CLI telemetry.
func (r *Runner) runExtractionSuccessor(ctx context.Context, req Request, a DutyAuthority) (Result, error) {
	if !r.cfg.Enabled || !extractionDutyAllowed(req.MigratedDuty, a) || req.Profile != "" || req.Skills != "" || len(req.Prompt) > extractionPromptLimit || !utf8.ValidString(req.Prompt) || strings.ContainsRune(req.Prompt, 0) || strings.TrimSpace(req.Prompt) == "" {
		return Result{}, refuse("invalid Hermes extraction duty")
	}
	scratch, err := os.MkdirTemp("", "manifest-extraction-*")
	if err != nil {
		return Result{}, refuse("Hermes scratch unavailable")
	}
	defer os.RemoveAll(scratch)
	if err = os.WriteFile(filepath.Join(scratch, "config.yaml"), []byte(extractionConfig), 0600); err != nil {
		return Result{}, refuse("Hermes config unavailable")
	}
	timeout := a.TimeoutSeconds
	if req.TimeoutSeconds > 0 && req.TimeoutSeconds < timeout {
		timeout = req.TimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	// Prompt bytes travel by pipe; the contained launcher builds Hermes argv
	// in-process so Linux MAX_ARG_STRLEN cannot reject accepted large prompts.
	cmd := extractionCommand(ctx)
	cmd.Stdin = strings.NewReader(req.Prompt)
	cmd.Dir = scratch
	cmd.Env = []string{"HOME=" + scratch, "HERMES_HOME=" + scratch, "TMPDIR=" + scratch, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HERMES_MAX_TOKENS=4096", "HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1", "PYTHONDONTWRITEBYTECODE=1", "NO_COLOR=1", "TERM=dumb"}
	cmd.WaitDelay = time.Second
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return Result{}, refuse("bounded Hermes timeout or cancellation")
		}
		return Result{}, refuse("bounded Hermes execution failed")
	}
	return parseExtractionResult(output.Bytes(), a)
}

func parseExtractionResult(raw []byte, a DutyAuthority) (Result, error) {
	// Full candidate schema/source checks belong to domainextract.ValidateReply.
	// The installed CLI warns about the explicit empty toolset even in quiet
	// mode. Strip only that fixed startup line, never arbitrary model chatter.
	text := strings.TrimPrefix(string(raw), "Warning: Unknown toolsets: none\n")
	text = strings.TrimPrefix(text, "  ⚠ tirith security scanner enabled but not available — command scanning will use pattern matching only\n")
	raw = []byte(text)
	fields, ok := strictUsageFields(raw)
	var candidates []json.RawMessage
	for key := range fields {
		if key != "candidates" && key != "summary" {
			return Result{}, refuse("invalid Hermes candidate result")
		}
	}
	if summary, exists := fields["summary"]; exists {
		var text string
		if json.Unmarshal(summary, &text) != nil {
			return Result{}, refuse("invalid Hermes summary")
		}
	}
	if !ok || len(fields) > 2 || json.Unmarshal(fields["candidates"], &candidates) != nil || string(fields["candidates"]) == "null" {
		return Result{}, refuse("invalid Hermes candidate result")
	}
	return Result{Reply: strings.TrimSpace(string(raw)), Model: a.Model, CostPolicy: LocalCostPolicy, CostTelemetry: "unavailable", dutyVerified: true}, nil
}

func (r *Runner) ValidateExtractionDuty(ritual string) error {
	if r == nil || !r.cfg.Enabled {
		return refuse("successor disabled")
	}
	duty := "extractor/" + ritual
	a, err := r.dutyAuthority(Request{MigratedDuty: duty})
	if err != nil {
		return err
	}
	if !extractionDutyAllowed(duty, a) {
		return refuse("extraction requires lab-sparks/deepseek-v4.1-flash")
	}
	return nil
}

// DutyAuthorities projects the resolved contracts, including extractor defaults.
// These declarations describe routing bounds, not runtime liveness or parity.
func (r *Runner) DutyAuthorities() map[string]DutyAuthority {
	if r == nil {
		return nil
	}
	out := make(map[string]DutyAuthority, len(r.cfg.Duties))
	for name, a := range r.cfg.Duties {
		a.Tools = append([]string(nil), a.Tools...)
		if a.CeilingUSD != nil {
			ceiling := *a.CeilingUSD
			a.CeilingUSD = &ceiling
		}
		out[name] = a
	}
	return out
}
