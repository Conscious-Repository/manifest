package goals

import (
	"regexp"
	"strings"

	"manifest/record"
)

// ArchiveEntry is one closed Rock recorded in a quarter archive (goals <quarter>.md).
// Archives are read-only history: a Rock lands here only when it closes (§6).
type ArchiveEntry struct {
	Area     string `json:"area"`
	Text     string `json:"text"`
	GoalID   string `json:"goalId"`
	Outcome  string `json:"outcome"`  // "win" | "learn"
	Closed   string `json:"closed"`   // YYYY-MM-DD
	Reached  string `json:"reached"`  // last stage name in the trail at close
	Evidence string `json:"evidence"` // proof of the win (text or [[wikilink]]); required for a Win (§5)
	Serves   string `json:"serves"`   // annual slug this Rock served ("" if none)
	Note     string `json:"note"`     // optional (typically why it was a learn)
	// History: the Rock's frozen task lines (verbatim, indented) — checked
	// pre-split work travels with the Rock into the archive (task-substrate).
	History []string `json:"history,omitempty"`
}

// ArchiveQuarter groups a quarter's closed Rocks (newest quarter first when listed).
type ArchiveQuarter struct {
	Quarter string         `json:"quarter"`
	Entries []ArchiveEntry `json:"entries"`
}

var archiveLineRe = regexp.MustCompile(`^[-*]\s+(.*\S)\s*$`)

// quarterRe matches a bare quarter slug, e.g. "2026-Q3".
var quarterRe = regexp.MustCompile(`^\d{4}-Q[1-4]$`)

// parseArchive reads a goals <quarter>.md file into entries. "## " headings group by
// area; each "- " line is a closed Rock with inline [key:: value] fields.
func parseArchive(content string) []ArchiveEntry {
	var out []ArchiveEntry
	area := ""
	for _, line := range strings.Split(content, "\n") {
		if isAreaHeading(line) {
			area = areaName(line)
			continue
		}
		m := archiveLineRe.FindStringSubmatch(line)
		if m == nil {
			// indented non-blank lines under an entry = its frozen history
			if len(out) > 0 && strings.TrimSpace(line) != "" &&
				(strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
				out[len(out)-1].History = append(out[len(out)-1].History, line)
			}
			continue
		}
		// Evidence is emit-LAST with a possibly-wikilink value — the kernel's
		// trailing-field affordance pulls it before the generic scan.
		evidence, content := record.ExtractTrailingField(m[1], "evidence")
		text, fields := parseFields(content)
		e := ArchiveEntry{Area: area, Text: text, Evidence: evidence}
		for _, f := range fields {
			switch strings.ToLower(f.Key) {
			case "goal":
				e.GoalID = f.Value
			case "outcome":
				e.Outcome = strings.ToLower(f.Value)
			case "closed":
				e.Closed = f.Value
			case "reached":
				e.Reached = f.Value
			case "serves":
				e.Serves = f.Value
			case "note":
				e.Note = f.Value
			}
		}
		out = append(out, e)
	}
	return out
}

// ReserializeArchive parse→emits a quarter archive's content verbatim —
// golden-corpus heartbeat support (byte-stability proof).
func ReserializeArchive(quarter, content string) string {
	return serializeArchive(quarter, parseArchive(content))
}

// serializeArchive renders a quarter's entries as goals <quarter>.md, grouped by area
// in first-seen order. A fixpoint: re-parsing and re-serializing yields identical bytes.
func serializeArchive(quarter string, entries []ArchiveEntry) string {
	out := []string{"# goals " + quarter}
	var order []string
	byArea := map[string][]ArchiveEntry{}
	for _, e := range entries {
		if _, ok := byArea[e.Area]; !ok {
			order = append(order, e.Area)
		}
		byArea[e.Area] = append(byArea[e.Area], e)
	}
	for _, area := range order {
		out = append(out, "", "## "+area)
		for _, e := range byArea[area] {
			out = append(out, archiveLine(e))
			out = append(out, e.History...)
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func archiveLine(e ArchiveEntry) string {
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(e.Text)
	add := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			b.WriteString(" " + record.EmitField(k, v))
		}
	}
	add("goal", e.GoalID)
	add("outcome", e.Outcome)
	add("closed", e.Closed)
	add("reached", e.Reached)
	add("serves", e.Serves)
	add("note", e.Note)
	// Evidence is emitted LAST (§5) because it may be a [[wikilink]] whose ]]
	// would otherwise break the [^\]]* inline-field scan of a later field.
	add("evidence", e.Evidence)
	return b.String()
}
