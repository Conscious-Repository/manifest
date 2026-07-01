package hermes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// defaultBaseURL is the known Tailscale-served Hermes endpoint (verified reachable:
// /health 200, /v1/skills 401). Overridable via config.json or the HERMES_BASE_URL env.
const defaultBaseURL = "https://ubuntu-s-1vcpu-1gb-amd-atl1-01.tail8f89de.ts.net"

const defaultModel = "hermes-agent"

// fileConfig is the on-disk shape at <dataDir>/agents/config.json. The API key is
// NEVER stored here — only where to find it (an env var name and/or a 0600 file).
type fileConfig struct {
	BaseURL   string `json:"baseURL"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"apiKeyEnv"`
	APIKey    string `json:"apiKeyFile"` // path to a 0600 file holding the key
}

// LoadConfig resolves Hermes connection config from <dataDir>/agents/. It always
// returns a usable Config (never errors): a missing file / missing key just yields
// an unconfigured client, so the rest of the app keeps working. The agents dir is
// created (0700) so the user has a place to drop the key.
//
// Precedence:
//   - baseURL: config.json → $HERMES_BASE_URL → built-in default
//   - model:   config.json → built-in default (hermes-agent)
//   - apiKey:  $<apiKeyEnv> (default $HERMES_API_KEY) → the 0600 key file
//     (default <dataDir>/agents/hermes_key)
func LoadConfig(dataDir string) Config {
	agentsDir := filepath.Join(dataDir, "agents")
	_ = os.MkdirAll(agentsDir, 0o700)

	var fc fileConfig
	if b, err := os.ReadFile(filepath.Join(agentsDir, "config.json")); err == nil {
		_ = json.Unmarshal(b, &fc) // a malformed file falls back to defaults
	}

	cfg := Config{
		BaseURL: firstNonEmpty(fc.BaseURL, os.Getenv("HERMES_BASE_URL"), defaultBaseURL),
		Model:   firstNonEmpty(fc.Model, defaultModel),
	}

	// Key: env var (named by apiKeyEnv, default HERMES_API_KEY) wins; else a 0600 file.
	envName := firstNonEmpty(fc.APIKeyEnv, "HERMES_API_KEY")
	if k := strings.TrimSpace(os.Getenv(envName)); k != "" {
		cfg.APIKey = k
	} else {
		keyFile := fc.APIKey
		if keyFile == "" {
			keyFile = filepath.Join(agentsDir, "hermes_key")
		} else {
			keyFile = expandHome(keyFile)
		}
		if b, err := os.ReadFile(keyFile); err == nil {
			cfg.APIKey = strings.TrimSpace(string(b))
		}
	}
	return cfg
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
