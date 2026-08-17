package fundraising

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SheetRow preserves the source Sheet1 row number for idempotent migration.
type SheetRow struct {
	Row      int
	Section  string
	Firm     string
	Warm     string
	Touch    string
	Next     string
	Interest string
	Notes    string
}

type ExactContactResolver func(name string) (PersonRef, bool)

// ReadSheetCSV reads the exact simple-table shape used by the source workbook.
func ReadSheetCSV(r io.Reader) ([]SheetRow, []Resource, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	offset := 0
	if len(rows) > 0 && len(rows[0]) > 1 && strings.TrimSpace(rows[0][0]) == "" && strings.EqualFold(strings.TrimSpace(rows[0][1]), "Firm") {
		offset = 1 // Drive's readable CSV projection includes a dataframe index
	}
	section := "main"
	out := []SheetRow{}
	resources := []Resource{}
	for i, row := range rows {
		if i == 0 {
			continue
		}
		cell := func(n int) string {
			n += offset
			if n >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[n])
		}
		firm := cell(0)
		switch firm {
		case "HitList":
			section = "hit"
			continue
		case "Resources":
			section = "resources"
			continue
		}
		nonEmpty := false
		for n := 0; n < 6; n++ {
			if cell(n) != "" {
				nonEmpty = true
				break
			}
		}
		if !nonEmpty {
			continue
		}
		if section == "resources" {
			if strings.HasPrefix(firm, "http://") || strings.HasPrefix(firm, "https://") {
				resources = append(resources, Resource{Title: firm, URL: firm})
			}
			continue
		}
		out = append(out, SheetRow{Row: i + 1, Section: section, Firm: firm, Warm: cell(1), Touch: cell(2), Next: cell(3), Interest: cell(4), Notes: cell(5)})
	}
	return out, resources, nil
}

// NormalizeSheet implements the locked one-time mapping. Only the case-only
// 8VC duplicate merges; generic Angel/LP rows remain separate opportunities.
func NormalizeSheet(rows []SheetRow, exact ExactContactResolver) []Opportunity {
	out := []Opportunity{}
	byMerge := map[string]int{}
	for _, row := range rows {
		firm := row.Firm
		if firm == "" {
			firm = row.Warm
		}
		op := Opportunity{Firm: firm, Status: StatusActive, Interest: normalizeInterest(row.Interest), Currency: "USD", Source: textSource(row.Warm), LastTouchpoint: row.Touch, NextStep: row.Next, Notes: row.Notes, SourceRows: []int{row.Row}, People: []PersonRef{}}
		if row.Section == "hit" {
			op.Status = StatusProspect
		}
		lowInterest := strings.ToLower(row.Interest)
		all := strings.ToLower(strings.Join([]string{row.Touch, row.Next, row.Notes}, " "))
		switch {
		case lowInterest == "pass" || strings.Contains(all, "paid back safe"):
			op.Status = StatusPassed
		case lowInterest == "closed" && committedLanguage(all):
			op.Status = StatusCommitted
		case lowInterest == "closed":
			op.Status = StatusActive
			op.ImportReview = true
		}
		op.Amount, op.ImportReview = clearAmount(strings.Join([]string{row.Touch, row.Next, row.Notes}, " "), op.ImportReview)
		if row.Firm == "" {
			op.ImportReview = true
		}
		if exact != nil {
			matched, residual := resolveWarm(row.Warm, exact)
			op.People = matched
			if len(matched) == 1 && residual == "" {
				contact := matched[0]
				op.Source = &SourceRef{Contact: &contact}
			}
		}
		merge := ""
		if strings.EqualFold(strings.TrimSpace(row.Firm), "8vc") {
			merge = "8vc"
		}
		if idx, ok := byMerge[merge]; merge != "" && ok {
			have := &out[idx]
			have.SourceRows = append(have.SourceRows, op.SourceRows...)
			have.People = mergePeople(have.People, op.People)
			have.Source = mergeSources(have.Source, op.Source)
			if have.LastTouchpoint == "" {
				have.LastTouchpoint = op.LastTouchpoint
			}
			if have.NextStep == "" {
				have.NextStep = op.NextStep
			}
			have.Notes = joinText(have.Notes, op.Notes)
			have.ImportReview = have.ImportReview || op.ImportReview
			continue
		}
		if merge != "" {
			byMerge[merge] = len(out)
		}
		out = append(out, op)
	}
	return out
}

func textSource(v string) *SourceRef {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &SourceRef{Text: v}
}

func sourceText(s *SourceRef) string {
	if s == nil {
		return ""
	}
	if s.Contact != nil {
		return s.Contact.Display
	}
	return s.Text
}

func mergeSources(a, b *SourceRef) *SourceRef {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.Contact != nil && b.Contact != nil && strings.EqualFold(a.Contact.Key, b.Contact.Key) {
		return a
	}
	return textSource(joinText(sourceText(a), sourceText(b)))
}

func normalizeInterest(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high":
		return InterestHigh
	case "medium":
		return InterestMedium
	case "low":
		return InterestLow
	default:
		return InterestUnknown
	}
}
func committedLanguage(s string) bool {
	for _, needle := range []string{"confirmed", "invested", "wants to invest", " in spv", " angel", "angel check"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

var moneyToken = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*([km])\b`)

func clearAmount(text string, review bool) (float64, bool) {
	low := strings.ToLower(text)
	matches := moneyToken.FindAllStringSubmatchIndex(low, -1)
	vals := map[float64]bool{}
	for _, m := range matches {
		start := m[0]
		end := m[1]
		before := low[maxInt(0, start-24):start]
		after := low[end:minInt(len(low), end+24)]
		if strings.HasSuffix(before, "at ") || strings.Contains(before, "valuation") ||
			strings.Contains(after, "valuation") || strings.Contains(before, "post-money") ||
			strings.Contains(before, "pre-money") || strings.Contains(after, "post-money") ||
			strings.Contains(after, "pre-money") {
			continue
		}
		n, _ := strconv.ParseFloat(low[m[2]:m[3]], 64)
		if low[m[4]:m[5]] == "k" {
			n *= 1000
		} else {
			n *= 1000000
		}
		vals[n] = true
	}
	if len(vals) != 1 {
		if len(vals) > 1 {
			review = true
		}
		return 0, review
	}
	for v := range vals {
		return v, review
	}
	return 0, review
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func resolveWarm(raw string, exact ExactContactResolver) ([]PersonRef, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []PersonRef{}, ""
	}
	parts := []string{raw}
	if strings.Contains(raw, ",") {
		parts = strings.Split(raw, ",")
	}
	matched := []PersonRef{}
	unmatched := []string{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if p, ok := exact(name); ok {
			matched = append(matched, p)
		} else {
			unmatched = append(unmatched, name)
		}
	}
	return mergePeople(nil, matched), strings.Join(unmatched, ", ")
}
func mergePeople(a, b []PersonRef) []PersonRef {
	by := map[string]PersonRef{}
	for _, xs := range [][]PersonRef{a, b} {
		for _, p := range xs {
			if p.Key != "" {
				by[strings.ToLower(p.Key)] = p
			}
		}
	}
	out := make([]PersonRef, 0, len(by))
	for _, p := range by {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display) })
	return out
}
func joinText(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" || a == b {
		return a
	}
	return fmt.Sprintf("%s; %s", a, b)
}
