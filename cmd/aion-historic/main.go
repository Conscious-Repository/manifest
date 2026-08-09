// Command aion-historic reconstructs the past 90-day rocks the historic AION
// corpus belonged to (aion-historic-rocks scope, one-time): it creates
// thematic "historic rocks" as CLOSED entries in the goals quarter archives,
// adds one persistent live "Operations & Company Health" rock, drops the
// now-obsolete thymus alias, and links every backlog item to a rock via the
// priority classifier below. Writes go through the vaultwriter goals + aion
// capabilities (audited, fixpoint-preserving).
//
// Default is a DRY RUN printing the rock definitions + the full item→rock
// table for review; pass -apply to write.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/aion"
	"manifest/goals"
	"manifest/record"
	"manifest/vaultwriter"
)

// historicRock is one past 90-day priority to reconstruct as a closed
// archive entry (goals <quarter>.md).
type historicRock struct {
	ID, Title string
	Serves    []string // current annual ids it fed ("" none)
	Quarter   string   // archive quarter (center of mass)
	Outcome   string   // win | learn
	Evidence  string   // one honest line
}

var historicRocks = []historicRock{
	{"aion/ultrasound-platform", "Ultrasound platform: drivers, wafers, focusing",
		[]string{"aion/mri-prototype", "aion/write-candidate"}, "2026-Q2", "win",
		"vertically-integrated custom drivers (~$70k→low-thousands), repeatable wafer fab, first focus in water"},
	{"aion/mri-read-architecture", "Low-field MRI read architecture",
		[]string{"aion/mri-prototype"}, "2026-Q2", "win",
		"selected single-sided permanent-magnet low-field MRF; seeded the human-scale-spec rock"},
	{"aion/mechanism-validation", "Magnetoacoustic / ICR / CISS mechanism validation",
		[]string{"aion/write-candidate", "aion/cell-state-control"}, "2026-Q1", "learn",
		"rebuilt Mechanism-1 for precision; isolated ultrasound-only controls after mixed magnetoacoustic results"},
	{"aion/thymus-program", "Thymus immuno-reconstitution program",
		[]string{"aion/write-candidate"}, "2026-Q2", "win",
		"thymus chosen as first therapeutic beachhead (post-chemo immune reconstitution); organoid model stood up"},
	{"aion/cell-reprogramming-wetlab", "Cell reprogramming & voltage-reporter wetlab",
		[]string{"aion/cell-state-control"}, "2026-Q1", "win",
		"stable GEVI/GECI lines, iPSC + dermal-fibroblast reprogramming, orthogonal ion reporters"},
	{"aion/regulatory-strategy", "Regulatory & reimbursement strategy",
		[]string{"aion/write-candidate"}, "2026-Q1", "win",
		"Class II biomarker vs Class III PMA precedents mapped; reimbursement discovery sprint completed"},
	{"aion/team-building", "Hiring & team building",
		nil, "2026-Q2", "win",
		"founding engineer/scientist/architect hires (Heye, Nirosha, Yashiro, Morgan) + visa paths"},
	{"aion/partnerships-advisory", "Partnerships & advisory",
		nil, "2026-Q2", "win",
		"Columbia/Nirosha license, ETH collaboration, DiPersio SAB, Confluence vivarium, CRO pig path"},
}

// opsRock — the persistent live catch-all (goals.md ## Aion), no serves.
const opsRockID = "aion/operations-health"
const opsRockTitle = "Operations & Company Health"

