// transcript-cutover prepares or applies an explicitly fenced transcript handoff.
// It never writes the vault or approval folders. Ownership publication remains
// the existing Excalibur dispatch-owner CLI's OS-account-controlled operation.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"manifest/transcriptsync"
	_ "modernc.org/sqlite"
)

func main() {
	source := flag.String("source", "", "granola or pocket")
	root := flag.String("legacy-root", "", "absolute Excalibur root")
	data := flag.String("data-dir", "", "Manifest dataDir")
	account := flag.String("account", "", "explicit owner-asserted source account")
	staged := flag.String("staged-hash", "", "immutable staged checkpoint SHA-256")
	index := flag.String("index-snapshot", "", "consistent SQLite backup without WAL")
	expected := flag.String("expect-plan-hash", "", "prepare hash for explicit apply")
	revision := flag.Uint64("revision", 0, "expected pre-transfer ownership revision")
	apply := flag.Bool("apply", false, "activate only after matching explicit dispatch-owner transfer")
	preflight := flag.Bool("preflight", false, "read-only staged continuity before transfer")
	verify := flag.Bool("verify", false, "read-only continuity, no state/proposal/vault writes")
	keyFile := flag.String("key-file", "", "explicit credential file for read-only verification")
	flag.Parse()
	if err := run(*source, *root, *data, *account, *staged, *index, *expected, *revision, *apply, *verify, *preflight, *keyFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(source, root, data, account, staged, index, expected string, revision uint64, apply, verify, preflight bool, keyFile string) error {
	if (source != "granola" && source != "pocket") || root == "" || data == "" || account == "" || index == "" || (apply && (verify || preflight)) || (verify && preflight) {
		return fmt.Errorf("explicit transcript source, root, dataDir, account and index snapshot required; apply and verify are exclusive")
	}
	for _, suffix := range []string{"-wal", "-journal"} {
		if _, e := os.Lstat(index + suffix); !os.IsNotExist(e) {
			return fmt.Errorf("index must be a consistent backup without WAL/journal")
		}
	}
	abs, err := filepath.Abs(index)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&immutable=1"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	cfg := transcriptsync.Config{LegacyRoot: root, Granola: transcriptsync.SourceConfig{Account: account}, Pocket: transcriptsync.SourceConfig{Account: account}}
	svc := transcriptsync.New(data, cfg, transcriptsync.NewIndex(db), nil)
	if verify || preflight {
		if keyFile != "" {
			b, e := os.ReadFile(keyFile)
			if e != nil {
				return fmt.Errorf("credential file unavailable")
			}
			name := "GRANOLA_API_KEY"
			if source == "pocket" {
				name = "POCKET_API_KEY"
			}
			if e = os.Setenv(name, string(b)); e != nil {
				return e
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		svc.WithHandoffGuard(data)
		var counts map[string]int
		var e error
		if preflight {
			counts, e = svc.PreviewContinuity(ctx, source, root, staged)
		} else {
			counts, e = svc.VerifyContinuity(ctx, source)
		}
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"source": source, "mode": "read-only", "continuity": counts})
	}
	var p transcriptsync.CutoverPlan
	if apply {
		p, err = svc.ApplyCutover(source, root, staged, expected, revision)
	} else {
		p, err = svc.PrepareCutover(source, root, staged)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"plan": p, "planHash": p.Hash(), "applied": apply})
}
