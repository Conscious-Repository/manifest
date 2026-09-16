// transcript-worker runs the existing successor cadence in read-only continuity
// mode without restarting the dashboard or granting vault/approval write access.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"manifest/transcriptsync"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "", "explicit worker config")
	flag.Parse()
	var cfg struct {
		DataDir        string                `json:"dataDir"`
		Index          string                `json:"index"`
		TranscriptSync transcriptsync.Config `json:"transcriptSync"`
		KeyFiles       map[string]string     `json:"keyFiles"`
	}
	b, err := os.ReadFile(*path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.DataDir == "" || cfg.Index == "" || cfg.TranscriptSync.LegacyRoot == "" {
		return fmt.Errorf("explicit dataDir, index and legacy root required")
	}
	for _, source := range []string{"granola", "pocket"} {
		c := cfg.TranscriptSync.Granola
		if source == "pocket" {
			c = cfg.TranscriptSync.Pocket
		}
		if !c.Enabled {
			continue
		}
		if !c.ContinuityOnly || c.Account == "" {
			return fmt.Errorf("worker requires named accounts and continuityOnly=true")
		}
		key, err := os.ReadFile(cfg.KeyFiles[source])
		if err != nil {
			return fmt.Errorf("%s credential file unavailable", source)
		}
		name := "GRANOLA_API_KEY"
		if source == "pocket" {
			name = "POCKET_API_KEY"
		}
		if err = os.Setenv(name, string(key)); err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(cfg.Index)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.Ping(); err != nil {
		return err
	}
	svc := transcriptsync.New(cfg.DataDir, cfg.TranscriptSync, transcriptsync.NewIndex(db), nil).WithHandoffGuard(cfg.DataDir)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Refuse startup before scheduling if any configured source is not activated.
	for _, source := range []string{"granola", "pocket"} {
		if svc.Enabled(source) {
			if _, err = svc.VerifyContinuity(ctx, source); err != nil {
				return fmt.Errorf("%s continuity: %w", source, err)
			}
		}
	}
	svc.Start(ctx)
	<-ctx.Done()
	return nil
}
