package connectorhandoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/mdfm"
)

// WriteCheckpoint publishes one immutable, file-readable snapshot under dataDir.
// A hard link provides atomic no-replace semantics even between processes. This
// does not create an active cursor or a routing record, and never copies tokens.
func WriteCheckpoint(dataDir, source string, checkpoint any) (string, error) {
	if !validSource(source) || dataDir == "" {
		return "", fmt.Errorf("explicit dataDir and connector source required")
	}
	b, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	hash := hex.EncodeToString(digest[:])
	dir := filepath.Join(dataDir, "connector-handoff", "checkpoints", source)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("checkpoint directory unavailable [REDACTED]")
	}
	f, err := os.CreateTemp(dir, ".checkpoint-*")
	if err != nil {
		return "", fmt.Errorf("checkpoint staging failed [REDACTED]")
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return "", fmt.Errorf("checkpoint write failed [REDACTED]")
	}
	if err = os.Link(f.Name(), filepath.Join(dir, hash+".json")); err != nil {
		return "", fmt.Errorf("checkpoint already exists or publication failed [REDACTED]")
	}
	d, err := os.Open(dir)
	if err != nil {
		return "", fmt.Errorf("checkpoint directory unavailable [REDACTED]")
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return "", fmt.Errorf("checkpoint durability uncertain; do not retry automatically")
	}
	return hash, nil
}

// CheckLegacyPause checks a conservative, read-only drain observation. It is
// deliberately NOT proof of exclusion: legacy scheduling/spooling has no shared
// fence, and may race even two identical observations.
func CheckLegacyPause(root, source string) error {
	if !validSource(source) {
		return fmt.Errorf("invalid connector source")
	}
	b, err := os.ReadFile(filepath.Join(root, "spirits", "ea-coordinator", "rituals", source+"-sync.md"))
	if err != nil {
		return fmt.Errorf("legacy schedule unreadable")
	}
	fm, _ := mdfm.Split(string(b))
	if fm["enabled"] != "false" || strings.TrimSpace(fm["paused_reason"]) == "" {
		return fmt.Errorf("legacy schedule enabled or pause reason missing")
	}
	entries, err := os.ReadDir(filepath.Join(root, "vessel", "spool"))
	if err != nil {
		return fmt.Errorf("legacy queue unreadable")
	}
	if len(entries) != 0 {
		return fmt.Errorf("legacy queued or uncertain work exists")
	}
	dir := filepath.Join(root, "artifacts", "runs")
	entries, err = os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("legacy runs unreadable")
	}
	for _, ent := range entries {
		if !strings.HasSuffix(ent.Name(), ".md") {
			continue
		}
		if !ent.Type().IsRegular() {
			return fmt.Errorf("legacy run inventory uncertain")
		}
		b, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			return fmt.Errorf("legacy run unreadable")
		}
		fm, _ := mdfm.Split(string(b))
		if fm["outcome"] == "running" || fm["outcome"] == "" {
			return fmt.Errorf("legacy running or uncertain work exists")
		}
	}
	return nil
}
