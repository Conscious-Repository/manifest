// Command aion-dates is the one-time prep for the portal timeline sync
// (linkage-scope §7 + sync-contract): it seeds explicit start/due dates on the
// live Aion rocks (from each rock's quarter, as editable starting values),
// normalizes the last owner-initials drift (YS→Y), fills undated decided
// decisions from their source-note date, and clears prose needed_by values the
// portal can't read as ISO. Writes go through the audited vaultwriter goals +
// aion capabilities and are fixpoint-preserving.
//
// Default is a DRY RUN printing every proposed change; pass -apply to write.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"manifest/aion"
	"manifest/goals"
	"manifest/record"
	"manifest/vaultwriter"
)

var isoRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var leadingDateRe = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
var endOfRe = regexp.MustCompile(`(?i)end of\s+([a-z]+)\s+(\d{4})`)

var months = map[string]string{
	"january": "01", "february": "02", "march": "03", "april": "04",
	"may": "05", "june": "06", "july": "07", "august": "08",
	"september": "09", "october": "10", "november": "11", "december": "12",
}
var lastDay = map[string]string{
	"01": "31", "02": "28", "03": "31", "04": "30", "05": "31", "06": "30",
	"07": "31", "08": "31", "09": "30", "10": "31", "11": "30", "12": "31",
}

func quarterStart(q string) string {
	switch tail(q) {
	case "Q1":
		return q[:4] + "-01-01"
	case "Q2":
		return q[:4] + "-04-01"
	case "Q3":
		return q[:4] + "-07-01"
	default:
		return q[:4] + "-10-01"
	}
}
func quarterEnd(q string) string {
	switch tail(q) {
	case "Q1":
		return q[:4] + "-03-31"
	case "Q2":
		return q[:4] + "-06-30"
	case "Q3":
		return q[:4] + "-09-30"
	default:
		return q[:4] + "-12-31"
	}
}
func tail(q string) string {
	if len(q) >= 2 {
		return q[len(q)-2:]
	}
	return ""
}

// normalizeNeededBy maps a prose deadline to ISO where unambiguous, else clears
// it. Returns (value, change): change=false means already ISO — leave it.
func normalizeNeededBy(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || isoRe.MatchString(s) {
		return "", false
	}
	if m := endOfRe.FindStringSubmatch(s); m != nil {
		if mm, ok := months[strings.ToLower(m[1])]; ok {
			return m[2] + "-" + mm + "-" + lastDay[mm], true // "end of July 2026" → 2026-07-31
		}
	}
	return "", true // ambiguous prose → clear (text stays in the item title)
}

func main() {
	home, _ := os.UserHomeDir()
	vault := flag.String("vault", filepath.Join(home, "Documents", "index.ben"), "vault root")
	dataDir := flag.String("datadir", filepath.Join(home, ".config", "manifest"), "dataDir (write-audit log)")
	apply := flag.Bool("apply", false, "write the changes (default: dry run)")
	flag.Parse()

	aionRoot := filepath.ToSlash(filepath.Join("system", "aion"))
	vw := vaultwriter.New(*vault).WithZoneRoots("system", "extrinsic").WithAudit(*dataDir).
		Grant(
			vaultwriter.Capability{Name: "goals", Zone: record.ZoneKnowledge, Pattern: "goals*", Actor: vaultwriter.ActorUserAction},
			vaultwriter.Capability{Name: "aion", Zone: record.ZoneSystem, Pattern: aionRoot + "/**", Actor: vaultwriter.ActorUserAction},
		)
	goalsStore := goals.NewStore(stubLoc{}, *vault, "goals.md", vw.BindAbs("goals"))
	aionStore := aion.NewStore(*vault, aionRoot, vw.BindAbs("aion"))

	// ---- 1) goals.md: seed rock dates + fix YS→Y ----
	gd := goalsStore.Load()
	goalsChanged := false
	fmt.Println("== GOALS: seed rock start/due (from quarter) + owner fix ==")
	for _, a := range gd.Areas {
		if a.Name != "Aion" {
			continue
		}
		var walk func(g *goals.Goal, isRock bool)
		walk = func(g *goals.Goal, isRock bool) {
			if g.Owner == "YS" {
				fmt.Printf("  owner  %-40s YS → Y\n", short(g.Text))
				g.Owner = "Y"
				goalsChanged = true
			}
			if isRock && !g.Checked && g.Quarter != "" && (g.Start == "" || g.Due == "") {
				if g.Start == "" {
					g.Start = quarterStart(g.Quarter)
				}
				if g.Due == "" {
					g.Due = quarterEnd(g.Quarter)
				}
				fmt.Printf("  dates  %-40s %s → %s..%s\n", short(g.Text), g.Quarter, g.Start, g.Due)
				goalsChanged = true
			}
			for _, c := range g.Children {
				walk(c, false)
			}
		}
		for _, r := range a.Rocks {
			walk(r, true)
		}
		for _, an := range a.Annuals {
			walk(an, false)
		}
	}

	// ---- 2) backlog: fill undated decided + normalize prose needed_by ----
	doc := aionStore.LoadBacklog()
	var edits []aion.LinkEdit
	fmt.Println("\n== BACKLOG: undated decided + prose needed_by ==")
	for _, it := range doc.Items() {
		if it.Kind == aion.KindDecision && it.Status == aion.StatusDecided && it.Decided == "" {
			d := sourceDate(it.Sources)
			if d != "" {
				dd := d
				edits = append(edits, aion.LinkEdit{ID: it.ID, Decided: &dd})
				fmt.Printf("  decided  %-38s (from source) → %s\n", short(it.Text), d)
			} else {
				fmt.Printf("  decided  %-38s UNRECOVERABLE (left blank → coverage warn)\n", short(it.Text))
			}
		}
		if it.NeededBy != "" {
			if v, change := normalizeNeededBy(it.NeededBy); change {
				vv := v
				edits = append(edits, aion.LinkEdit{ID: it.ID, NeededBy: &vv})
				act := "→ " + v
				if v == "" {
					act = "→ (cleared; prose kept in title)"
				}
				fmt.Printf("  needed_by %-37s %q %s\n", short(it.Text), it.NeededBy, act)
			}
		}
	}

	fmt.Printf("\n%d goals change; %d backlog edits.\n", boolInt(goalsChanged), len(edits))
	if !*apply {
		fmt.Println("\nDRY RUN — re-run with -apply to write.")
		return
	}

	if goalsChanged {
		if err := goalsStore.Save(gd); err != nil {
			fatal("save goals: %v", err)
		}
	}
	if len(edits) > 0 {
		n, err := aionStore.BatchLink(edits)
		if err != nil {
			fatal("batchlink: %v", err)
		}
		fmt.Printf("applied: goals saved (%d), %d backlog items updated.\n", boolInt(goalsChanged), n)
	} else {
		fmt.Printf("applied: goals saved (%d).\n", boolInt(goalsChanged))
	}
	fmt.Println("rebuild + PUBLISH to ship.")
}

// sourceDate pulls the leading ISO date from the first [[wikilink]] source
// (e.g. "2026-07-27 derya ii" → 2026-07-27), the recoverable decided fallback.
func sourceDate(sources []string) string {
	for _, s := range sources {
		if m := leadingDateRe.FindStringSubmatch(s); m != nil {
			return m[1]
		}
	}
	return ""
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 40 {
		return s[:37] + "…"
	}
	return s
}
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type stubLoc struct{}

func (stubLoc) GoalsPath() string { return "" }

func fatal(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }
