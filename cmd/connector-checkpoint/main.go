// connector-checkpoint reads frozen connector state and canonical approval
// history. Default is strictly read-only; -stage publishes an immutable snapshot
// under an explicit dataDir. Neither action transfers ownership or credentials.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/transcriptsync"
	_ "modernc.org/sqlite"
)

type options struct {
	Source, Root, Account, EmailState, Index, DataDir, ExpectState, ExpectApprovals string
	Stage, Apply                                                                    bool
}

func main() {
	var o options
	flag.StringVar(&o.Source, "source", "", "email, granola or pocket")
	flag.StringVar(&o.Root, "legacy-root", "", "frozen Excalibur tree")
	flag.StringVar(&o.Account, "account", "", "explicit source account binding (never inferred from credentials)")
	flag.StringVar(&o.EmailState, "email-state", "", "explicit primary or extra-account legacy state JSON")
	flag.StringVar(&o.Index, "index-snapshot", "", "consistent SQLite backup, without WAL (read-only)")
	flag.StringVar(&o.DataDir, "data-dir", "", "destination for optional immutable staging")
	flag.StringVar(&o.ExpectState, "expect-state-hash", "", "refuse state drift against prior preview")
	flag.StringVar(&o.ExpectApprovals, "expect-approval-hash", "", "refuse canonical inbox drift against prior preview")
	flag.BoolVar(&o.Stage, "stage", false, "save immutable checkpoint only; no active cursor or ownership change")
	flag.BoolVar(&o.Apply, "apply", false, "request handoff (blocked until shared dispatch fence exists)")
	flag.Parse()
	if err := run(o, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(o options, out io.Writer) error {
	if o.Source != "email" && o.Source != "granola" && o.Source != "pocket" {
		return fmt.Errorf("valid source required")
	}
	if o.Root == "" || o.Account == "" {
		return fmt.Errorf("legacy root and explicit account binding required")
	}
	if o.Apply {
		if err := connectorhandoff.CheckLegacyPause(o.Root, o.Source); err != nil {
			return err
		}
		return fmt.Errorf("handoff blocked: legacy dispatch fence and verified account/source reconciliation required; no files written")
	}
	var checkpoint any
	var stateHash, approvalHash string
	var count, uncertain int
	if o.Source == "email" {
		inv, err := approvals.ReadConnectorInventory(filepath.Join(o.Root, "artifacts"))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(o.EmailState)
		if err != nil {
			return fmt.Errorf("explicit email state unreadable [REDACTED]")
		}
		cp, err := connectorhandoff.PrepareEmail(b, o.Account, inv)
		if err != nil {
			return err
		}
		again, err := approvals.ReadConnectorInventory(filepath.Join(o.Root, "artifacts"))
		if err != nil || again.Hash != inv.Hash {
			return fmt.Errorf("approval snapshot changed")
		}
		latest, err := os.ReadFile(o.EmailState)
		if err != nil || string(latest) != string(b) {
			return fmt.Errorf("email state changed")
		}
		checkpoint, stateHash, approvalHash, count, uncertain = cp, cp.LegacyHash, inv.Hash, len(cp.Threads), len(cp.Uncertain)
	} else {
		if o.Index == "" {
			return fmt.Errorf("frozen source index required")
		}
		// immutable=1 guarantees SQLite does not create journal/SHM sidecars. A
		// WAL-bearing input is refused rather than silently reading a stale snapshot.
		for _, suffix := range []string{"-wal", "-journal"} {
			if _, err := os.Lstat(o.Index + suffix); !os.IsNotExist(err) {
				return fmt.Errorf("index must be a consistent SQLite backup without WAL/journal")
			}
		}
		abs, err := filepath.Abs(o.Index)
		if err != nil {
			return fmt.Errorf("invalid index path")
		}
		u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&immutable=1"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			return fmt.Errorf("index unavailable")
		}
		defer db.Close()
		cfg := transcriptsync.Config{Granola: transcriptsync.SourceConfig{Account: o.Account}, Pocket: transcriptsync.SourceConfig{Account: o.Account}}
		// An explicit dataDir also checks that no successor state will be overwritten.
		if o.DataDir == "" {
			return fmt.Errorf("explicit dataDir required to check existing successor state")
		}
		s := transcriptsync.New(o.DataDir, cfg, transcriptsync.NewIndex(db), nil)
		st, hash, err := s.ReconcileCheckpoint(o.Source, o.Root)
		if err != nil {
			return err
		}
		checkpoint = struct {
			State        transcriptsync.State `json:"state"`
			ApprovalHash string               `json:"approvalHash"`
		}{st, hash}
		stateHash, approvalHash, count = st.ImportedFrom, hash, len(st.Items)
	}
	if o.ExpectState != "" && o.ExpectState != stateHash || o.ExpectApprovals != "" && o.ExpectApprovals != approvalHash {
		return fmt.Errorf("checkpoint comparison failed; source state or canonical approvals changed")
	}
	stagedHash := ""
	if o.Stage {
		var err error
		stagedHash, err = connectorhandoff.WriteCheckpoint(o.DataDir, o.Source, checkpoint)
		if err != nil {
			return err
		}
	}
	phase := connectorhandoff.CheckpointReady
	if uncertain != 0 {
		phase = connectorhandoff.Blocked
	} // snapshot needs review; does not pause runtime
	return json.NewEncoder(out).Encode(map[string]any{"source": o.Source, "phase": phase, "stateHash": stateHash, "approvalHash": approvalHash, "items": count, "uncertain": uncertain, "stagedHash": stagedHash, "account": "[REDACTED]", "ownership": "excalibur", "activation": "blocked: shared dispatch fence, verified account binding, complete historical source reconciliation and live continuity evidence required"})
}
