package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// One model catalog for every agent the chat surface can address: the coding
// CLIs (Codex, Claude Code) and each Hermes-backed native agent. It is
// metadata only — which models, efforts and permission modes an agent
// accepts, with the agent's own names — so the composer can offer one picker
// that behaves the same everywhere and applies choices in each agent's own
// terms (launch flags, the agent's own commands, or a per-message recipient).

type chatModelEffort struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

type chatModelOption struct {
	ID            string            `json:"id"`
	Label         string            `json:"label"`
	Provider      string            `json:"provider,omitempty"`
	Description   string            `json:"description,omitempty"`
	Context       int               `json:"context,omitempty"`
	Efforts       []chatModelEffort `json:"efforts,omitempty"`
	DefaultEffort string            `json:"defaultEffort,omitempty"`
	// LastRan is the model this alias resolved to the last time one of the
	// owner's sessions ran with it, as the CLI recorded it ("" = never seen).
	LastRan string `json:"lastRan,omitempty"`
}

type chatPermissionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Danger      bool   `json:"danger,omitempty"`
}

type chatAgentModels struct {
	Backend           string                 `json:"backend"`
	Default           string                 `json:"default,omitempty"`
	DefaultProvider   string                 `json:"defaultProvider,omitempty"`
	Models            []chatModelOption      `json:"models"`
	Efforts           []chatModelEffort      `json:"efforts,omitempty"`
	DefaultEffort     string                 `json:"defaultEffort,omitempty"`
	Permissions       []chatPermissionOption `json:"permissions,omitempty"`
	DefaultPermission string                 `json:"defaultPermission,omitempty"`
	// How a change reaches a running conversation, in the agent's own terms:
	// "recipient" (every message names its model), "command" (the agent's own
	// slash command, confirmed from its transcript), "native-picker" (the
	// agent's interactive picker in its terminal), "launch" (next launch only).
	LiveModel      string `json:"liveModel"`
	LiveEffort     string `json:"liveEffort"`
	LivePermission string `json:"livePermission,omitempty"`
}

var claudeEfforts = []chatModelEffort{{"low", "Fastest; light reasoning"}, {"medium", "Balanced"}, {"high", "Deeper reasoning"}, {"xhigh", "Extended reasoning"}, {"max", "Most thorough; slowest"}}

// Claude Code's own model aliases (claude --help, 2.1.283). Full model names
// are accepted by the CLI too; the aliases track the latest of each family.
var claudeModelAliases = []chatModelOption{
	{ID: "fable", Label: "Fable", Description: "Most capable Claude for complex, long-running work"},
	{ID: "opus", Label: "Opus", Description: "Strong reasoning and coding"},
	{ID: "sonnet", Label: "Sonnet", Description: "Fast, capable everyday coding"},
	{ID: "haiku", Label: "Haiku", Description: "Fastest and cheapest"},
	{ID: "opusplan", Label: "Opus plan", Description: "Opus while planning, Sonnet while executing"},
	// full model ids pin a version where an alias follows the family's latest
	{ID: "claude-fable-5-1", Label: "claude-fable-5-1", Description: "Pinned model id"},
	{ID: "claude-opus-5-5", Label: "claude-opus-5-5", Description: "Pinned model id"},
}

// Claude Code --permission-mode choices (claude --help, 2.1.283).
var claudePermissions = []chatPermissionOption{
	{"manual", "Ask", "Reads freely; asks before edits and commands", false},
	{"acceptEdits", "Accept edits", "Edits files without asking; asks before other commands", false},
	{"plan", "Plan", "Read-only exploration; proposes a plan before changing anything", false},
	{"auto", "Auto", "Runs actions a second model judges safe; asks otherwise", false},
	{"dontAsk", "Allowed tools only", "Runs only the tools your Claude settings allow; refuses everything else", false},
	{"bypassPermissions", "Bypass", "Runs everything without asking", true},
}

