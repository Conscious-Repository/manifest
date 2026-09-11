package sources

import (
	"regexp"
	"strings"
	"unicode"
)

// PEOPLE AGGREGATION — papers in, people out (sourcing-effectiveness plan
// Phase 1, 2026-09-11).
//
// A paper index returns papers. Every author on a paper is a MENTION: a
// printed name, a byline, a position, whatever affiliation and identifier
// the record carried. This file turns mentions into people by identity
// precedence, and nothing else:
//
//  1. ORCID — the registry's own durable id.
//  2. OpenAlex author id — only when a caller already resolved it
//     deterministically from evidence (PubMed carries none; the branch exists
//     so the precedence is the plan's, not this file's).
//  3. Full printed name + the organization token of the first affiliation.
//  4. Everything else — initials-only bylines, a full name with no
//     affiliation — is a SOURCE-LOCAL key marked ambiguous: it aggregates the
//     identical byline at the identical organization across papers and
//     nothing more.
//
// No fuzzy matching, no model: two mentions are one person only when their
// keys are byte-equal after a deterministic fold (case, punctuation,
// spacing). One bridge exists and is exact: a full-name+org key that
// coincides with EXACTLY ONE stronger key across the batch adopts it, so the
// paper where someone omitted their ORCID lands on the same row as the paper
// where they gave it. Two ORCIDs behind one name+org is a namesake pair, and
// the bridge refuses. An initials-only byline never bridges to anyone.
//
// This is pure: no I/O, no clock, no source. sources_pubmed.go feeds it and
// turns its output into drafts.

// authorMention is one author entry on one paper, as the record printed it,
// with any email-shaped text already removed from the affiliations.
type authorMention struct {
	// Paper is the caller's index of the paper this mention was read from
	// (PubMed relevance order). Mentions arrive in paper order, then byline
	// order, and people are emitted in order of first mention.
	Paper int
	// Name is the printed name: "Dana M Reyes" when the record gave a
	// forename, the byline ("Reyes DM") when it gave only initials. Never
	// empty for a person.
	Name string
	// Byline is the Medline form, LastName + Initials ("Reyes DM").
	Byline string
	// FullName reports whether the forename was more than initials — the
	// difference between a name and an abbreviation.
	FullName bool
	// ORCID is the bare 16-character id ("0000-0001-2345-6789"), "" when the
	// record carried none or carried one that is not an ORCID.
	ORCID string
	// OpenAlexID is a bare author id ("A123…") a caller resolved
	// deterministically from evidence. Always "" from PubMed.
	OpenAlexID string
	// Affiliations are the affiliation strings as printed, email-stripped,
	// in record order. The FIRST one supplies the organization token.
	Affiliations []string
	// Position is the 1-based index in the printed author list; Total is
	// the list's length (collective names included — they are on the byline).
	Position, Total int
}

// personKeyKind names which rung of the precedence produced a key.
type personKeyKind string

const (
	keyORCID    personKeyKind = "orcid"
	keyOpenAlex personKeyKind = "openalex"
	keyName     personKeyKind = "name"
	keyByline   personKeyKind = "byline"
)

// personKey is the identity a mention resolved to. String() is the exact
// text two mentions must share to be one person, and what the draft carries
// as its ExternalID.
type personKey struct {
	Kind personKeyKind
	// ID is the folded identity under Kind: the ORCID, the OpenAlex id, or
	// "<folded name>/<org token>".
	ID string
	// Ambiguous marks a source-local key (Kind byline, or a name with no
	// organization token) that must never merge with a stronger one.
	Ambiguous bool
	// Reason says why, in words the card can print. "" when not ambiguous.
	Reason string
}

func (k personKey) String() string { return string(k.Kind) + "/" + k.ID }

// person is one aggregated row: the key, the display facts taken from the
// FIRST mention (the source that found the person keeps the last word), and
// every mention in order.
type person struct {
	Key      personKey
	Name     string
	Org      string
	ORCID    string
	Mentions []authorMention
}

// positionLabel names where a mention sat on the byline: first, middle,
// last, or sole for a one-author paper.
func (m authorMention) positionLabel() string {
	switch {
	case m.Total <= 1:
		return "sole"
	case m.Position <= 1:
		return "first"
	case m.Position >= m.Total:
		return "last"
	default:
		return "middle"
	}
}

