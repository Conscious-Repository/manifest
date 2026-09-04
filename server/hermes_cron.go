package server

// Alfred (Hermes) on the board — agents plan Phase 4.
//
// Every read here is a PROJECTION of the owner's ~/.hermes/cron tree (D4 =
// files first): jobs.json for the schedule, usage_audit.jsonl + output/<job>/
// <ts>.md for the fires, plus manifest's own ledger `run.*` lines for the
// in-process Hermes turns (digs, delegations). Nothing is cached or stored.
// The graceful-degrade rule is absolute: a missing or reshaped file yields
// `outcome: unknown` (or an empty list with a `why`), never an error — the
// page must render with usage_audit.jsonl renamed away. `hermes cron list`
// is the fallback for the job list ONLY when jobs.json is missing.
//
// The writes (D5 = yes) are the three real controls — pause / resume / run —
// and they go through the `hermes` CLI exclusively: manifest never edits
// jobs.json. The verbatim command is returned so the row can show it.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// hermesJob is one cron job as the board needs it: identity, schedule, the
// model pin (empty = unpinned → the --warn chip; the 08-30..09-01 skips were
// exactly an unpinned job failing closed on drift), state and last outcome.
type hermesJob struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Schedule      string   `json:"schedule"`      // cron expr, or the display for once/interval kinds
	ScheduleKind  string   `json:"scheduleKind"`  // cron | once | interval | "" (unknown)
	ScheduleHuman string   `json:"scheduleHuman"` // Hermes' own display string
	Deliver       string   `json:"deliver"`
	Skills        []string `json:"skills"`
	Model         string   `json:"model"`         // the pin; "" = unpinned
	ModelSnapshot string   `json:"modelSnapshot"` // what the last fire actually used, when recorded
	Provider      string   `json:"provider"`
	Enabled       bool     `json:"enabled"`
	State         string   `json:"state"` // scheduled | paused | completed | … (Hermes' word)
	NextRunAt     string   `json:"nextRunAt"`
	LastRunAt     string   `json:"lastRunAt"`
	LastStatus    string   `json:"lastStatus"` // ok | error | "" (never ran)
	LastError     string   `json:"lastError"`
	PausedReason  string   `json:"pausedReason"`
	Prompt        string   `json:"prompt"` // first 200 chars, for the tooltip
	RepeatDone    int      `json:"repeatCompleted"`
	RepeatTimes   *int     `json:"repeatTimes"` // nil = ∞
	Outcome       string   `json:"outcome"`     // "" | unknown (a reshaped entry)
}

// hermesFire is one row of the RUNS log for the alfred runtime: a cron fire
// (usage_audit joined to its output file), or a ledger run.* line.
type hermesFire struct {
	ID               string   `json:"id"`
	Runtime          string   `json:"runtime"` // always "alfred"
	Job              string   `json:"job"`     // job id ("" for ledger turns)
	JobName          string   `json:"jobName"`
	Started          string   `json:"started"` // RFC3339
	Finished         string   `json:"finished,omitempty"`
	Outcome          string   `json:"outcome"` // completed | error | unknown
	Why              string   `json:"why"`     // first line of the Error block / Response / last_error
	Tokens           *int     `json:"tokens"`  // nil = unknown (no usage line)
	PromptTokens     *int     `json:"promptTokens,omitempty"`
	CompletionTokens *int     `json:"completionTokens,omitempty"`
	DurationMs       *int     `json:"durationMs"` // nil = unknown
	Model            string   `json:"model"`
	USD              *float64 `json:"usd"` // nil = the chargebook does not price this model
	File             string   `json:"file,omitempty"`
	Source           string   `json:"source"` // usage+output | output | usage | ledger
	ItemsWritten     *int     `json:"itemsWritten,omitempty"`
}

