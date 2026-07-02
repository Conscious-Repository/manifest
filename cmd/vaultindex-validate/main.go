// Command vaultindex-validate rebuilds the vault index headlessly and prints the
// M0 acceptance checks (plans/vault-audit-and-revised-recs.md): it reproduces the
// result sets of categories/_index_people and categories/_index_syncs, walks the
// Shoumik Dabir entity case, and prints the Vocabulary (drift) view — then stops.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"manifest/vaultindex"
)

func main() {
	vaultPath := flag.String("vault", os.ExpandEnv("$HOME/Documents/index.ben"), "vault root")
	db := flag.String("db", "", "SQLite path (empty = in-memory)")
	list := flag.Int("list", 12, "how many rows of each result set to print")
	dump := flag.String("dump", "", "if set, write full sorted result sets to <dir>/{people,sync,first-meeting}.txt")
	flag.Parse()

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: *vaultPath, DBPath: *db})
	if err != nil {
		fatal(err)
	}
	defer ix.Close()

	n, err := ix.Rebuild()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Indexed %d markdown notes from %s\n", n, *vaultPath)

	rule("VALIDATION 1 — _index_people  ·  WHERE contains(categories, \"people\")  SORT file.name ASC")
	people, _ := ix.Category("people", vaultindex.SortNameAsc)
	fmt.Printf("result set: %d notes  (audit §0 expected: 94)\n", len(people))
	printRefs(people, *list)

	rule("VALIDATION 2 — _index_syncs  ·  WHERE contains(categories, \"sync\")  SORT file.mtime DESC")
	syncs, _ := ix.Category("sync", vaultindex.SortMtimeDesc)
	fmt.Printf("result set: %d notes  (audit §0 expected: 68)\n", len(syncs))
	printRefs(syncs, *list)

	fm, _ := ix.Category("first-meeting", vaultindex.SortMtimeDesc)
	fmt.Printf("\n(bonus) _index_first_meetings · contains(categories, \"first-meeting\"): %d notes  (audit §0 expected: 50)\n", len(fm))

	rule("VALIDATION 3 — Shoumik Dabir entity case")
	shoumikCase(ix, "shoumik dabir")

	rule("VALIDATION 4 — Vocabulary view (near-duplicate category values → drift)")
	clusters, _ := ix.Vocabulary()
	if len(clusters) == 0 {
		fmt.Println("EMPTY — no category value has more than one spelling. The vault is")
		fmt.Println("clean (hand-unified); an empty result is the validation. Future drift")
		fmt.Println("would surface here as a cluster.")
	} else {
		fmt.Printf("%d cluster(s) of near-duplicate category values:\n", len(clusters))
		for _, c := range clusters {
			fmt.Printf("  • stem %q (%d notes):\n", c.Stem, c.Total)
			for _, v := range c.Variants {
				ex := ""
				if len(v.Examples) > 0 {
					ex = "  e.g. " + strings.Join(v.Examples, ", ")
				}
				fmt.Printf("      %-24q %3d%s\n", v.Value, v.Count, ex)
			}
		}
	}

	if *dump != "" {
		writeSet(*dump+"/people.txt", people)
		writeSet(*dump+"/sync.txt", syncs)
		writeSet(*dump+"/first-meeting.txt", fm)
		fmt.Printf("\n(dumped full result sets to %s for cross-check)\n", *dump)
	}

	rule("STOP — index layer (M0) validated. Not starting the contacts UI.")
}

func writeSet(path string, refs []vaultindex.NoteRef) {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	sort.Strings(names)
	_ = os.WriteFile(path, []byte(strings.Join(names, "\n")+"\n"), 0o644)
}

func shoumikCase(ix *vaultindex.Index, key string) {
	e, ok := ix.Entity(key)
	fmt.Printf("entity %q exists: %v\n", key, ok)
	if !ok {
		fmt.Println("  (not found — FAIL)")
		return
	}
	fmt.Printf("  note behind it: %v  (expected: none — it is a bare link target)\n", e.HasNote)
	if e.NotePath != "" {
		fmt.Printf("  note_path = %s\n", e.NotePath)
	}

	bl, _ := ix.Interactions(key)
	fmt.Printf("dated backlinks (interactions, AI-authored excluded): %d\n", countDated(bl))
	for _, b := range bl {
		src := b.Date
		if src == "" {
			src = "(undated)"
		}
		fmt.Printf("  %-12s  %s\n", src, b.Name)
	}

	date, srcPath, ok := ix.LastMet(key)
	fmt.Printf("computed last-met: %s  (from %s)\n", date, srcPath)
	fmt.Printf("  → derives only from a DATED source: %v\n", ok && date != "")
}

func printRefs(refs []vaultindex.NoteRef, limit int) {
	for i, r := range refs {
		if i >= limit {
			fmt.Printf("  … and %d more\n", len(refs)-limit)
			break
		}
		fmt.Printf("  %s\n", r.Name)
	}
}

func countDated(bl []vaultindex.Backlink) int {
	n := 0
	for _, b := range bl {
		if b.Date != "" {
			n++
		}
	}
	return n
}

func rule(title string) {
	fmt.Printf("\n%s\n%s\n", strings.Repeat("─", 72), title)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
