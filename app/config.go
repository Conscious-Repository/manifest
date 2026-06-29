package main

import (
	"encoding/json"
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
}

func defaultConfig() Config {
	return Config{
		VaultPath:       "",
		NewDailyDir:     "intrinsic",
		DailyNoteDir:    "Daily",
		DailyNoteFormat: "2006-01-02",
		PeriodNoteDir:   "Manifest",
		GoalsFileName:   "goals.md",
		SkipDirs:        []string{".git", ".obsidian", ".trash", "attachments", "Agents"},
		ScheduleStart:   8,
		ScheduleEnd:     18,
		Port:            7777,
	}
}

// LoadConfig reads the config file (a missing file is fine — defaults + flags
// take over), applies defaults for missing fields and expands a leading ~.
func LoadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				cfg.VaultPath = expandHome(cfg.VaultPath)
				return cfg, nil
			}
			return cfg, err
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
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
	cfg.VaultPath = expandHome(cfg.VaultPath)
	return cfg, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
