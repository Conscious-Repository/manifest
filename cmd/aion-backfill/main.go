// Command aion-backfill seeds the AION backlog from an audited handoff
// document (aion-domain spec §7) — through the SAME proposal→approve path
// as the live pipeline, never by direct writes: every record becomes an
// editable pending proposal in the approvals inbox; the owner bulk-approves
// from the FEED. Re-runs are idempotent (sha1 id dedupe spans pending +
// approved + rejected), so a rejected record never resurfaces.
//
// Usage:
//
//	aion-backfill -handoff <handoff.md> -excalibur <excalibur root> [-dry-run]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/aion"
	"manifest/approvals"
	"manifest/secrets"
)

func main() {
	handoffPath := flag.String("handoff", "", "path to the audited handoff document")
	excalibur := flag.String("excalibur", "", "excalibur root (the approvals store lives under <root>/artifacts)")
	dryRun := flag.Bool("dry-run", false, "parse + report only; file nothing")
	flag.Parse()

	if *handoffPath == "" || (*excalibur == "" && !*dryRun) {
		fmt.Fprintln(os.Stderr, "usage: aion-backfill -handoff <file> -excalibur <path> [-dry-run]")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*handoffPath)
	if err != nil {
		fatal("reading handoff: %v", err)
	}
	payloads, err := aion.ParseHandoff(string(raw))
	if err != nil {
		fatal("parsing handoff: %v", err)
	}

	counts := map[string]int{}
	var blocked, filed, deduped int
	var store *approvals.Store
	if !*dryRun {
		store = approvals.NewStore(filepath.Join(*excalibur, "artifacts"))
	}
	for _, p := range payloads {
		counts[p.Kind]++
		if err := p.Validate(nil); err != nil {
			fmt.Printf("SKIP (invalid) %s: %v\n", p.Title, err)
			blocked++
			continue
		}
		action, body := aion.ProposalAction(p), aion.ProposalBody(p)
		if fs := secrets.Scan(action + "\n" + body); len(fs) > 0 {
			// name the source + class, never the value (handoff rule 10)
			src := "(no source)"
			if len(p.Sources) > 0 {
				src = "[[" + p.Sources[0] + "]]"
			}
			fmt.Printf("BLOCKED (secret: %s) from %s — record skipped\n",
				strings.Join(secrets.Classes(fs), ", "), src)
			blocked++
			continue
		}
		if *dryRun {
			filed++
			continue
		}
		typ, applyPath := aion.ProposalTarget(p)
		_, disposition, err := store.ProposeBackfill(approvals.Proposal{
			Type: typ, Action: action, Body: body, ApplyPath: applyPath,
			Agent: "aion-backfill", Ritual: "backfill",
		})
		if err != nil {
			fmt.Printf("ERROR filing %q: %v\n", p.Title, err)
			blocked++
			continue
		}
		if disposition == "filed" {
			filed++
		} else {
			deduped++
		}
	}

	mode := "filed"
	if *dryRun {
		mode = "would file"
	}
	fmt.Printf("\n%s %d proposals (%d tasks, %d decisions, %d heuristics) · %d blocked/skipped · %d deduped\n",
		mode, filed, counts["task"], counts["decision"], counts["heuristic"], blocked, deduped)
	if !*dryRun {
		fmt.Println("review + bulk-approve from the FEED proposals lane.")
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
