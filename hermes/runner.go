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
//	-p <profile>         run under a named profile's own ~/.hermes state
//	-m <model>           model override for this invocation
//	-t <toolsets>        scope the tools available — the approval-gate lever
//	--usage-file <path>  write a JSON cost/token report after the run
//
// Flags we deliberately do NOT pass. `-z` dispatches straight to
// run_oneshot(prompt, model, provider, toolsets, usage_file) (hermes_cli/
// main.py, oneshot.py); the top-level parser accepts `--resume` and `--skills`
// but the one-shot path never reads them, so both are silently dropped. Live-
// verified on v0.20.0 (2026-09-04): `-z --resume <id>` starts a brand-new
// session every time. So:
//
//   - Continuity is MANIFEST'S job. Each turn is a fresh Hermes session whose
//     only memory is the prompt; callers compose the window (plan + recent
//     thread entries) into the prompt themselves. The session_id Hermes minted
//     for the turn comes back in the usage report and is recorded on the
//     Result so a ledger entry can point at it (`hermes sessions search`).
//   - Skills are loaded ON DEMAND: Hermes loads a skill when the turn names it
//     (skill_view), so Request.Skills is turned into a one-line preamble at the
//     top of the prompt rather than a flag. Live-verified the same day.
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
	// TimeoutSeconds is the DEFAULT bound on one agent turn (config
	// `hosts.hermes.timeoutSeconds`); 0 → DefaultTimeout. It governs only the
	// turns that don't ask for their own budget: a Request.TimeoutSeconds > 0
	// overrides it per turn (an execution turn — plan + tool loop + verify —
	// asks for a longer one; see server.hermesTurnBudget). Raising this raises
	// the fallback for every budget-less turn, asks included.
	TimeoutSeconds int
}

// DefaultTimeout bounds a turn when neither the config nor the Request sets a
// budget. Sized for a quick ask/comment turn; multi-step execution turns are
// expected to carry their own Request.TimeoutSeconds.
const DefaultTimeout = 8 * time.Minute

// Runner invokes the Hermes CLI. Safe for concurrent use (each Run is its own
// process and its own fresh Hermes session).
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
		to = DefaultTimeout
	}
	return &Runner{cfg: cfg, timeout: to}
}

// turnTimeout is the effective bound for one turn: the Request's own budget
// when it sets one, else the runner's configured default. Pure.
func (r *Runner) turnTimeout(req Request) time.Duration {
	if req.TimeoutSeconds > 0 {
		return time.Duration(req.TimeoutSeconds) * time.Second
	}
	return r.timeout
}

// Enabled reports whether Run will attempt an invocation.
func (r *Runner) Enabled() bool { return r != nil && r.cfg.Enabled && r.cfg.Bin != "" }

// Request is one agent turn. There is no session/resume field on purpose: a
// `-z` turn is always a fresh Hermes session (see the package comment), so the
// caller composes whatever context the turn needs into Prompt.
type Request struct {
	Prompt   string // the composed work-order / message text
	Model    string // -m override for this turn; "" → the runner default
	Toolsets string // -t override for this turn; "" → the runner default
	// Skills the turn must load before it starts (comma-separated skill names,
	// e.g. a cron job's `skills`). `-z` has no preload flag, so the runner
	// names them at the top of the prompt and Hermes loads them on demand.
	// "" → the prompt goes out untouched.
	Skills string
	// Profile targets a named Hermes profile (`hermes -p <name>`, agents plan
	// Phase 5): the turn runs under that profile's own ~/.hermes/profiles/<name>
	// state — its model, keys, SOUL.md, skills, sessions. "" → the default
	// profile, argv unchanged.
	Profile string
	// TimeoutSeconds bounds THIS turn, overriding the runner's configured
	// default (Config.TimeoutSeconds / DefaultTimeout) when > 0. The one cap
	// used to apply to every turn alike, so an execution turn that legitimately
	// runs a long tool loop was killed at the quick-ask bound (2026-09-05: a
	// DO on a real implementation task died "timed out after 8m0s"). Callers
	// that know the turn is multi-step set a longer budget here; 0 → default.
	TimeoutSeconds int
}

