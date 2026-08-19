package realestate

import (
	"strconv"
	"strings"
	"time"
)

// Bank exports disagree about two things that silently corrupt an import: how
// a date is spelled, and which sign means "money left the account". Both are
// settled HERE, once, on the way in — the parking lot and the ledgers hold ISO
// dates and unsigned amounts with an explicit Inflow flag, so the same
// transaction arriving by CSV and by bank feed produces the same DedupeKey
// (statements.go). Before this layer the ingest handler stored the CSV's date
// string verbatim, which is why a 08/18/2026 row never matched the feed's
// 2026-08-18 copy of itself.

// Sign conventions for a single signed amount column.
const (
	SignExpenseNegative = "expense-negative" // charges are −, deposits + (most US bank exports)
	SignExpensePositive = "expense-positive" // charges are +, deposits −
)

// SignConventionOK reports whether s names a convention the ingest understands.
func SignConventionOK(s string) bool {
	return s == SignExpenseNegative || s == SignExpensePositive
}

// SuggestSign infers a file's convention from its amounts: an operating
// account posts more charges than deposits, so the majority sign is the
// expense sign. It is a SUGGESTION — the mapping strip shows it and the owner
// confirms, because a wrong guess inverts every row in the file.
func SuggestSign(amounts []float64) string {
	neg, pos := 0, 0
	for _, a := range amounts {
		switch {
		case a < 0:
			neg++
		case a > 0:
			pos++
		}
	}
	if neg >= pos {
		return SignExpenseNegative
	}
	return SignExpensePositive
}

// namedLayouts are the spelled-month forms; the numeric forms are parsed by
// hand so the day/month order can be chosen per file (see DateOrder).
var namedLayouts = []string{
	"Jan 2, 2006", "January 2, 2006", "Jan 2 2006", "January 2 2006",
	"2 Jan 2006", "02 Jan 2006", "2-Jan-2006", "02-Jan-06",
}

// NormalizeDate converts one export's date cell to ISO (YYYY-MM-DD). dayFirst
// selects DD/MM over MM/DD for the ambiguous numeric forms — pass the verdict
// DateOrder gives for the whole column, never a per-row guess. ok is false
// when the cell is not a date at all; callers skip those rows rather than
// storing an unmatchable key.
func NormalizeDate(s string, dayFirst bool) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	// drop a trailing clock ("2026-08-18 14:45:00", "08/18/2026 2:45 PM") —
	// the colon test keeps "Jan 2, 2026" intact
	if i := strings.IndexAny(s, " T"); i > 0 && strings.Contains(s[i:], ":") {
		s = strings.TrimSpace(s[:i])
	}
	if a, b, c, ok := splitNumericDate(s); ok {
		return assembleDate(a, b, c, dayFirst)
	}
	for _, layout := range namedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// DateOrder reports whether a whole date column reads day-first. A component
// above 12 can only be a day, so one unambiguous row settles the file; with no
// evidence either way it answers month-first (US bank exports). Conflicting
// evidence also answers month-first — the column is then not uniformly
// day-first and the ambiguous rows are the safer reading.
func DateOrder(values []string) bool {
	day, month := false, false
	for _, v := range values {
		a, b, _, ok := splitNumericDate(strings.TrimSpace(v))
		if !ok || len(a) == 4 { // ISO — carries no ordering evidence
			continue
		}
		x, errA := strconv.Atoi(a)
		y, errB := strconv.Atoi(b)
		if errA != nil || errB != nil {
			continue
		}
		if x > 12 && y <= 12 {
			day = true
		}
		if y > 12 && x <= 12 {
			month = true
		}
	}
	return day && !month
}

