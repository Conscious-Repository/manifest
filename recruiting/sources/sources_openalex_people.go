package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// OPENALEX WORKS → PEOPLE (sourcing-effectiveness plan Phase 2, 2026-09-11).
//
// Before this pass every OpenAlex query went to /authors?search=…, which
// matches the TEXT OF AUTHOR NAMES. "field cycling MRI" returned nobody,
// because nobody is called that, while /works held ~2,000 papers with the
// phrase in their title or abstract and named the field's leaders on them.
// Now a query is read for INTENT, conservatively and without a model:
//
//   - A NAME-SHAPED query — two to six tokens that are all capitalised
//     words, initials or name particles ("Dana Reyes", "Reyes D",
//     "K. Jarrod Millman", "Stéfan J. van der Walt") — keeps the author
//     lookup: /authors?search=<name>, one page, exactly as before.
//   - Anything else — a lowercase word, a digit, an acronym like "MRI" —
//     is a KEYWORD and searches WORKS: title-and-abstract text by default,
//     full text on request, optionally within publication years and a work
//     type. The works are read under a WORK BUDGET (scope field "works",
//     default openAlexDefaultWorks, ceiling openAlexMaxWorks) through cursor
//     pagination; every authorship on every work is a mention; mentions
//     aggregate into people by the registry's own author id, else ORCID.
//     An authorship the registry gives neither is not attributable and
//     becomes no draft — a row nobody could point at again is not a row.
//   - The scope field "mode" (authors | works) overrides the reading, and
//     PrepareScope writes the effective mode, budget and filter back into
//     the scope so the run records which branch it took and exactly what it
//     sent upstream. Title-cased keywords ("Low Field") read as a name; the
//     override exists for exactly that case.
//
// The people shown are the first Scope.Max in order of first mention, works
// in the registry's relevance order — source order, the same rule PubMed
// uses. No rank is computed here (that is Phase 4); the registry's own count
// of matching works per author, from one group_by=authorships.author.id
// call over the same search, rides along as evidence so a reader can see
// that a person with 77 matching works is not a person with one. Author
// records for the people shown are fetched in batches of openAlexAuthorBatch
// so each draft carries the same author-canonical topics, counts and
// last-known institution the name lookup gives. Per work, one publication
// row (title, venue, date, type, ids, citation count, author position,
// structured institution ids) and one affiliation row; coauthor and
// same_lab claims follow the single-work rules (sources_openalex_works.go),
// deduplicated across the works one person appears on. Nothing is merged
// by name, and no raw affiliation string is read (D15).

const (
	// openAlexFieldMode is the scope field naming the branch: "authors"
	// (name lookup) or "works" (keyword → papers → people). Absent, the
	// query's shape decides.
	openAlexFieldMode   = "mode"
	openAlexModeAuthors = "authors"
	openAlexModeWorks   = "works"
	// openAlexFieldWorks is the scope field naming the work budget.
	openAlexFieldWorks = "works"
	// openAlexDefaultWorks is the work budget when the scope names none:
	// four pages, enough to see well past a field's first screen.
	openAlexDefaultWorks = 200
	// openAlexMaxWorks is the most works one run may read, however large the
	// budget asked for.
	openAlexMaxWorks = 500
	// openAlexWorksPerPage is how many works one page asks for. A work with
	// its authorships runs 2–30 KB; a consortium paper far more, which is
	// why the page is small and the body bound is the larger one.
	openAlexWorksPerPage = 50
	// openAlexWorksMaxBody bounds how much of a works page is read.
	openAlexWorksMaxBody = 16 << 20
	// openAlexFieldYears is the optional publication-year window ("2018" or
	// "2018-2026"); openAlexFieldType the optional work type ("article",
	// "article|preprint"); openAlexFieldText where the query must match:
	// "title-abstract" (default) or "fulltext".
	openAlexFieldYears        = "years"
	openAlexFieldType         = "type"
	openAlexFieldText         = "text"
	openAlexTextTitleAbstract = "title-abstract"
	openAlexTextFulltext      = "fulltext"
	// openAlexFieldFilter is written back by PrepareScope: the exact
	// `filter=` value the works search sends. Derived, never read as input.
	openAlexFieldFilter = "filter"
	// openAlexAuthorBatch is how many author ids one /authors?filter=… names.
	openAlexAuthorBatch = 50
	// openAlexGroupPerPage is how many author groups the group_by call asks
	// for — the registry's ceiling.
	openAlexGroupPerPage = 200
	// openAlexWorkSelect trims a works page to the fields this adapter reads;
	// the abstract index alone would otherwise dominate every page.
	openAlexWorkSelect = "id,doi,title,display_name,publication_year,publication_date,type,cited_by_count,primary_location,authorships"
	// openAlexWorkTitleChars caps how much of a title is quoted.
	openAlexWorkTitleChars = 300
)

