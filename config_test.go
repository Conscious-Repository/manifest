package main

import (
	"path/filepath"
	"testing"
)

func TestPathIsUnder(t *testing.T) {
	cases := []struct {
		path, root string
		want       bool
	}{
		{"/home/u/.config/manifest", "/home/u/vault", false},            // external — fine
		{"/home/u/vault/Manifest/cache", "/home/u/vault", true},         // nested — forbidden
		{"/home/u/vault/intrinsic/2026-06-30.md", "/home/u/vault", true}, // nested file
		{"/home/u/vault", "/home/u/vault", true},                        // root itself
		{"/home/u/vault-2", "/home/u/vault", false},                     // sibling prefix, not nested
		{"/home/u", "/home/u/vault", false},                             // parent, not nested
		{"", "/home/u/vault", false},
		{"/x", "", false},
	}
	for _, c := range cases {
		if got := pathIsUnder(c.path, c.root); got != c.want {
			t.Errorf("pathIsUnder(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
		}
	}
}

// Regression: a MISSING config file must still apply all defaults — including
// DataDir — not return a half-populated config (which left DataDir empty and made
// the calendar cache a relative path under the CWD).
func TestLoadConfigMissingFileStillDefaultsDataDir(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", "/home/u/.config/manifest")
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/home/u/.config/manifest" {
		t.Fatalf("DataDir = %q, want the external default", cfg.DataDir)
	}
	if cfg.NewDailyDir == "" || cfg.Port == 0 {
		t.Fatalf("missing-file load left other defaults empty: %+v", cfg)
	}
}

func TestDefaultDataDirHonorsEnv(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", "/tmp/custom-manifest")
	if got := defaultDataDir(); got != "/tmp/custom-manifest" {
		t.Fatalf("defaultDataDir() = %q, want /tmp/custom-manifest", got)
	}
}

// SystemRoot (system-root-plan §3): defaults to "system", must be a plain
// vault-relative folder name — never empty-after-clean, never escaping the vault,
// never absolute, and never a dotfolder (Obsidian hides those).
func TestSystemRootDefaultAndValidation(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", "/home/u/.config/manifest")
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemRoot != "system" {
		t.Fatalf("SystemRoot default = %q, want \"system\"", cfg.SystemRoot)
	}
	valid := []string{"system", "sys", "system/nested"} // nested is tolerated (cleaned)
	for _, v := range valid {
		if err := validateSystemRoot(v); err != nil {
			t.Errorf("validateSystemRoot(%q) = %v, want ok", v, err)
		}
	}
	invalid := []string{"", ".", "..", "../outside", "/abs/path", "a/../..", ".hidden"}
	for _, v := range invalid {
		if err := validateSystemRoot(v); err == nil {
			t.Errorf("validateSystemRoot(%q) = ok, want error", v)
		}
	}
}

// The zone line only exists if it is WIRED: vaultConfig must carry SystemRoot
// into the daily scanner (regression: a dropped edit left it "" and date-named
// engine files under system/excalibur classified as dailies).
func TestVaultConfigCarriesSystemRoot(t *testing.T) {
	vc := vaultConfig(Config{VaultPath: "/v", SystemRoot: "system"})
	if vc.SystemRoot != "system" {
		t.Fatalf("vaultConfig dropped SystemRoot: %+v", vc)
	}
}

// Derived-data home (and everything under it: calendar-cache, index.db) must live
// OUTSIDE the vault — the invariant the startup guard enforces.
func TestDerivedDataLivesOutsideVault(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", "/home/u/.config/manifest")
	vault := "/home/u/Documents/my-vault"
	dataDir := defaultDataDir()
	for _, p := range []string{dataDir, filepath.Join(dataDir, "calendar-cache"), filepath.Join(dataDir, "index.db")} {
		if pathIsUnder(p, vault) {
			t.Fatalf("derived path %q must be outside the vault %q", p, vault)
		}
	}
}