var (
	hermesJobIDRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)
	hermesFileStem  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$`)
	hermesCronBlock = regexp.MustCompile(`^\s*([A-Za-z0-9_-]{4,64})\s+\[(active|paused|[a-z]+)\]\s*$`)
)

// ---- jobs ----

// hermesJobs reads cron/jobs.json (any of the shapes Hermes has used: {jobs:
// [...]}, a bare list, or a dict keyed by id) and degrades to `hermes cron
// list` only when the file is absent. Returns the list, its source word and
// a why when the list is unknown.
func (s *Server) hermesJobs(ctx context.Context) (jobs []hermesJob, source, why string) {
	path := filepath.Join(s.hermesHome(), "cron", "jobs.json")
	b, err := os.ReadFile(path)
	if err == nil {
		jobs, perr := parseHermesJobsJSON(b)
		if perr == nil {
			return jobs, "files", ""
		}
		why = "jobs.json: " + perr.Error()
	} else {
		why = "jobs.json: " + err.Error()
	}
	// fallback (D4): the CLI's structured list
	text, cerr := hermesCronList(ctx, s.hermesBin(), s.hermesEnv())
	if cerr != nil {
		return []hermesJob{}, "unknown", why + "; hermes cron list: " + cerr.Error()
	}
	return parseHermesCronList(text), "cli", ""
}

// parseHermesJobsJSON tolerates the file's shape and every entry's keys.
func parseHermesJobsJSON(b []byte) ([]hermesJob, error) {
	var top any
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, err
	}
	var raw []any
	switch v := top.(type) {
	case []any:
		raw = v
	case map[string]any:
		if l, ok := v["jobs"].([]any); ok {
			raw = l
		} else if m, ok := v["jobs"].(map[string]any); ok {
			raw = hermesDictJobs(m)
		} else {
			raw = hermesDictJobs(v)
		}
	default:
		return nil, errors.New("unexpected shape")
	}
	out := make([]hermesJob, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, hermesJobFromMap(m))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// hermesDictJobs turns {<id>: {...}} into a list, stamping the key as the id
// when the entry lacks one. Non-object values (updated_at etc.) are skipped.
func hermesDictJobs(m map[string]any) []any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []any
	for _, k := range keys {
		j, ok := m[k].(map[string]any)
		if !ok {
			continue
		}
		if _, has := j["id"]; !has {
			j["id"] = k
		}
		out = append(out, j)
	}
	return out
}

