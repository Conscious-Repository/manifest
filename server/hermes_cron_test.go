package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jobs.json in the shape Hermes writes today ({jobs: [...]}) and the two
// other shapes the reader tolerates (a bare list, a dict keyed by id); a
// pinned job and an unpinned one; a paused once-job.
const hermesJobsFixture = `{"jobs": [
  {"id": "18bd7812832e", "name": "aion-domain-scout-daily", "enabled": true, "deliver": "local",
   "skills": ["aion-domain-scout", "aion-biosciences"], "model": "deepseek-v4-flash-vision-exp",
   "schedule": {"kind": "cron", "expr": "0 7 * * *", "display": "0 7 * * *"},
   "next_run_at": "2026-09-04T07:00:00+00:00", "last_run_at": "2026-09-03T07:09:32+00:00",
   "last_status": "ok", "last_error": null, "repeat": {"completed": 11, "times": null},
   "prompt": "Run the aion-domain-scout daily scan.", "state": "scheduled"},
  {"id": "021aba2ccdb5", "name": "AION recruiting Gmail send setup", "enabled": false, "deliver": "origin",
   "skills": [], "model": null, "model_snapshot": "deepseek-v4-flash-vision-exp",
   "schedule": {"kind": "once", "display": "once at 2026-09-03 09:00", "run_at": "2026-09-03T09:00:00+00:00"},
   "next_run_at": null, "last_status": "ok", "repeat": {"completed": 1, "times": 1}, "state": "completed"}
], "updated_at": "2026-09-03T09:00:50+00:00"}`

