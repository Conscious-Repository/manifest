// personal-email-preflight emits redacted derived evidence to stdout only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"manifest/approvals"
	"manifest/personalemail"
	"os"
	"time"
)

type options struct {
	State, Artifacts, Account string
	Live                      bool
}

func main() {
	var o options
	flag.StringVar(&o.State, "email-state", "", "explicit legacy account state")
	flag.StringVar(&o.Artifacts, "artifacts", "", "canonical legacy artifacts directory")
	flag.StringVar(&o.Account, "account", "", "explicit personal mailbox binding")
	flag.BoolVar(&o.Live, "live-read", false, "explicitly activate read-only Gmail anchor checks; no cutover")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := run(ctx, o, os.Stdout, personalemail.GmailReader); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, o options, out io.Writer, open personalemail.OpenReader) error {
	if o.State == "" || o.Artifacts == "" || o.Account == "" {
		return fmt.Errorf("explicit state, artifacts, and account required")
	}
	raw, err := os.ReadFile(o.State)
	if err != nil {
		return fmt.Errorf("legacy state unavailable")
	}
	inv, err := approvals.ReadEmailContinuityInventory(o.Artifacts)
	if err != nil {
		return err
	}
	r, err := personalemail.Check(ctx, personalemail.Options{Enabled: o.Live, Account: o.Account}, raw, inv, open)
	if err != nil {
		return err
	}
	latest, err := os.ReadFile(o.State)
	if err != nil || !bytes.Equal(raw, latest) {
		return fmt.Errorf("legacy state changed; retry observation")
	}
	again, err := approvals.ReadEmailContinuityInventory(o.Artifacts)
	if err != nil || again.Hash != inv.Hash {
		return fmt.Errorf("approval evidence changed; retry observation")
	}
	return json.NewEncoder(out).Encode(r)
}
