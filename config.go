package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Config controls where the vault is and how the schedule is laid out. It is
// loaded from a JSON file (see config.example.json); any field left empty falls
// back to a sensible default.
// FilesAgent is one remote device running cmd/manifest-agent.
type FilesAgent struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// TerminalDevice is one ssh-reachable box the terminal cockpit can launch
// sessions on (metis itself is implicit). Remote sessions run `ssh -tt`
// INSIDE a metis tmux, so keep-alive is inherent. Fields are validated at
// load — they are interpolated into ssh argv (never a shell string).
type TerminalDevice struct {
	Name     string `json:"name"`               // display + registry key
	Host     string `json:"host"`               // tailnet IP/DNS or LAN address
	User     string `json:"user"`               // ssh login
	Port     int    `json:"port,omitempty"`     // default 22
	Identity string `json:"identity,omitempty"` // abs path to a key on this box
	Agent    string `json:"agent,omitempty"`    // FilesAgent name (stats via /stats)
}

type Config struct {
	// VaultPath is the absolute path to your Obsidian vault (the whole vault —
	// notes are found by convention, not by a single folder).
	VaultPath string `json:"vaultPath"`
	// NewDailyDir is the folder (relative to the vault) where new daily notes are
	// created on save. Existing notes are still resolved anywhere in the vault.
	NewDailyDir string `json:"newDailyDir"`
	// DailyNoteDir is the legacy single-folder setting; when set to a custom value
	// it seeds NewDailyDir for backward compatibility.
	DailyNoteDir string `json:"dailyNoteDir"`
	// DailyNoteFormat is the Go time layout for daily filenames (default ISO).
	DailyNoteFormat string `json:"dailyNoteFormat"`
	// GoalsFileName is the filename convention for the goals master file.
	GoalsFileName string `json:"goalsFileName"`
	// SkipDirs are directory base names the scanner ignores (besides dotdirs).
	SkipDirs []string `json:"skipDirs"`
	// ScheduleStart and ScheduleEnd are the first/last hours (24h) shown, inclusive.
	ScheduleStart int `json:"scheduleStart"`
	ScheduleEnd   int `json:"scheduleEnd"`
	// Timezone is an IANA name for mapping calendar events to slots ("" = local).
	Timezone string `json:"timezone"`
	// OwnerInitials identify "me" in the unified todo projection (redesign
	// stage 4): a todo whose [owner::] is empty, "me", or contains these
	// initials is mine. Default "BA".
	OwnerInitials string `json:"ownerInitials"`
	// Port is the local port the web UI is served on.
	Port int `json:"port"`
	// PortalPort is the local port the standalone AION portal is served on —
	// a SECOND listener, mutually exclusive with the dashboard mux on Port
	// (AION portal move, phase 1). Default 7778.
	PortalPort int `json:"portalPort"`
	// DataDir is where ALL derived/operational state lives — OUTSIDE the vault
	// (calendar cache today, the read-only index next). Defaults to
	// $MANIFEST_CONFIG_DIR or ~/.config/manifest (same external root as the
	// calendar credentials). The vault holds only your hand-authored notes; the
	// app never writes derived data into it.
	DataDir string `json:"dataDir"`
	// SystemRoot is the vault-relative folder that holds the SYSTEM ZONE
	// (system-root-plan §1): structured, app-managed markdown (agents, excalibur,
	// CRMs, home board). Everything OUTSIDE it is the knowledge zone — 100% the
	// user's language — where daily/goals classification applies and app writes
	// stay limited to the existing explicit user actions.
	SystemRoot string `json:"systemRoot"`
	// ExtrinsicRoot is the vault-relative folder for the EXTRINSIC ZONE
	// (reading-plan amendment): content the user did NOT originate — articles,
	// books, movies, papers — which may carry his commentary. Like the system
	// zone it is indexed/searchable and writable, but produces no contact/triage
	// extraction (a `[[name]]` in a book record is a reference, not a meeting).
	ExtrinsicRoot string `json:"extrinsicRoot"`
	// ExcaliburPath is the root of the sibling excalibur harness tree (spirit
	// feed, run reports, run-now spool). Empty disables the SPIRITS tab.
	// Legacy spelling of Harnesses — honored as a single-entry list.
	ExcaliburPath string `json:"excaliburPath"`
	// Harnesses is the federation list (big-change Phase 4): N harness trees
	// behind one on-disk contract (CONTRACT.md in the harnesses repo). The
	// FIRST entry is the primary — it keeps the write surfaces (spool, ritual
	// editor, aion sink); the rest surface read-side (runs, feed,
	// approvals) tagged by name. When empty, ExcaliburPath synthesizes
	// [{name:"excalibur", path:ExcaliburPath}].
	Harnesses []HarnessRef `json:"harnesses"`
	// LabSttUrl is the self-hosted speech-to-text endpoint (OpenAI-compatible
	// POST /v1/audio/transcriptions on the lab; reachable from metis only).
	// Empty disables the mic buttons' dictation. LabSttModel names the model
	// the service expects (default granite-speech-4.1-2b).
	LabSttUrl   string `json:"labSttUrl"`
	LabSttModel string `json:"labSttModel"`
	// FILES fleet browser (cmd-ctr FAST-agent pattern): FilesRoots are the
	// absolute paths of THIS machine's filesystem the FILES tab may browse
	// (empty = local browsing off); FilesAgents are remote per-device agents
	// (cmd/manifest-agent) reachable over the tailnet. The agent-auth master
	// secret lives at <dataDir>/agent_master (auto-created 0600).
	FilesRoots  []string     `json:"filesRoots"`
	FilesAgents []FilesAgent `json:"filesAgents"`
	// TerminalDevices are the ssh boxes the terminal cockpit's device selector
	// offers beyond metis itself (see TerminalDevice).
	TerminalDevices []TerminalDevice `json:"terminalDevices"`
	// RealEstate mirrors AionPortal's team-state shape for the RE side
	// (todo-panel plan D3): TeamDir is a shared on-disk thread store
	// (production: /shared/apps/re-team) that a future RE team surface reads
	// as-is. Empty = RE todo threads stay in the private store.
	RealEstate struct {
		TeamDir string `json:"teamDir"`
	} `json:"realEstate"`
	// Ooda configures the standalone OODA real-estate portal (ooda-portal plan):
	// a THIRD listener over the realestate domain, served behind
	// portal.ooda.group. Like AION it is a bounded many-writer surface — but its
	// derived team state lives on PRIVATE (owner decision 2026-08-20), because
	// the properties, ledgers, and partner names it composes are not
	// shared-volume material. Port 0 (or equal to Port/PortalPort) disables it.
	Ooda OodaConfig `json:"ooda"`
	// RePortalPath is the absolute path to the ooda site checkout (the re-portal
	// repo). When set, PROPERTIES gains "publish → deals.json": recompose
	// src/data/deals.json from the vault's source sidecars for owner review
	// (manifest never commits that repo). Empty disables the feature.
	RePortalPath string `json:"rePortalPath"`
	// TasksFileName is the vault-root tasks file (the third surface, peer of
	// goals.md). Default "tasks.md" — the owner's existing file.
	TasksFileName string `json:"tasksFileName"`
	// AionPortal configures the live listener/team store. Path/Remote/Branch
	// remain readable for one transition release; only Path is consulted by
	// the stable-ID migration, and runtime sync ignores all three.
	AionPortal AionPortalConfig `json:"aionPortal"`
	// FundraisingSheets enables the private Markdown↔Google Sheet collaboration
	// bridge. It remains disabled unless Enabled is explicitly true.
	FundraisingSheets FundraisingSheetsConfig `json:"fundraisingSheets"`
	// ErrandTimeoutMinutes kills a hung aside errand (errands-aside §6).
	// 0 → 15. Guard mode is not configurable — the CLI has no mode flag and
	// the app defaults new tasks to Guard (§0 probe).
	ErrandTimeoutMinutes int `json:"errandTimeoutMinutes"`
	// ErrandAccounts, when set, is the only set of aside account ids the
	// compose picker offers and the API accepts (§6 allowlist).
	ErrandAccounts []string `json:"errandAccounts"`
	// Hermes wires the app's @hermes to the owner's REAL do-bot — the local
	// NousResearch Hermes Agent CLI (`hermes -z`), the same agent he pings on
	// Telegram — instead of the excalibur-harness copy. Off by default, so this
	// lands dark: enabled=false keeps the legacy harness path unchanged.
	Hermes HermesConfig `json:"hermes"`
}