// Result is a completed turn.
type Result struct {
	Reply    string  // stdout — the agent's final reply
	SpentUSD float64 // parsed from the usage report (0 if unavailable)
	Model    string  // model reported by the usage file, if any
	// SessionID is the Hermes session the turn ran as (usage report
	// `session_id`, e.g. "20260904_135845_2214c8"), "" if unavailable. It is
	// a pointer into Hermes' own store (`hermes sessions search`, `hermes chat
	// --resume`), not something the runner ever feeds back in.
	SessionID string
}

// composePrompt returns the text that rides as the -z value: the caller's
// prompt, preceded by a skill-load preamble when Skills is set. Pure.
func composePrompt(req Request) string {
	names := splitSkills(req.Skills)
	if len(names) == 0 {
		return req.Prompt
	}
	var b strings.Builder
	b.WriteString("Before anything else, load and follow ")
	if len(names) == 1 {
		b.WriteString("the skill ")
	} else {
		b.WriteString("these skills (skill_view each one) ")
	}
	b.WriteString("`" + strings.Join(names, "`, `") + "`")
	b.WriteString(" — the instructions below assume it is loaded.\n\n")
	b.WriteString(req.Prompt)
	return b.String()
}

// splitSkills normalizes a comma-separated skill list (trims, drops blanks).
func splitSkills(s string) []string {
	var out []string
	for _, n := range strings.Split(s, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// buildArgs assembles the argv for one turn (pure, unit-tested). The prompt
// rides as the -z value — no shell is involved (exec, not sh -c), so arbitrary
// text is safe. usageFile, when non-empty, adds --usage-file. A Profile goes
// FIRST (`-p <name>` selects the state root before anything else is parsed);
// without one the argv is exactly what it was before profiles existed. Never
// emits --resume or --skills: the one-shot path ignores both (package comment).
func (r *Runner) buildArgs(req Request, usageFile string) []string {
	var args []string
	if p := strings.TrimSpace(req.Profile); p != "" {
		args = append(args, "-p", p)
	}
	args = append(args, "-z", composePrompt(req))
	if m := firstNonEmpty(req.Model, r.cfg.Model); m != "" {
		args = append(args, "-m", m)
	}
	if t := firstNonEmpty(req.Toolsets, r.cfg.Toolsets); t != "" {
		args = append(args, "-t", t)
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
	timeout := r.turnTimeout(req)
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
			return Result{}, fmt.Errorf("hermes turn timed out after %s", timeout)
		}
		// surface a trimmed stderr tail — the CLI's error is the useful part
		return Result{}, fmt.Errorf("hermes: %v: %s", err, tail(errb.String(), 300))
	}

	res := Result{Reply: strings.TrimSpace(out.String())}
	if usageFile != "" {
		u := parseUsage(usageFile)
		res.SpentUSD, res.Model, res.SessionID = u.usd(), u.Model, strings.TrimSpace(u.SessionID)
	}
	return res, nil
}

// usageReport is the tolerant view of --usage-file JSON; unknown fields ignored,
// field names kept loose since the CLI's report shape is a versioned contract.
// (v0.20.0 writes estimated_cost_usd, token counts, model, provider,
// session_id, completed, failed — hermes_cli/oneshot.py.)
type usageReport struct {
	CostUSD       float64 `json:"cost_usd"`
	EstimatedCost float64 `json:"estimated_cost_usd"`
	Model         string  `json:"model"`
	SessionID     string  `json:"session_id"`
}

// usd picks the reported cost: an exact figure wins over the estimate.
func (u usageReport) usd() float64 {
	if u.CostUSD != 0 {
		return u.CostUSD
	}
	return u.EstimatedCost
}

// parseUsage reads the report; a missing or malformed file is the zero report.
func parseUsage(path string) usageReport {
	b, err := os.ReadFile(path)
	if err != nil {
		return usageReport{}
	}
	var u usageReport
	if json.Unmarshal(b, &u) != nil {
		return usageReport{}
	}
	return u
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
