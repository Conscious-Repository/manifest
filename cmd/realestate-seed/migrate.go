package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"manifest/realestate"
	"manifest/vaultwriter"
)

// -migrate-budget (pass-5 §2): one-time best-effort — legacy `## budget` table
// rows whose category matches a work-stage name seed that stage's [est::]
// (only when the stage carries none). The table text is left in the md
// VERBATIM — the UI simply stopped rendering it; nothing is deleted. Dry-run
// by default; prints a per-property report.
func runMigrateBudget(cfg miniConfig, apply bool) {
	reRoot := filepath.ToSlash(filepath.Join(cfg.SystemRoot, "realestate"))
	w := vaultwriter.New(cfg.VaultPath).WithZoneRoots(cfg.SystemRoot, "extrinsic")
	files, _ := filepath.Glob(filepath.Join(cfg.VaultPath, filepath.FromSlash(reRoot), "properties", "*.md"))
	matched, skippedNoWork, untouched := 0, 0, 0
	for _, full := range files {
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		sections := splitSections(string(raw))
		budget := realestate.ParseBudgetSection(sections["budget"])
		if len(budget) == 0 {
			continue
		}
		stages := realestate.ParseWork(sections["work"])
		if len(stages) == 0 {
			skippedNoWork++
			continue // no work plan yet — nothing to seed into
		}
		slug := strings.TrimSuffix(filepath.Base(full), ".md")
		changed := false
		for _, b := range budget {
			for i := range stages {
				if !nameMatch(stages[i].Text, b.Category) {
					continue
				}
				if stages[i].Est > 0 {
					untouched++
					break // stage already estimated — never overwrite
				}
				fmt.Printf("  [%s] budget %q $%.0f → stage %q [est::]\n", slug, b.Category, b.Amount, stages[i].Text)
				realestate.SetWorkField(stages, stages[i].ID, "est", strconv.FormatFloat(b.Amount, 'f', -1, 64))
				matched++
				changed = true
				break
			}
		}
		if changed && apply {
			rel := reRoot + "/properties/" + slug + ".md"
			if err := w.ReplaceSection(rel, "work", realestate.EmitWork(stages)); err != nil {
				fatal("migrate %s: %v", slug, err)
			}
		}
	}
	fmt.Printf("\nmigrate-budget: %d rows seeded est · %d stages already estimated · %d properties without work plans (tables left verbatim everywhere)\n",
		matched, untouched, skippedNoWork)
	if !apply {
		fmt.Println("DRY RUN — re-run with -migrate-budget -apply to write.")
	}
}

// nameMatch: case-insensitive equality or slug equality ("Rough-in" ≈ "rough in").
func nameMatch(stage, category string) bool {
	return strings.EqualFold(strings.TrimSpace(stage), strings.TrimSpace(category)) ||
		slugify(stage) == slugify(category)
}

// splitSections mirrors realestate.parseSections for the cmd (## blocks).
func splitSections(body string) map[string][]string {
	out := map[string][]string{}
	cur := ""
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimRight(ln, " \t")
		if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
			cur = strings.ToLower(strings.TrimSpace(t[3:]))
			out[cur] = []string{}
			continue
		}
		if cur != "" {
			out[cur] = append(out[cur], ln)
		}
	}
	return out
}
