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
	// PeriodNoteDir holds the (legacy) quarterly goals / monthly milestone notes.
	PeriodNoteDir string `json:"periodNoteDir"`
	// GoalsFileName is the filename convention for the goals master file.
	GoalsFileName string `json:"goalsFileName"`
	// SkipDirs are directory base names the scanner ignores (besides dotdirs).
	SkipDirs []string `json:"skipDirs"`
	// ScheduleStart and ScheduleEnd are the first/last hours (24h) shown, inclusive.
	ScheduleStart int `json:"scheduleStart"`
	ScheduleEnd   int `json:"scheduleEnd"`
	// Timezone is an IANA name for mapping calendar events to slots ("" = local).
	Timezone string `json:"timezone"`
	// Port is the local port the web UI is served on.
	Port int `json:"port"`
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
	// ExcaliburPath is the root of the sibling excalibur harness tree (spirit
	// feed, run reports, run-now spool). Empty disables the SPIRITS tab.
	ExcaliburPath string `json:"excaliburPath"`
}

func defaultConfig() Config {
	return Config{
		VaultPath:       "",
		NewDailyDir:     "intrinsic",
		DailyNoteDir:    "Daily",
		DailyNoteFormat: "2006-01-02",
		PeriodNoteDir:   "Manifest",
		GoalsFileName:   "goals.md",
		SkipDirs:        []string{".git", ".obsidian", ".trash", "attachments", "Agents", "excalibur"},
		ScheduleStart:   8,
		ScheduleEnd:     18,
		Port:            7777,
		SystemRoot:      "system",
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
	if cfg.PeriodNoteDir == "" {
		cfg.PeriodNoteDir = d.PeriodNoteDir
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
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}
	if cfg.SystemRoot == "" {
		cfg.SystemRoot = d.SystemRoot
	}
	if err := validateSystemRoot(cfg.SystemRoot); err != nil {
		return cfg, err
	}
	cfg.SystemRoot = filepath.ToSlash(filepath.Clean(cfg.SystemRoot))
	cfg.VaultPath = expandHome(cfg.VaultPath)
	cfg.DataDir = expandHome(cfg.DataDir)
	cfg.ExcaliburPath = expandHome(cfg.ExcaliburPath)
	return cfg, nil
}

// validateSystemRoot refuses a system root that is empty, the vault itself,
// escaping, absolute, or hidden — the zone line must be a real, visible,
// vault-relative folder (system-root-plan §3).
func validateSystemRoot(r string) error {
	clean := filepath.ToSlash(filepath.Clean(r))
	switch {
	case r == "" || clean == "" || clean == ".":
		return errors.New("systemRoot must name a folder (default \"system\")")
	case clean == ".." || strings.HasPrefix(clean, "../"):
		return errors.New("systemRoot must stay inside the vault")
	case filepath.IsAbs(clean):
		return errors.New("systemRoot must be vault-relative, not absolute")
	case strings.HasPrefix(filepath.Base(clean), "."):
		return errors.New("systemRoot must not be a dotfolder (Obsidian hides those)")
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