func TestParseHermesJobsJSONShapes(t *testing.T) {
	jobs, err := parseHermesJobsJSON([]byte(hermesJobsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d", len(jobs))
	}
	var scout, once hermesJob
	for _, j := range jobs {
		switch j.ID {
		case "18bd7812832e":
			scout = j
		case "021aba2ccdb5":
			once = j
		}
	}
	if scout.Name != "aion-domain-scout-daily" || scout.Schedule != "0 7 * * *" || scout.ScheduleKind != "cron" {
		t.Fatalf("scout schedule: %+v", scout)
	}
	if !scout.Enabled || scout.Model != "deepseek-v4-flash-vision-exp" || scout.RepeatTimes != nil || scout.RepeatDone != 11 {
		t.Fatalf("scout pin/repeat: %+v", scout)
	}
	if len(scout.Skills) != 2 || scout.NextRunAt != "2026-09-04T07:00:00+00:00" || scout.LastStatus != "ok" {
		t.Fatalf("scout fields: %+v", scout)
	}
	if once.Enabled || once.Model != "" || once.ModelSnapshot == "" || once.ScheduleKind != "once" || once.RepeatTimes == nil || *once.RepeatTimes != 1 {
		t.Fatalf("once job: %+v", once)
	}
	if once.ScheduleHuman != "once at 2026-09-03 09:00" {
		t.Fatalf("once display: %q", once.ScheduleHuman)
	}

	// a dict keyed by id, no id inside, enabled as a string, cron as a string
	dict := `{"abc123": {"name": "x", "enabled": "false", "cron": "*/5 * * * *"}, "updated_at": "now"}`
	jobs, err = parseHermesJobsJSON([]byte(dict))
	if err != nil || len(jobs) != 1 {
		t.Fatalf("dict shape: %v %+v", err, jobs)
	}
	if jobs[0].ID != "abc123" || jobs[0].Enabled || jobs[0].Schedule != "*/5 * * * *" || jobs[0].ScheduleKind != "cron" {
		t.Fatalf("dict job: %+v", jobs[0])
	}
	// a bare list with a reshaped entry (no id) → shown as outcome unknown
	jobs, err = parseHermesJobsJSON([]byte(`[{"name": "nameless"}, 7]`))
	if err != nil || len(jobs) != 1 || jobs[0].Outcome != "unknown" {
		t.Fatalf("bare list: %v %+v", err, jobs)
	}
	if _, err := parseHermesJobsJSON([]byte(`"nope"`)); err == nil {
		t.Fatal("a string is not a jobs file")
	}
}

func TestParseHermesCronListBlocks(t *testing.T) {
	text := `
┌──────────────┐
│ Scheduled Jobs │
└──────────────┘

  18bd7812832e [active]
    Name:      aion-domain-scout-daily
    Schedule:  0 7 * * *
    Repeat:    ∞
    Next run:  2026-09-04T07:00:00+00:00
    Deliver:   local
    Skills:    aion-domain-scout, aion-biosciences
    Last run:  2026-09-03T07:09:32.908807+00:00  ok
    Execution: completed  b9823ba0c5964fce97c8d4b8508f6181

  021aba2ccdb5 [paused]
    Name:      AION recruiting Gmail send setup
    Schedule:  once at 2026-09-03 09:00
    Repeat:    1
    Next run:  —
    Deliver:   origin
    Skills:    —
    Last run:  never
`
	jobs := parseHermesCronList(text)
	if len(jobs) != 2 {
		t.Fatalf("want 2 blocks, got %d: %+v", len(jobs), jobs)
	}
	a, b := jobs[0], jobs[1]
	if a.ID != "18bd7812832e" || !a.Enabled || a.Name != "aion-domain-scout-daily" || a.Schedule != "0 7 * * *" || a.ScheduleKind != "cron" {
		t.Fatalf("active block: %+v", a)
	}
	if a.NextRunAt != "2026-09-04T07:00:00+00:00" || a.LastStatus != "ok" || len(a.Skills) != 2 || a.RepeatTimes != nil {
		t.Fatalf("active fields: %+v", a)
	}
	if b.ID != "021aba2ccdb5" || b.Enabled || b.NextRunAt != "" || len(b.Skills) != 0 || b.LastRunAt != "" || b.RepeatTimes == nil {
		t.Fatalf("paused block: %+v", b)
	}
}

func TestParseHermesOutputSections(t *testing.T) {
	failed := "# Cron Job: aion-domain-scout-daily (FAILED)\n\n**Job ID:** x\n\n## Prompt\n\nstuff\n\n## Error\n\n```\nRuntimeError: Skipped to prevent unintended spend: global inference config drifted\n```\n"
	name, oc, why := parseHermesOutput(failed)
	if name != "aion-domain-scout-daily" || oc != "error" || !strings.HasPrefix(why, "RuntimeError: Skipped to prevent unintended spend") {
		t.Fatalf("failed: %q %q %q", name, oc, why)
	}
	ok := "# Cron Job: aion-domain-scout-daily\n\n## Prompt\n\nstuff\n\n## Response\n\nAll three feed items are written.\n\nmore\n"
	name, oc, why = parseHermesOutput(ok)
	if name != "aion-domain-scout-daily" || oc != "completed" || why != "All three feed items are written." {
		t.Fatalf("ok: %q %q %q", name, oc, why)
	}
	if _, oc, _ = parseHermesOutput("garbage"); oc != "unknown" {
		t.Fatalf("reshaped file should be unknown, got %q", oc)
	}
}

// writeHermesTree lays down a ~/.hermes/cron fixture: jobs.json, three
// output files (two ok, one drift skip with NO usage line — the skip made no
// inference call, exactly like 08-30..09-01), and the usage audit.
func writeHermesTree(t *testing.T, withUsage bool) (home string, now time.Time) {
	t.Helper()
	home = t.TempDir()
	cron := filepath.Join(home, "cron")
	out := filepath.Join(cron, "output", "18bd7812832e")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(cron, "jobs.json"), hermesJobsFixture)
	now = time.Now()
	stem := func(d time.Duration) string { return now.Add(-d).In(time.Local).Format("2006-01-02_15-04-05") }
	ts := func(d time.Duration) string { return now.Add(-d).UTC().Format(time.RFC3339Nano) }
	must(filepath.Join(out, stem(3*time.Hour)+".md"), "# Cron Job: aion-domain-scout-daily\n\n## Prompt\n\np\n\n## Response\n\nThree items written.\n")
	must(filepath.Join(out, stem(27*time.Hour)+".md"), "# Cron Job: aion-domain-scout-daily (FAILED)\n\n## Prompt\n\np\n\n## Error\n\n```\nRuntimeError: Skipped to prevent unintended spend: drift and this job is unpinned.\n```\n")
	must(filepath.Join(out, stem(51*time.Hour)+".md"), "# Cron Job: aion-domain-scout-daily\n\n## Response\n\nTwo items.\n")
	// an old fire outside the 7d window
	must(filepath.Join(out, stem(20*24*time.Hour)+".md"), "# Cron Job: aion-domain-scout-daily\n\n## Response\n\nold\n")
	if withUsage {
		lines := []string{
			`{"ts": "` + ts(3*time.Hour) + `", "job_id": "18bd7812832e", "fire_id": "f1", "prompt_tokens": 1000, "completion_tokens": 200, "total_tokens": 1200, "model": "deepseek-v4-flash-vision-exp", "duration_ms": 325925, "error": null}`,
			`{"ts": "` + ts(51*time.Hour) + `", "job_id": "18bd7812832e", "fire_id": "f3", "prompt_tokens": 500, "completion_tokens": 50, "total_tokens": 550, "model": "deepseek-v4-flash-vision-exp", "duration_ms": 1000, "error": null}`,
			`{"ts": "` + ts(60*time.Hour) + `", "job_id": "18bd7812832e", "fire_id": "f4", "total_tokens": 10, "model": "m", "duration_ms": 5, "error": "RuntimeError: Connection error.\nmore"}`,
			`not json at all`,
		}
		must(filepath.Join(cron, "usage_audit.jsonl"), strings.Join(lines, "\n")+"\n")
	}
	return home, now
}

func hermesTestServer(t *testing.T, home string) *Server {
	t.Helper()
	s := New(nil, nil, nil)
	var info HostsInfo
	info.Hermes.Home = home
	info.Hermes.Bin = "/nonexistent/hermes-bin-for-tests"
	s.UseHosts(info)
	return s
}

func TestHermesRunsJoinAndDegrade(t *testing.T) {
	home, _ := writeHermesTree(t, true)
	s := hermesTestServer(t, home)
	w := httptest.NewRecorder()
	s.handleAgentsHermesRuns(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes/runs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Data     []hermesFire `json:"data"`
		Degraded []string     `json:"degraded"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Degraded) != 0 {
		t.Fatalf("nothing should degrade: %v", res.Degraded)
	}
	// 3 output files in the window + 1 usage-only line (the pruned narration); the 20-day-old file is out
	if len(res.Data) != 4 {
		t.Fatalf("want 4 fires, got %d: %+v", len(res.Data), res.Data)
	}
	// newest first
	for i := 1; i < len(res.Data); i++ {
		if res.Data[i].Started > res.Data[i-1].Started {
			t.Fatalf("not newest-first: %v", res.Data)
		}
	}
	f := res.Data[0]
	if f.Runtime != "alfred" || f.JobName != "aion-domain-scout-daily" || f.Outcome != "completed" || f.Why != "Three items written." {
		t.Fatalf("joined fire: %+v", f)
	}
	if f.Tokens == nil || *f.Tokens != 1200 || f.DurationMs == nil || *f.DurationMs != 325925 || f.Source != "usage+output" || f.Finished == "" {
		t.Fatalf("usage join: %+v", f)
	}
	if f.USD != nil {
		t.Fatalf("no spirits store → the chargebook can't price it → usd must be nil, got %v", *f.USD)
	}
	skip := res.Data[1]
	if skip.Outcome != "error" || !strings.Contains(skip.Why, "unpinned") || skip.Tokens != nil || skip.Source != "output" {
		t.Fatalf("the drift skip (no usage line) should be error with the drift message and unknown tokens: %+v", skip)
	}
	only := res.Data[3]
	if only.Source != "usage" || only.Outcome != "error" || only.Why != "RuntimeError: Connection error." || only.Tokens == nil {
		t.Fatalf("usage-only fire: %+v", only)
	}
	// windowing: ?since=all pulls the 20-day-old file in
	w = httptest.NewRecorder()
	s.handleAgentsHermesRuns(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes/runs?since=all", nil))
	res.Data = nil
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Data) != 5 {
		t.Fatalf("since=all: want 5, got %d", len(res.Data))
	}
}

// usage_audit.jsonl renamed away: rows still render from the output files,
// tokens/duration are unknown (nil), the response names what degraded, and
// nothing errors (plan Phase 4 verify step).
func TestHermesRunsWithoutUsageAudit(t *testing.T) {
	home, _ := writeHermesTree(t, false)
	s := hermesTestServer(t, home)
	w := httptest.NewRecorder()
	s.handleAgentsHermesRuns(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes/runs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var res struct {
		Data     []hermesFire `json:"data"`
		Degraded []string     `json:"degraded"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Degraded) != 1 || !strings.Contains(res.Degraded[0], "usage_audit.jsonl") {
		t.Fatalf("degrade note missing: %v", res.Degraded)
	}
	if len(res.Data) != 3 {
		t.Fatalf("want the 3 output files, got %d", len(res.Data))
	}
	for _, f := range res.Data {
		if f.Tokens != nil || f.DurationMs != nil || f.Source != "output" {
			t.Fatalf("tokens must be unknown without the audit: %+v", f)
		}
	}
	// and with the whole cron dir gone: an empty list, still 200
	s = hermesTestServer(t, t.TempDir())
	w = httptest.NewRecorder()
	s.handleAgentsHermesRuns(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes/runs", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"data":[]`) {
		t.Fatalf("empty tree: %d %s", w.Code, w.Body.String())
	}
}

