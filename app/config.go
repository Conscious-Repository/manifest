package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config controls where notes live and how the schedule is laid out.
// It is loaded from a JSON file (see config.example.json) and any field
// left empty falls back to a sensible default.
type Config struct {
	// VaultPath is the absolute path to your Obsidian vault.
	VaultPath string `json:"vaultPath"`
	// DailyNoteDir is the folder (relative to the vault) holding daily notes.
	DailyNoteDir string `json:"dailyNoteDir"`
	// DailyNoteFormat is a Go time layout used to build the daily note
	// filename, e.g. "2006-01-02" -> 2026-06-29.md
	DailyNoteFormat string `json:"dailyNoteFormat"`
	// PeriodNoteDir is the folder (relative to the vault) holding the
	// quarterly goals and monthly milestone notes.
	PeriodNoteDir string `json:"periodNoteDir"`
	// ScheduleStart and ScheduleEnd are the first and last hours (24h) shown
	// in the schedule, inclusive. Default 8..18 (8A..6P).
	ScheduleStart int `json:"scheduleStart"`
	ScheduleEnd   int `json:"scheduleEnd"`
	// Port is the local port the web UI is served on.
	Port int `json:"port"`
}

func defaultConfig() Config {
	return Config{
		VaultPath:       "",
		DailyNoteDir:    "Daily",
		DailyNoteFormat: "2006-01-02",
		PeriodNoteDir:   "Manifest",
		ScheduleStart:   8,
		ScheduleEnd:     18,
		Port:            7777,
	}
}

// LoadConfig reads the config file, applies defaults for any missing fields
// and expands a leading ~ in the vault path.
func LoadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	// re-apply defaults for fields that unmarshalled to zero values
	d := defaultConfig()
	if cfg.DailyNoteDir == "" {
		cfg.DailyNoteDir = d.DailyNoteDir
	}
	if cfg.DailyNoteFormat == "" {
		cfg.DailyNoteFormat = d.DailyNoteFormat
	}
	if cfg.PeriodNoteDir == "" {
		cfg.PeriodNoteDir = d.PeriodNoteDir
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
