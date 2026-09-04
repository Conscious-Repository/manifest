// Package hermes runs the local NousResearch "Hermes Agent" CLI as a one-shot
// subprocess. The app's Hermes IS the owner's real do-bot — the same one he
// pings on Telegram — not a compartmentalized excalibur harness copy: it shares
// the one `~/.hermes` state (sessions, skills, toolsets, memory, approvals). We
// reach it by invoking `hermes -z` on the box rather than through the excalibur
// spool/file contract.
//
// `hermes -z PROMPT` is one-shot mode: it runs a full agent turn (tools +
// memory) and prints ONLY the final reply on stdout — no tool previews, no
// session-id line. Flags we lean on:
//
//	--resume <session>   per-thread continuity (a todo's thread ↔ one session)
//	-m <model>           model override for this invocation
//	-t <toolsets>        scope the tools available — the approval-gate lever
//	--skills <names>     preload skills for the turn (a cron job's `skills`)
//	--usage-file <path>  write a JSON cost/token report after the run
//
// The manifest process runs on the same box as the Hermes CLI (metis), so this
// is a local exec. On any box without the binary (e.g. the Mac dev twin) the
// runner reports NOT enabled and callers fall back to their existing path — the
// same graceful-degradation discipline as the calendar/gmail clients.
package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNotEnabled is returned by Run when the runner isn't configured to invoke
// the CLI. Callers treat it as "Hermes isn't wired here" and fall back.
var ErrNotEnabled = errors.New("hermes runner not enabled")

// Config is the resolved runner configuration (from manifest config.json).
type Config struct {
	Enabled  bool   // master switch — off by default so this lands dark
	Bin      string // hermes binary path or name on $PATH (default "hermes")
	Model    string // default -m model override ("" → the CLI's configured model)
	Toolsets string // default -t toolset scope ("" → the CLI's configured toolsets)
	// TimeoutSeconds bounds a single agent turn; 0 → a sane default. An agent
	// turn can be long (tool loop), so this is generous.
	TimeoutSeconds int
}

// Runner invokes the Hermes CLI. Safe for concurrent use (each Run is its own
// process); the CLI itself serializes writes to a resumed session.
type Runner struct {
	cfg     Config
	timeout time.Duration
}

// NewRunner resolves defaults. A missing Bin defaults to "hermes" on $PATH.
func NewRunner(cfg Config) *Runner {
	if cfg.Bin == "" {
		cfg.Bin = "hermes"
	}
	to := time.Duration(cfg.TimeoutSeconds) * time.Second
	if to <= 0 {
		to = 8 * time.Minute
	}
	return &Runner{cfg: cfg, timeout: to}
}

// Enabled reports whether Run will attempt an invocation.
func (r *Runner) Enabled() bool { return r != nil && r.cfg.Enabled && r.cfg.Bin != "" }

// Request is one agent turn.
type Request struct {
	Prompt   string // the composed work-order / message text
	Session  string // --resume target (per-todo continuity); "" → a fresh session
	Model    string // -m override for this turn; "" → the runner default
	Toolsets string // -t override for this turn; "" → the runner default
	Skills   string // --skills preload (comma-separated); "" → none
	// Profile targets a named Hermes profile (`hermes -p <name>`, agents plan
	// Phase 5): the turn runs under that profile's own ~/.hermes/profiles/<name>
	// state — its model, keys, SOUL.md, skills, sessions. "" → the default
	// profile, argv unchanged.
	Profile string
}

// Result is a completed turn.
type Result struct {
	Reply    string  // stdout — the agent's final reply
	SpentUSD float64 // parsed from the usage report (0 if unavailable)
	Model    string  // model reported by the usage file, if any
}

// buildArgs assembles the argv for one turn (pure, unit-tested). The prompt
// rides as the -z value — no shell is involved (exec, not sh -c), so arbitrary
// text is safe. usageFile, when non-empty, adds --usage-file. A Profile goes
// FIRST (`-p <name>` selects the state root before anything else is parsed);
// without one the argv is exactly what it was before profiles existed.
func (r *Runner) buildArgs(req Request, usageFile string) []string {
	var args []string
	if p := strings.TrimSpace(req.Profile); p != "" {
		args = append(args, "-p", p)
	}
	args = append(args, "-z", req.Prompt)
	if m := firstNonEmpty(req.Model, r.cfg.Model); m != "" {
		args = append(args, "-m", m)
	}
	if t := firstNonEmpty(req.Toolsets, r.cfg.Toolsets); t != "" {
		args = append(args, "-t", t)
	}
	if s := strings.TrimSpace(req.Session); s != "" {
		args = append(args, "--resume", s)
	}
	if k := strings.TrimSpace(req.Skills); k != "" {
		args = append(args, "--skills", k)
	}
	if usageFile != "" {
		args = append(args, "--usage-file", usageFile)
	}
	return args
}

// Run executes one agent turn and returns the reply. It never runs the tool
// loop unattended forever — the context timeout kills a hung turn.
func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if !r.Enabled() {
		return Result{}, ErrNotEnabled
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, errors.New("empty prompt")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// usage report → a temp file the CLI writes JSON into after the run.
	usageFile := ""
	if f, err := os.CreateTemp("", "hermes-usage-*.json"); err == nil {
		usageFile = f.Name()
		f.Close()
		defer os.Remove(usageFile)
	}

	cmd := exec.CommandContext(ctx, r.cfg.Bin, r.buildArgs(req, usageFile)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return Result{}, fmt.Errorf("hermes turn timed out after %s", r.timeout)
		}
		// surface a trimmed stderr tail — the CLI's error is the useful part
		return Result{}, fmt.Errorf("hermes: %v: %s", err, tail(errb.String(), 300))
	}

	res := Result{Reply: strings.TrimSpace(out.String())}
	if usageFile != "" {
		res.SpentUSD, res.Model = parseUsage(usageFile)
	}
	return res, nil
}

// usageReport is the tolerant view of --usage-file JSON; unknown fields ignored,
// field names kept loose since the CLI's report shape is a versioned contract.
type usageReport struct {
	CostUSD       float64 `json:"cost_usd"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
	Model         string  `json:"model"`
}

func parseUsage(path string) (usd float64, model string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, ""
	}
	var u usageReport
	if json.Unmarshal(b, &u) != nil {
		return 0, ""
	}
	usd = u.CostUSD
	if usd == 0 {
		usd = u.EstimatedCost
	}
	return usd, u.Model
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
