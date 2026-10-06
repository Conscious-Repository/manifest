package hermes

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// CLAUDE AS THE STAND-IN EXTRACTION MODEL (owner decision 2026-10-06: "can
// you switch this to use an openai or claude endpoint in the meantime" — the
// lab Sparks unreachable since about 2026-10-02).
//
// This is the "claude-code-subscription" recovery choice fallback.go names,
// made by the owner and recorded in config (ExtractionFallback.OwnerAction);
// it is NOT an automatic failover. The caller (domainextract) decides per job
// whether a note may leave the lab at all — by the transcript tier map, with
// Jev's conservative advice for an unmapped note — and only while the lab
// model is unreachable. Held notes never come here.
//
// The run is as bounded as the lab path: one turn, no tools ("--tools ''"),
// no MCP servers, no settings files (so no hooks), no saved session, a
// scratch working directory (no CLAUDE.md), a minimal environment, the duty's
// timeout and ceiling, and the same strict candidate JSON contract.

// ClaudeExtractionProvider labels the execution receipt.
const ClaudeExtractionProvider = "claude-code-subscription"

// ClaudeExtraction is the owner's recorded choice.
type ClaudeExtraction struct {
	Binary      string // absolute path to the claude CLI
	Model       string // e.g. "sonnet"
	OwnerAction string // where the owner authorized it (required)
}

// claudeExtractionCommand is the process seam; tests replace it.
var claudeExtractionCommand = func(ctx context.Context, bin string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, bin, args...)
}

// claudeResult is the subset of `claude -p --output-format json` we read.
type claudeResult struct {
	Type       string                     `json:"type"`
	Subtype    string                     `json:"subtype"`
	IsError    bool                       `json:"is_error"`
	NumTurns   int                        `json:"num_turns"`
	Result     string                     `json:"result"`
	CostUSD    float64                    `json:"total_cost_usd"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
}

// RunClaudeExtraction runs one extraction turn on Claude. The duty must be
// configured exactly as the lab duty is (the same authority bounds apply).
func (r *Runner) RunClaudeExtraction(ctx context.Context, ritual, prompt string, c ClaudeExtraction) (Result, error) {
	duty := "extractor/" + ritual
	if r == nil || !r.cfg.Enabled {
		return Result{}, refuse("successor disabled")
	}
	a, err := r.dutyAuthority(Request{MigratedDuty: duty})
	if err != nil {
		return Result{}, err
	}
	if !extractionDutyAllowed(duty, a) {
		return Result{}, refuse("extraction duty authority not verified")
	}
	if strings.TrimSpace(c.OwnerAction) == "" || strings.TrimSpace(c.Model) == "" || !filepath.IsAbs(c.Binary) {
		return Result{}, refuse("Claude extraction needs the owner's recorded choice, a model and an absolute CLI path")
	}
	if len(prompt) > extractionPromptLimit || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0) || strings.TrimSpace(prompt) == "" {
		return Result{}, refuse("invalid extraction prompt")
	}
	scratch, err := os.MkdirTemp("", "manifest-claude-extraction-*")
	if err != nil {
		return Result{}, refuse("Claude scratch unavailable")
	}
	defer os.RemoveAll(scratch)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(a.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := claudeExtractionCommand(ctx, c.Binary, "-p", "--model", c.Model, "--tools", "",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--setting-sources", "",
		"--no-session-persistence", "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = scratch
	// HOME stays real: the subscription login lives there and refreshes in
	// place. Nothing else is inherited (no API keys, no proxies).
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + filepath.Dir(c.Binary) + ":/usr/bin:/bin", "LANG=C.UTF-8", "TERM=dumb", "NO_COLOR=1", "TMPDIR=" + scratch}
	cmd.WaitDelay = time.Second
	var output limitedOutput
	cmd.Stdout = &output
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return Result{}, refuse("bounded Claude timeout or cancellation")
	}
	var out claudeResult
	if json.Unmarshal(output.Bytes(), &out) != nil || out.Type != "result" {
		return Result{}, refuse("Claude execution receipt unreadable")
	}
	models := make([]string, 0, len(out.ModelUsage))
	for m := range out.ModelUsage {
		models = append(models, m)
	}
	sort.Strings(models)
	responseModel := ""
	if len(models) > 0 {
		responseModel = models[0]
	}
	completed := runErr == nil && !out.IsError && out.Subtype == "success"
	status := 0
	if completed {
		status = 200
	}
	observed := ExtractionExecution{Provider: ClaudeExtractionProvider, Model: c.Model, ResponseModel: responseModel, Steps: out.NumTurns, Completed: completed, Status: status}
	partial := Result{Extraction: &observed, Model: responseModel}
	if !completed || out.NumTurns != 1 || len(models) != 1 {
		return partial, refuse("Claude execution receipt not verified")
	}
	if a.CeilingUSD != nil && out.CostUSD > *a.CeilingUSD {
		return partial, refuse("Claude turn exceeded the duty ceiling")
	}
	result, err := parseExtractionResult([]byte(out.Result), a)
	result.Extraction = &observed
	result.Model = responseModel
	result.SpentUSD = out.CostUSD
	result.CostPolicy = "subscription"
	return result, err
}