// Codex sandbox + approval presets, mapped to CLI flags in codexAccessFlags.
var codexPermissions = []chatPermissionOption{
	{"full", "Full access", "No sandbox and never asks (how Manifest has always launched Codex)", true},
	{"auto", "Auto", "Workspace-write sandbox; asks before leaving it", false},
	{"read-only", "Read only", "Reads only; asks before any change", false},
}

// Hermes --reasoning levels (hermes chat --help).
var hermesEfforts = []chatModelEffort{{"none", ""}, {"minimal", ""}, {"low", ""}, {"medium", ""}, {"high", ""}, {"xhigh", ""}, {"max", ""}, {"ultra", ""}}

func effortIDs(list []chatModelEffort) []string {
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}

// ---- Codex: the installed CLI's models cache ----

type codexCacheModel struct {
	Slug          string `json:"slug"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	Visibility    string `json:"visibility"`
	DefaultEffort string `json:"default_reasoning_level"`
	Efforts       []struct {
		Effort      string `json:"effort"`
		Description string `json:"description"`
	} `json:"supported_reasoning_levels"`
	Context int `json:"context_window"`
}

func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// cachedFile reads a small metadata file at most once per mtime.
type cachedFile struct {
	mu    sync.Mutex
	path  string
	mod   time.Time
	bytes []byte
}

func (c *cachedFile) read(path string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if path == c.path && st.ModTime().Equal(c.mod) {
		return c.bytes
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	c.path, c.mod, c.bytes = path, st.ModTime(), b
	return b
}

var codexCacheFile, hermesProvidersFile, hermesConfigFile cachedFile

func (s *Server) hermesModelCatalog(defaultModel string) chatAgentModels {
	home := s.hermesHome()
	return hermesCatalog(hermesProvidersFile.read(filepath.Join(home, "provider_models_cache.json")), hermesConfigFile.read(filepath.Join(home, "config.yaml")), defaultModel)
}

func codexCatalog(cache []byte) chatAgentModels {
	policy := codingModels["codex"]
	out := chatAgentModels{Backend: "terminal", Default: policy.best, Permissions: codexPermissions, DefaultPermission: "full", LiveModel: "native-picker", LiveEffort: "native-picker", LivePermission: "native-picker"}
	var cached struct {
		Models []codexCacheModel `json:"models"`
	}
	seen := map[string]bool{}
	effortSeen := map[string]bool{}
	if json.Unmarshal(cache, &cached) == nil {
		for _, m := range cached.Models {
			if m.Visibility != "list" || m.Slug == "" || seen[m.Slug] {
				continue
			}
			seen[m.Slug] = true
			opt := chatModelOption{ID: m.Slug, Label: m.DisplayName, Provider: "OpenAI", Description: m.Description, Context: m.Context, DefaultEffort: m.DefaultEffort}
			if opt.Label == "" {
				opt.Label = m.Slug
			}
			for _, e := range m.Efforts {
				if e.Effort == "" {
					continue
				}
				opt.Efforts = append(opt.Efforts, chatModelEffort{e.Effort, e.Description})
				if !effortSeen[e.Effort] {
					effortSeen[e.Effort] = true
					out.Efforts = append(out.Efforts, chatModelEffort{ID: e.Effort})
				}
			}
			out.Models = append(out.Models, opt)
		}
	}
	for _, id := range policy.allowed {
		if !seen[id] {
			out.Models = append(out.Models, chatModelOption{ID: id, Label: id, Provider: "OpenAI"})
		}
	}
	if len(out.Efforts) == 0 {
		out.Efforts = []chatModelEffort{{ID: "low"}, {ID: "medium"}, {ID: "high"}, {ID: "xhigh"}}
	}
	return out
}

func claudeCatalog() chatAgentModels {
	models := make([]chatModelOption, len(claudeModelAliases))
	for i, m := range claudeModelAliases {
		m.Provider = "Anthropic"
		m.Efforts = claudeEfforts
		models[i] = m
	}
	return chatAgentModels{Backend: "terminal", Default: codingModels["claude"].best, Models: models, Efforts: claudeEfforts, Permissions: claudePermissions, LiveModel: "command", LiveEffort: "command", LivePermission: "launch"}
}

// codingCatalogIDs is every model id the chat surface may launch for a
// coding agent: the deployment policy plus the installed CLI's own list.
func codingCatalogIDs(kind string) []string {
	var cat chatAgentModels
	switch kind {
	case "codex":
		cat = codexCatalog(codexCacheFile.read(filepath.Join(codexHome(), "models_cache.json")))
	case "claude":
		cat = claudeCatalog()
	default:
		return nil
	}
	ids := make([]string, 0, len(cat.Models))
	for _, m := range cat.Models {
		ids = append(ids, m.ID)
	}
	return ids
}

// codingSettingsError names why an effort or permission is refused, in words.
func codingSettingsError(kind, effort, permission string) error {
	switch kind {
	case "claude":
		if effort != "" && !containsString(effortIDs(claudeEfforts), effort) {
			return errBadRequest("Claude Code effort must be one of " + strings.Join(effortIDs(claudeEfforts), ", "))
		}
		if permission != "" && !containsPermission(claudePermissions, permission) {
			return errBadRequest("unknown Claude Code permission mode " + permission)
		}
	case "codex":
		if effort != "" && !containsString(effortIDs(codexCatalog(codexCacheFile.read(filepath.Join(codexHome(), "models_cache.json"))).Efforts), effort) {
			return errBadRequest("unknown Codex reasoning effort " + effort)
		}
		if permission != "" && !containsPermission(codexPermissions, permission) {
			return errBadRequest("unknown Codex access mode " + permission)
		}
	default:
		if effort != "" || permission != "" {
			return errBadRequest("effort and permission apply only to coding agents")
		}
	}
	return nil
}

func containsPermission(list []chatPermissionOption, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}

// ---- Hermes: providers the owner's Hermes has listed models for ----

func hermesProviderLabel(key string) string {
	switch {
	case key == "openai-codex":
		return "OpenAI"
	case key == "anthropic":
		return "Anthropic"
	case key == "xai-oauth":
		return "xAI"
	case strings.HasPrefix(key, "custom:"):
		return "Lab (" + strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(key, "custom:"), "http://"), "https://") + ")"
	}
	return key
}

// hermesProviderFlag is the --provider value for a provider cache key. A
// custom endpoint is addressed by the custom_providers name whose base_url it
// is ("user-defined name from providers:" in hermes --help), else "custom".
func hermesProviderFlag(key string, named map[string]string) string {
	if url, ok := strings.CutPrefix(key, "custom:"); ok {
		if name := named[strings.TrimRight(url, "/")]; name != "" {
			return name
		}
		return "custom"
	}
	return key
}

// hermesCustomProviders reads `custom_providers:` name → base_url pairs from a
// Hermes config.yaml. A line scanner, not a YAML parser: it reads only the
// two-space list entries of that one top-level block.
func hermesCustomProviders(config []byte) map[string]string {
	out := map[string]string{}
	in, name := false, ""
	for _, line := range strings.Split(string(config), "\n") {
		if !strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			in = strings.HasPrefix(line, "custom_providers:")
			name = ""
			continue
		}
		if !in {
			continue
		}
		t := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(t, "- name:"); ok {
			name = strings.Trim(strings.TrimSpace(v), `"'`)
		} else if v, ok := strings.CutPrefix(t, "name:"); ok && strings.HasPrefix(line, "    ") {
			name = strings.Trim(strings.TrimSpace(v), `"'`)
		} else if v, ok := strings.CutPrefix(t, "base_url:"); ok && name != "" {
			out[strings.TrimRight(strings.Trim(strings.TrimSpace(v), `"'`), "/")] = name
		}
	}
	return out
}