// HermesConfig configures the local Hermes Agent CLI runner (see the hermes
// package). Bin defaults to "hermes" on $PATH; Model/Toolsets are optional
// per-invocation overrides (-m / -t); TimeoutSeconds bounds one agent turn.
type HermesConfig struct {
	Enabled bool   `json:"enabled"`
	Bin     string `json:"bin"`
	Model   string `json:"model"`
	// Toolsets is the general -t scope (used by the go phase in Phase 2).
	Toolsets string `json:"toolsets"`
	// ReadToolsets is the read-only -t scope applied to plan/comment turns so a
	// planning turn cannot act — the approval-gate pre-stage. Empty → a safe
	// default (web/search/memory/skills; no code_execution/terminal/file/…).
	ReadToolsets   string `json:"readToolsets"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// DefaultHermesReadToolsets is the read-only scope for plan/comment turns —
// research + memory only, no world-changing tools.
const DefaultHermesReadToolsets = "web,session_search,memory,x_search,skills,clarify,context_engine,vision"

// HarnessRef names one harness tree (federation, big-change Phase 4).
// Surface scopes where the harness's agent identity is offered (kairos plan):
// "" = the personal dashboard (default); "team" = the AION portal roster only —
// the delegation machinery still orchestrates it, but personal typeaheads
// never offer it.
type HarnessRef struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Surface string `json:"surface"`
}

// AionPortalConfig is the git coordinates of the aionbio checkout, plus the
// team write layer (portal move Phase 2–3). TeamDir is the derived team-state
// directory on metis (production: /shared/apps/aion-portal — activity.log +
// items.ext.json); empty disables sign-in and every write endpoint (the
// portal stays the Phase-1 static site). AdminEmail is the portal owner's
// @aion.bio account — the second authorized decider on team proposals.
type AionPortalConfig struct {
	Path       string `json:"path"`
	Remote     string `json:"remote"`
	Branch     string `json:"branch"`
	TeamDir    string `json:"teamDir"`
	AdminEmail string `json:"adminEmail"`
}

type FundraisingSheetsConfig struct {
	Enabled             bool   `json:"enabled"`
	SpreadsheetID       string `json:"spreadsheetId"`
	SheetID             int64  `json:"sheetId"`
	CredentialsPath     string `json:"credentialsPath"`
	SyncIntervalMinutes int    `json:"syncIntervalMinutes"`
}

// OodaConfig is the OODA portal's block. Domain is the Workspace gate; every
// partner without an address under it is admitted by name through the team
// store's emails.json (which is ALSO the email→initials map — one file, an
// invariant: an admitted-but-unmapped address files work under a garbage owner).
type OodaConfig struct {
	Port       int    `json:"port"`       // default 7779
	TeamDir    string `json:"teamDir"`    // /private/ooda/team — derived state, never the vault
	AdminEmail string `json:"adminEmail"` // ben@ooda.group
	Domain     string `json:"domain"`     // ooda.group
	// PackDir is zeck's standing context pack — a flat markdown projection of
	// the live snapshot, regenerated when the source revision moves (ooda_pack.go).
	// Defaults under the zeck harness; stays INSIDE /private, never /shared.
	PackDir string `json:"packDir"` // default /private/harnesses/zeck/realestate
}

func defaultConfig() Config {
	return Config{
		VaultPath:       "",
		NewDailyDir:     "intrinsic",
		DailyNoteDir:    "Daily",
		DailyNoteFormat: "2006-01-02",
		GoalsFileName:   "goals.md",
		SkipDirs:        []string{".git", ".obsidian", ".trash", "attachments", "Agents"},
		ScheduleStart:   8,
		ScheduleEnd:     18,
		Port:            7777,
		PortalPort:      7778,
		Ooda:            OodaConfig{Port: 7779, Domain: "ooda.group", PackDir: "/private/harnesses/zeck/realestate"},
		SystemRoot:      "system",
		ExtrinsicRoot:   "extrinsic",
	}
}

// LoadConfig reads the config file (a missing file is fine — defaults + flags
// take over), applies defaults for missing fields and expands a leading ~.
func LoadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := json.Unmarshal(b, &cfg); err != nil {
				return cfg, err
			}
		case !os.IsNotExist(err):
			return cfg, err
			// a missing config file is fine — fall through and apply defaults
			// (incl. DataDir + ~ expansion) just as if every field were empty.
		}
	}
	d := defaultConfig()
	if cfg.DailyNoteFormat == "" {
		cfg.DailyNoteFormat = d.DailyNoteFormat
	}
	if cfg.GoalsFileName == "" {
		cfg.GoalsFileName = d.GoalsFileName
	}
	if cfg.NewDailyDir == "" {
		if cfg.DailyNoteDir != "" && cfg.DailyNoteDir != "Daily" {
			cfg.NewDailyDir = cfg.DailyNoteDir // honor a legacy custom folder
		} else {
			cfg.NewDailyDir = d.NewDailyDir
		}
	}
	if len(cfg.SkipDirs) == 0 {
		cfg.SkipDirs = d.SkipDirs
	}
	if cfg.ScheduleStart == 0 && cfg.ScheduleEnd == 0 {
		cfg.ScheduleStart = d.ScheduleStart
		cfg.ScheduleEnd = d.ScheduleEnd
	}
	if cfg.Port == 0 {
		cfg.Port = d.Port
	}
	if cfg.PortalPort == 0 {
		cfg.PortalPort = d.PortalPort
	}
	if cfg.Ooda.Port == 0 {
		cfg.Ooda.Port = d.Ooda.Port
	}
	if strings.TrimSpace(cfg.Ooda.Domain) == "" {
		cfg.Ooda.Domain = d.Ooda.Domain
	}
	if strings.TrimSpace(cfg.Ooda.PackDir) == "" {
		cfg.Ooda.PackDir = d.Ooda.PackDir
	}
	if cfg.FundraisingSheets.SyncIntervalMinutes == 0 {
		cfg.FundraisingSheets.SyncIntervalMinutes = 5
	}
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}
	if cfg.SystemRoot == "" {
		cfg.SystemRoot = d.SystemRoot
	}
	if cfg.ExtrinsicRoot == "" {
		cfg.ExtrinsicRoot = d.ExtrinsicRoot
	}
	if cfg.LabSttModel == "" {
		cfg.LabSttModel = "granite-speech-4.1-2b"
	}
	if cfg.TasksFileName == "" {
		cfg.TasksFileName = "tasks.md"
	}
	if err := validateZoneRoot("systemRoot", cfg.SystemRoot); err != nil {
		return cfg, err
	}
	if err := validateZoneRoot("extrinsicRoot", cfg.ExtrinsicRoot); err != nil {
		return cfg, err
	}
	cfg.SystemRoot = filepath.ToSlash(filepath.Clean(cfg.SystemRoot))
	cfg.ExtrinsicRoot = filepath.ToSlash(filepath.Clean(cfg.ExtrinsicRoot))
	if cfg.SystemRoot == cfg.ExtrinsicRoot {
		return cfg, errors.New("systemRoot and extrinsicRoot must differ")
	}
	cfg.VaultPath = expandHome(cfg.VaultPath)
	cfg.DataDir = expandHome(cfg.DataDir)
	cfg.ExcaliburPath = expandHome(cfg.ExcaliburPath)
	cfg.RePortalPath = expandHome(cfg.RePortalPath)
	cfg.RealEstate.TeamDir = expandHome(cfg.RealEstate.TeamDir)
	cfg.Ooda.TeamDir = expandHome(cfg.Ooda.TeamDir)
	cfg.Ooda.PackDir = expandHome(cfg.Ooda.PackDir)
	cfg.AionPortal.Path = expandHome(cfg.AionPortal.Path)
	cfg.AionPortal.TeamDir = expandHome(cfg.AionPortal.TeamDir)
	if cfg.FundraisingSheets.CredentialsPath == "" {
		cfg.FundraisingSheets.CredentialsPath = filepath.Join(cfg.DataDir, "fundraising", "google-service-account.json")
	} else {
		cfg.FundraisingSheets.CredentialsPath = expandHome(cfg.FundraisingSheets.CredentialsPath)
	}
	// Harness federation (Phase 4): normalize the two spellings into BOTH —
	// Harnesses is the canonical list (legacy ExcaliburPath synthesizes a
	// single entry); ExcaliburPath mirrors the primary for the code paths
	// that are primary-only by design (aion sink).
	for i := range cfg.Harnesses {
		cfg.Harnesses[i].Path = expandHome(cfg.Harnesses[i].Path)
		if strings.TrimSpace(cfg.Harnesses[i].Name) == "" {
			cfg.Harnesses[i].Name = filepath.Base(cfg.Harnesses[i].Path)
		}
	}
	if len(cfg.Harnesses) == 0 && cfg.ExcaliburPath != "" {
		cfg.Harnesses = []HarnessRef{{Name: "excalibur", Path: cfg.ExcaliburPath}}
	}
	if len(cfg.Harnesses) > 0 && cfg.ExcaliburPath == "" {
		cfg.ExcaliburPath = cfg.Harnesses[0].Path
	}
	if cfg.AionPortal.Remote == "" {
		cfg.AionPortal.Remote = "origin"
	}
	if cfg.AionPortal.Branch == "" {
		cfg.AionPortal.Branch = "main"
	}
	// Terminal devices: these fields reach ssh argv — validate hard at load so
	// a config typo fails the boot, not a shell.
	for i := range cfg.TerminalDevices {
		d := &cfg.TerminalDevices[i]
		d.Identity = expandHome(d.Identity)
		if err := d.validate(); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

var (
	devNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	devUserRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,31}$`)
	devHostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:_-]{0,127}$`)
)

func (d TerminalDevice) validate() error {
	if !devNameRe.MatchString(d.Name) {
		return errors.New("terminalDevices: bad name " + strconv.Quote(d.Name))
	}
	if !devUserRe.MatchString(d.User) {
		return errors.New("terminalDevices " + d.Name + ": bad user")
	}
	if !devHostRe.MatchString(d.Host) {
		return errors.New("terminalDevices " + d.Name + ": bad host")
	}
	if d.Port < 0 || d.Port > 65535 {
		return errors.New("terminalDevices " + d.Name + ": bad port")
	}
	if d.Identity != "" && !filepath.IsAbs(d.Identity) {
		return errors.New("terminalDevices " + d.Name + ": identity must be absolute")
	}
	return nil
}

// validateZoneRoot refuses a zone root that is empty, the vault itself,
// escaping, absolute, or hidden — a zone line must be a real, visible,
// vault-relative folder (system-root-plan §3; reading-plan amendment). name is
// the config key for the error message ("systemRoot" | "extrinsicRoot").
func validateZoneRoot(name, r string) error {
	clean := filepath.ToSlash(filepath.Clean(r))
	switch {
	case r == "" || clean == "" || clean == ".":
		return errors.New(name + " must name a folder")
	case clean == ".." || strings.HasPrefix(clean, "../"):
		return errors.New(name + " must stay inside the vault")
	case filepath.IsAbs(clean):
		return errors.New(name + " must be vault-relative, not absolute")
	case strings.HasPrefix(filepath.Base(clean), "."):
		return errors.New(name + " must not be a dotfolder (Obsidian hides those)")
	}
	return nil
}

// defaultDataDir is the external home for derived/operational state. It mirrors
// the calendar package's configDir(): $MANIFEST_CONFIG_DIR if set, else
// ~/.config/manifest. Kept here (not imported) to avoid a dependency cycle.
func defaultDataDir() string {
	if d := os.Getenv("MANIFEST_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".manifest") // last-resort relative dir
	}
	return filepath.Join(home, ".config", "manifest")
}

// pathIsUnder reports whether path is root or nested inside it (after cleaning).
// Used to enforce that DataDir is never inside the vault.
func pathIsUnder(path, root string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
