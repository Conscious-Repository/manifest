package signals

// SyncConflicts — the manifest-sync daemon's parked-conflict marker as a FEED
// card (big-change Phase 2b). The daemon (cmd/manifest-sync) writes
// <dataDir>/sync/<name>.conflict.json when a pull --rebase conflicts and it
// parks; the human resolves rebase-style and the daemon deletes the file —
// so this signal auto-clears by construction. The human is the mutex; the
// FEED is where the mutex gets paged.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type syncConflict struct {
	Root  string    `json:"root"`
	Name  string    `json:"name"`
	Paths []string  `json:"paths"`
	Since time.Time `json:"since"`
}

// SyncConflicts emits one signal per parked sync root. stateDir is the
// daemon's state dir (<dataDir>/sync).
func SyncConflicts(stateDir string) Emitter { return syncEmitter{stateDir} }

type syncEmitter struct{ dir string }

func (e syncEmitter) Emit(now time.Time) ([]Signal, error) {
	matches, err := filepath.Glob(filepath.Join(e.dir, "*.conflict.json"))
	if err != nil {
		return nil, err
	}
	var out []Signal
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var c syncConflict
		if json.Unmarshal(b, &c) != nil || c.Name == "" {
			continue
		}
		age := int(now.Sub(c.Since).Hours() / 24)
		if age < 0 {
			age = 0
		}
		n := len(c.Paths)
		label := fmt.Sprintf("sync parked · %s · %d file%s — resolve rebase-style in %s",
			c.Name, n, plural(n), c.Root)
		sort.Strings(c.Paths)
		out = append(out, Signal{
			ID:     "sync-conflict:" + c.Name,
			Kind:   "sync-conflict",
			Entity: c.Name,
			Label:  label,
			Age:    age,
			Hash:   c.Since.Format(time.RFC3339) + ":" + strings.Join(c.Paths, ","),
		})
	}
	return out, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
