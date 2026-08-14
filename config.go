package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	// editor, studio, aion sink); the rest surface read-side (runs, feed,
	// approvals) tagged by name. When empty, ExcaliburPath synthesizes
	// [{name:"excalibur", path:ExcaliburPath}].
	Harnesses []HarnessRef `json:"harnesses"`
	// XPostsFile is the vault-relative X-posts file the Content Studio appends
	// approved posts to (a `# queue`/`# posted` bullet list). Default "x posts.md".
	XPostsFile string `json:"xPostsFile"`
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
	// RePortalPath is the absolute path to the ooda site checkout (the re-portal
	// repo). When set, PROPERTIES gains "publish → deals.json": recompose
	// src/data/deals.json from the vault's source sidecars for owner review
	// (manifest never commits that repo). Empty disables the feature.
	RePortalPath string `json:"rePortalPath"`
	// TodosFileName is the vault-root todos file (the third surface, peer of
	// goals.md). Default "to do.md" — the owner's existing file.
	TodosFileName string `json:"todosFileName"`
	// AionPortal points the AION publish effector at the aionbio checkout.
	// Path empty disables PUBLISH (the tab still works); Remote/Branch default
	// origin/main. The effector writes ONLY the export-contract paths, commits
	// once, pushes, and leaves a receipt (aion-domain spec §5).
	AionPortal AionPortalConfig `json:"aionPortal"`
	// ErrandTimeoutMinutes kills a hung aside errand (errands-aside §6).
	// 0 → 15. Guard mode is not configurable — the CLI has no mode flag and
	// the app defaults new tasks to Guard (§0 probe).
	ErrandTimeoutMinutes int `json:"errandTimeoutMinutes"`
	// ErrandAccounts, when set, is the only set of aside account ids the
	// compose picker offers and the API accepts (§6 allowlist).
	ErrandAccounts []string `json:"errandAccounts"`
}

// HarnessRef names one harness tree (federation, big-change Phase 4).
type HarnessRef struct {
	Name string `json:"name"`
	Path string `json:"path"`
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
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}
	if cfg.SystemRoot == "" {
		cfg.SystemRoot = d.SystemRoot
	}
	if cfg.ExtrinsicRoot == "" {
		cfg.ExtrinsicRoot = d.ExtrinsicRoot
	}
	if cfg.XPostsFile == "" {
		cfg.XPostsFile = "x posts.md"
	}
	if cfg.LabSttModel == "" {
		cfg.LabSttModel = "granite-speech-4.1-2b"
	}
	if cfg.TodosFileName == "" {
		cfg.TodosFileName = "to do.md"
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
	cfg.AionPortal.Path = expandHome(cfg.AionPortal.Path)
	cfg.AionPortal.TeamDir = expandHome(cfg.AionPortal.TeamDir)
	// Harness federation (Phase 4): normalize the two spellings into BOTH —
	// Harnesses is the canonical list (legacy ExcaliburPath synthesizes a
	// single entry); ExcaliburPath mirrors the primary for the code paths
	// that are primary-only by design (studio, corpus, aion sink).
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
	return cfg, nil
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
