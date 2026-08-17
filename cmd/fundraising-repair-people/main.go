// Command fundraising-repair-people reverses the 2026-08-17 migration that
// briefly reclassified legacy fundraising people as text-valued sources.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"manifest/fundraising"
	"manifest/record"
	"manifest/vaultwriter"
)

func main() {
	vault := flag.String("vault", "", "ConsciousRepo vault root")
	system := flag.String("system-root", "system", "vault-relative system root")
	apply := flag.Bool("apply", false, "apply the repair; without this flag the command is read-only")
	flag.Parse()
	if *vault == "" {
		fmt.Fprintln(os.Stderr, "-vault is required")
		os.Exit(2)
	}
	frRoot := filepath.ToSlash(filepath.Join(*system, "crm", "fundraising"))
	registry := filepath.ToSlash(filepath.Join(*system, "crm", "contacts.md"))
	vw := vaultwriter.New(*vault).WithZoneRoots(*system, "extrinsic").Grant(
		vaultwriter.Capability{Name: "fundraising-people-repair", Zone: record.ZoneSystem, Pattern: frRoot + "/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "fundraising-contacts-repair", Zone: record.ZoneSystem, Pattern: registry, Actor: vaultwriter.ActorUserAction},
	)
	store := fundraising.NewStore(*vault, frRoot, registry, vw.BindAbs("fundraising-people-repair"), vw.BindAbs("fundraising-contacts-repair"))
	ops, people, err := store.RepairTextSourcesAsPeople(!*apply)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mode := "would repair"
	if *apply {
		mode = "repaired"
	}
	fmt.Printf("%s %d opportunities and %d missing people\n", mode, ops, people)
}