// The Alfred card projection: jobs list from files with the pin/enabled
// counts, heartbeat mtimes, and `outcome: unknown` (never an error) when the
// tree is missing. The profile shell-out fails here (no binary) and must
// degrade to an empty list, and nothing under ~/.hermes that is a secret
// (auth.json, .env) may appear in the body.
func TestAgentsHermesProjectionAndDegrade(t *testing.T) {
	home, _ := writeHermesTree(t, true)
	const secret = "tg-bot-token-MUST-NOT-LEAK"
	_ = os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"telegram":"`+secret+`"}`), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".env"), []byte("TELEGRAM_BOT_TOKEN="+secret+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(home, "cron", "ticker_heartbeat"), []byte("1"), 0o644)
	_ = os.WriteFile(filepath.Join(home, "gateway_state.json"), []byte(`{"gateway_state":"running","platforms":{"telegram":{"state":"connected"}}}`), 0o644)
	s := hermesTestServer(t, home)
	w := httptest.NewRecorder()
	s.handleAgentsHermes(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, secret) {
		t.Fatalf("secret leaked: %s", body)
	}
	var out struct {
		Cron struct {
			Jobs      int         `json:"jobs"`
			Enabled   int         `json:"enabled"`
			Unpinned  int         `json:"unpinned"`
			Source    string      `json:"source"`
			Heartbeat string      `json:"heartbeat"`
			List      []hermesJob `json:"list"`
		} `json:"cron"`
		Gateway  map[string]any  `json:"gateway"`
		Profiles []hermesProfile `json:"profiles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Cron.Jobs != 2 || out.Cron.Enabled != 1 || out.Cron.Unpinned != 0 || out.Cron.Source != "files" || out.Cron.Heartbeat == "" || len(out.Cron.List) != 2 {
		t.Fatalf("cron projection: %+v", out.Cron)
	}
	if out.Gateway["state"] != "running" {
		t.Fatalf("gateway: %v", out.Gateway)
	}
	if out.Profiles == nil {
		t.Fatalf("profiles must be [] when the CLI is missing: %s", body)
	}
	// missing tree → unknown, still 200
	s = hermesTestServer(t, filepath.Join(t.TempDir(), "nope"))
	w = httptest.NewRecorder()
	s.handleAgentsHermes(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"outcome":"unknown"`) || !strings.Contains(w.Body.String(), `"source":"unknown"`) {
		t.Fatalf("missing tree should be unknown: %d %s", w.Code, w.Body.String())
	}
}

// The fire body endpoint only reaches files whose job id and stem match the
// strict shapes — a traversal attempt is a 400, a missing file is unknown.
func TestHermesRunBodyValidation(t *testing.T) {
	home, now := writeHermesTree(t, true)
	s := hermesTestServer(t, home)
	stem := now.Add(-3 * time.Hour).In(time.Local).Format("2006-01-02_15-04-05")
	get := func(q string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handleAgentsHermesRun(w, httptest.NewRequest(http.MethodGet, "/api/agents/hermes/run?"+q, nil))
		return w
	}
	if w := get("job=18bd7812832e&file=" + stem); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Three items written.") {
		t.Fatalf("body: %d %s", w.Code, w.Body.String())
	}
	if w := get("job=../../auth&file=" + stem); w.Code != http.StatusBadRequest {
		t.Fatalf("traversal job must be 400, got %d", w.Code)
	}
	if w := get("job=18bd7812832e&file=../jobs"); w.Code != http.StatusBadRequest {
		t.Fatalf("traversal file must be 400, got %d", w.Code)
	}
	if w := get("job=18bd7812832e&file=2000-01-01_00-00-00"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"outcome":"unknown"`) {
		t.Fatalf("missing file should be unknown: %d %s", w.Code, w.Body.String())
	}
}

// The D5 controls validate before they shell out, and a failing binary is a
// 502 that carries the verbatim command (never a panic, never a jobs.json
// write).
func TestHermesJobActionValidation(t *testing.T) {
	home, _ := writeHermesTree(t, true)
	s := hermesTestServer(t, home)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/agents/hermes/job/{id}/{action}", s.handleAgentsHermesJobAction)
	post := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, p, nil))
		return w
	}
	if w := post("/api/agents/hermes/job/18bd7812832e/delete"); w.Code != http.StatusBadRequest {
		t.Fatalf("delete is not a control: %d", w.Code)
	}
	if w := post("/api/agents/hermes/job/x/pause"); w.Code != http.StatusBadRequest {
		t.Fatalf("a 1-char id is not a job: %d", w.Code)
	}
	w := post("/api/agents/hermes/job/18bd7812832e/pause")
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), `"command":"hermes-bin-for-tests cron pause 18bd7812832e"`) {
		t.Fatalf("missing binary should be a 502 with the command: %d %s", w.Code, w.Body.String())
	}
	before, _ := os.ReadFile(filepath.Join(home, "cron", "jobs.json"))
	if string(before) != hermesJobsFixture {
		t.Fatal("jobs.json must never be edited by manifest")
	}
}