// primaryKey applies the precedence to one mention on its own evidence.
func primaryKey(m authorMention) personKey {
	if m.ORCID != "" {
		return personKey{Kind: keyORCID, ID: m.ORCID}
	}
	if m.OpenAlexID != "" {
		return personKey{Kind: keyOpenAlex, ID: m.OpenAlexID}
	}
	org := ""
	if len(m.Affiliations) > 0 {
		org = orgToken(m.Affiliations[0])
	}
	if m.FullName && org != "" {
		return personKey{Kind: keyName, ID: foldName(m.Name) + "/" + org}
	}
	if m.FullName {
		return personKey{Kind: keyName, ID: foldName(m.Name) + "/", Ambiguous: true,
			Reason: "the paper printed no affiliation for " + strings.TrimSpace(m.Name) + ", so this name alone cannot be told from a namesake"}
	}
	byline := foldName(m.Byline)
	if byline == "" {
		byline = foldName(m.Name)
	}
	return personKey{Kind: keyByline, ID: byline + "/" + org, Ambiguous: true,
		Reason: "PubMed printed initials only (" + strings.TrimSpace(m.Byline) + "); one row per byline and affiliation, never merged with a full name"}
}

// nameOrgKey is the rung-3 key a mention WOULD have, for the bridge: "" when
// the mention is not a full name with an organization.
func nameOrgKey(m authorMention) string {
	if !m.FullName || len(m.Affiliations) == 0 {
		return ""
	}
	org := orgToken(m.Affiliations[0])
	if org == "" {
		return ""
	}
	return foldName(m.Name) + "/" + org
}

// aggregatePeople folds mentions into people by exact key, applying the one
// exact bridge described above. People come out in order of first mention;
// each person's mentions stay in input order.
func aggregatePeople(mentions []authorMention) []person {
	keys := make([]personKey, len(mentions))
	for i, m := range mentions {
		keys[i] = primaryKey(m)
	}

	// the bridge: a weaker exact key that coincides with exactly one
	// stronger key across the batch adopts it — openalex → orcid first (a
	// mention that carried both), then name+org → whatever those resolved
	// to, so a name that coincides with one id reached two ways still sees
	// one id.
	strongerByOpenAlex := map[string]map[string]personKey{}
	for i, m := range mentions {
		if keys[i].Kind == keyORCID && m.OpenAlexID != "" {
			addStronger(strongerByOpenAlex, m.OpenAlexID, keys[i])
		}
	}
	for i := range mentions {
		if keys[i].Kind == keyOpenAlex {
			if only, ok := onlyStronger(strongerByOpenAlex[keys[i].ID]); ok {
				keys[i] = only
			}
		}
	}
	strongerByName := map[string]map[string]personKey{}
	for i, m := range mentions {
		if k := keys[i]; k.Kind == keyORCID || k.Kind == keyOpenAlex {
			if nk := nameOrgKey(m); nk != "" {
				addStronger(strongerByName, nk, k)
			}
		}
	}
	for i := range mentions {
		if k := keys[i]; k.Kind == keyName && !k.Ambiguous {
			if only, ok := onlyStronger(strongerByName[k.ID]); ok {
				keys[i] = only
			}
		}
	}

	var out []person
	index := map[string]int{}
	for i, m := range mentions {
		k := keys[i]
		id := k.String()
		if at, seen := index[id]; seen {
			p := &out[at]
			p.Mentions = append(p.Mentions, m)
			if p.ORCID == "" && m.ORCID != "" {
				p.ORCID = m.ORCID
			}
			if p.Org == "" && len(m.Affiliations) > 0 {
				p.Org = orgDisplay(m.Affiliations[0])
			}
			continue
		}
		index[id] = len(out)
		p := person{Key: k, Name: strings.TrimSpace(m.Name), ORCID: m.ORCID, Mentions: []authorMention{m}}
		if len(m.Affiliations) > 0 {
			p.Org = orgDisplay(m.Affiliations[0])
		}
		out = append(out, p)
	}
	return out
}

func addStronger(m map[string]map[string]personKey, weak string, strong personKey) {
	if m[weak] == nil {
		m[weak] = map[string]personKey{}
	}
	m[weak][strong.String()] = strong
}

func onlyStronger(m map[string]personKey) (personKey, bool) {
	if len(m) != 1 {
		return personKey{}, false
	}
	for _, k := range m {
		return k, true
	}
	return personKey{}, false
}

// ---- folding ----