// mget: a case-insensitive, first-hit lookup across key spellings.
func mget(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	for _, k := range keys {
		for mk, v := range m {
			if v != nil && strings.EqualFold(mk, k) {
				return v
			}
		}
	}
	return nil
}
func mstr(m map[string]any, keys ...string) string {
	switch v := mget(m, keys...).(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}
func mbool(m map[string]any, def bool, keys ...string) bool {
	switch v := mget(m, keys...).(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
func mint(m map[string]any, keys ...string) (int, bool) {
	switch v := mget(m, keys...).(type) {
	case float64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n, true
		}
	}
	return 0, false
}

func hermesJobFromMap(m map[string]any) hermesJob {
	j := hermesJob{
		ID:            mstr(m, "id", "job_id"),
		Name:          mstr(m, "name", "title"),
		Deliver:       mstr(m, "deliver", "deliver_target", "delivery"),
		Model:         mstr(m, "model", "model_pin"),
		ModelSnapshot: mstr(m, "model_snapshot"),
		Provider:      mstr(m, "provider"),
		State:         mstr(m, "state", "status"),
		NextRunAt:     mstr(m, "next_run_at", "next_run", "next"),
		LastRunAt:     mstr(m, "last_run_at", "last_run"),
		LastStatus:    mstr(m, "last_status"),
		LastError:     mstr(m, "last_error"),
		PausedReason:  mstr(m, "paused_reason"),
		Skills:        []string{},
	}
	if j.LastError == "" {
		j.LastError = mstr(m, "last_delivery_error")
	}
	j.Enabled = mbool(m, true, "enabled")
	if strings.EqualFold(j.State, "paused") {
		j.Enabled = false
	}
	// schedule: an object {kind, expr, display, run_at} or a bare string
	switch sc := mget(m, "schedule", "cron").(type) {
	case string:
		j.Schedule, j.ScheduleKind = sc, "cron"
	case map[string]any:
		j.ScheduleKind = mstr(sc, "kind")
		j.Schedule = mstr(sc, "expr", "cron")
		j.ScheduleHuman = mstr(sc, "display")
		if j.Schedule == "" {
			j.Schedule = mstr(sc, "run_at", "every", "display")
		}
	}
	if j.ScheduleHuman == "" {
		j.ScheduleHuman = mstr(m, "schedule_display")
	}
	if j.ScheduleHuman == "" {
		j.ScheduleHuman = j.Schedule
	}
	if j.ScheduleKind == "" && j.Schedule != "" && len(strings.Fields(j.Schedule)) == 5 {
		j.ScheduleKind = "cron"
	}
	switch sk := mget(m, "skills").(type) {
	case []any:
		for _, x := range sk {
			if str, ok := x.(string); ok && str != "" {
				j.Skills = append(j.Skills, str)
			}
		}
	case string:
		for _, p := range strings.Split(sk, ",") {
			if t := strings.TrimSpace(p); t != "" {
				j.Skills = append(j.Skills, t)
			}
		}
	}
	if len(j.Skills) == 0 {
		if sk := mstr(m, "skill"); sk != "" {
			j.Skills = []string{sk}
		}
	}
	if rp, ok := mget(m, "repeat").(map[string]any); ok {
		j.RepeatDone, _ = mint(rp, "completed", "done")
		if n, ok := mint(rp, "times", "count"); ok {
			j.RepeatTimes = &n
		}
	}
	if p := mstr(m, "prompt", "message"); p != "" {
		j.Prompt = snipRunes(p, 200)
	}
	if j.ID == "" || j.Name == "" {
		j.Outcome = "unknown" // a reshaped entry — shown, never trusted
		if j.Name == "" {
			j.Name = j.ID
		}
	}
	return j
}

func snipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// hermesCronList runs `hermes cron list --all` (read-only, bounded). --all
// keeps the paused jobs in — the plain list hides them, and the board's
// Paused group needs them.
func hermesCronList(ctx context.Context, bin string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "cron", "list", "--all")
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// parseHermesCronList reads the CLI's block format:
//
//	<id> [active|paused]
//	  Name:      …
//	  Schedule:  …
//	  Next run:  …
//	  Deliver:   …
//	  Skills:    a, b
//	  Last run:  <ts>  ok|error
func parseHermesCronList(text string) []hermesJob {
	out := []hermesJob{}
	var cur *hermesJob
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if m := hermesCronBlock.FindStringSubmatch(line); m != nil {
			flush()
			cur = &hermesJob{ID: m[1], Enabled: m[2] == "active", State: m[2], Skills: []string{}}
			continue
		}
		if cur == nil {
			continue
		}
		t := strings.TrimSpace(line)
		i := strings.Index(t, ":")
		if i <= 0 {
			continue
		}
		key, val := strings.ToLower(strings.TrimSpace(t[:i])), strings.TrimSpace(t[i+1:])
		switch key {
		case "name":
			cur.Name = val
		case "schedule":
			cur.Schedule, cur.ScheduleHuman = val, val
			if len(strings.Fields(val)) == 5 {
				cur.ScheduleKind = "cron"
			}
		case "next run":
			if val != "—" && val != "-" {
				cur.NextRunAt = val
			}
		case "deliver":
			cur.Deliver = val
		case "skills":
			for _, p := range strings.Split(val, ",") {
				if s := strings.TrimSpace(p); s != "" && s != "—" {
					cur.Skills = append(cur.Skills, s)
				}
			}
		case "model":
			if val != "—" && val != "-" {
				cur.Model = val
			}
		case "last run":
			f := strings.Fields(val)
			if len(f) >= 1 && f[0] != "never" && f[0] != "—" {
				cur.LastRunAt = f[0]
			}
			if len(f) >= 2 {
				cur.LastStatus = f[1]
			}
		case "repeat":
			if val != "∞" {
				if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					cur.RepeatTimes = &n
				}
			}
		}
	}
	flush()
	return out
}

// ---- fires (RUNS) ----

// hermesSince parses ?since= — "7d" / "30d" / "all" / RFC3339; default 7d.
func hermesSince(q string) time.Time {
	q = strings.TrimSpace(q)
	switch q {
	case "", "7d":
		return time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		return time.Now().Add(-30 * 24 * time.Hour)
	case "all":
		return time.Time{}
	}
	if strings.HasSuffix(q, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(q, "d")); err == nil && n > 0 {
			return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
		}
	}
	if t, err := time.Parse(time.RFC3339, q); err == nil {
		return t
	}
	return time.Now().Add(-7 * 24 * time.Hour)
}