// splitNumericDate breaks "08/18/2026", "2026-08-18" or "18.08.2026" into its
// three components. It rejects anything that is not three numeric parts.
func splitNumericDate(s string) (a, b, c string, ok bool) {
	sep := strings.IndexAny(s, "/-.")
	if sep < 0 {
		return "", "", "", false
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' || r == '.' })
	if len(parts) != 3 {
		return "", "", "", false
	}
	for _, p := range parts {
		if p == "" || strings.TrimFunc(p, func(r rune) bool { return r >= '0' && r <= '9' }) != "" {
			return "", "", "", false
		}
	}
	return parts[0], parts[1], parts[2], true
}

// assembleDate turns three numeric components into ISO, rejecting impossible
// calendar dates (a round-trip through time.Date catches 02/30 and friends).
func assembleDate(a, b, c string, dayFirst bool) (string, bool) {
	var y, m, d int
	var err error
	if len(a) == 4 { // YYYY-MM-DD / YYYY/MM/DD
		if y, err = strconv.Atoi(a); err != nil {
			return "", false
		}
		if m, err = strconv.Atoi(b); err != nil {
			return "", false
		}
		if d, err = strconv.Atoi(c); err != nil {
			return "", false
		}
	} else {
		first, second := a, b
		if dayFirst {
			first, second = b, a
		}
		if m, err = strconv.Atoi(first); err != nil {
			return "", false
		}
		if d, err = strconv.Atoi(second); err != nil {
			return "", false
		}
		if y, err = strconv.Atoi(c); err != nil {
			return "", false
		}
		if len(c) == 2 { // two-digit years are this century
			y += 2000
		}
	}
	if y < 1900 || y > 2200 || m < 1 || m > 12 || d < 1 || d > 31 {
		return "", false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

// vendorTrailers are the ACH/wire suffixes a bank staples onto a payee: routing
// participant numbers and the /INDIVIDUALID · /ORGNAME addenda. None of it
// names a merchant, all of it is unique per transaction, and leaving it in
// makes vendor memory (vendor → category) useless.
var vendorTrailers = []string{"ACHPARNBR", "/INDIVIDUALID", "/ORGNAME"}

// TidyVendor turns a raw statement description into the vendor name shown in
// the workbench and keyed by vendor memory: whitespace collapsed (these
// exports are fixed-width padded), ACH noise cut, the doubled payee head
// dropped ("THE HOME DEPOT THE HOME DEPOT # …"), length capped. The untouched
// description is kept as the row's note, so nothing is lost.
func TidyVendor(raw string) string {
	s := strings.Join(strings.Fields(raw), " ")
	for _, t := range vendorTrailers {
		if i := indexFold(s, t); i > 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	s = dropHeadRepeat(s)
	if len(s) > 60 {
		s = strings.TrimSpace(s[:60])
	}
	return s
}

// dropHeadRepeat removes a leading phrase that immediately repeats itself —
// the fixed-width abbreviation the bank prints before the full descriptor.
func dropHeadRepeat(s string) string {
	w := strings.Fields(s)
	for k := len(w) / 2; k >= 1; k-- {
		same := true
		for i := 0; i < k; i++ {
			if !strings.EqualFold(w[i], w[k+i]) {
				same = false
				break
			}
		}
		if same {
			return strings.Join(w[k:], " ")
		}
	}
	return s
}

func indexFold(s, needle string) int {
	return strings.Index(strings.ToUpper(s), strings.ToUpper(needle))
}

// MerchantKey reduces a statement description to the identity of the MERCHANT,
// for grouping recurring charges: Ozark Electric's WEB PMTS reference and
// PayPal's transfer id change every month, so exact-string matching never sees
// the repeat. Digits and punctuation go (store numbers, ACH refs), then the
// leading two words name the merchant. Deliberately coarse — it exists to
// PROPOSE a grouping the owner previews row by row, never to act alone.
//
// Returns "" — never groups — when the description's identity IS its digits:
// bare check rows ("Check Paid 107") are distinct payees wearing one label.
func MerchantKey(vendor string) string {
	s := strings.ToUpper(strings.Join(strings.Fields(vendor), " "))
	if s == "" || strings.HasPrefix(s, "CHECK ") || s == "CHECK" || s == "DEPOSIT" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || r == ' ' {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	words := make([]string, 0, 2)
	for _, w := range strings.Fields(b.String()) {
		if len(w) < 2 { // dropped-digit shrapnel ("4TE*CITY" → "TE CITY" keeps TE; single letters go)
			continue
		}
		if len(words) == 0 && (w == "THE" || w == "AN") {
			continue // a leading article wastes one of the two identity slots
		}
		words = append(words, w)
		if len(words) == 2 {
			break
		}
	}
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, " ")
}
