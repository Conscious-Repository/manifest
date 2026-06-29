package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"manifest/calendar"
	"manifest/daily"
	"manifest/goals"
	"manifest/server"
	"manifest/vault"
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

	calClient := calendar.NewClient(ctx, cfg.Timezone)
	calSource := calendar.NewSource(calClient, filepath.Join(cfg.VaultPath, cfg.PeriodNoteDir, "cache"))

	svc := daily.NewService(dailyConfig(cfg), idx)
	svc.UseGoals(goalsStore)
	svc.UseEvents(calSource)
	srv := server.New(svc, goalsStore, calClient)
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
