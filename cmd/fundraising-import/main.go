// Command fundraising-import performs the one-time Google Sheet CSV migration.
// It has no network or Google dependency; export the sheet to CSV first.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"manifest/fundraising"
	"manifest/record"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

func main() {
	vault := flag.String("vault", "", "ConsciousRepo vault root")
	system := flag.String("system-root", "system", "vault-relative system root")
	csvPath := flag.String("csv", "", "exported Sheet1 CSV")
	dryRun := flag.Bool("dry-run", false, "parse and report without writing")
	flag.Parse()
	if *vault == "" || *csvPath == "" {
		fmt.Fprintln(os.Stderr, "-vault and -csv are required")
		os.Exit(2)
	}
	f, err := os.Open(*csvPath)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	rows, resources, err := fundraising.ReadSheetCSV(f)
	if err != nil {
		fatal(err)
	}
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: *vault, SystemRoot: *system})
	if err != nil {
		fatal(err)
	}
	defer ix.Close()
	if _, err = ix.Rebuild(); err != nil {
		fatal(err)
	}
	exact := func(name string) (fundraising.PersonRef, bool) {
		name = strings.TrimSpace(name)
		if name == "" {
			return fundraising.PersonRef{}, false
		}
		refs, e := ix.Search(name)
		if e != nil {
			return fundraising.PersonRef{}, false
		}
		var found *vaultindex.SearchRef
		for i := range refs {
			r := &refs[i]
			if (strings.EqualFold(r.Key, name) || strings.EqualFold(r.Display, name)) && r.IsPerson {
				if found != nil && found.Key != r.Key {
					return fundraising.PersonRef{}, false
				}
				found = r
			}
		}
		if found == nil {
			return fundraising.PersonRef{}, false
		}
		return fundraising.PersonRef{Key: found.Key, Display: name, NotePath: found.NotePath}, true
	}
	ops := fundraising.NormalizeSheet(rows, exact)
	fmt.Printf("source rows: %d; normalized opportunities: %d; resources: %d\n", len(rows), len(ops), len(resources))
	if *dryRun {
		return
	}
	frRoot := filepath.ToSlash(filepath.Join(*system, "crm", "fundraising"))
	registry := filepath.ToSlash(filepath.Join(*system, "crm", "contacts.md"))
	vw := vaultwriter.New(*vault).WithZoneRoots(*system, "extrinsic").Grant(
		vaultwriter.Capability{Name: "fundraising-import", Zone: record.ZoneSystem, Pattern: frRoot + "/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "fundraising-contacts-import", Zone: record.ZoneSystem, Pattern: registry, Actor: vaultwriter.ActorUserAction},
	)
	store := fundraising.NewStore(*vault, frRoot, registry, vw.BindAbs("fundraising-import"), vw.BindAbs("fundraising-contacts-import"))
	if err := store.Ensure(); err != nil {
		fatal(err)
	}
	for _, op := range ops {
		if _, err := store.ImportUpsert(op); err != nil {
			fatal(err)
		}
	}
	if err := store.SaveResources(resources); err != nil {
		fatal(err)
	}
	fmt.Printf("imported %d opportunities into %s\n", len(ops), frRoot)
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