// hermesMediaModel filters generators Hermes lists but a chat turn cannot use.
func hermesMediaModel(id string) bool {
	l := strings.ToLower(id)
	return strings.Contains(l, "imagine") || strings.Contains(l, "image") || strings.Contains(l, "video") || strings.Contains(l, "tts") || strings.Contains(l, "whisper") || strings.Contains(l, "embedding")
}

func hermesCatalog(cache, config []byte, defaultModel string) chatAgentModels {
	named := hermesCustomProviders(config)
	out := chatAgentModels{Backend: "hermes", Default: defaultModel, Efforts: hermesEfforts, LiveModel: "recipient", LiveEffort: "recipient"}
	var providers map[string]struct {
		Models []string `json:"models"`
	}
	_ = json.Unmarshal(cache, &providers)
	keys := make([]string, 0, len(providers))
	for k := range providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seen := map[string]bool{}
	for _, k := range keys {
		flag := hermesProviderFlag(k, named)
		for _, id := range providers[k].Models {
			if id == "" || hermesMediaModel(id) || seen[flag+"/"+id] {
				continue
			}
			seen[flag+"/"+id] = true
			out.Models = append(out.Models, chatModelOption{ID: id, Label: id, Provider: flag, Description: hermesProviderLabel(k)})
			if id == defaultModel && out.DefaultProvider == "" {
				out.DefaultProvider = flag
			}
		}
	}
	if defaultModel != "" && !seen[out.DefaultProvider+"/"+defaultModel] {
		out.Models = append([]chatModelOption{{ID: defaultModel, Label: defaultModel, Description: "Profile default"}}, out.Models...)
	}
	return out
}

