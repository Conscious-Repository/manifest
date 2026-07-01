package hermes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The documented sample stream from agents-milestone-build.md.
const sampleSSE = `data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}

data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"her"}}]}

data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"mes"}}]}

data: {"object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":3}}

data: [DONE]

`

func TestListWrapperUnwraps(t *testing.T) {
	// The footgun: skills come wrapped in {object:"list", data:[…]}, not a bare array.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("missing/wrong auth header: %q", got)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"name":"web.search","description":"search"},{"name":"files"}]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "secret"})
	skills, err := c.ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills: %v", err)
	}
	if len(skills) != 2 || skills[0].Name != "web.search" || skills[1].Name != "files" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
}

func TestStreamChatPassesBytesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the proxy forces model + stream and forwards messages.
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "hermes-agent" {
			t.Errorf("model not forced: %v", body["model"])
		}
		if body["stream"] != true {
			t.Errorf("stream not forced: %v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sampleSSE))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k"})
	var sb strings.Builder
	flushes := 0
	err := c.StreamChat(context.Background(),
		ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}},
		&sb, func() { flushes++ })
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	// Bytes pass through verbatim (no re-parsing in Go).
	if sb.String() != sampleSSE {
		t.Fatalf("stream not passed through verbatim:\n got: %q", sb.String())
	}
	if flushes == 0 {
		t.Fatal("expected at least one flush during streaming")
	}
	// And the browser-side reconstruction of delta.content should be "hermes".
	if got := reconstruct(sb.String()); got != "hermes" {
		t.Fatalf("reconstructed %q, want %q", got, "hermes")
	}
}

func TestUnconfiguredIsGraceful(t *testing.T) {
	c := NewClient(Config{}) // no base URL / key
	if c.Configured() {
		t.Fatal("empty config should not be Configured()")
	}
	if _, err := c.ListSkills(context.Background()); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	err := c.StreamChat(context.Background(), ChatRequest{}, nil, nil)
	if err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERMES_API_KEY", "")  // ensure clean
	t.Setenv("HERMES_BASE_URL", "") // ensure clean
	cfg := LoadConfig(dir)
	if cfg.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want default", cfg.BaseURL)
	}
	if cfg.Model != defaultModel {
		t.Errorf("Model = %q, want %q", cfg.Model, defaultModel)
	}
	if cfg.Configured() {
		t.Error("no key present, should be unconfigured")
	}
	// The agents dir should have been created.
	if fi, err := os.Stat(filepath.Join(dir, "agents")); err != nil || !fi.IsDir() {
		t.Errorf("agents dir not created: %v", err)
	}
}

func TestLoadConfigEnvKeyWins(t *testing.T) {
	dir := t.TempDir()
	// Write a key file too; the env var must take precedence.
	agents := filepath.Join(dir, "agents")
	_ = os.MkdirAll(agents, 0o700)
	_ = os.WriteFile(filepath.Join(agents, "hermes_key"), []byte("file-key\n"), 0o600)
	t.Setenv("HERMES_API_KEY", "env-key")
	cfg := LoadConfig(dir)
	if cfg.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key (env wins over file)", cfg.APIKey)
	}
	if !cfg.Configured() {
		t.Error("should be configured with a key")
	}
}

func TestLoadConfigFileKeyFallback(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "agents")
	_ = os.MkdirAll(agents, 0o700)
	_ = os.WriteFile(filepath.Join(agents, "hermes_key"), []byte("  file-key\n"), 0o600)
	t.Setenv("HERMES_API_KEY", "") // no env → file is used, trimmed
	cfg := LoadConfig(dir)
	if cfg.APIKey != "file-key" {
		t.Errorf("APIKey = %q, want trimmed file-key", cfg.APIKey)
	}
}

func TestLoadConfigJSONOverrides(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "agents")
	_ = os.MkdirAll(agents, 0o700)
	_ = os.WriteFile(filepath.Join(agents, "config.json"),
		[]byte(`{"baseURL":"https://example.test","model":"custom-model"}`), 0o600)
	t.Setenv("HERMES_BASE_URL", "")
	cfg := LoadConfig(dir)
	if cfg.BaseURL != "https://example.test" {
		t.Errorf("BaseURL = %q, want json override", cfg.BaseURL)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("Model = %q, want json override", cfg.Model)
	}
}

// reconstruct mimics the browser's SSE parsing so the test proves the contract:
// parse data: lines, stop on [DONE], concatenate choices[0].delta.content.
func reconstruct(sse string) string {
	var out strings.Builder
	for _, line := range strings.Split(sse, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 {
			out.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	return out.String()
}