// classifier: priority-ordered raw text → rock id. First match wins; the
// default is the ops rock. Existing current rocks absorb their history
// (series-a-15m ← fundraising, mouse-to-pig ← animal).
var rules = []struct {
	rock string
	re   *regexp.Regexp
}{
	{"aion/mri-read-architecture", reI(`\bMRI\b|single-sided|fingerprint|voxel|hyperfine|benchtop|read problem|read platform`)},
	{"aion/ultrasound-platform", reI(`ultrasound|acoustic|transducer|wafer|sonicat|hydrophone|\bSAW\b|standing wave|pMUT|microbubble|focused`)},
	{"aion/mechanism-validation", reI(`\bICR\b|\bIPR\b|magnetoacoustic|CISS|mitochond|hyperpolar|mechanism-1|mechanism 1|field param|\bcoil\b|\bEPR\b|spin`)},
	{"aion/thymus-program", reI(`thymus|immune|dipersio|organoid|T-cell|reconstitution|chemo|nirosha|columbia|murugan`)},
	{"aion/cell-reprogramming-wetlab", reI(`cell line|gevi|geci|reprogram|ipsc|voltage indicator|calcium|potassium|planaria|embryonic stem|\bH1\b|assay|buffer|ki67|rna-seq`)},
	{"aion/mouse-to-pig", reI(`\bmouse\b|\bmice\b|\bpig\b|in vivo|vivarium|confluence|iacuc|\banimal\b`)},
	{"aion/regulatory-strategy", reI(`\bFDA\b|regulat|class i|\bPMA\b|de novo|reimburse|indication|tool claim|insurance|\bCPT\b`)},
	{"aion/series-a-15m", reI(`fundrais|\braise\b|series a|\bSAFE\b|investor|\bLOI\b|term sheet|\bTAM\b|\bdeck\b|\bLP\b|angel|valuation|merger|tetrahedral`)},
	{"aion/team-building", reI(`\bhire\b|hiring|interview|candidate|onboard|recruit|O-1|visa|payroll|yashiro|heye|morgan|\bjack\b|ellie|hannah|gleeson|intern|advisor|\bSAB\b|advisory board`)},
	{"aion/partnerships-advisory", reI(`\bETH\b|collaborat|levin|fraunhofer|\bCRO\b|partner|\bintro\b|justin|laurier|sponsored research|acquisition`)},
	{"aion/operations-health", reI(`website|portal|overview|whitepaper|white paper|logo|brand|media|press|warp|cadence|standing sync|budget|spend|office|lab space|immigration|\bNDA\b|china|timeline|canon|policy`)},
}

func reI(p string) *regexp.Regexp { return regexp.MustCompile(`(?i)` + p) }

// overrides pin specific historic items to a rock the regex classifier gets
// wrong (owner-reviewed in the proposal, 2026-08-09: "accept"). Matched by a
// normalized key (lowercase alphanumerics only) so curly-apostrophe / spacing
// drift between the review text and the corpus can't cause a silent miss.
var overrideMap = func() map[string]string {
	m := make(map[string]string, len(overrides))
	for _, o := range overrides {
		m[normKey(o.text)] = o.rock
	}
	return m
}()

// normKey reduces text to lowercase alphanumerics — robust to punctuation and
// whitespace variants across the two sources.
func normKey(s string) string {
	var b []rune
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b = append(b, r)
		}
	}
	return string(b)
}

