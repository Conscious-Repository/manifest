package server

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const codexCacheSample = `{"models":[
 {"slug":"gpt-6-astra","display_name":"GPT-6 Astra","description":"Frontier coding","visibility":"list","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low","description":"fast"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}],"context_window":400000},
 {"slug":"gpt-reserve","visibility":"hide"},
 {"slug":"gpt-5.6-luna","display_name":"GPT-5.6 Luna","visibility":"list","supported_reasoning_levels":[{"effort":"minimal"},{"effort":"low"}]}]}`

// The catalog names every model, effort and permission each agent accepts, in
// the agent's own terms, and says how a change reaches a running session.
func TestChatModelCatalogPerAgent(t *testing.T) {
	codex := codexCatalog([]byte(codexCacheSample))
	ids := []string{}
	for _, m := range codex.Models {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "gpt-6-astra,gpt-5.6-luna,gpt-5.5" {
		t.Fatalf("codex models (hidden omitted, policy kept): %v", ids)
	}
	if codex.Models[0].DefaultEffort != "medium" || len(codex.Models[0].Efforts) != 4 || codex.Models[0].Context != 400000 || codex.Models[0].Efforts[0].Description != "fast" {
		t.Fatalf("codex model detail lost: %+v", codex.Models[0])
	}
	if got := strings.Join(effortIDs(codex.Efforts), ","); got != "low,medium,high,xhigh,minimal" {
		t.Fatalf("codex effort union: %s", got)
	}
	if codex.DefaultPermission != "full" || codex.LiveModel != "native-picker" {
		t.Fatalf("codex must default to the access it always launched with and say changes go through its own picker: %+v", codex)
	}
	claude := claudeCatalog()
	if claude.LiveModel != "command" || claude.LivePermission != "launch" || len(claude.Permissions) != 6 || claude.DefaultPermission != "" {
		t.Fatalf("claude catalog: %+v", claude)
	}
	for _, p := range claude.Permissions {
		if p.ID == "bypassPermissions" && !p.Danger {
			t.Fatal("bypass must be marked dangerous")
		}
	}
	config := []byte("model:\n  default: gpt-5.6-luna\ncustom_providers:\n  - name: lab-sparks\n    base_url: http://192.168.87.11:8000/v1\n    model: ds\nother:\n  - name: nope\n    base_url: http://x/\n")
	cache := []byte(`{"openai-codex":{"models":["gpt-5.6-luna","gpt-5.5"]},"custom:http://192.168.87.11:8000/v1":{"models":["deepseek-v4.1-flash"]},"xai-oauth":{"models":["grok-4.6","grok-imagine-video"]},"custom:http://10.0.0.9/v1":{"models":["local"]}}`)
	hermes := hermesCatalog(cache, config, "gpt-5.6-luna")
	byID := map[string]chatModelOption{}
	for _, m := range hermes.Models {
		byID[m.Provider+"/"+m.ID] = m
	}
	if _, ok := byID["lab-sparks/deepseek-v4.1-flash"]; !ok {
		t.Fatalf("a custom endpoint named in custom_providers is addressed by that name: %v", byID)
	}
	if _, ok := byID["custom/local"]; !ok {
		t.Fatalf("an unnamed custom endpoint falls back to provider custom: %v", byID)
	}
	if _, ok := byID["xai-oauth/grok-imagine-video"]; ok {
		t.Fatal("media generators are not chat models")
	}
	if hermes.DefaultProvider != "openai-codex" || hermes.LiveModel != "recipient" {
		t.Fatalf("hermes default provider: %+v", hermes)
	}
	if byID["xai-oauth/grok-4.6"].Description != "xAI" {
		t.Fatalf("provider label: %+v", byID["xai-oauth/grok-4.6"])
	}
	if err := codingSettingsError("claude", "turbo", ""); err == nil {
		t.Fatal("unknown Claude effort accepted")
	}
	if err := codingSettingsError("claude", "max", "plan"); err != nil {
		t.Fatal(err)
	}
	if err := codingSettingsError("shell", "high", ""); err == nil {
		t.Fatal("a shell has no effort")
	}
}

// Effort and permission ride every launch, including a resume; unset keeps
// the CLI's own default, and Codex keeps the full access it always had.
func TestCodingLaunchCarriesEffortAndPermission(t *testing.T) {
	c := termSession{Kind: "claude", ResumeID: "11111111-2222-3333-4444-555555555555", Model: "opus", Effort: "max", Permission: "plan"}
	if got := c.launchCmd(); got != "claude --session-id 11111111-2222-3333-4444-555555555555 --model 'opus' --effort 'max' --permission-mode 'plan'" {
		t.Fatalf("claude launch: %s", got)
	}
	c.Started = true
	if got := c.launchCmd(); !strings.HasPrefix(got, "claude --resume ") || !strings.HasSuffix(got, "--effort 'max' --permission-mode 'plan'") {
		t.Fatalf("claude relaunch lost settings: %s", got)
	}
	x := termSession{Kind: "codex"}
	if got := x.launchCmd(); got != "codex --yolo" {
		t.Fatalf("codex default changed: %s", got)
	}
	x = termSession{Kind: "codex", Model: "gpt-6-astra", Effort: "high", Permission: "auto", ResumeID: "11111111-2222-3333-4444-555555555555"}
	if got := x.launchCmd(); got != `codex resume --sandbox workspace-write --ask-for-approval on-request 11111111-2222-3333-4444-555555555555 -m 'gpt-6-astra' -c 'model_reasoning_effort="high"'` {
		t.Fatalf("codex launch: %s", got)
	}
	x.Permission = "read-only"
	if !strings.Contains(x.launchCmd(), "--sandbox read-only --ask-for-approval on-request") {
		t.Fatalf("codex read-only: %s", x.launchCmd())
	}
}

// A draft's launch settings may change until the first message launches it;
// the launch uses them; afterwards the route refuses in words (409).
func TestDraftLaunchSettingsApplyOnceThenFreeze(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var mu sync.Mutex
	var launched []string
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		if text, _ := r.Params["text"].(string); text != "" {
			mu.Lock()
			launched = append(launched, strings.ReplaceAll(text, `'\''`, `'`))
			mu.Unlock()
		}
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input", "agent.prompt":
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	w := httptest.NewRecorder()
	s.handleTermCreate(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"kind":"claude","draft":true,"effort":"low","permission":"acceptEdits"}`)))
	var se termSession
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &se) != nil || se.Effort != "low" || se.Permission != "acceptEdits" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	update := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PATCH", "/", strings.NewReader(body))
		r.SetPathValue("id", se.ID)
		s.handleTermUpdate(w, r)
		return w
	}
	if w := update(`{"effort":"turbo"}`); w.Code != 400 {
		t.Fatalf("unknown effort: %d", w.Code)
	}
	if w := update(`{"model":"not-a-model"}`); w.Code != 400 {
		t.Fatalf("unknown model: %d", w.Code)
	}
	if w := update(`{"model":"haiku","effort":"max","permission":"plan"}`); w.Code != 200 {
		t.Fatalf("draft update: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"text":"Start"}`))
	r.SetPathValue("id", se.ID)
	s.handleTermInput(w, r)
	if w.Code != 200 {
		t.Fatalf("first message: %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	joined := strings.Join(launched, "\n")
	mu.Unlock()
	if !strings.Contains(joined, "--model 'haiku' --effort 'max' --permission-mode 'plan'") {
		t.Fatalf("launch did not carry the draft's settings:\n%s", joined)
	}
	if w := update(`{"effort":"low"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "agent's own command") {
		t.Fatalf("started session settings must be refused in words: %d %s", w.Code, w.Body.String())
	}
	if w := update(`{"name":"Renamed"}`); w.Code != 200 {
		t.Fatalf("renaming a started session still works: %d", w.Code)
	}
}

// The transcript says what the session actually ran with — the latest model,
// effort and permission the CLI recorded — never a guessed default.
func TestTranscriptObservedSettings(t *testing.T) {
	claude := strings.Join([]string{
		`{"type":"permission-mode","permissionMode":"auto"}`,
		`{"type":"user","timestamp":"2026-09-26T10:00:00Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","timestamp":"2026-09-26T10:00:01Z","effort":"high","message":{"role":"assistant","model":"claude-fable-5-1","stop_reason":"end_turn","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"permission-mode","permissionMode":"plan","timestamp":"2026-09-26T10:01:00Z"}`,
	}, "\n") + "\n"
	tr := parseClaudeTranscript(strings.NewReader(claude))
	if tr.Settings == nil || tr.Settings.Model != "claude-fable-5-1" || tr.Settings.Effort != "high" || tr.Settings.Permission != "plan" {
		t.Fatalf("claude settings: %+v", tr.Settings)
	}
	codex := `{"timestamp":"2026-09-26T10:00:00Z","type":"turn_context","payload":{"model":"gpt-6-astra","approval_policy":"never","sandbox_policy":{"type":"danger-full-access"},"collaboration_mode":{"settings":{"reasoning_effort":"high"}}}}` + "\n"
	tr = parseCodexTranscript(strings.NewReader(codex))
	if tr.Settings == nil || tr.Settings.Model != "gpt-6-astra" || tr.Settings.Effort != "high" || tr.Settings.Permission != "full" {
		t.Fatalf("codex settings: %+v", tr.Settings)
	}
	if tr := parseClaudeTranscript(strings.NewReader(`{"type":"user","message":{"role":"user","content":"x"}}` + "\n")); tr.Settings != nil {
		t.Fatalf("no record must mean no settings, not defaults: %+v", tr.Settings)
	}
	if got := codexPermission("untrusted", "workspace-write"); got != "workspace-write · untrusted" {
		t.Fatalf("unmapped pair should read raw: %s", got)
	}
}

// A native agent's provider and effort are validated against the catalog
// before anything is recorded.
func TestHermesChoiceRefusesUnlistedPairs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "provider_models_cache.json"), []byte(`{"anthropic":{"models":["claude-fable-5"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	if err := s.hermesChoiceError("claude-fable-5", "anthropic", "high"); err != nil {
		t.Fatal(err)
	}
	if err := s.hermesChoiceError("gpt-5.5", "anthropic", ""); err == nil {
		t.Fatal("a model the provider does not list was accepted")
	}
	if err := s.hermesChoiceError("claude-fable-5", "anthropic", "warp"); err == nil {
		t.Fatal("unknown effort accepted")
	}
	if err := s.hermesChoiceError("", "anthropic", ""); err == nil {
		t.Fatal("provider without model accepted")
	}
}

// The live status line reads only what the CLI recorded: an assistant turn's
// last record time ("Worked for") and the last call's context — with the
// window when the CLI names it (Codex), without it when it does not (Claude).
func TestTranscriptTurnEndAndContext(t *testing.T) {
	claude := strings.Join([]string{
		`{"type":"user","timestamp":"2026-09-26T10:00:00Z","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","timestamp":"2026-09-26T10:00:05Z","message":{"role":"assistant","model":"claude-fable-5-1","usage":{"input_tokens":32,"cache_creation_input_tokens":1617,"cache_read_input_tokens":874952},"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test"}}]}}`,
		`{"type":"user","timestamp":"2026-09-26T10:04:12Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","timestamp":"2026-09-26T10:04:17Z","message":{"role":"assistant","model":"claude-fable-5-1","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`,
	}, "\n") + "\n"
	tr := parseClaudeTranscript(strings.NewReader(claude))
	last := tr.Turns[len(tr.Turns)-1]
	if last.Who != "assistant" || last.TS != "2026-09-26T10:00:05Z" || last.End != "2026-09-26T10:04:17Z" {
		t.Fatalf("assistant turn span: %+v", last)
	}
	if tr.Context == nil || tr.Context.Used != 32+1617+874952 || tr.Context.Window != 0 {
		t.Fatalf("claude context (no window recorded): %+v", tr.Context)
	}
	codex := strings.Join([]string{
		`{"timestamp":"2026-09-26T10:00:00Z","type":"event_msg","payload":{"type":"token_count","info":null}}`,
		`{"timestamp":"2026-09-26T10:00:09Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":81709},"model_context_window":258400}}}`,
	}, "\n") + "\n"
	tr = parseCodexTranscript(strings.NewReader(codex))
	if tr.Context == nil || tr.Context.Used != 81709 || tr.Context.Window != 258400 {
		t.Fatalf("codex context: %+v", tr.Context)
	}
}