// foldName is the blunt equality fold: lowercase, letters and digits only,
// single-spaced. The same rule recruiting's lookup uses, for the same
// reason — being blunt costs a split, being clever costs a stranger's
// papers on someone's record.
func foldName(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// forenameIsFull reports whether a printed forename is more than initials:
// some segment (split on space, dot and hyphen) carries two or more letters
// and the whole is not merely the Initials field spelled out. "Dana M" is
// full; "D M", "D.M.", "J-P" and "DM" (when Initials is "DM") are not. A
// two-letter forename ("Wu", "Li") is a name, not initials, because it is
// printed as one segment and is not the Initials field.
func forenameIsFull(forename, initials string) bool {
	fore := strings.TrimSpace(forename)
	if fore == "" {
		return false
	}
	if strings.TrimSpace(initials) != "" && foldName(fore) == foldName(initials) {
		return false
	}
	for _, seg := range strings.FieldsFunc(fore, func(r rune) bool { return r == ' ' || r == '.' || r == '-' || r == ' ' }) {
		letters := 0
		for _, r := range seg {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters >= 2 {
			return true
		}
	}
	return false
}

// ---- organization token ----

// orgSegmentTiers rank the comma-separated segments of an affiliation by how
// much of an institution they name. The token is the FIRST segment of the
// best tier present: "Department of Radiology, University of Aberdeen,
// Aberdeen, UK" and "Aberdeen Biomedical Imaging Centre, University of
// Aberdeen, UK" both fold to "university of aberdeen"; the department is
// the part that changes between papers. A segment naming a sub-unit only
// (department, division, lab) is the last resort before the bare first
// segment. Keywords are matched as folded substrings, so "Universität",
// "Université" and "Universidade" all land on "universit"; the short
// corporate suffixes match as whole words only.
var orgSegmentTiers = [][]string{
	{"universit", "college", "hospital", "clinic", "academy", "foundation", "polytechnic", "hochschule", "school of medicine", "medical school",
		"inc", "ltd", "gmbh", "llc", "corp", "company", "pharma", "therapeutics", "biosciences"},
	{"institut", "center", "centre", "laboratory", "laboratories", "school", "riken", "cnrs", "inserm", "max planck", "national institutes", "nih", "cern", "academy of sciences"},
	{"department", "dept", "division", "unit", "section", "faculty", "program", "programme", "lab"},
}

// orgSegments splits one affiliation into its printed segments.
func orgSegments(affiliation string) []string {
	var out []string
	for _, seg := range strings.FieldsFunc(affiliation, func(r rune) bool { return r == ',' || r == ';' }) {
		if seg = strings.TrimSpace(seg); seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// orgSegment picks the segment that names the organization, "" for an
// affiliation with no segments at all.
func orgSegment(affiliation string) string {
	segs := orgSegments(affiliation)
	if len(segs) == 0 {
		return ""
	}
	for _, tier := range orgSegmentTiers {
		for _, seg := range segs {
			folded := " " + foldName(seg) + " "
			for _, kw := range tier {
				if len(kw) <= 4 {
					if strings.Contains(folded, " "+kw+" ") {
						return seg
					}
					continue
				}
				if strings.Contains(folded, kw) {
					return seg
				}
			}
		}
	}
	return segs[0]
}

// orgToken is the folded identity of an affiliation's organization, "" when
// there is none. A leading "the" is dropped so "The University of X" and
// "University of X" agree.
func orgToken(affiliation string) string {
	tok := foldName(orgSegment(affiliation))
	tok = strings.TrimPrefix(tok, "the ")
	return strings.TrimSpace(tok)
}

// orgDisplay is the organization segment as printed — what the draft's Org
// shows. Trailing sentence punctuation is dropped.
func orgDisplay(affiliation string) string {
	return strings.TrimRight(strings.TrimSpace(orgSegment(affiliation)), ".")
}

// ---- email stripping (D15) ----

// addressLabelRe matches the label affiliations print before an address —
// "Electronic address:", "Email:", "E-mail address:" — so that removing the
// address leaves no dangling label behind.
var addressLabelRe = regexp.MustCompile(`(?i)\b(?:electronic address|e-?mail(?: address)?)\s*:?\s*`)

// stripAddresses removes every email-shaped token from free text and the
// label that introduced it, then tidies what is left. Everything else is
// returned as printed — a bracket that wrapped the address stays, sentence
// punctuation that trailed it goes. "Dept of X, University of Y, UK.
// Electronic address: a@b.org." becomes "Dept of X, University of Y, UK."
// — the affiliation is still verbatim, minus the one thing this system
// must not carry.
func stripAddresses(s string) string {
	fields := strings.Fields(s)
	keep := make([]string, 0, len(fields))
	for _, tok := range fields {
		if !containsAddress(tok) {
			keep = append(keep, tok)
			continue
		}
		// keep the brackets that wrapped it, drop the address and any
		// sentence punctuation that trailed it
		lead := strings.TrimRight(tok[:len(tok)-len(strings.TrimLeft(tok, "([<\"'"))], "<\"'")
		trail := strings.TrimLeft(tok[len(strings.TrimRight(tok, ")]>\"'.,;:")):], ">\"'.,;:")
		if rest := lead + trail; rest != "" {
			keep = append(keep, rest)
		}
	}
	out := addressLabelRe.ReplaceAllString(strings.Join(keep, " "), "")
	out = strings.Join(strings.Fields(out), " ")
	// a label whose address was removed can leave " ." or " ," behind
	for _, bad := range []string{" .", " ,", " ;"} {
		out = strings.ReplaceAll(out, bad, bad[1:])
	}
	out = strings.TrimLeft(out, " ,;:")
	return strings.TrimSpace(out)
}
