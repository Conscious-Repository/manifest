package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnotationDiagnosticCodesNeverExposeProviderText(t *testing.T) {
	for _, tc := range []struct{ diagnostic, code string }{{`{"code":"token_limit"}`, "token_limit"}, {`{"code":"http_503"}`, "http_503"}, {`{"code":"private provider body"}`, "provider_error"}, {`provider exception with private context`, "provider_error"}} {
		t.Run(tc.code, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "python")
			script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + tc.diagnostic + "' >&2\nexit 1\n"
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			r := NewRunner(Config{Enabled: true, AnnotationPython: bin})
			_, err := r.Annotate(context.Background(), map[string]string{"question": "private question"})
			var ae *AnnotationError
			if !errors.As(err, &ae) || ae.Code != tc.code {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(ae.UserMessage(), "private") {
				t.Fatal("context leaked", err)
			}
		})
	}
}
