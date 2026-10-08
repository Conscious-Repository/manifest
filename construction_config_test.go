package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Construction has one setting, Disabled. A config that asks for remote
// access (trustedHosts) or carries keys this build does not know keeps
// Construction off with an explicit reason: there is no remote, relay or
// gateway mode to configure, and a setting is refused rather than ignored.
func TestConstructionConfigRefusesRemoteAccess(t *testing.T) {
	for raw, want := range map[string]string{
		`{}`:                   "",
		`null`:                 "",
		`{"disabled": false}`:  "",
		`{"disabled": true}`:   "",
		`{"trustedHosts": []}`: "",
		`{"trustedHosts": ["metis.tail1234.ts.net"]}`: "asks for remote access, which is unsupported",
		`{"allowRemote": true}`:                       "keys this build does not know (allowRemote)",
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
		if err != nil && !strings.Contains(err.Error(), "remote") {
			t.Fatalf("%s: the refusal names the unsupported remote mode: %v", raw, err)
		}
	}
	// through LoadConfig, as main reads it
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"construction": {"trustedHosts": ["metis.tail1234.ts.net"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Construction.Problem(); err == nil || !strings.Contains(err.Error(), "authenticated owner gateway") {
		t.Fatalf("a loaded config asking for remote access: %v", err)
	}
}