type hermesUsageLine struct {
	TS               string `json:"ts"`
	JobID            string `json:"job_id"`
	FireID           string `json:"fire_id"`
	PromptTokens     *int   `json:"prompt_tokens"`
	CompletionTokens *int   `json:"completion_tokens"`
	TotalTokens      *int   `json:"total_tokens"`
	Model            string `json:"model"`
	DurationMs       *int   `json:"duration_ms"`
	Error            string `json:"error"`
	Deliver          string `json:"deliver_target"`
}

// hermesFires joins usage_audit.jsonl to output/<job>/*.md (by job id and a
// ±2 min timestamp match), then appends the ledger's run.* lines for hermes
// turns. `notes` names what degraded (a missing file) so the page can say so.
func (s *Server) hermesFires(since time.Time, jobsByID map[string]hermesJob) (fires []hermesFire, notes []string) {
	return s.hermesFiresIn(s.hermesHome(), since, jobsByID, true)
}

// hermesFiresIn is hermesFires over an explicit Hermes state root — a
// profile's own ~/.hermes/profiles/<name> tree (Phase 5) reads the same way.
// withLedger adds manifest's in-process turns, which belong to the default
// profile only (the runner targets it unless a Request names a Profile).
func (s *Server) hermesFiresIn(home string, since time.Time, jobsByID map[string]hermesJob, withLedger bool) (fires []hermesFire, notes []string) {
	cronDir := filepath.Join(home, "cron")
	fires = []hermesFire{}

	// 1. the output files — one per fire, the durable narration
	type outFile struct {
		job, stem string
		at        time.Time
	}
	var outs []outFile
	if ents, err := os.ReadDir(filepath.Join(cronDir, "output")); err == nil {
		for _, e := range ents {
			if !e.IsDir() || !hermesJobIDRe.MatchString(e.Name()) {
				continue
			}
			files, ferr := os.ReadDir(filepath.Join(cronDir, "output", e.Name()))
			if ferr != nil {
				continue
			}
			for _, f := range files {
				stem := strings.TrimSuffix(f.Name(), ".md")
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") || !hermesFileStem.MatchString(stem) {
					continue
				}
				at, perr := time.ParseInLocation("2006-01-02_15-04-05", stem, time.Local)
				if perr != nil || (!since.IsZero() && at.Before(since)) {
					continue
				}
				outs = append(outs, outFile{job: e.Name(), stem: stem, at: at})
			}
		}
	} else {
		notes = append(notes, "output/: "+err.Error())
	}
	sort.Slice(outs, func(i, j int) bool { return outs[i].at.After(outs[j].at) })
	if len(outs) > 400 {
		outs = outs[:400]
	}

	// 2. the usage audit — tokens / duration / error per fire
	var usage []hermesUsageLine
	usageOK := false
	if f, err := os.Open(filepath.Join(cronDir, "usage_audit.jsonl")); err == nil {
		usageOK = true
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var u hermesUsageLine
			if json.Unmarshal(sc.Bytes(), &u) != nil || u.TS == "" {
				continue
			}
			if t, terr := time.Parse(time.RFC3339Nano, u.TS); terr == nil && !since.IsZero() && t.Before(since) {
				continue
			}
			usage = append(usage, u)
		}
		f.Close()
	} else {
		notes = append(notes, "usage_audit.jsonl: "+err.Error()+" — tokens and duration unknown")
	}
	usedLine := make([]bool, len(usage))
	findUsage := func(job string, at time.Time) int {
		best, bestD := -1, 3*time.Minute
		for i, u := range usage {
			if usedLine[i] || u.JobID != job {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, u.TS)
			if err != nil {
				continue
			}
			d := t.Sub(at)
			if d < 0 {
				d = -d
			}
			if d < bestD {
				best, bestD = i, d
			}
		}
		return best
	}

	for _, o := range outs {
		fire := hermesFire{
			ID: o.job + "/" + o.stem, Runtime: "alfred", Job: o.job, JobName: jobsByID[o.job].Name,
			Started: o.at.UTC().Format(time.RFC3339), File: o.stem, Source: "output", Outcome: "unknown",
		}
		if fire.JobName == "" {
			fire.JobName = o.job
		}
		if md, err := os.ReadFile(filepath.Join(cronDir, "output", o.job, o.stem+".md")); err == nil {
			name, outcome, why := parseHermesOutput(string(md))
			if name != "" && jobsByID[o.job].Name == "" {
				fire.JobName = name
			}
			fire.Outcome, fire.Why = outcome, why
		}
		if i := findUsage(o.job, o.at); i >= 0 {
			usedLine[i] = true
			s.applyUsage(&fire, usage[i])
			fire.Source = "usage+output"
			if fire.Outcome == "unknown" {
				fire.Outcome = "completed"
				if usage[i].Error != "" {
					fire.Outcome = "error"
				}
			}
		} else if !usageOK {
			fire.Source = "output" // tokens unknown — the audit file is gone
		}
		fires = append(fires, fire)
	}
	// usage lines with no output file (a fire whose narration was pruned)
	for i, u := range usage {
		if usedLine[i] {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, u.TS)
		if err != nil {
			continue
		}
		fire := hermesFire{
			ID: u.JobID + "/" + u.FireID, Runtime: "alfred", Job: u.JobID, JobName: jobsByID[u.JobID].Name,
			Started: t.UTC().Format(time.RFC3339), Source: "usage", Outcome: "completed",
		}
		if fire.JobName == "" {
			fire.JobName = u.JobID
		}
		s.applyUsage(&fire, u)
		if u.Error != "" {
			fire.Outcome, fire.Why = "error", firstLine(u.Error, 300)
		}
		fires = append(fires, fire)
	}

	// 3. manifest's own ledger: run.* lines for in-process hermes turns
	if withLedger && s.ledgerStore != nil {
		for _, day := range s.ledgerStore.Days() {
			if !since.IsZero() && day < since.Add(-24*time.Hour).Format("2006-01-02") {
				continue
			}
			entries, err := s.ledgerStore.Day(day)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.Source != "run" || e.Harness != "hermes" {
					continue
				}
				if !since.IsZero() && e.TS.Before(since) {
					continue
				}
				fire := hermesFire{
					ID:      "ledger/" + day + "/" + e.TS.UTC().Format("150405") + "/" + strings.TrimPrefix(e.Kind, "run."),
					Runtime: "alfred", JobName: strings.TrimPrefix(e.Actor, "agent:") + " · turn",
					Started: e.TS.UTC().Format(time.RFC3339), Why: firstLine(e.Text, 300), Source: "ledger",
					Outcome: "completed",
				}
				if e.Kind == "run.failed" {
					fire.Outcome = "error"
				}
				if e.Meta != nil {
					if v, ok := e.Meta["model"].(string); ok {
						fire.Model = v
					}
					if v, ok := e.Meta["itemsWritten"].(float64); ok {
						n := int(v)
						fire.ItemsWritten = &n
					}
					if v, ok := e.Meta["spentUsd"].(float64); ok && v > 0 {
						fire.USD = &v
					}
				}
				fires = append(fires, fire)
			}
		}
	}
	sort.SliceStable(fires, func(i, j int) bool { return fires[i].Started > fires[j].Started })
	return fires, notes
}