// openAlexPlan is one keyword or name search, resolved from the scope: the
// branch, the exact query, and for a works search the budget, the text
// field and the exact filter string sent upstream.
type openAlexPlan struct {
	Mode   string
	Query  string
	Text   string
	Budget int
	Filter string
}

// openAlexPlanScope reads the scope into a plan. It refuses what it cannot
// send exactly: an unknown mode, a malformed year window, a work type that
// is not a bare type name, a works-only field on an authors search.
func openAlexPlanScope(s Scope) (openAlexPlan, error) {
	p := openAlexPlan{Query: strings.TrimSpace(s.Query)}
	if p.Query == "" {
		return p, errors.New("openalex: a search needs a query, or one paper")
	}
	field := func(k string) string { return strings.TrimSpace(s.Fields[k]) }
	mode := strings.ToLower(field(openAlexFieldMode))
	switch mode {
	case "":
		mode = openAlexModeWorks
		if openAlexNameShaped(p.Query) {
			mode = openAlexModeAuthors
		}
	case openAlexModeAuthors, openAlexModeWorks:
	default:
		return p, fmt.Errorf("openalex: mode %q is not %q or %q", field(openAlexFieldMode), openAlexModeAuthors, openAlexModeWorks)
	}
	p.Mode = mode
	if mode == openAlexModeAuthors {
		for _, k := range []string{openAlexFieldYears, openAlexFieldType, openAlexFieldText} {
			if field(k) != "" {
				return p, fmt.Errorf("openalex: %s applies to a works search; set %s to %s", k, openAlexFieldMode, openAlexModeWorks)
			}
		}
		return p, nil
	}

	budget, err := openAlexWorkBudget(s)
	if err != nil {
		return p, err
	}
	p.Budget = budget

	switch strings.ToLower(strings.Join(strings.FieldsFunc(field(openAlexFieldText), func(r rune) bool { return r == ' ' || r == '_' || r == '-' }), "-")) {
	case "", "title-abstract", "titleabstract", "title-and-abstract":
		p.Text = openAlexTextTitleAbstract
	case "fulltext", "full-text":
		p.Text = openAlexTextFulltext
	default:
		return p, fmt.Errorf("openalex: %s %q is not %q or %q", openAlexFieldText, field(openAlexFieldText), openAlexTextTitleAbstract, openAlexTextFulltext)
	}

	var filters []string
	if p.Text == openAlexTextTitleAbstract {
		// a comma separates filters and a bar separates OR values in the
		// registry's syntax; inside a search phrase they are word breaks
		q := strings.Join(strings.FieldsFunc(p.Query, func(r rune) bool { return r == ',' || r == '|' }), " ")
		filters = append(filters, "title_and_abstract.search:"+strings.Join(strings.Fields(q), " "))
	}
	if years := field(openAlexFieldYears); years != "" {
		m := openAlexYearsRe.FindStringSubmatch(years)
		if m == nil {
			return p, fmt.Errorf("openalex: %s %q is not a year (2020) or a window (2018-2026)", openAlexFieldYears, years)
		}
		from, to := m[1], m[2]
		if to == "" {
			to = from
		}
		if to < from {
			return p, fmt.Errorf("openalex: %s %q ends before it starts", openAlexFieldYears, years)
		}
		filters = append(filters, "from_publication_date:"+from+"-01-01", "to_publication_date:"+to+"-12-31")
	}
	if typ := strings.ToLower(field(openAlexFieldType)); typ != "" {
		if !openAlexTypeRe.MatchString(typ) {
			return p, fmt.Errorf("openalex: %s %q is not a work type such as article, or types joined by |", openAlexFieldType, field(openAlexFieldType))
		}
		filters = append(filters, "type:"+typ)
	}
	p.Filter = strings.Join(filters, ",")
	return p, nil
}

