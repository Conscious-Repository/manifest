// Package realestate is the PROPERTIES surface over the vault's real-estate
// records: the `categories: [property]` markdown notes under the system zone
// (system/realestate/), projected into a Board + property pages. Read-only here;
// the writes (create, quick-add log line, quick-add ledger row) go through the
// vaultwriter database-class allow-list in the server layer.
//
// Design rule from the spec: records, not forms. Frontmatter carries only what
// rollups need; the `## budget` table and `## log` lines are free markdown; the
// money facts live in a `<slug>.ledger.csv` sidecar. Numbers (paid%, committed%)
// are DERIVED at read time, never stored.
package realestate

import (
	"encoding/csv"
	"strconv"
	"strings"

	"manifest/tasks"
)

// Property is one Board row / page — a projection of a property record + its
// ledger sidecar. All money figures in Rollup are derived, never persisted.
type Property struct {
	Path       string `json:"path"`                 // vault-relative .md (for the note view)
	Slug       string `json:"slug"`                 // basename without .md (the record key)
	Name       string `json:"name"`                 // note title
	Address    string `json:"address"`              // full standardized address (street, city, ST zip)
	Short      string `json:"short"`                // display name — the street part (before the first comma)
	Entity     string `json:"entity"`               // holding entity — Board groups by this ("" → unassigned)
	Status     string `json:"status"`               // negotiating | under_contract | pre_development | construction | completed | leased | listed | sold
	Kind       string `json:"kind"`                 // rehab | new-construction | mixed | hold
	Control    string `json:"control"`              // owned | tracked
	Deal       string `json:"deal"`                 // [[deal]] wikilink display, if bundled
	Owner      string `json:"owner,omitempty"`      // current owner of record (assessor-stamped by cmd/owner-pull, or hand-set)
	OwnerAddr  string `json:"ownerAddr,omitempty"`  // owner's mailing address
	OwnerSince string `json:"ownerSince,omitempty"` // assessor sale date (their acquisition)
	// The OWNER model (RE spec §2): Entity above = the books the property lands
	// on (required for owned AND acquiring); From = the seller, present only
	// while acquiring. Holdings derive from these — owned and acquiring are
	// counted separately, never summed.
	From         string         `json:"from,omitempty"`  // seller (acquiring only; "" once closed)
	Until        string         `json:"until,omitempty"` // the exit condition ("Refinanced at 75% LTV, DSCR ≥ 1.25")
	Drive        string         `json:"drive,omitempty"` // artifact-canon folder URL (linked, never mirrored)
	Agc          string         `json:"agc,omitempty"`   // AGC project URL (bids/new-construction, link-out)
	Hidden       bool           `json:"hidden"`
	Units        int            `json:"units,omitempty"` // total_units from the source sidecar
	Lat          float64        `json:"lat,omitempty"`   // optional frontmatter map override
	Lng          float64        `json:"lng,omitempty"`
	Budget       []BudgetLine   `json:"budget"`
	Log          []string       `json:"log"`    // free lines, newest-first as written
	Ledger       []LedgerRow    `json:"ledger"` // money facts from the csv sidecar
	Rollup       Rollup         `json:"rollup"` // derived paid/committed/%out
	LastLog      string         `json:"lastLog"`
	Work         []WorkStage    `json:"work"`                   // `## work` stages+todos (management core)
	Tasks        []*tasks.Task  `json:"todos"`                  // `## todos` action lines (redesign stage 4)
	CurrentStage string         `json:"currentStage,omitempty"` // first unchecked stage's text
	WorkStart    string         `json:"workStart,omitempty"`    // frontmatter work-start (schedule anchor)
	Schedule     []StageSpan    `json:"schedule,omitempty"`     // derived spans (§3 — never stored)
	Project      *ProjectBudget `json:"project,omitempty"`      // full-project budget (plan vs spend)
	Intel        *ParcelIntel   `json:"intel,omitempty"`        // research-parcel join (assessor owner/tax facts)
}

// BudgetLine is one `## budget` table row: a category and its budgeted amount.
type BudgetLine struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

// LedgerRow is one money fact from `<slug>.ledger.csv`.
type LedgerRow struct {
	Date       string  `json:"date"`
	Type       string  `json:"type"` // expense | bid
	Category   string  `json:"category"`
	Vendor     string  `json:"vendor"`
	Contractor string  `json:"contractor"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"` // expense: paid · bid: requested|received|accepted|declined
	Note       string  `json:"note"`   // display text — the [work:: id] tether token is stripped into WorkID
	Doc        string  `json:"doc"`
	WorkID     string  `json:"workId,omitempty"`  // tether to a `## work` stage/todo
	PaidBy     string  `json:"paidBy,omitempty"`  // [paid-by:: entity] — the paying entity (statements workbench)
	Cat        string  `json:"cat,omitempty"`     // [cat:: soft|acquisition] — budget category (blank/tethered = hard; legacy carry → soft)
	Stmt       string  `json:"stmt,omitempty"`    // [stmt:: label] — bank-statement provenance (written at workbench apply)
	RawNote    string  `json:"rawNote,omitempty"` // note as stored on disk (token intact) — mutation matching
}

// LedgerHeader is the canonical csv column order (also the seed's empty-file header).
var LedgerHeader = []string{"date", "type", "category", "vendor", "contractor", "amount", "status", "note", "doc"}