// applyUsage copies the audit line's figures onto the fire and prices the
// tokens when the chargebook knows the model (else USD stays nil → tokens).
func (s *Server) applyUsage(f *hermesFire, u hermesUsageLine) {
	f.Tokens, f.PromptTokens, f.CompletionTokens, f.DurationMs = u.TotalTokens, u.PromptTokens, u.CompletionTokens, u.DurationMs
	if f.Model == "" {
		f.Model = u.Model
	}
	if f.Why == "" && u.Error != "" {
		f.Why = firstLine(u.Error, 300)
	}
	if u.DurationMs != nil && f.Started != "" {
		if st, err := time.Parse(time.RFC3339, f.Started); err == nil {
			f.Finished = st.Add(time.Duration(*u.DurationMs) * time.Millisecond).UTC().Format(time.RFC3339)
		}
	}
	if s.spirits != nil && u.Model != "" {
		if in, out, ok := s.spirits.ModelPrice(u.Model); ok {
			pt, ct := 0, 0
			if u.PromptTokens != nil {
				pt = *u.PromptTokens
			}
			if u.CompletionTokens != nil {
				ct = *u.CompletionTokens
			}
			usd := float64(pt)/1e6*in + float64(ct)/1e6*out
			f.USD = &usd
		}
	}
}