func classify(text string) string {
	if rk, ok := overrideMap[normKey(text)]; ok {
		return rk
	}
	for _, r := range rules {
		if r.re.MatchString(text) {
			return r.rock
		}
	}
	return opsRockID
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

	// ---- classify every item ----
	doc := aionStore.LoadBacklog()
	var edits []aion.LinkEdit
	byRock := map[string][]string{}
	hitOverride := map[string]bool{}
	for _, it := range doc.Items() {
		if _, ok := overrideMap[normKey(it.Text)]; ok {
			hitOverride[normKey(it.Text)] = true
		}
		rk := classify(it.Text)
		byRock[rk] = append(byRock[rk], it.Text)
		if it.Rock != rk {
			r := rk
			edits = append(edits, aion.LinkEdit{ID: it.ID, Rock: &r})
		}
	}
	// every reviewed override must land on a real item — a miss means the
	// review text drifted from the corpus and the pin silently did nothing.
	var unmatched []string
	for _, o := range overrides {
		if !hitOverride[normKey(o.text)] {
			unmatched = append(unmatched, o.text)
		}
	}
	if len(unmatched) > 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %d of %d overrides matched no item:\n", len(unmatched), len(overrides))
		for _, u := range unmatched {
			fmt.Fprintf(os.Stderr, "  · %s\n", u)
		}
	}

	// ---- report ----
	fmt.Println("== HISTORIC ROCKS (closed archive entries) ==")
	for _, hr := range historicRocks {
		fmt.Printf("  %-34s %s · %s · serves %v\n    → %s\n", hr.ID, hr.Quarter, hr.Outcome, hr.Serves, hr.Title)
	}
	fmt.Printf("  %-34s LIVE · open · no serves\n    → %s\n", opsRockID, opsRockTitle)
	fmt.Println("\n== ITEM COUNTS PER ROCK ==")
	var keys []string
	for k := range byRock {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(byRock[keys[i]]) > len(byRock[keys[j]]) })
	for _, k := range keys {
		fmt.Printf("  %3d  %s\n", len(byRock[k]), k)
	}
	fmt.Printf("\n%d of %d items change rock.\n", len(edits), len(doc.Items()))

	if v := os.Getenv("AION_HISTORIC_TABLE"); v != "" {
		fmt.Println("\n== FULL ITEM→ROCK TABLE ==")
		for _, k := range keys {
			fmt.Println("\n### " + k)
			for _, t := range byRock[k] {
				fmt.Println("  · " + t)
			}
		}
	}

	if !*apply {
		fmt.Println("\nDRY RUN — re-run with -apply to write. (AION_HISTORIC_TABLE=1 prints the full table.)")
		return
	}

	// ---- apply ----
	// 1) historic rocks → quarter archives
	created := 0
	for _, hr := range historicRocks {
		e := goals.ArchiveEntry{
			Area: "Aion", Text: hr.Title, GoalID: hr.ID, Outcome: hr.Outcome,
			Closed: quarterEnd(hr.Quarter), Serves: hr.Serves, Evidence: hr.Evidence,
		}
		ok, err := goalsStore.AddArchiveEntry(hr.Quarter, e)
		if err != nil {
			fatal("archive %s: %v", hr.ID, err)
		}
		if ok {
			created++
		}
	}
	// 2) persistent ops rock in goals.md ## Aion (forced id, open, no serves)
	gd := goalsStore.Load()
	if _, g := gd.FindGoal(opsRockID); g == nil {
		ng, ok := gd.AddGoal("Aion", "", "rock", opsRockTitle, "")
		if !ok {
			fatal("could not add ops rock")
		}
		ng.Fields = append(ng.Fields, goals.Field{Key: "goal", Value: opsRockID})
		ng.Quarter = goals.CurrentQuarter(time.Now())
		if err := goalsStore.Save(gd); err != nil {
			fatal("save ops rock: %v", err)
		}
	}
	// 3) drop the obsolete thymus alias on cell-state-control
	gd2 := goalsStore.Load()
	if _, g := gd2.FindGoal("aion/cell-state-control"); g != nil {
		var keep []string
		for _, a := range g.Aliases {
			if a != "thymus program" {
				keep = append(keep, a)
			}
		}
		gd2.EditGoal("aion/cell-state-control", goals.GoalEdit{Aliases: &keep})
		_ = goalsStore.Save(gd2)
	}
	// 4) link every item to its rock
	n, err := aionStore.BatchLink(edits)
	if err != nil {
		fatal("batchlink: %v", err)
	}
	fmt.Printf("\napplied: %d historic rocks created, ops rock ensured, %d items relinked.\n", created, n)
	fmt.Println("rebuild + PUBLISH to ship.")
}

// quarterEnd maps "2026-Q2" → its last day (archive closed date).
func quarterEnd(q string) string {
	switch q[5:] {
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

type stubLoc struct{}

func (stubLoc) GoalsPath() string { return "" }

func fatal(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }
