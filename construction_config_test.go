package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Construction's settings are disabled and trustedHosts (extra Host names
// the owner reaches Manifest by). An unknown key is named, not fatal.
func TestConstructionConfig(t *testing.T) {
	for raw, want := range map[string]string{
		`{}`:                   "",
		`null`:                 "",
		`{"disabled": false}`:  "",
		`{"disabled": true}`:   "",
		`{"trustedHosts": []}`: "",
		`{"trustedHosts": ["manifest.home.example"]}`: "",
		`{"allowRemote": true}`:                       "keys this build ignores (allowRemote)",
		`{"tunnel": "ssh", "gateway": "https://x"}`:   "(gateway, tunnel)",
	} {
		var c ConstructionConfig
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		err := c.Problem()
		if (want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want %q, got %v", raw, want, err)
		}
	}
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"construction": {"trustedHosts": ["manifest.home.example"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Construction.Problem(); err != nil || len(cfg.Construction.TrustedHosts) != 1 {
		t.Fatalf("a loaded config with trustedHosts: %v %v", err, cfg.Construction.TrustedHosts)
	}
}
