// Olga serves only the personal planner, on its own loopback listener.
package main

import (
	"flag"
	"log"
	"manifest/olgachat"
	"manifest/server"
	"net/http"
	"os"
	"time"
)

func main() {
	vault := flag.String("vault", "", "Parent Manifest vault path")
	addr := flag.String("addr", "127.0.0.1:7781", "Loopback listen address")
	password := flag.String("password-file", "", "File containing the sign-in password (outside vault)")
	audit := flag.String("audit-dir", "", "Write audit directory")
	preview := flag.String("preview", "", "Serve a read-only preview under this path prefix (/preview/<change>); no sign-in, writes refused")
	// Liber, her assistant (plan system/workbench/plans/2026-10-07-olga-chat.md).
	liber := flag.Bool("liber", false, "Turn on Liber")
	hermesBin := flag.String("hermes", "", "Hermes binary (default: hermes on PATH)")
	typesafeKey := flag.String("typesafe-key", "", "File holding the Jev (TypeSafe) key; empty → no router")
	liberLog := flag.String("liber-log", "", "Liber's log for Benjamin (outside the vault)")
	work := flag.String("work-root", "", "Where Liber builds app changes (empty → app changes are noted for Benjamin)")
	runtime := flag.String("runtime", "", "Directory of the live binary and deploy markers")
	origin := flag.String("origin", "git@github.com:Conscious-Repository/manifest.git", "Git remote her changes are pushed to")
	seed := flag.String("seed", "", "Local checkout to clone from first")
	claudeBin := flag.String("claude", "", "Claude Code binary")
	goBin := flag.String("go", "", "Go binary")
	nodeBin := flag.String("node", "", "Node binary for the phone check (empty → check skipped)")
	nodePath := flag.String("node-path", "", "NODE_PATH with playwright")
	flag.Parse()
	if *vault == "" || (*password == "" && *preview == "") {
		log.Fatal("-vault and -password-file are required")
	}
	opts := server.OlgaOptions{Vault: *vault, PasswordFile: *password, AuditDir: *audit, Preview: *preview}
	if *liber && *preview == "" {
		cfg := &server.LiberConfig{HermesBin: *hermesBin, HermesProfile: "olga", VoiceModel: "gpt-5.6-sol", VoiceProvider: "openai-codex", TypesafeKey: *typesafeKey, LogFile: *liberLog}
		if *work != "" && *runtime != "" {
			cfg.Builder = &olgachat.GitBuilder{Origin: *origin, Seed: *seed, Root: *work, Vault: *vault, Runtime: *runtime,
				ClaudeBin: *claudeBin, GoBin: *goBin, NodeBin: *nodeBin, NodePath: *nodePath,
				Restart: func() {
					log.Print("Liber shipped a change; restarting on the new build")
					os.Exit(75)
				}}
		}
		opts.Liber = cfg
	}
	h, err := server.NewOlgaHandlerWith(opts)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Olga Manifest listening on %s", *addr)
	// No WriteTimeout: Liber's updates stream (SSE); handlers bound their own work.
	log.Fatal((&http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 16384}).ListenAndServe())
}
