package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	vault := flag.String("vault", "", "override vault path from config")
	port := flag.Int("port", 0, "override port from config")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config %s: %v", *configPath, err)
	}
	if *vault != "" {
		cfg.VaultPath = expandHome(*vault)
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if cfg.VaultPath == "" {
		fmt.Fprintln(os.Stderr, "error: vaultPath is not set. Edit config.json or pass -vault /path/to/vault")
		os.Exit(1)
	}
	if _, err := os.Stat(cfg.VaultPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: vault path %q not found yet; notes will be created there on save\n", cfg.VaultPath)
	}

	srv := NewServer(NewStore(cfg))
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	fmt.Printf("manifest → http://%s  (vault: %s)\n", addr, cfg.VaultPath)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
