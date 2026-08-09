// Command aion-linkage is the ONE-TIME backlog↔goal linkage migration
// (aion-linkage-scope): it rewrites free-text `rock::` values in
// system/aion/backlog.md to canonical goal ids or null, fills missing
// `[decided::]` on decided decisions from `[captured::]`, and normalizes
// owner initials — the owner-reviewed map below. Writes go through the
// vaultwriter "aion" capability (audited, fixpoint-preserving), same as the
// live dashboard. Aliases (goals.md) are applied separately via the GOALS
// alias UI / PATCH, not here.
//
// Usage:
//
//	aion-linkage [-vault <path>] [-datadir <path>] [-apply]
//
// Default is a DRY RUN that prints the diff; pass -apply to write.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"manifest/aion"
	"manifest/record"
	"manifest/vaultwriter"
)

// rockMap: reviewed raw-rock-value → goal id ("" = drop to null/unanchored).
// Only values listed here change; canonical aion/* ids and alias-resolved
// values (fundraising family, "ICR program", "thymus program") are absent
// and thus left untouched.
var rockMap = map[string]string{
	// one compound that can't alias-match a slug, and a deleted goal
	"thymus program / FDA": "aion/cell-state-control",
	"aion/one-new-idea":    "",

	// sentence-valued capture noise → null
	"Target dream basic-science members Michael Levin and Douglas Blackstone plus translational/clinical experts who review data ~once a year":                     "",
	"One group built an open-hardware version of a ~$100k ultrasound endpoint device":                                                                              "",
	"Luca Turin independently converged on the same SYS-EPR spin-chemistry idea as RJ":                                                                             "",
	"Consider spending $2-10k for a polished static page":                                                                                                          "",
	"Chheda delivers Zika viruses to glioblastoma cancer stem cells and uses an Optune device to improve transduction efficiency":                                  "",
	"BA willing to spend ~$100k over 2 months to solve the field-precision problem":                                                                                "",
	"Also simulate the final therapy (toxicity, removal from body) before pursuing a nanoparticle path":                                                            "",
	"Aim: an experiment AION can't justify internally but that would create value for AION if the Levin lab ran it, to cement an advisory relationship with Levin": "",

	// free slugs with no current rock → honest unanchored (owner declined
	// to force these; alias later via the GOALS UI if wanted)
	"animal-studies":                   "",
	"MRI readout":                      "",
	"nirosha-collaboration":            "",
	"Nirosha collaboration":            "",
	"Nirosha collaboration / hiring":   "",
	"columbia-partnership":             "",
	"regulatory":                       "",
	"regulatory strategy":              "",
	"hardware":                         "",
	"wetlab":                           "",
	"ultrasound-read":                  "",
	"ultrasound-platform":              "",
	"ultrasound delivery":              "",
	"ultrasound simulations":           "",
	"ultrasound hardware":              "",
	"ultrasound cell work":             "",
	"heye-o1":                          "",
	"field-control":                    "",
	"credibility":                      "",
	"cell-experiments":                 "",
	"research-network":                 "",
	"research-alignment":               "",
	"partnerships":                     "",
	"operations":                       "",
	"network":                          "",
	"med-bed":                          "",
	"ip-strategy":                      "",
	"in vivo":                          "",
	"immigration":                      "",
	"hiring":                           "",
	"hiring / intern":                  "",
	"gtm":                              "",
	"bizdev":                           "",
	"timelines":                        "",
	"wearable patch":                   "",
	"BD moonshot":                      "",
	"FDA":                              "",
	"content / regulatory positioning": "",
	"thymus program":                   "", // NOTE: overridden below — see comment
}

// ownerMap: reviewed initials normalization (token → replacement, "" drops).
var ownerMap = map[string]string{
	"YS": "Y",
	"MO": "MM",
	"KY": "",
}

func main() {
	home, _ := os.UserHomeDir()
	vault := flag.String("vault", filepath.Join(home, "Documents", "index.ben"), "vault root")
	dataDir := flag.String("datadir", filepath.Join(home, ".config", "manifest"), "dataDir (for the write-audit log)")
	apply := flag.Bool("apply", false, "write the changes (default is a dry run)")
	flag.Parse()

	// "thymus program" resolves via a goals.md ALIAS, not a rewrite — remove
	// it from the rewrite map so those items keep their raw value (which the
	// portal matches through the alias). Kept in the literal above only to
	// document the decision; deleted here so it's never rewritten.
	delete(rockMap, "thymus program")

	aionRoot := filepath.ToSlash(filepath.Join("system", "aion"))
	vw := vaultwriter.New(*vault).
		WithZoneRoots("system", "extrinsic").
		WithAudit(*dataDir).
		Grant(vaultwriter.Capability{
			Name: "aion", Zone: record.ZoneSystem,
			Pattern: aionRoot + "/**", Actor: vaultwriter.ActorUserAction,
		})
	store := aion.NewStore(*vault, aionRoot, vw.BindAbs("aion"))

	rep, err := store.BackfillLinkage(rockMap, ownerMap, !*apply)
	if err != nil {
		fmt.Fprintln(os.Stderr, "backfill:", err)
		os.Exit(1)
	}
	for _, c := range rep.Changes {
		fmt.Println("  " + c)
	}
	mode := "DRY RUN — would change"
	if *apply {
		mode = "applied"
	}
	fmt.Printf("\n%s: %d rocks→id, %d rocks→null, %d decided dates filled, %d owners normalized (%d total edits)\n",
		mode, rep.RocksRewritten, rep.RocksNulled, rep.DecidedFilled, rep.OwnersFixed, len(rep.Changes))
	if !*apply {
		fmt.Println("re-run with -apply to write.")
	}
}
