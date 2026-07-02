// Command manifest serves the local daily-planner dashboard over an Obsidian vault.
//
// Architectural invariant (see obsidian-as-database.md): the app is read-only on the
// knowledge vault. The ONLY writes into the vault are the user's own note saves through
// explicit dashboard actions (daily notes, goals) — never AI-authored content, never in
// the background. All derived/operational state (the calendar cache, and the read-only
// index to come) lives under cfg.DataDir, OUTSIDE the vault, and is disposable/rebuildable.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"manifest/approvals"
	"manifest/calendar"
	"manifest/daily"
	"manifest/goals"
	"manifest/server"
	"manifest/spirits"
	"manifest/vault"
	"manifest/vaultwriter"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	vaultFlag := flag.String("vault", "", "override vault path from config")
	port := flag.Int("port", 0, "override port from config")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config %s: %v", *configPath, err)
	}
	if *vaultFlag != "" {
		cfg.VaultPath = expandHome(*vaultFlag)
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if cfg.VaultPath == "" {
		fmt.Fprintln(os.Stderr, "error: vaultPath is not set. Edit config.json or pass -vault /path/to/vault")
		os.Exit(1)
	}
	if fi, err := os.Stat(cfg.VaultPath); err != nil || !fi.IsDir() {
		log.Fatalf("vault path %q is not a readable directory: %v", cfg.VaultPath, err)
	}
	// Hard invariant: the app never writes derived data into the vault. All
	// derived/operational state (the calendar cache, and the read-only index next)
	// lives under DataDir, which must therefore sit OUTSIDE the vault.
	if pathIsUnder(cfg.DataDir, cfg.VaultPath) {
		log.Fatalf("dataDir %q must live outside the vault %q (derived data never goes in the vault)", cfg.DataDir, cfg.VaultPath)
	}

	idx, err := vault.NewIndex(vaultConfig(cfg))
	if err != nil {
		log.Fatalf("scanning vault: %v", err)
	}
	log.Printf("indexed %d daily notes (goals.md: %s)", len(idx.Dates()), orNone(idx.GoalsPath()))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if w, err := vault.NewWatcher(idx, vaultConfig(cfg)); err != nil {
		log.Printf("file watcher disabled: %v", err)
	} else if err := w.Start(ctx); err != nil {
		log.Printf("file watcher start failed: %v", err)
	} else {
		defer w.Close()
	}

	goalsStore := goals.NewStore(idx, cfg.VaultPath, cfg.GoalsFileName)
	if err := goalsStore.Seed(); err != nil {
		log.Printf("seeding goals.md: %v", err)
	}
	// Silent one-time upgrade from the pre-v2 cascade to the horizon ladder; writes a
	// goals.md.pre-migration backup before its first migrated save (idempotent after).
	if migrated, err := goalsStore.Migrate(time.Now()); err != nil {
		log.Printf("migrating goals.md: %v", err)
	} else if migrated {
		log.Printf("goals.md migrated to the horizon ladder (backup: goals.md.pre-migration)")
	}

	calClient := calendar.NewClient(ctx, cfg.Timezone)
	// Offline calendar mirror is derived data → lives under DataDir, never the vault.
	calSource := calendar.NewSource(calClient, filepath.Join(cfg.DataDir, "calendar-cache"))

	svc := daily.NewService(dailyConfig(cfg), idx)
	svc.UseGoals(server.NewGoalsAdapter(goalsStore))
	svc.UseEvents(calSource)
	srv := server.New(svc, goalsStore, calClient)

	// SPIRITS — the excalibur harness console (plan §2.5: this replaced the
	// Hermes cockpit). The dashboard renders the sibling tree and records
	// user decisions; the engine (a separate process) owns all execution. The
	// approvals inbox is the excalibur surface (warden findings today, the
	// goals-Phase-2 EA later). Save-to-vault stays the one vault write.
	srv.UseVault(vaultwriter.New(cfg.VaultPath))
	if cfg.ExcaliburPath != "" {
		srv.UseSpirits(spirits.NewStore(cfg.ExcaliburPath))
		srv.UseApprovals(approvals.NewStore(filepath.Join(cfg.ExcaliburPath, "artifacts")))
		log.Printf("spirits: %s (approvals inbox: artifacts/approvals)", cfg.ExcaliburPath)
	} else {
		log.Printf("spirits: disabled (set excaliburPath in config to enable the SPIRITS tab)")
	}
	switch {
	case calClient.Enabled():
		log.Printf("google calendar: connected")
	case calClient.NeedsAuth():
		log.Printf("google calendar: credentials found but not authorized (connect from the dashboard)")
	default:
		log.Printf("google calendar: disabled (no credentials in ~/.config/manifest/)")
	}
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	fmt.Printf("manifest → http://%s  (vault: %s)\n", addr, cfg.VaultPath)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

func orNone(s string) string {
	if s == "" {
		return "(none yet)"
	}
	return s
}

func vaultConfig(c Config) vault.Config {
	return vault.Config{
		Root:        c.VaultPath,
		NewDailyDir: c.NewDailyDir,
		GoalsName:   c.GoalsFileName,
		SkipDirs:    c.SkipDirs,
	}
}

func dailyConfig(c Config) daily.Config {
	return daily.Config{
		VaultPath:     c.VaultPath,
		PeriodNoteDir: c.PeriodNoteDir,
		ScheduleStart: c.ScheduleStart,
		ScheduleEnd:   c.ScheduleEnd,
	}
}
