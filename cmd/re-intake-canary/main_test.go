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
	if receiptDirectory != "/home/benjamin/workbench-staging/excalibur-retirement" || receiptName != "36-deepseek-primary-canary.jsonl" {
		t.Fatal("owner-authorized attempt must use its reviewed fixed receipt")
	}
	for _, status := range []string{"canary passed", "refused"} {
		t.Run(status, func(t *testing.T) {
			root, err := os.OpenRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			const priorName = "33-deepseek-primary-canary.jsonl"
			const prior35 = "35-deepseek-primary-canary.jsonl"
			if err := root.WriteFile(prior35, []byte("historical 35"), 0600); err != nil {
				t.Fatal(err)
			}
			info35, _ := root.Stat(prior35)
			prior := []byte("prior failed attempt: permanently latched\n")
			if err := root.WriteFile(priorName, prior, 0600); err != nil {
				t.Fatal(err)
			}
			priorInfo, err := root.Stat(priorName)
			if err != nil {
				t.Fatal(err)
			}
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
			info, err := root.Stat(receiptName)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("new receipt must be mode 0600", err)
			}
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
			after, err := root.ReadFile(receiptName)
			if err != nil || !bytes.Equal(b, after) {
				t.Fatal("latched new receipt changed", err)
			}
			after35, err35 := root.ReadFile(prior35)
			afterInfo35, statErr35 := root.Stat(prior35)
			if err35 != nil || statErr35 != nil || string(after35) != "historical 35" || !os.SameFile(info35, afterInfo35) || !info35.ModTime().Equal(afterInfo35.ModTime()) || info35.Mode() != afterInfo35.Mode() {
				t.Fatal("historical 35 changed")
			}
			priorAfter, err := root.ReadFile(priorName)
			if err != nil || !bytes.Equal(prior, priorAfter) {
				t.Fatal("prior failed attempt changed", err)
			}
			priorAfterInfo, err := root.Stat(priorName)
			if err != nil || !os.SameFile(priorInfo, priorAfterInfo) || priorInfo.Mode() != priorAfterInfo.Mode() || !priorInfo.ModTime().Equal(priorAfterInfo.ModTime()) {
				t.Fatal("prior failed attempt metadata changed", err)
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
