// Olga serves only the personal planner, on its own loopback listener.
package main

import (
	"flag"
	"log"
	"manifest/server"
	"net/http"
	"time"
)

func main() {
	vault := flag.String("vault", "", "Parent Manifest vault path")
	addr := flag.String("addr", "127.0.0.1:7781", "Loopback listen address")
	password := flag.String("password-file", "", "File containing the sign-in password (outside vault)")
	audit := flag.String("audit-dir", "", "Write audit directory")
	flag.Parse()
	if *vault == "" || *password == "" {
		log.Fatal("-vault and -password-file are required")
	}
	h, err := server.NewOlgaHandler(*vault, *password, *audit)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Olga Manifest listening on %s", *addr)
	log.Fatal((&http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 16384}).ListenAndServe())
}