var (
	openAlexYearsRe = regexp.MustCompile(`^(\d{4})(?:\s*[-–]\s*(\d{4}))?$`)
	openAlexTypeRe  = regexp.MustCompile(`^[a-z][a-z-]*(?:\|[a-z][a-z-]*)*$`)
	// openAlexInitialRe is one or two initials, with or without periods:
	// "D", "D.", "DM", "D.M." — not "MRI", which is an acronym.
	openAlexInitialRe = regexp.MustCompile(`^(?:\p{Lu}\.?){1,2}$`)
	// openAlexAuthorIDRe is a bare OpenAlex author id.
	openAlexAuthorIDRe = regexp.MustCompile(`^A\d+$`)
)

// openAlexNameParticles are the lowercase words a printed name may carry
// between its parts. A particle is never the last token.
var openAlexNameParticles = map[string]bool{
	"van": true, "von": true, "der": true, "den": true, "de": true, "da": true, "di": true, "del": true,
	"della": true, "la": true, "le": true, "du": true, "dos": true, "das": true, "bin": true, "ibn": true,
	"al": true, "el": true, "ter": true, "ten": true, "af": true, "av": true, "y": true, "e": true,
}

// openAlexWorkBudget reads the work budget from the scope: absent means the
// default, non-numeric is refused, and the result is clamped to
// [1, openAlexMaxWorks]. It never grows to cover Scope.Max: the two are
// separate numbers on purpose.
func openAlexWorkBudget(s Scope) (int, error) {
	raw := strings.TrimSpace(s.Fields[openAlexFieldWorks])
	if raw == "" {
		return openAlexDefaultWorks, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("openalex: %s %q is not a number", openAlexFieldWorks, raw)
	}
	if n < 1 {
		n = openAlexDefaultWorks
	}
	if n > openAlexMaxWorks {
		n = openAlexMaxWorks
	}
	return n, nil
}

// openAlexNameShaped reports whether a query reads as a printed person name:
// two to six tokens, every one a capitalised word, an initial or a name
// particle, at least one a word. "Dana Reyes", "Reyes D", "van der Walt"
// are names; "field cycling MRI", "diffusion MRI", "relaxometry", "T1
// dispersion" are not. A title-cased keyword pair ("Low Field") reads as a
// name — the mode field exists for that case, and the rule stays blunt on
// purpose: guessing harder is what a model would do.
func openAlexNameShaped(q string) bool {
	toks := strings.Fields(q)
	if len(toks) < 2 || len(toks) > 6 {
		return false
	}
	words := 0
	for i, t := range toks {
		switch {
		case openAlexInitialRe.MatchString(t):
		case openAlexCapWord(t):
			words++
		case openAlexNameParticles[strings.ToLower(t)] && i < len(toks)-1:
		default:
			return false
		}
	}
	return words >= 1
}

// openAlexCapWord is one capitalised name part: an uppercase letter, then
// letters with at most one uppercase in a row (McDonald, DeLuca), internal
// apostrophes or hyphens (O'Brien, Jean-Pierre), an optional trailing
// period (Jr.), and at least one lowercase letter. "MRI" and "DM" are not.
func openAlexCapWord(tok string) bool {
	tok = strings.TrimSuffix(tok, ".")
	runes := []rune(tok)
	if len(runes) == 0 || !unicode.IsUpper(runes[0]) {
		return false
	}
	lower, upperRun := 0, 0
	for i, r := range runes {
		switch {
		case unicode.IsUpper(r):
			upperRun++
			if upperRun > 1 {
				return false
			}
		case unicode.IsLower(r):
			lower++
			upperRun = 0
		case r == '\'' || r == '’' || r == '-':
			if i == 0 || i == len(runes)-1 {
				return false
			}
			upperRun = 0
		default:
			return false
		}
	}
	return lower > 0
}

// params are the query parameters every call of this plan shares: the
// search text (as `search=` for full text, inside `filter=` for
// title-and-abstract) and the filters.
func (p openAlexPlan) params() url.Values {
	v := url.Values{}
	if p.Text == openAlexTextFulltext {
		v.Set("search", p.Query)
	}
	if p.Filter != "" {
		v.Set("filter", p.Filter)
	}
	return v
}

// upstream is the exact search as sent, in words an evidence row can quote.
func (p openAlexPlan) upstream() string {
	var parts []string
	if p.Text == openAlexTextFulltext {
		parts = append(parts, "search="+p.Query)
	}
	if p.Filter != "" {
		parts = append(parts, "filter="+p.Filter)
	}
	return strings.Join(parts, "&")
}

// ---- responses ----

// openAlexWorksResponse is the works list envelope. Results is a pointer so
// a response with NO results key is told apart from an honest empty page;
// meta.count is the field, meta.next_cursor the way to the next page.
type openAlexWorksResponse struct {
	Meta *struct {
		Count      *int   `json:"count"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
	Results *[]openAlexWork `json:"results"`
}

// openAlexGroupResponse is the group_by envelope: one row per author id with
// the registry's count of matching works for that author.
type openAlexGroupResponse struct {
	GroupBy *[]struct {
		Key            string `json:"key"`
		KeyDisplayName string `json:"key_display_name"`
		Count          int    `json:"count"`
	} `json:"group_by"`
}

// openAlexAuthorID is the bare author id ("A5023888391") a record or
// authorship carries, "" when it carries none or something that is not one.
func openAlexAuthorID(raw string) string {
	id := strings.ToUpper(strings.TrimPrefix(openAlexAuthorURL(raw), openAlexAuthorRoot))
	if !openAlexAuthorIDRe.MatchString(id) {
		return ""
	}
	return id
}

// openAlexWorkID is the bare work id ("W3035965352"), "" when absent.
func openAlexWorkID(raw string) string {
	id := strings.TrimSpace(raw)
	if j := strings.LastIndex(id, "/"); j >= 0 {
		id = id[j+1:]
	}
	id = strings.ToUpper(id)
	if !openAlexWorkIDRe.MatchString(id) {
		return ""
	}
	return id
}

// ---- works search ----

// searchWorks is the keyword branch: read works under the budget, aggregate
// their authorships into people, count everyone, then enrich and draft the
// first Scope.Max. Any failed request fails the run: a partial field
// returned as if it were the field is the bug Phase 0 exists to prevent.
func (oa OpenAlex) searchWorks(ctx context.Context, s Scope, plan openAlexPlan) ([]CandidateDraft, Retrieval, error) {
	ret := Retrieval{Unit: "works"}
	max := s.Max
	if max <= 0 {
		max = openAlexDefaultMax
	}
	if max > openAlexMaxPerPage {
		max = openAlexMaxPerPage
	}

	works, available, err := oa.readWorks(ctx, plan)
	if err != nil {
		return nil, ret, err
	}
	ret.Available = available
	ret.Read = len(works)
	people := openAlexAggregate(works)
	ret.PeopleSeen = len(people)
	if len(people) == 0 {
		return []CandidateDraft{}, ret, nil
	}
	shown := people[:min(max, len(people))]

	groups, err := oa.groupByAuthor(ctx, plan)
	if err != nil {
		return nil, ret, err
	}
	ids := make([]string, 0, len(shown))
	for _, p := range shown {
		if p.AuthorID != "" {
			ids = append(ids, p.AuthorID)
		}
	}
	records, err := oa.authorRecords(ctx, ids)
	if err != nil {
		return nil, ret, err
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, len(shown))
	for _, p := range shown {
		var rec *openAlexAuthor
		if r, ok := records[p.AuthorID]; ok && p.AuthorID != "" {
			rec = &r
		}
		count, known := groups[p.AuthorID]
		out = append(out, oa.worksDraft(p, works, rec, count, known && p.AuthorID != "", plan, s.Role, retrieved))
	}
	return out, ret, nil
}

// readWorks pages /works with a cursor until the budget is met, a page
// comes back empty, the registry offers no next cursor, or it offers the
// one just used. Works are deduplicated by id across pages; the count on
// the first page is the field. The page count is bounded on its own too,
// so a registry that keeps issuing new cursors for the same page cannot
// keep this loop alive.
func (oa OpenAlex) readWorks(ctx context.Context, plan openAlexPlan) ([]openAlexWork, *int, error) {
	var (
		works     []openAlexWork
		available *int
		seen      = map[string]bool{}
		cursor    = "*"
		maxPages  = openAlexMaxWorks/openAlexWorksPerPage + 1
	)
	for page := 0; len(works) < plan.Budget && page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("openalex: %v", err)
		}
		params := plan.params()
		params.Set("per-page", strconv.Itoa(min(openAlexWorksPerPage, plan.Budget-len(works))))
		params.Set("cursor", cursor)
		params.Set("select", openAlexWorkSelect)
		body, err := oa.fetch(ctx, "/works", params, openAlexWorksMaxBody)
		if err != nil {
			return nil, nil, err
		}
		var resp openAlexWorksResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, nil, fmt.Errorf("openalex: malformed response from /works: %v", err)
		}
		if resp.Results == nil {
			return nil, nil, errors.New("openalex: response from /works has no results field")
		}
		if page == 0 && resp.Meta != nil && resp.Meta.Count != nil && *resp.Meta.Count >= 0 {
			available = Known(*resp.Meta.Count)
		}
		for _, w := range *resp.Results {
			id := openAlexWorkID(w.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			works = append(works, w)
			if len(works) >= plan.Budget {
				break
			}
		}
		if len(*resp.Results) == 0 {
			break
		}
		next := ""
		if resp.Meta != nil {
			next = strings.TrimSpace(resp.Meta.NextCursor)
		}
		if next == "" || next == cursor {
			break
		}
		cursor = next
	}
	return works, available, nil
}

// groupByAuthor is one call over the same search asking the registry how
// many matching works each author has — its top openAlexGroupPerPage
// authors by that count. An author absent from the answer is not "zero":
// the registry simply did not list them, and no row says otherwise.
func (oa OpenAlex) groupByAuthor(ctx context.Context, plan openAlexPlan) (map[string]int, error) {
	params := plan.params()
	params.Set("group_by", "authorships.author.id")
	params.Set("per-page", strconv.Itoa(openAlexGroupPerPage))
	body, err := oa.get(ctx, "/works", params)
	if err != nil {
		return nil, err
	}
	var resp openAlexGroupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openalex: malformed response from /works (group_by): %v", err)
	}
	if resp.GroupBy == nil {
		return nil, errors.New("openalex: response from /works (group_by) has no group_by field")
	}
	out := make(map[string]int, len(*resp.GroupBy))
	for _, g := range *resp.GroupBy {
		if id := openAlexAuthorID(g.Key); id != "" && g.Count >= 0 {
			out[id] = g.Count
		}
	}
	return out, nil
}

// authorRecords fetches author records in batches of openAlexAuthorBatch
// ids per request, keyed by bare id. An id the registry does not return —
// merged away, or simply missing — has no entry: the draft is built from
// its authorships alone, and says nothing the record would have said.
func (oa OpenAlex) authorRecords(ctx context.Context, ids []string) (map[string]openAlexAuthor, error) {
	out := map[string]openAlexAuthor{}
	for start := 0; start < len(ids); start += openAlexAuthorBatch {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("openalex: %v", err)
		}
		end := min(start+openAlexAuthorBatch, len(ids))
		params := url.Values{}
		params.Set("filter", "ids.openalex:"+strings.Join(ids[start:end], "|"))
		params.Set("per-page", strconv.Itoa(end-start))
		body, err := oa.get(ctx, "/authors", params)
		if err != nil {
			return nil, err
		}
		var resp openAlexAuthorsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("openalex: malformed response from /authors (batch): %v", err)
		}
		if resp.Results == nil {
			return nil, errors.New("openalex: response from /authors (batch) has no results field")
		}
		for _, a := range *resp.Results {
			if id := openAlexAuthorID(a.ID); id != "" {
				out[id] = a
			}
		}
	}
	return out, nil
}

// ---- aggregation ----

// openAlexMention is one authorship on one read work.
type openAlexMention struct{ Work, Index int }

// openAlexPerson is one aggregated row: the registry key, the name the
// first authorship printed, and every mention in order of reading.
type openAlexPerson struct {
	AuthorID, ORCID, Name string
	Mentions              []openAlexMention
}

// openAlexAggregate folds authorships into people by exact key: the
// registry's author id, else the ORCID it printed. No name ever keys a
// person; an authorship with neither id is left out. People come out in
// order of first mention.
func openAlexAggregate(works []openAlexWork) []openAlexPerson {
	var people []openAlexPerson
	index := map[string]int{}
	for wi, w := range works {
		for ai, a := range w.Authorships {
			id := openAlexAuthorID(a.Author.ID)
			orcid := orcidID(a.Author.ORCID)
			name := orStr(strings.TrimSpace(a.Author.DisplayName), strings.Join(strings.Fields(a.RawAuthorName), " "))
			key := ""
			switch {
			case id != "":
				key = "id/" + id
			case orcid != "":
				key = "orcid/" + orcid
			}
			if key == "" || name == "" {
				continue
			}
			if at, ok := index[key]; ok {
				p := &people[at]
				p.Mentions = append(p.Mentions, openAlexMention{wi, ai})
				if p.ORCID == "" {
					p.ORCID = orcid
				}
				continue
			}
			index[key] = len(people)
			people = append(people, openAlexPerson{AuthorID: id, ORCID: orcid, Name: name, Mentions: []openAlexMention{{wi, ai}}})
		}
	}
	return people
}

// ---- drafts ----

// worksDraft turns one aggregated person into a draft. With an author
// record it starts from the same draft the name lookup builds (author page
// cited, topics, counts, last-known institution); without one it starts
// from what the authorships said. Then, in order: the registry's count of
// matching works when it listed this author, and per work read — once each
// — a publication row, an affiliation row when the authorship names an
// institution, and the coauthor/same_lab claims that work supports,
// deduplicated by far endpoint and kind.
func (oa OpenAlex) worksDraft(p openAlexPerson, works []openAlexWork, rec *openAlexAuthor, groupCount int, groupKnown bool, plan openAlexPlan, role string, retrieved time.Time) CandidateDraft {
	var d CandidateDraft
	if rec != nil {
		d, _ = oa.draft(*rec, role, retrieved)
	}
	if d.Name == "" {
		d = CandidateDraft{SourceID: oa.ID(), ExternalID: p.AuthorID, Name: p.Name, Role: strings.TrimSpace(role)}
		if u := openAlexAuthorURL(p.AuthorID); u != "" {
			d.Links = append(d.Links, u)
		}
	}
	if d.Orcid == "" {
		if orcid := openAlexORCIDURL(p.ORCID); orcid != "" {
			d.Links = append(d.Links, orcid)
			d.Orcid = orcid
		}
	}
	page := orStr(openAlexAuthorURL(p.AuthorID), d.Orcid)

	seenWork := map[int]bool{}
	var mentions []openAlexMention
	for _, m := range p.Mentions {
		if m.Work < 0 || m.Work >= len(works) || seenWork[m.Work] {
			continue
		}
		seenWork[m.Work] = true
		mentions = append(mentions, m)
	}
	if d.Org == "" {
		for _, m := range mentions {
			if org := works[m.Work].Authorships[m.Index].org(); org != "" {
				d.Org = org
				break
			}
		}
	}

	note := []string{"matching works read: " + strconv.Itoa(len(mentions)) + " of " + strconv.Itoa(len(works))}
	if groupKnown {
		note = append(note, "matching works in OpenAlex: "+strconv.Itoa(groupCount))
	}
	if d.Note != "" {
		note = append(note, d.Note)
	}
	d.Note = strings.Join(note, " · ")

	if groupKnown {
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: oa.ID(), URLOrFile: page, RetrievedAt: retrieved,
			Snippet: "works matching this search attributed to this author: " + strconv.Itoa(groupCount) +
				" (group_by authorships.author.id · " + plan.upstream() + ")",
			Kind: EvidencePublication, Trust: TrustMedium,
		})
	}
	edgeSeen := map[string]bool{}
	for _, m := range mentions {
		w := works[m.Work]
		a := w.Authorships[m.Index]
		workURL := orStr(w.url(), page)
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: oa.ID(), URLOrFile: workURL, RetrievedAt: retrieved,
			Snippet: oa.workSnippet(w, a), Kind: EvidencePublication, Trust: TrustMedium,
		})
		if insts := openAlexInstitutionsLabel(a); insts != "" {
			d.Evidence = append(d.Evidence, Evidence{
				SourceID: oa.ID(), URLOrFile: workURL, RetrievedAt: retrieved,
				Snippet: "affiliation on " + w.citation() + " (" + a.position() + "): " + insts,
				Kind:    EvidenceAffiliation, Trust: TrustMedium,
			})
		}
		for _, e := range oa.workEdges(w, m.Index) {
			if k := e.Key(); !edgeSeen[k] {
				edgeSeen[k] = true
				d.Edges = append(d.Edges, e)
			}
		}
	}
	return d
}

// workSnippet quotes what the registry said about this person on this
// work, as labelled parts the UI can split: author, position, title,
// venue, pubdate, type, openalex, doi, cited_by_count, authors,
// institutions. Every value is the record's own.
func (oa OpenAlex) workSnippet(w openAlexWork, a openAlexAuthorsh) string {
	title := orStr(strings.TrimSpace(w.Title), strings.TrimSpace(w.DisplayName))
	if r := []rune(title); len(r) > openAlexWorkTitleChars {
		title = string(r[:openAlexWorkTitleChars]) + "…"
	}
	name := orStr(strings.TrimSpace(a.Author.DisplayName), strings.Join(strings.Fields(a.RawAuthorName), " "))
	parts := []string{"author: " + name, "position: " + a.position(), "title: " + title}
	if venue := strings.TrimSpace(w.PrimaryLocation.Source.DisplayName); venue != "" {
		parts = append(parts, "venue: "+venue)
	}
	switch {
	case strings.TrimSpace(w.PublicationDate) != "":
		parts = append(parts, "pubdate: "+strings.TrimSpace(w.PublicationDate))
	case w.PublicationYear > 0:
		parts = append(parts, "pubdate: "+strconv.Itoa(w.PublicationYear))
	}
	if typ := strings.TrimSpace(w.Type); typ != "" {
		parts = append(parts, "type: "+typ)
	}
	if id := openAlexWorkID(w.ID); id != "" {
		parts = append(parts, "openalex: "+id)
	}
	if doi := w.doi(); doi != "" {
		parts = append(parts, "doi: "+doi)
	}
	if w.CitedByCount != nil {
		parts = append(parts, "cited_by_count: "+strconv.Itoa(*w.CitedByCount))
	}
	authors := "authors: " + strconv.Itoa(len(w.Authorships))
	if len(w.Authorships) > openAlexMaxEdgeAuthors {
		authors += " (too many to call coauthorship a relationship — no edges claimed)"
	}
	parts = append(parts, authors)
	if insts := openAlexInstitutionsLabel(a); insts != "" {
		parts = append(parts, "institutions: "+insts)
	}
	return strings.Join(parts, " · ")
}

// openAlexInstitutionsLabel names every structured institution on one
// authorship, each with its registry id when it has one — the id is what
// makes "same institution" a claim that can be checked later.
func openAlexInstitutionsLabel(a openAlexAuthorsh) string {
	var out []string
	for _, i := range a.Institutions {
		name := strings.TrimSpace(i.DisplayName)
		if name == "" {
			continue
		}
		if k := i.key(); k != "" {
			name += " (" + openAlexInstitutionRoot + k + ")"
		}
		out = append(out, name)
	}
	return strings.Join(out, "; ")
}