// StarterTemplate is the boot-seeded gut-rehab template (write-once). Pass-5:
// its `## stages` section seeds work plans (names + default [weeks::]) — the
// stage list and the schedule are the same object from birth.
const StarterTemplate = `---
categories: [budget-template]
---

# gut rehab

## stages
- [ ] Pre-development [weeks:: 2]
- [ ] Exterior & structural [weeks:: 3]
- [ ] Demo [weeks:: 1]
- [ ] Rough-in [weeks:: 4]
- [ ] Insulation & drywall [weeks:: 2]
- [ ] Finishes [weeks:: 4]
- [ ] Punch & final inspection [weeks:: 1]
- [ ] Exit [weeks:: 2]
`

// NewBuildTemplate is the boot-seeded ground-up template (write-once).
const NewBuildTemplate = `---
categories: [budget-template]
---

# new build

## stages
- [ ] Pre-construction [weeks:: 6]
- [ ] Site work [weeks:: 2]
- [ ] Foundation [weeks:: 3]
- [ ] Framing & dry-in [weeks:: 5]
- [ ] MEP rough-in [weeks:: 4]
- [ ] Insulation & drywall [weeks:: 3]
- [ ] Finishes [weeks:: 6]
- [ ] Final inspections & CO [weeks:: 2]
- [ ] Close-out [weeks:: 3]
`

// ParseLedgerBytes exposes the ledger csv parse (entity admin ledgers, exports).
func ParseLedgerBytes(raw []byte) []LedgerRow { return parseLedger(raw) }

// ParseBudgetSection exposes the legacy `## budget` table parse for the
// one-shot -migrate-budget command (the table is retired from the UI).
func ParseBudgetSection(lines []string) []BudgetLine { return parseBudget(lines) }

// parseBudget scans a `## budget` markdown table into lines. It skips the header
// row and any `|---|` separator by requiring the amount cell to parse as a number
// — so `| category | budget |` and `| --- | --- |` are ignored, data rows are kept.
func parseBudget(sectionLines []string) []BudgetLine {
	var out []BudgetLine
	for _, ln := range sectionLines {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "|") {
			continue
		}
		cells := splitRow(ln)
		if len(cells) < 2 {
			continue
		}
		amt, err := parseMoney(cells[1])
		if err != nil {
			continue // header ("budget") or separator ("---") — not a data row
		}
		out = append(out, BudgetLine{Category: cells[0], Amount: amt})
	}
	return out
}

// splitRow splits a `| a | b |` markdown row into trimmed cells.
func splitRow(ln string) []string {
	parts := strings.Split(strings.Trim(ln, "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// parseMoney reads "42000", "1,240.55", "$18500" → float.
func parseMoney(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseFloat(s, 64)
}

// parseLog collects the free bullet lines under `## log` (verbatim text after "- ").
func parseLog(sectionLines []string) []string {
	var out []string
	for _, ln := range sectionLines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "- ") {
			out = append(out, strings.TrimSpace(t[2:]))
		}
	}
	return out
}

// parseSections splits a markdown body into its `## <name>` blocks (lowercased
// heading → the lines beneath it, until the next `## `). Content before the first
// heading is dropped (it's the record's free prose, shown via the note view).
func parseSections(body string) map[string][]string {
	out := map[string][]string{}
	cur := ""
	for _, ln := range strings.Split(body, "\n") {
		if h, ok := headingName(ln); ok {
			cur = h
			out[cur] = []string{}
			continue
		}
		if cur != "" {
			out[cur] = append(out[cur], ln)
		}
	}
	return out
}

// headingName returns the lowercased name of a `## name` line (level 2 only).
func headingName(ln string) (string, bool) {
	t := strings.TrimRight(ln, " \t")
	if !strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(t[3:])), true
}

// parseLedger reads the csv sidecar bytes into rows (tolerant: a header line is
// skipped by column name, ragged/short rows are padded, bad amounts → 0).
func parseLedger(raw []byte) []LedgerRow {
	if len(raw) == 0 {
		return nil
	}
	rd := csv.NewReader(strings.NewReader(string(raw)))
	rd.FieldsPerRecord = -1 // tolerate ragged rows
	records, err := rd.ReadAll()
	if err != nil {
		return nil
	}
	var out []LedgerRow
	for i, rec := range records {
		if i == 0 && len(rec) > 0 && strings.EqualFold(strings.TrimSpace(rec[0]), "date") {
			continue // header row
		}
		get := func(n int) string {
			if n < len(rec) {
				return strings.TrimSpace(rec[n])
			}
			return ""
		}
		amt, _ := parseMoney(get(5))
		row := LedgerRow{
			Date: get(0), Type: get(1), Category: get(2), Vendor: get(3),
			Contractor: get(4), Amount: amt, Status: get(6), Note: get(7), Doc: get(8),
		}
		// tokens ride inside note: strip [work:: id] → WorkID, [paid-by::
		// entity] → PaidBy, [cat:: x] → Cat; display note is the cleaned text.
		row.RawNote = row.Note
		for _, m := range workFieldRe.FindAllStringSubmatch(row.Note, -1) {
			switch strings.ToLower(m[1]) {
			case "work":
				row.WorkID = strings.TrimSpace(m[2])
			case "paid-by":
				row.PaidBy = strings.TrimSpace(m[2])
			case "cat":
				row.Cat = strings.TrimSpace(m[2])
			case "stmt":
				row.Stmt = strings.TrimSpace(m[2])
			}
		}
		if row.WorkID != "" || row.PaidBy != "" || row.Cat != "" || row.Stmt != "" {
			row.Note = strings.Join(strings.Fields(workFieldRe.ReplaceAllString(row.Note, "")), " ")
		}
		if row.Date == "" && row.Type == "" && row.Amount == 0 {
			continue // blank line
		}
		out = append(out, row)
	}
	return out
}