// parseHermesOutput reads one output/<job>/<ts>.md: the title line names the
// job (and says FAILED), a `## Error` block carries the failure's first line,
// `## Response` the reply's. Anything else → unknown.
func parseHermesOutput(md string) (name, outcome, why string) {
	outcome = "unknown"
	lines := strings.Split(md, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# Cron Job:") {
		t := strings.TrimSpace(strings.TrimPrefix(lines[0], "# Cron Job:"))
		if strings.HasSuffix(t, "(FAILED)") {
			outcome = "error"
			t = strings.TrimSpace(strings.TrimSuffix(t, "(FAILED)"))
		} else {
			outcome = "completed"
		}
		name = t
	}
	section := func(h string) string {
		i := strings.Index(md, "\n"+h+"\n")
		if i < 0 {
			return ""
		}
		rest := md[i+len(h)+2:]
		if j := strings.Index(rest, "\n## "); j >= 0 {
			rest = rest[:j]
		}
		for _, ln := range strings.Split(rest, "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "```") {
				continue
			}
			return ln
		}
		return ""
	}
	if e := section("## Error"); e != "" {
		if outcome == "unknown" {
			outcome = "error"
		}
		return name, outcome, e
	}
	return name, outcome, section("## Response")
}

// hermesEnv: the child's environment, with HERMES_HOME pinned to what the
// projections read so a config.hermes.home box and the CLI agree.
func (s *Server) hermesEnv() []string {
	env := os.Environ()
	if s.hosts != nil && strings.TrimSpace(s.hosts.Hermes.Home) != "" {
		env = append(env, "HERMES_HOME="+s.hosts.Hermes.Home)
	}
	return env
}

// ---- handlers ----

// GET /api/agents/hermes/runs?since=7d|30d|all|RFC3339
func (s *Server) handleAgentsHermesRuns(w http.ResponseWriter, r *http.Request) {
	since := hermesSince(r.URL.Query().Get("since"))
	jobs, _, jwhy := s.hermesJobs(r.Context())
	byID := map[string]hermesJob{}
	for _, j := range jobs {
		byID[j.ID] = j
	}
	fires, notes := s.hermesFires(since, byID)
	if jwhy != "" {
		notes = append(notes, jwhy)
	}
	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, map[string]any{"data": fires, "degraded": notes, "since": since.UTC().Format(time.RFC3339)})
}

// GET /api/agents/hermes/run?job=<id>&file=<stem> → the fire's output file
// (the narration: prompt, response or error). Both parts are validated
// against a strict shape so the path can only land inside cron/output/.
func (s *Server) handleAgentsHermesRun(w http.ResponseWriter, r *http.Request) {
	job, stem := r.URL.Query().Get("job"), r.URL.Query().Get("file")
	if !hermesJobIDRe.MatchString(job) || !hermesFileStem.MatchString(stem) {
		http.Error(w, "bad job/file", http.StatusBadRequest)
		return
	}
	b, err := os.ReadFile(filepath.Join(s.hermesHome(), "cron", "output", job, stem+".md"))
	if err != nil {
		writeJSON(w, map[string]any{"id": job + "/" + stem, "body": "", "outcome": "unknown", "why": err.Error()})
		return
	}
	if len(b) > 512*1024 {
		b = append(b[:512*1024], []byte("\n… (truncated)")...)
	}
	name, outcome, why := parseHermesOutput(string(b))
	writeJSON(w, map[string]any{"id": job + "/" + stem, "jobName": name, "outcome": outcome, "why": why, "body": string(b)})
}

// POST /api/agents/hermes/job/{id}/{action} — the D5 controls. action ∈
// {pause, resume, run}; the job id must look like one. The verbatim command
// is echoed so the row tooltip and the toast can show exactly what ran.
func (s *Server) handleAgentsHermesJobAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	if !hermesJobIDRe.MatchString(id) {
		http.Error(w, "bad job id", http.StatusBadRequest)
		return
	}
	switch action {
	case "pause", "resume", "run":
	default:
		http.Error(w, "action must be pause, resume or run", http.StatusBadRequest)
		return
	}
	bin := s.hermesBin()
	command := fmt.Sprintf("%s cron %s %s", filepath.Base(bin), action, id)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "cron", action, id)
	cmd.Env = s.hermesEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := snipRunes(strings.TrimSpace(out.String()), 2000)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		writeJSON(w, map[string]any{"ok": false, "command": command, "output": text, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "command": command, "output": text})
}
