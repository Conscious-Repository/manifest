package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"manifest/hermes"
)

func TestCanaryRequiresExactConfirmation(t *testing.T) {
	for _, args := range [][]string{nil, {"--confirm-live=false"}, {"--confirm-live", "--confirm-live"}, {"--prompt", "secret"}, {"--config", "secret"}, {"--provider", "secret"}, {"--model", "secret"}, {"--output", "secret"}, {"--fallback", "secret"}, {"--confirm-live", "secret"}} {
		var out bytes.Buffer
		if run(args, &out) != 2 || strings.Contains(out.String(), "secret") {
			t.Fatal(out.String())
		}
	}
}

func TestCanaryDurableOneShot(t *testing.T) {
	for _, status := range []string{"canary passed", "refused"} {
		t.Run(status, func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			calls := 0
			invoke := func(context.Context) hermes.PrimaryCanaryReport {
				calls++
				b, err := root.ReadFile(receiptName)
				if err != nil || !strings.Contains(string(b), "outcome uncertain") {
					t.Fatal("no prelaunch refusal")
				}
				r := hermes.PrimaryCanaryRefusal("missing usage evidence")
				r.Status = status
				return r
			}
			var out bytes.Buffer
			code := once(t.Context(), root, &out, invoke)
			if (code == 0) != (status == "canary passed") || calls != 1 {
				t.Fatal(code, calls)
			}
			b, _ := root.ReadFile(receiptName)
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) != 2 {
				t.Fatal(string(b))
			}
			var r hermes.PrimaryCanaryReport
			if json.Unmarshal([]byte(lines[1]), &r) != nil || r.Status != status {
				t.Fatal(string(b))
			}
			if once(t.Context(), root, &out, invoke) != 1 || calls != 1 {
				t.Fatal("retried")
			}
		})
	}
}

func TestCanaryInterruptedOrSymlinkNeverLaunches(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if symlink {
			err = root.Symlink("absent", receiptName)
		} else {
			err = root.WriteFile(receiptName, []byte("interrupted"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if once(t.Context(), root, &out, func(context.Context) hermes.PrimaryCanaryReport {
			t.Fatal("launched")
			return hermes.PrimaryCanaryReport{}
		}) != 1 {
			t.Fatal("accepted")
		}
		root.Close()
	}
}

func TestCanaryCrashLeavesRefusal(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected synthetic interruption")
			}
		}()
		once(t.Context(), root, &bytes.Buffer{}, func(context.Context) hermes.PrimaryCanaryReport { panic("synthetic interruption") })
	}()
	b, err := root.ReadFile(receiptName)
	if err != nil || !strings.Contains(string(b), "outcome uncertain") {
		t.Fatal("lost uncertain outcome")
	}
	if once(t.Context(), root, &bytes.Buffer{}, func(context.Context) hermes.PrimaryCanaryReport {
		t.Fatal("retried interrupted run")
		return hermes.PrimaryCanaryReport{}
	}) != 1 {
		t.Fatal("accepted")
	}
}

func TestCanaryUnavailableReceiptNeverLaunches(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if once(t.Context(), root, &bytes.Buffer{}, func(context.Context) hermes.PrimaryCanaryReport {
		t.Fatal("launched without evidence")
		return hermes.PrimaryCanaryReport{}
	}) != 1 {
		t.Fatal("accepted")
	}
}
