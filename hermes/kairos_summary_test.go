package hermes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKairosSummaryUsesPrivateProfileAndNoFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERMES_HOME", "/wrong")
	t.Setenv("HERMES_CONFIG", "/wrong/config.yaml")
	bin := filepath.Join(home, "python")
	script := `#!/bin/sh
cat > /dev/null
[ "$3" = "--kairos-summary" ] || exit 1
[ "$HERMES_HOME" = "$HOME/.hermes/profiles/kairos-private" ] || exit 1
[ "$HERMES_CONFIG" = "$HERMES_HOME/config.yaml" ] || exit 1
printf '%s' '{"reply":"fixture","model":"kairos-fixture"}'
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{Enabled: true, AnnotationPython: bin})
	if _, err := r.KairosSummary(context.Background(), map[string]string{}); err == nil || !strings.Contains(err.Error(), "profile is unavailable") {
		t.Fatalf("fallback: %v", err)
	}
	dir := filepath.Join(home, ".hermes", "profiles", "kairos-private")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("model: fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := r.KairosSummary(context.Background(), map[string]string{})
	if err != nil || got.Model != "kairos-fixture" {
		t.Fatalf("%+v %v", got, err)
	}
}
