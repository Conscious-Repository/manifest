// Command re-contracts-migrate is the one-shot Pass-2 migration
// (plans/realestate-overhaul-handoff.md §8 step 2): ledger BID rows become
// contract records — the committed-money source swaps from accepted-bid sums
// to accepted-contract allocations in the same release.
//
//   - group bid rows by (contractor, work-id, status)
//   - accepted → an accepted contract with a single allocation
//   - requested/received → thin proposed contracts; declined → declined
//   - contractors without a record get a thin one (name only)
//   - original bid rows STAY in the csv (history; tolerant-read doctrine) —
//     the money engine ignores bids once allocations exist, so nothing
//     double-counts (verified by the acceptance gate)
//
// ACCEPTANCE (refuses to apply on failure): per property, the per-rock money
// picture (Paid / Committed / Recognized / Unreconciled) is identical under
// the legacy bid source vs the new contract source.
//
// Default is a DRY RUN printing every proposed record; pass -apply to write.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/mdfm"
	"manifest/realestate"
	"manifest/record"
	"manifest/vaultwriter"
)

type moneyRow struct{ paid, committed, recognized, unrec float64 }

func main() {
	home, _ := os.UserHomeDir()
	vault := flag.String("vault", filepath.Join(home, "Documents", "index.ben"), "vault root")
	dataDir := flag.String("datadir", filepath.Join(home, ".config", "manifest"), "dataDir (write-audit log)")
	apply := flag.Bool("apply", false, "write the changes (default: dry run)")
	flag.Parse()

	reRoot := filepath.Join("system", "realestate")
	vw := vaultwriter.New(*vault).WithZoneRoots("system", "extrinsic").WithAudit(*dataDir).Grant(
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: filepath.ToSlash(reRoot) + "/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "re-contracts", Zone: record.ZoneSystem,
			Pattern: filepath.ToSlash(reRoot) + "/contracts/**", Actor: vaultwriter.ActorUserAction},
	)

	// ---- load contractors (name → slug) ----
	type contractor struct{ slug, name string }
	contractors := map[string]contractor{} // lower(name) → rec
	ctrDir := filepath.Join(*vault, reRoot, "contractors")
	if entries, err := os.ReadDir(ctrDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(ctrDir, e.Name()))
			if err != nil {
				continue
			}
			fm, _ := mdfm.Split(string(raw))
			slug := strings.TrimSuffix(e.Name(), ".md")
			name := strings.TrimSpace(fm["name"])
			if name == "" {
				name = slug
			}
			contractors[strings.ToLower(name)] = contractor{slug, name}
			contractors[slug] = contractor{slug, name}
		}
	}

	// ---- walk property ledgers, group bid rows ----
	propsDir := filepath.Join(*vault, reRoot, "properties")
	entries, err := os.ReadDir(propsDir)
	if err != nil {
		fatal("read properties dir: %v", err)
	}
	type group struct {
		propSlug, propShort, contractorName, workID, status, date, nodeText string
		amount                                                              float64
	}
	var groups []group
	trees := map[string][]realestate.WorkStage{}
	ledgers := map[string][]realestate.LedgerRow{}
	var slugs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		raw, err := os.ReadFile(filepath.Join(propsDir, e.Name()))
		if err != nil || !strings.Contains(string(raw), "categories: [property]") {
			continue
		}
		fm, body := mdfm.Split(string(raw))
		short := strings.TrimSpace(strings.SplitN(strings.Trim(strings.TrimSpace(fm["address"]), `"`), ",", 2)[0])
		led, err := os.ReadFile(record.Sidecar(filepath.Join(propsDir, e.Name()), record.SidecarLedger))
		if err != nil {
			continue
		}
		rows := realestate.ParseLedgerBytes(led)
		ledgers[slug] = rows
		stages := realestate.ParseWork(sectionLines(body, "rocks"))
		trees[slug] = stages
		slugs = append(slugs, slug)
		nodeText := func(id string) string {
			if _, n := realestate.FindWorkNode(stages, id); n != nil {
				return n.Task.Text
			}
			for _, st := range stages {
				if st.ID == id {
					return st.Text
				}
			}
			seg := id
			if i := strings.LastIndex(id, "/"); i >= 0 {
				seg = id[i+1:]
			}
			return strings.ReplaceAll(seg, "-", " ")
		}
		byKey := map[string]*group{}
		var order []string
		for _, r := range rows {
			if !strings.EqualFold(r.Type, "bid") {
				continue
			}
			who := r.Contractor
			if who == "" {
				who = r.Vendor
			}
			key := strings.ToLower(who) + "|" + r.WorkID + "|" + strings.ToLower(r.Status)
			g, ok := byKey[key]
			if !ok {
				g = &group{propSlug: slug, propShort: short, contractorName: who,
					workID: r.WorkID, status: strings.ToLower(r.Status), date: r.Date,
					nodeText: nodeText(r.WorkID)}
				byKey[key] = g
				order = append(order, key)
			}
			g.amount += r.Amount
			if r.Date != "" && (g.date == "" || r.Date < g.date) {
				g.date = r.Date
			}
		}
		for _, k := range order {
			groups = append(groups, *byKey[k])
		}
	}
	sort.Strings(slugs)

	if len(groups) == 0 {
		fmt.Println("no bid rows anywhere — nothing to migrate")
		return
	}

	// ---- plan the records ----
	fmt.Println("== contractors ==")
	newContractors := map[string]string{} // lower(name) → slug
	for _, g := range groups {
		if _, ok := contractors[strings.ToLower(g.contractorName)]; ok {
			continue
		}
		if _, ok := newContractors[strings.ToLower(g.contractorName)]; ok {
			continue
		}
		slug := record.Slug(g.contractorName, 60)
		newContractors[strings.ToLower(g.contractorName)] = slug
		fmt.Printf("  + create contractor %q → %s\n", g.contractorName, slug)
	}
	resolve := func(name string) string {
		if c, ok := contractors[strings.ToLower(name)]; ok {
			return c.slug
		}
		return newContractors[strings.ToLower(name)]
	}

	fmt.Println("== contracts ==")
	seenSlug := map[string]bool{}
	var newContracts []realestate.Contract
	for _, g := range groups {
		status := map[string]string{
			"accepted": "accepted", "declined": "declined",
			"requested": "proposed", "received": "proposed",
		}[g.status]
		if status == "" {
			status = "proposed"
		}
		tail := g.workID
		if i := strings.LastIndex(tail, "/"); i >= 0 {
			tail = tail[i+1:]
		}
		firstTok := strings.Fields(g.propShort)
		short := g.propSlug
		if len(firstTok) > 0 {
			short = firstTok[0]
		}
		base := record.Slug(resolve(g.contractorName)+" "+tail+" "+short, 80)
		slug := base
		for n := 2; seenSlug[slug]; n++ {
			slug = base + "-" + fmt.Sprint(n)
		}
		seenSlug[slug] = true
		c := realestate.Contract{
			Slug: slug, Name: g.contractorName + " — " + g.nodeText + ", " + g.propShort,
			Contractor: resolve(g.contractorName), Status: status,
			Total: g.amount, Date: g.date,
			Allocations: []realestate.ContractAllocation{{Property: g.propSlug, NodeID: g.workID, Amount: g.amount}},
		}
		newContracts = append(newContracts, c)
		fmt.Printf("  + %-11s %-52s $%-9.0f %s → %s\n", status, slug, g.amount, g.propSlug, g.workID)
	}

	// ---- acceptance: legacy-bid money == contract money, per rock ----
	ok := true
	for _, slug := range slugs {
		before := snapshot(trees, ledgers, slug, nil)
		after := snapshot(trees, ledgers, slug, realestate.AllocationsFor(newContracts, slug))
		if fmt.Sprint(before) != fmt.Sprint(after) {
			ok = false
			fmt.Printf("  ✗ %s MONEY DRIFT\n    legacy:   %v\n    contract: %v\n", slug, before, after)
		}
	}
	if !ok {
		fatal("acceptance failed — nothing written")
	}
	fmt.Println("acceptance: per-rock money identical under both sources ✓")

	if !*apply {
		fmt.Println("\nDRY RUN — pass -apply to write.")
		return
	}
	for lower, slug := range newContractors {
		name := ""
		for _, g := range groups {
			if strings.ToLower(g.contractorName) == lower {
				name = g.contractorName
				break
			}
		}
		rel := filepath.ToSlash(filepath.Join(reRoot, "contractors", slug+".md"))
		content := "---\ncategories: [contractor]\nname: " + name + "\n---\n\n# " + name + "\n"
		if _, err := vw.CreateRecord(rel, content); err != nil {
			fatal("create contractor %s: %v", slug, err)
		}
	}
	for _, c := range newContracts {
		rel := filepath.ToSlash(filepath.Join(reRoot, "contracts", c.Slug+".md"))
		if err := vw.WriteCap("re-contracts", rel, []byte(realestate.NewContractRecord(c))); err != nil {
			fatal("write contract %s: %v", c.Slug, err)
		}
	}
	fmt.Printf("\nAPPLIED: %d contracts, %d contractors created. Bid rows stay in the csvs as history.\n",
		len(newContracts), len(newContractors))
}

// snapshot derives per-rock money on a fresh tree parse under a given
// committed source (nil allocs = legacy bids).
func snapshot(trees map[string][]realestate.WorkStage, ledgers map[string][]realestate.LedgerRow, slug string, allocs []realestate.NodeAllocation) map[string]moneyRow {
	stages := realestate.ParseWork(emitLines(trees[slug]))
	realestate.JoinWorkLedger(stages, ledgers[slug], allocs)
	out := map[string]moneyRow{}
	for _, st := range stages {
		out[st.ID] = moneyRow{st.Paid, st.Committed, st.Recognized, st.Unreconciled}
	}
	return out
}

func emitLines(stages []realestate.WorkStage) []string {
	return strings.Split(strings.TrimRight(realestate.EmitWork(stages), "\n"), "\n")
}

func sectionLines(body, name string) []string {
	var out []string
	in := false
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimRight(ln, " \t")
		if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
			in = strings.EqualFold(t, "## "+name)
			continue
		}
		if in {
			out = append(out, ln)
		}
	}
	return out
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
