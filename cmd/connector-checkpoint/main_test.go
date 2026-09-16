package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
)

func fixture(t *testing.T) options {
	t.Helper()
	root := t.TempDir()
	approvals.NewStore(filepath.Join(root, "artifacts"))
	state := filepath.Join(root, "email-state.json")
	if err := os.WriteFile(state, []byte(`{"watermark":"2026-09-10T00:00:00Z","threads":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	return options{Source: "email", Root: root, Account: "private@example.test", EmailState: state, DataDir: t.TempDir()}
}
func TestDryRunCompareStageAndRedaction(t *testing.T) {
	o := fixture(t)
	var out bytes.Buffer
	if err := run(o, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), o.Account) || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatal("account leaked")
	}
	entries, _ := os.ReadDir(o.DataDir)
	if len(entries) != 0 {
		t.Fatal("dry-run wrote files")
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	o.ExpectState = report["stateHash"].(string)
	o.ExpectApprovals = report["approvalHash"].(string)
	o.Stage = true
	if err := run(o, &out); err != nil {
		t.Fatal(err)
	}
	if err := run(o, &out); err == nil {
		t.Fatal("existing checkpoint overwritten")
	}
	o.Stage = false
	o.ExpectApprovals = strings.Repeat("0", 64)
	if err := run(o, &out); err == nil {
		t.Fatal("hash drift ignored")
	}
}
func TestApplyRefusesEnabledQueuedRunningAndUnfencedLegacy(t *testing.T) {
	for _, mode := range []string{"enabled", "queued", "running", "paused"} {
		t.Run(mode, func(t *testing.T) {
			o := fixture(t)
			o.Apply = true
			ritual := filepath.Join(o.Root, "spirits", "ea-coordinator", "rituals", "email-sync.md")
			os.MkdirAll(filepath.Dir(ritual), 0700)
			enabled := "false"
			if mode == "enabled" {
				enabled = "true"
			}
			os.WriteFile(ritual, []byte("---\nenabled: "+enabled+"\npaused_reason: fixture\n---\n"), 0600)
			spool := filepath.Join(o.Root, "vessel", "spool")
			os.MkdirAll(spool, 0700)
			runs := filepath.Join(o.Root, "artifacts", "runs")
			os.MkdirAll(runs, 0700)
			if mode == "queued" {
				os.WriteFile(filepath.Join(spool, "fixture.json"), []byte("{}"), 0600)
			}
			if mode == "running" {
				os.WriteFile(filepath.Join(runs, "fixture.md"), []byte("---\noutcome: running\n---\n"), 0600)
			}
			var out bytes.Buffer
			err := run(o, &out)
			if err == nil {
				t.Fatal("unsafe apply accepted")
			}
			if mode == "paused" && !strings.Contains(err.Error(), "dispatch fence") {
				t.Fatal("pause treated as sufficient", err)
			}
			entries, _ := os.ReadDir(o.DataDir)
			if len(entries) != 0 {
				t.Fatal("refusal wrote files")
			}
		})
	}
}

func TestTranscriptPreviewUsesReadOnlyFrozenIndex(t *testing.T) {
	o := fixture(t)
	o.Source = "granola"
	o.Index = filepath.Join(t.TempDir(), "index.sqlite")
	db, err := sql.Open("sqlite", o.Index)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE notes(path TEXT, granola_id TEXT, pocket_id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	watermark := filepath.Join(o.Root, "vessel", "state", "granola", "watermark")
	os.MkdirAll(filepath.Dir(watermark), 0700)
	os.WriteFile(watermark, []byte("2026-09-10T00:00:00Z\n"), 0600)
	before, err := os.ReadFile(o.Index)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = run(o, &out); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(o.Index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview modified index", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(o.Index))
	if len(entries) != 1 {
		t.Fatal("preview created SQLite sidecars")
	}
	entries, _ = os.ReadDir(o.DataDir)
	if len(entries) != 0 {
		t.Fatal("preview created state")
	}
	os.WriteFile(o.Index+"-wal", []byte("uncheckpointed"), 0600)
	if err = run(o, &out); err == nil {
		t.Fatal("WAL input silently read stale")
	}
}
