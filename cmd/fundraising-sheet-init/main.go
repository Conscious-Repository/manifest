// Command fundraising-sheet-init performs the deliberate, recoverable upgrade
// of the legacy fundraising workbook. Runtime sync remains owned by manifest.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"manifest/fundraising"
	"manifest/record"
	"manifest/vaultwriter"
)

func main() {
	vault := flag.String("vault", "", "Manifest vault root")
	dataDir := flag.String("data-dir", "", "Manifest data directory")
	system := flag.String("system-root", "system", "vault-relative system root")
	spreadsheet := flag.String("spreadsheet", "", "Google spreadsheet ID")
	sheetID := flag.Int64("sheet-id", 0, "Google sheet gid")
	credentials := flag.String("credentials", "", "service-account JSON path (mode 0600)")
	dryRun := flag.Bool("dry-run", false, "validate and report without changing the workbook")
	flag.Parse()
	if *vault == "" || *dataDir == "" || *spreadsheet == "" || *credentials == "" {
		fmt.Fprintln(os.Stderr, "-vault, -data-dir, -spreadsheet, and -credentials are required")
		os.Exit(2)
	}
	frRoot := filepath.ToSlash(filepath.Join(*system, "crm", "fundraising"))
	registry := filepath.ToSlash(filepath.Join(*system, "crm", "contacts.md"))
	vw := vaultwriter.New(*vault).WithZoneRoots(*system, "extrinsic").Grant(
		vaultwriter.Capability{Name: "fundraising-sheet-init", Zone: record.ZoneSystem, Pattern: frRoot + "/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "fundraising-sheet-contacts", Zone: record.ZoneSystem, Pattern: registry, Actor: vaultwriter.ActorUserAction},
	)
	store := fundraising.NewStore(*vault, frRoot, registry, vw.BindAbs("fundraising-sheet-init"), vw.BindAbs("fundraising-sheet-contacts"))
	backend, err := fundraising.NewGoogleSheetBackend(context.Background(), fundraising.GoogleSheetConfig{
		SpreadsheetID: *spreadsheet, SheetID: *sheetID, CredentialsPath: *credentials,
	})
	if err != nil {
		fatal(err)
	}
	syncer := fundraising.NewSheetSync(store, backend, filepath.Join(*dataDir, "fundraising", "sheet-sync.json"),
		"https://docs.google.com/spreadsheets/d/"+*spreadsheet+"/edit#gid="+fmt.Sprint(*sheetID), nil)
	result, err := syncer.Initialize(context.Background(), *dryRun)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("rows: %d; dry-run: %t; already initialized: %t", result.Rows, result.DryRun, result.AlreadyDone)
	if result.BackupTitle != "" {
		fmt.Printf("; backup: %s", result.BackupTitle)
	}
	fmt.Println()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
