// personal-email-worker is default-off. Prepare/apply never polls or files
// proposals. The worker only files canonical proposals; the dashboard owns apply.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"manifest/approvals"
	"manifest/personalemail"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "", "explicit JSON config")
	mode := flag.String("mode", "prepare", "prepare, apply, or worker")
	expected := flag.String("expect-plan-hash", "", "reviewed prepare hash")
	rev := flag.Uint64("revision", 0, "pre-transfer fence revision")
	flag.Parse()
	f, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer f.Close()
	var c personalemail.Config
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return err
	}
	svc := personalemail.New(c, nil, nil)
	if *mode == "prepare" || *mode == "apply" {
		var p personalemail.Plan
		if *mode == "prepare" {
			p, err = svc.Prepare()
		} else {
			p, err = svc.Apply(*expected, *rev)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"plan": p, "planHash": p.Hash(), "applied": *mode == "apply"})
	}
	if *mode != "worker" {
		return fmt.Errorf("unknown mode")
	}
	if !c.Enabled {
		return nil
	}
	if !filepath.IsAbs(c.Index) {
		return fmt.Errorf("absolute index required")
	}
	u := url.URL{Scheme: "file", Path: c.Index, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	ap, err := approvals.OpenEmailStore(filepath.Join(c.LegacyRoot, "artifacts"))
	if err != nil {
		return err
	}
	svc = personalemail.New(c, &personalemail.Index{DB: db}, ap)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	for {
		if err = svc.Poll(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(15 * time.Minute):
		}
	}
}