// hermesChoiceError refuses a provider/effort pair the catalog does not name.
func (s *Server) hermesChoiceError(model, provider, effort string) error {
	if effort != "" && !containsString(effortIDs(hermesEfforts), effort) {
		return errBadRequest("reasoning effort must be one of " + strings.Join(effortIDs(hermesEfforts), ", "))
	}
	if provider == "" {
		return nil
	}
	if model == "" {
		return errBadRequest("a provider needs a model")
	}
	cat := s.hermesModelCatalog("")
	for _, m := range cat.Models {
		if m.ID == model && m.Provider == provider {
			return nil
		}
	}
	return errBadRequest("model " + model + " is not listed for provider " + provider)
}

// GET /api/chat/models — the catalog for every addressable agent.
func (s *Server) handleChatModels(w http.ResponseWriter, r *http.Request) {
	agents := map[string]chatAgentModels{}
	if s.terminal != nil {
		agents["codex"] = codexCatalog(codexCacheFile.read(filepath.Join(codexHome(), "models_cache.json")))
		claude := claudeCatalog()
		ran := s.claudeLastRan()
		for i := range claude.Models {
			claude.Models[i].LastRan = ran[claude.Models[i].ID]
		}
		agents["claude"] = claude
	}
	if s.agentChat != nil {
		for _, a := range s.agentChatRoster(r.Context()) {
			agents[a.Name] = s.hermesModelCatalog(a.Model)
		}
	}
	writeJSON(w, map[string]any{"agents": agents})
}

// claudeLastRan maps each alias to the model it resolved to in the most
// recent session launched with it: that session's first recorded model (a
// later /model switch is the owner's choice, not what the alias means). From
// the transcripts' own records, never assumed; reads the cached projections.
func (s *Server) claudeLastRan() map[string]string {
	out, at := map[string]string{}, map[string]string{}
	for _, se := range s.terminal.load() {
		if se.Kind != "claude" || se.Model == "" || strings.HasPrefix(se.Model, "claude-") {
			continue
		}
		path := s.terminal.transcriptPath(se)
		if path == "" {
			continue
		}
		tr, ok := readTranscript(se.Kind, path, 0)
		if !ok || tr.Settings == nil || tr.Settings.First == "" || tr.Settings.FirstAt < at[se.Model] {
			continue
		}
		out[se.Model], at[se.Model] = tr.Settings.First, tr.Settings.FirstAt
	}
	return out
}
