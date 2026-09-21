// recruiting-contacts runs the published-address pass over the recruiting
// run cache: for one web run or every web run, read the lab pages the queue
// was found on and file each address the page binds to a queued name as
// contact_published evidence on that draft (recruiting/contacts.go).
//
// It opens the record store READ-ONLY (no vault writer is bound) and writes
// only the run cache under <dataDir>/recruiting/runs, through the same
// writeRun path every queue mutation takes. It creates no record, changes
// no status, sends nothing. --dry-run reads and reports without writing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// excludedSeeds are runs the owner named out of scope for this pass
// (neither is a web run, so the source check refuses them too).
var excludedSeeds = map[string]bool{
	"seed/lab-yablonskiy-lab":                          true,
	"seed/work-whole-body-human-ultrasound-tomography": true,
}

func main() {
	config := flag.String("config", "config.json", "Manifest configuration path")
	runIDs := flag.String("run", "", "comma-separated run ids to pass over")
	all := flag.Bool("all", false, "pass over every web run in the cache")
	dry := flag.Bool("dry-run", false, "read and report; write nothing")
	asJSON := flag.Bool("json", false, "print the results as JSON")
	flag.Parse()
	if err := run(*config, *runIDs, *all, *dry, *asJSON); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type report struct {
	Run      string                    `json:"run"`
	Subject  string                    `json:"subject"`
	Seed     string                    `json:"seed"`
	Result   recruiting.ContactsResult `json:"result"`
	Bindings []string                  `json:"bindings"`
	Error    string                    `json:"error,omitempty"`
}

func run(path, runIDs string, all, dry, asJSON bool) error {
	var cfg struct {
		DataDir    string `json:"dataDir"`
		Vault      string `json:"vaultPath"`
		SystemRoot string `json:"systemRoot"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if !filepath.IsAbs(cfg.DataDir) || !filepath.IsAbs(cfg.Vault) {
		return fmt.Errorf("config needs absolute dataDir and vaultPath")
	}
	if cfg.SystemRoot == "" {
		cfg.SystemRoot = "system"
	}
	// nil writer: the store can read seeds, records and tombstones, and
	// structurally cannot write the vault from here
	records := recruiting.NewStore(cfg.Vault, filepath.ToSlash(filepath.Join(cfg.SystemRoot, "aion", "recruiting")), nil)
	runs, err := recruiting.NewRunStore(filepath.Join(cfg.DataDir, "recruiting", "runs"), records)
	if err != nil {
		return err
	}
	// the same client the application registers (recruiting/run_defaults.go),
	// paced slower than a crawl: lab sites rate-limit a burst of page reads
	runs.Register(sources.Web{Client: http.Client{Timeout: 20 * time.Second}, Delay: 1500 * time.Millisecond})

	var ids []string
	switch {
	case all && runIDs != "":
		return fmt.Errorf("pass either -all or -run, not both")
	case all:
		ids = runs.WebRunIDs()
	case runIDs != "":
		for _, id := range strings.Split(runIDs, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	default:
		return fmt.Errorf("name a run with -run <id>[,<id>…] or pass -all")
	}
	pass, err := runs.NewContactPass(!dry)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var out []report
	for _, id := range ids {
		rep := report{Run: id}
		if prior, err := runs.Get(id); err == nil {
			rep.Subject, rep.Seed = prior.Subject, prior.Seed
			if excludedSeeds[prior.Seed] {
				rep.Error = "excluded by the owner: " + prior.Seed
				out = append(out, rep)
				continue
			}
		}
		got, res, err := pass.Run(ctx, id, time.Now())
		if err != nil {
			rep.Error = err.Error()
			out = append(out, rep)
			continue
		}
		rep.Subject, rep.Seed, rep.Result = got.Subject, got.Seed, res
		for _, d := range got.Drafts {
			if d.Status != recruiting.DraftNew {
				continue
			}
			for _, e := range d.Draft.Evidence {
				if e.Kind == sources.EvidenceContactPublished {
					rep.Bindings = append(rep.Bindings, d.ID+" "+d.Draft.Name+" ← "+e.Snippet)
				}
			}
		}
		sort.Strings(rep.Bindings)
		out = append(out, rep)
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	seen, resolved, unset := 0, 0, 0
	for _, rep := range out {
		if rep.Error != "" {
			fmt.Printf("%s  %s\n   ✗ %s\n", rep.Run, rep.Subject, rep.Error)
			continue
		}
		r := rep.Result
		mode := ""
		if dry {
			mode = " (dry run — nothing written)"
		}
		fmt.Printf("%s  %s\n   drafts seen %d · resolved %d · unset %d · rows added %d%s\n", rep.Run, rep.Subject, r.Seen, r.Resolved, r.Unset, r.Added, mode)
		for _, p := range r.Pages {
			line := "   " + fmt.Sprintf("%-12s", p.Status) + p.URL
			if p.Status == recruiting.ContactsPageRead {
				line += fmt.Sprintf("  (%d bound)", p.Bound)
			} else if p.Why != "" {
				line += "  — " + p.Why
			}
			fmt.Println(line)
		}
		for _, b := range rep.Bindings {
			fmt.Println("      " + b)
		}
		seen += r.Seen
		resolved += r.Resolved
		unset += r.Unset
	}
	fmt.Printf("total: %d runs · drafts seen %d · resolved %d · unset %d\n", len(out), seen, resolved, unset)
	return nil
}
