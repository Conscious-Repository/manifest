package connectorhandoff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFenceContractFailsClosed(t *testing.T) {
	for _, mode := range []string{"valid", "gap", "unknown", "duplicate", "owner", "symlink", "busy"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := "granola"
			duty := "ea-coordinator/granola-sync"
			r := RecordFence{1, 1, duty, "excalibur", "manifest", "transfer", strings.Repeat("a", 64), "2026-09-16T00:00:00Z"}
			dir := directory(root, duty)
			os.MkdirAll(dir, 0700)
			b, _ := json.Marshal(r)
			path := filepath.Join(dir, "00000000000000000001.json")
			switch mode {
			case "gap":
				path = filepath.Join(dir, "00000000000000000002.json")
			case "unknown":
				b = append([]byte(`{"extra":1,`), b[1:]...)
			case "duplicate":
				b = append([]byte(`{"version":1,`), b[1:]...)
			case "owner":
				r.PreviousOwner = "blocked"
				b, _ = json.Marshal(r)
			}
			os.WriteFile(path, b, 0600)
			if mode == "symlink" {
				os.Rename(path, path+".saved")
				os.Symlink(path+".saved", path)
			}
			record, release, err := AcquireFence(root, source)
			if mode != "valid" && mode != "busy" {
				if err == nil {
					release()
					t.Fatal("invalid history accepted")
				}
				return
			}
			if err != nil || record.Owner != "manifest" {
				t.Fatal(record, err)
			}
			defer release()
			if mode == "busy" {
				if _, release2, err := AcquireFence(root, source); err == nil {
					release2()
					t.Fatal("shared lock bypassed")
				}
			}
		})
	}
}
func TestFencePrepareDoesNotWriteAndRejectsUnknown(t *testing.T) {
	root := t.TempDir()
	r, err := FenceSnapshot(root, "pocket")
	if err != nil || r.Owner != "excalibur" || r.Revision != 0 {
		t.Fatal(r, err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("prepare wrote files")
	}
	if _, _, err := AcquireFence(root, "unknown"); err == nil {
		t.Fatal("unknown source admitted")
	}
}
