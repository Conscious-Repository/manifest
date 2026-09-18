// transcript-worker runs the guarded successor cadence. Proposal mode only
// creates pending approvals; the dashboard retains the vault apply boundary.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"manifest/approvals"
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
	once := flag.Bool("poll-once", false, "poll each enabled source once from its durable watermark, then exit")
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
		if c.Account == "" {
			return fmt.Errorf("worker requires named accounts")
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
			check, done := context.WithTimeout(ctx, 2*time.Minute)
			_, err = svc.VerifyContinuity(check, source)
			done()
			if err != nil {
				return fmt.Errorf("%s continuity: %w", source, err)
			}
		}
	}
	ap, err := proposalStore(cfg.TranscriptSync)
	if err != nil {
		return err
	}
	svc = transcriptsync.New(cfg.DataDir, cfg.TranscriptSync, transcriptsync.NewIndex(db), ap).WithHandoffGuard(cfg.DataDir)
	if *once {
		var result error
		for _, source := range []string{"granola", "pocket"} {
			if !svc.Enabled(source) {
				continue
			}
			run, done := context.WithTimeout(ctx, 15*time.Minute)
			st, pollErr := svc.Poll(run, source)
			done()
			if pollErr != nil {
				result = errors.Join(result, fmt.Errorf("%s poll: %w", source, pollErr))
			}
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"source": source, "lastAttempt": st.LastAttempt, "lastSuccess": st.LastSuccess, "watermark": st.Watermark, "fetched": st.Fetched, "filed": st.Filed, "skipped": st.Skipped, "waiting": st.Waiting, "error": st.Error}); err != nil {
				return errors.Join(result, err)
			}
		}
		return result
	}
	svc.Start(ctx)
	<-ctx.Done()
	return nil
}

// Open the existing canonical inbox without granting any vault apply capability.
// Continuity-only deployments retain their original read-only behavior.
func proposalStore(cfg transcriptsync.Config) (*approvals.Store, error) {
	if !(cfg.Granola.Enabled && !cfg.Granola.ContinuityOnly) && !(cfg.Pocket.Enabled && !cfg.Pocket.ContinuityOnly) {
		return nil, nil
	}
	artifacts := filepath.Join(cfg.LegacyRoot, "artifacts")
	for _, status := range []string{"pending", "approved", "rejected"} {
		info, err := os.Stat(filepath.Join(artifacts, "approvals", status))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("canonical approval directory required")
		}
	}
	return approvals.NewStore(artifacts), nil
}
