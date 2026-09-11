package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The works fixtures are three cursor pages of a title-and-abstract search
// (the third empty), the registry's group_by answer over the same search,
// and one batched author response. Together they carry every shape the
// keyword branch has to handle: an author on several works, an author
// keyed only by ORCID (id null on every work), an authorship with neither
// id nor ORCID, a work repeated across pages, an author id the batch does
// not return, a group row for an author never read, and affiliation
// strings that end in an email address.

// openAlexWorksServer routes by path and query the way the live API does:
// /works with a cursor → the page for that cursor; /works with group_by →
// the groups; /authors with a filter → the batch; /authors with search →
// the name fixture. It records every request it saw.
type openAlexWorksServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	reqs  []*http.Request
	pages map[string]string // cursor → body
	group string
	batch string
}

func newOpenAlexWorksServer(t *testing.T) *openAlexWorksServer {
	t.Helper()
	s := &openAlexWorksServer{
		pages: map[string]string{
			"*":        openAlexFixture(t, "openalex-works-search-p1.json"),
			"CURSOR-2": openAlexFixture(t, "openalex-works-search-p2.json"),
			"CURSOR-3": openAlexFixture(t, "openalex-works-search-p3.json"),
		},
		group: openAlexFixture(t, "openalex-works-group.json"),
		batch: openAlexFixture(t, "openalex-authors-batch.json"),
	}
	authors := openAlexFixture(t, "openalex-authors.json")
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/works" && q.Get("group_by") != "":
			_, _ = w.Write([]byte(s.group))
		case r.URL.Path == "/works":
			body, ok := s.pages[q.Get("cursor")]
			if !ok {
				http.Error(w, `{"error":"unknown cursor"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(body))
		case r.URL.Path == "/authors" && q.Get("filter") != "":
			_, _ = w.Write([]byte(s.batch))
		case r.URL.Path == "/authors" && q.Get("search") != "":
			_, _ = w.Write([]byte(authors))
		default:
			http.Error(w, `{"error":"unexpected"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *openAlexWorksServer) adapter() OpenAlex {
	return OpenAlex{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *openAlexWorksServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

// paths lists the requests as "path?key=value" for the keys that matter.
func (s *openAlexWorksServer) paths() []string {
	var out []string
	for _, r := range s.requests() {
		q := r.URL.Query()
		line := r.URL.Path
		for _, k := range []string{"search", "cursor", "group_by", "per-page"} {
			if v := q.Get(k); v != "" {
				line += " " + k + "=" + v
			}
		}
		if f := q.Get("filter"); f != "" {
			line += " filter=" + f
		}
		out = append(out, line)
	}
	return out
}

func openAlexDraftNamed(drafts []CandidateDraft, name string) (CandidateDraft, bool) {
	for _, d := range drafts {
		if d.Name == name {
			return d, true
		}
	}
	return CandidateDraft{}, false
}

// Intent is read from the query's shape, conservatively: names keep the
// author lookup, everything else searches works, and the mode field wins.
func TestOpenAlexQueryIntent(t *testing.T) {
	names := []string{"Dana Reyes", "Reyes D", "D. Reyes", "Dana M. Reyes", "K. Jarrod Millman",
		"Stéfan J. van der Walt", "O'Brien Conor", "Jean-Pierre Dupont", "van Gogh", "Li Wei", "Reyes DM", "John Smith Jr."}
	keywords := []string{"field cycling MRI", "diffusion MRI", "MRI", "relaxometry", "T1 dispersion", "low field mri",
		"Field Cycling MRI", "dana reyes", "Dana", "\"field cycling\" relaxometry", "MRI Engineer", "de novo protein design",
		"Reyes van", "A Very Long Name With Seven Whole Tokens", "10.1000/x"}
	for _, q := range names {
		if !openAlexNameShaped(q) {
			t.Errorf("%q should read as a name", q)
		}
		p, err := openAlexPlanScope(Scope{Query: q})
		if err != nil || p.Mode != openAlexModeAuthors {
			t.Errorf("%q → mode %q err %v, want authors", q, p.Mode, err)
		}
	}
	for _, q := range keywords {
		if openAlexNameShaped(q) {
			t.Errorf("%q should read as a keyword", q)
		}
		p, err := openAlexPlanScope(Scope{Query: q})
		if err != nil || p.Mode != openAlexModeWorks {
			t.Errorf("%q → mode %q err %v, want works", q, p.Mode, err)
		}
	}
	// the override
	p, err := openAlexPlanScope(Scope{Query: "Low Field", Fields: map[string]string{"mode": "works"}})
	if err != nil || p.Mode != openAlexModeWorks {
		t.Errorf("mode=works on a name-shaped query: %+v %v", p, err)
	}
	p, err = openAlexPlanScope(Scope{Query: "field cycling MRI", Fields: map[string]string{"mode": " Authors "}})
	if err != nil || p.Mode != openAlexModeAuthors {
		t.Errorf("mode=authors on a keyword: %+v %v", p, err)
	}
	if _, err := openAlexPlanScope(Scope{Query: "x", Fields: map[string]string{"mode": "people"}}); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Errorf("an unknown mode must be refused: %v", err)
	}
	if _, err := openAlexPlanScope(Scope{Query: "  "}); err == nil || !strings.Contains(err.Error(), "query") {
		t.Errorf("an empty query must be refused: %v", err)
	}
}

// The plan is exact: the filter string is what goes upstream, the budget is
// bounded, and inputs that cannot be sent exactly are refused. PrepareScope
// writes the effective values back so the run records them.
func TestOpenAlexWorksPlanAndPrepareScope(t *testing.T) {
	p, err := openAlexPlanScope(Scope{Query: "field cycling MRI", Fields: map[string]string{"years": "2018-2026", "type": "Article"}})
	if err != nil {
		t.Fatal(err)
	}
	wantFilter := "title_and_abstract.search:field cycling MRI,from_publication_date:2018-01-01,to_publication_date:2026-12-31,type:article"
	if p.Mode != openAlexModeWorks || p.Text != openAlexTextTitleAbstract || p.Budget != openAlexDefaultWorks || p.Filter != wantFilter {
		t.Errorf("plan: %+v", p)
	}
	if got := p.params(); got.Get("filter") != wantFilter || got.Get("search") != "" {
		t.Errorf("params: %v", got)
	}
	if p.upstream() != "filter="+wantFilter {
		t.Errorf("upstream: %q", p.upstream())
	}

	// full text moves the query to search= and leaves the filters to the
	// filters; a single year is a one-year window; a comma in the phrase is
	// a word break, never a filter separator
	p, err = openAlexPlanScope(Scope{Query: "field cycling, MRI|NMR", Fields: map[string]string{"text": "full text", "years": "2020", "works": "30"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Text != openAlexTextFulltext || p.Budget != 30 || p.Filter != "from_publication_date:2020-01-01,to_publication_date:2020-12-31" ||
		p.params().Get("search") != "field cycling, MRI|NMR" || p.upstream() != "search=field cycling, MRI|NMR&filter="+p.Filter {
		t.Errorf("fulltext plan: %+v upstream=%q", p, p.upstream())
	}
	p, _ = openAlexPlanScope(Scope{Query: "field cycling, MRI|NMR"})
	if p.Filter != "title_and_abstract.search:field cycling MRI NMR" {
		t.Errorf("comma and bar folded: %q", p.Filter)
	}

	// the budget: absent → default, zero → default, huge → ceiling, prose → refused
	for raw, want := range map[string]int{"": 200, "0": 200, "-5": 200, "7": 7, "9999": 500} {
		p, err := openAlexPlanScope(Scope{Query: "mri", Fields: map[string]string{"works": raw}})
		if err != nil || p.Budget != want {
			t.Errorf("works %q → %d, %v (want %d)", raw, p.Budget, err, want)
		}
	}
	for name, fields := range map[string]map[string]string{
		"prose budget":      {"works": "lots"},
		"bad years":         {"years": "20x"},
		"reversed years":    {"years": "2026-2018"},
		"three years":       {"years": "2018-2020-2022"},
		"type with a comma": {"type": "article,preprint"},
		"type injection":    {"type": "article,authorships.author.id:A1"},
		"unknown text":      {"text": "abstract-only"},
		"years on authors":  {"mode": "authors", "years": "2020"},
		"text on authors":   {"mode": "authors", "text": "fulltext"},
	} {
		if _, err := openAlexPlanScope(Scope{Query: "mri", Fields: fields}); err == nil || !strings.HasPrefix(err.Error(), "openalex:") {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if p, err := openAlexPlanScope(Scope{Query: "mri", Fields: map[string]string{"type": "article|preprint"}}); err != nil || !strings.HasSuffix(p.Filter, ",type:article|preprint") {
		t.Errorf("OR-joined types: %+v %v", p, err)
	}

	// PrepareScope records the branch and, for works, the effective budget,
	// text and filter — and leaves the caller's other fields alone
	s, err := (OpenAlex{}).PrepareScope(Scope{Query: "field cycling MRI", Max: 25, Fields: map[string]string{"years": "2018-2026", "filter": "stale", "note": "kept"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"mode": "works", "works": "200", "text": "title-abstract", "years": "2018-2026", "note": "kept",
		"filter": "title_and_abstract.search:field cycling MRI,from_publication_date:2018-01-01,to_publication_date:2026-12-31"}
	for k, v := range want {
		if s.Fields[k] != v {
			t.Errorf("prepared %s=%q want %q", k, s.Fields[k], v)
		}
	}
	if len(s.Fields) != len(want) {
		t.Errorf("prepared fields: %v", s.Fields)
	}
	s, err = (OpenAlex{}).PrepareScope(Scope{Query: "Dana Reyes", Max: 25})
	if err != nil || s.Fields["mode"] != "authors" || s.Fields["works"] != "" || s.Fields["filter"] != "" {
		t.Errorf("a name prepares as the author branch, with nothing works-only: %v %v", s.Fields, err)
	}
	// preparing twice is the same as preparing once
	again, err := (OpenAlex{}).PrepareScope(s)
	if err != nil || len(again.Fields) != len(s.Fields) {
		t.Errorf("idempotent: %v %v", again.Fields, err)
	}
	if _, err := (OpenAlex{}).PrepareScope(Scope{Query: "mri", Fields: map[string]string{"years": "soon"}}); err == nil {
		t.Error("PrepareScope must refuse what the search would refuse")
	}
	// the single-paper branch is untouched
	if s, err := (OpenAlex{}).PrepareScope(Scope{Fields: map[string]string{"work": "10.1000/x"}}); err != nil || s.Fields["mode"] != "" {
		t.Errorf("work scope: %v %v", s.Fields, err)
	}
}

// A keyword fixture with several pages and repeated authors yields one
// draft per stable author id, not one per work — and never touches
// /authors?search.
func TestOpenAlexKeywordSearchesWorksAndAggregatesPeople(t *testing.T) {
	s := newOpenAlexWorksServer(t)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "field cycling MRI", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}

	// the requests: three cursor pages (the per-page is the page size while
	// the budget has room for it; the empty third page ends it), one group_by, one
	// author batch naming exactly the shown ids — and no name search
	paths := s.paths()
	want := []string{
		"/works cursor=* per-page=50 filter=title_and_abstract.search:field cycling MRI",
		"/works cursor=CURSOR-2 per-page=50 filter=title_and_abstract.search:field cycling MRI",
		"/works cursor=CURSOR-3 per-page=50 filter=title_and_abstract.search:field cycling MRI",
		"/works group_by=authorships.author.id per-page=200 filter=title_and_abstract.search:field cycling MRI",
		"/authors per-page=3 filter=ids.openalex:A1|A2|A3",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
	for _, r := range s.requests() {
		if r.URL.Path == "/works" && r.URL.Query().Get("group_by") == "" && r.URL.Query().Get("select") != openAlexWorkSelect {
			t.Errorf("a works page is trimmed to the fields read: %v", r.URL)
		}
		if r.Header.Get("User-Agent") != OpenAlexUserAgent {
			t.Errorf("User-Agent: %q", r.Header.Get("User-Agent"))
		}
	}

	// the counts: the field, works decoded (the repeated work once), people
	// seen (the keyless authorship is nobody), people shown
	if ret.Unit != "works" || ret.Available == nil || *ret.Available != 1996 || ret.Read != 3 || ret.PeopleSeen != 4 || len(got) != 4 {
		t.Fatalf("counts: available=%v read=%d people=%d shown=%d unit=%q", deref(ret.Available), ret.Read, ret.PeopleSeen, len(got), ret.Unit)
	}
	// source order: first mention across works in the registry's order
	names := []string{got[0].Name, got[1].Name, got[2].Name, got[3].Name}
	if strings.Join(names, "|") != "Lionel Broche|Orcid Only|David J. Lurie|P. J. Ross" {
		t.Fatalf("people in order of first mention: %v", names)
	}
	if _, found := openAlexDraftNamed(got, "Nobody Nameable"); found {
		t.Fatal("an authorship with neither an author id nor an ORCID is not a person")
	}

	// Broche: two works, the author record's topics and institution, the
	// registry's count for him, one publication row per work read
	broche := got[0]
	if broche.SourceID != "openalex" || broche.ExternalID != "A1" || broche.Org != "University of Aberdeen" || broche.Location != "GB" ||
		broche.Role != "role/mri-engineer" || broche.Orcid != "https://orcid.org/0000-0001-0000-0001" {
		t.Fatalf("broche: %+v", broche)
	}
	if strings.Join(broche.Topics, "|") != "Field-Cycling NMR|Low-Field MRI" {
		t.Errorf("topics from the author record only: %v", broche.Topics)
	}
	if !strings.HasPrefix(broche.Note, "matching works read: 2 of 3 · matching works in OpenAlex: 61 · topics: Field-Cycling NMR; Low-Field MRI") {
		t.Errorf("note: %q", broche.Note)
	}
	if len(broche.Links) != 2 || broche.Links[0] != "https://openalex.org/A1" || broche.Links[1] != broche.Orcid {
		t.Errorf("links: %v", broche.Links)
	}
	var pubs, affs []Evidence
	for _, ev := range broche.Evidence {
		if ev.SourceID != "openalex" || !ev.Cited() || ev.RetrievedAt.IsZero() || ev.Trust != TrustMedium {
			t.Errorf("evidence shape: %+v", ev)
		}
		switch ev.Kind {
		case EvidencePublication:
			pubs = append(pubs, ev)
		case EvidenceAffiliation:
			affs = append(affs, ev)
		}
	}
	// author-page summary, the group count, W1001, W1003
	if len(pubs) != 4 || len(affs) != 3 {
		t.Fatalf("broche rows: %d publication, %d affiliation: %+v", len(pubs), len(affs), broche.Evidence)
	}
	if !strings.Contains(pubs[0].Snippet, "works_count: 120") || pubs[0].URLOrFile != "https://openalex.org/A1" {
		t.Errorf("author-record row first: %+v", pubs[0])
	}
	if pubs[1].URLOrFile != "https://openalex.org/A1" || pubs[1].Snippet != "works matching this search attributed to this author: 61 (group_by authorships.author.id · filter=title_and_abstract.search:field cycling MRI)" {
		t.Errorf("group row: %+v", pubs[1])
	}
	w1 := pubs[2]
	if w1.URLOrFile != "https://doi.org/10.1000/fc.2021.1" {
		t.Errorf("a work cites at its DOI: %q", w1.URLOrFile)
	}
	for _, part := range []string{"author: Lionel Broche", "position: first author", "title: Fast field-cycling MRI of the human brain",
		"venue: Magnetic Resonance in Medicine", "pubdate: 2021-03-15", "type: article", "openalex: W1001", "doi: 10.1000/fc.2021.1",
		"cited_by_count: 42", "authors: 4", "institutions: University of Aberdeen (https://openalex.org/I1)"} {
		if !strings.Contains(w1.Snippet, part) {
			t.Errorf("work row lacks %q: %q", part, w1.Snippet)
		}
	}
	if !strings.Contains(pubs[3].Snippet, "openalex: W1003") || !strings.Contains(pubs[3].Snippet, "cited_by_count: 3") {
		t.Errorf("second work row: %q", pubs[3].Snippet)
	}
	if !strings.Contains(affs[1].Snippet, "affiliation on Fast field-cycling MRI of the human brain, Magnetic Resonance in Medicine, 2021, 10.1000/fc.2021.1 (first author): University of Aberdeen (https://openalex.org/I1)") {
		t.Errorf("affiliation row: %q", affs[1].Snippet)
	}
	// edges: the coauthor and same_lab claims each work supports, once per
	// far endpoint and kind; nobody keyless is ever a far endpoint
	seen := map[string]bool{}
	for _, e := range broche.Edges {
		if seen[e.Key()] {
			t.Errorf("duplicate edge: %+v", e)
		}
		seen[e.Key()] = true
		if !strings.HasPrefix(e.From, ExtNodePrefix) || strings.Contains(e.Basis, "Nobody Nameable") {
			t.Errorf("edge: %+v", e)
		}
	}
	if !seen["ext/orcid/0000-0003-0000-0003\x00\x00coauthor"] || !seen["ext/orcid/0000-0003-0000-0003\x00\x00same_lab"] || !seen["ext/orcid/0000-0002-0000-0002\x00\x00coauthor"] {
		t.Errorf("expected claims missing: %+v", broche.Edges)
	}

	// Orcid Only: keyed by ORCID alone, no author page, no group row, both
	// works, no institution anywhere
	only := got[1]
	if only.ExternalID != "" || only.Orcid != "https://orcid.org/0000-0002-0000-0002" || len(only.Links) != 1 || only.Org != "" || len(only.Topics) != 0 {
		t.Fatalf("orcid-only: %+v", only)
	}
	if only.Note != "matching works read: 2 of 3" || len(only.Evidence) != 2 {
		t.Errorf("orcid-only note/evidence: %q %+v", only.Note, only.Evidence)
	}

	// Lurie: three works (the ORCID-less mention on W1003 still his by id),
	// institution through the plural field, the registry's 77
	lurie := got[2]
	if lurie.ExternalID != "A2" || lurie.Org != "University of Aberdeen" || !strings.Contains(lurie.Note, "matching works read: 3 of 3 · matching works in OpenAlex: 77") {
		t.Fatalf("lurie: %+v", lurie)
	}
	// Ross: no author record came back — the draft is what the authorship
	// said, cites the work at its OpenAlex page (no DOI), and no group row
	ross := got[3]
	if ross.ExternalID != "A3" || ross.Org != "University of Aberdeen" || len(ross.Links) != 1 || ross.Links[0] != "https://openalex.org/A3" ||
		len(ross.Topics) != 0 || ross.Note != "matching works read: 1 of 3" || len(ross.Evidence) != 2 {
		t.Fatalf("ross: %+v", ross)
	}
	if ross.Evidence[0].URLOrFile != "https://openalex.org/W1002" || !strings.Contains(ross.Evidence[0].Snippet, "openalex: W1002") || strings.Contains(ross.Evidence[0].Snippet, "doi:") {
		t.Errorf("ross cites the work page: %+v", ross.Evidence[0])
	}
}

// The display cap cuts people, never the field: fewer shown, the same
// works read and people seen, and only the shown ids are enriched.
func TestOpenAlexWorksDisplayCapDoesNotShrinkTheField(t *testing.T) {
	s := newOpenAlexWorksServer(t)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || ret.Read != 3 || ret.PeopleSeen != 4 || got[0].Name != "Lionel Broche" || got[1].Name != "Orcid Only" {
		t.Fatalf("capped at 2: shown=%d read=%d people=%d", len(got), ret.Read, ret.PeopleSeen)
	}
	batch := s.paths()[len(s.paths())-1]
	if batch != "/authors per-page=1 filter=ids.openalex:A1" {
		t.Errorf("only the shown ids are enriched: %q", batch)
	}
}

// Cursor pagination stops at the work budget, on an empty page, on a
// missing cursor, on a repeated cursor — and is bounded on its own when a
// registry keeps issuing fresh cursors for the same works.
func TestOpenAlexWorksCursorAndBudget(t *testing.T) {
	// budget 2: the first page fills it; no second page is asked for
	s := newOpenAlexWorksServer(t)
	_, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 25, Fields: map[string]string{"works": "2"}})
	if err != nil || ret.Read != 2 || ret.PeopleSeen != 4 {
		t.Fatalf("budget 2: read=%d people=%d err=%v", ret.Read, ret.PeopleSeen, err)
	}
	if p := s.paths(); len(p) != 3 || p[0] != "/works cursor=* per-page=2 filter=title_and_abstract.search:field cycling MRI" || strings.Contains(p[1], "cursor=") {
		t.Errorf("budget 2 requests: %v", p)
	}
	// budget 3: page one gives two, page two asks for the one remaining and
	// gets W1003 (W1001 again is not a work read twice)
	s = newOpenAlexWorksServer(t)
	_, ret, err = s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 25, Fields: map[string]string{"works": "3"}})
	if err != nil || ret.Read != 3 {
		t.Fatalf("budget 3: read=%d err=%v", ret.Read, err)
	}
	if p := s.paths(); p[0] != "/works cursor=* per-page=3 filter=title_and_abstract.search:field cycling MRI" ||
		p[1] != "/works cursor=CURSOR-2 per-page=1 filter=title_and_abstract.search:field cycling MRI" || strings.Contains(p[2], "cursor=") {
		t.Errorf("budget 3 requests: %v", p)
	}

	// a registry that hands back the cursor just used: one page, no loop
	// (one works page, the group_by, the author batch)
	s = newOpenAlexWorksServer(t)
	s.pages = map[string]string{"*": strings.Replace(s.pages["*"], `"next_cursor": "CURSOR-2"`, `"next_cursor": "*"`, 1)}
	_, ret, err = s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 25})
	if err != nil || ret.Read != 2 || len(s.paths()) != 3 {
		t.Errorf("repeated cursor: read=%d requests=%v err=%v", ret.Read, s.paths(), err)
	}
	// no next cursor at all: one page
	s = newOpenAlexWorksServer(t)
	s.pages = map[string]string{"*": strings.Replace(s.pages["*"], `"next_cursor": "CURSOR-2"`, `"next_cursor": null`, 1)}
	_, ret, err = s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 25})
	if err != nil || ret.Read != 2 || len(s.paths()) != 3 {
		t.Errorf("no cursor: read=%d requests=%v err=%v", ret.Read, s.paths(), err)
	}
	// fresh cursors forever over the same two works: the page bound ends it
	s = newOpenAlexWorksServer(t)
	page := s.pages["*"]
	s.pages = map[string]string{}
	for i := 0; i < 100; i++ {
		cur, next := "*", "C1"
		if i > 0 {
			cur, next = "C"+strconv.Itoa(i), "C"+strconv.Itoa(i+1)
		}
		s.pages[cur] = strings.Replace(page, `"next_cursor": "CURSOR-2"`, `"next_cursor": "`+next+`"`, 1)
	}
	_, ret, err = s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 25, Fields: map[string]string{"works": "500"}})
	if err != nil || ret.Read != 2 {
		t.Fatalf("looping cursors: read=%d err=%v", ret.Read, err)
	}
	pages := 0
	for _, p := range s.paths() {
		if strings.Contains(p, "cursor=") {
			pages++
		}
	}
	if pages != openAlexMaxWorks/openAlexWorksPerPage+1 {
		t.Errorf("page bound: %d works requests", pages)
	}
	// a search that matches nothing: the field is zero, no group or batch call
	s = newOpenAlexWorksServer(t)
	s.pages = map[string]string{"*": `{"meta":{"count":0,"next_cursor":null},"results":[]}`}
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "nothing at all here", Max: 25})
	if err != nil || len(got) != 0 || ret.Available == nil || *ret.Available != 0 || ret.Read != 0 || ret.PeopleSeen != 0 || len(s.paths()) != 1 {
		t.Errorf("empty field: drafts=%d available=%v read=%d people=%d requests=%v err=%v", len(got), deref(ret.Available), ret.Read, ret.PeopleSeen, s.paths(), err)
	}
}

// The two branches do not regress into each other: a name still calls
// /authors?search and nothing else; a keyword never does; the mode field
// moves either query to the other branch on purpose.
func TestOpenAlexNameAndKeywordBranchesStayApart(t *testing.T) {
	s := newOpenAlexWorksServer(t)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "Dana Reyes", Max: 10})
	if err != nil || len(got) != 3 || ret.Unit != "authors" {
		t.Fatalf("name: drafts=%d unit=%q err=%v", len(got), ret.Unit, err)
	}
	if p := s.paths(); len(p) != 1 || p[0] != "/authors search=Dana Reyes per-page=10" {
		t.Fatalf("a name is one author search: %v", p)
	}

	s = newOpenAlexWorksServer(t)
	if _, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 10, Fields: map[string]string{"mode": "authors"}}); err != nil || ret.Unit != "authors" {
		t.Fatalf("mode=authors: unit=%q err=%v", ret.Unit, err)
	}
	if p := s.paths(); len(p) != 1 || p[0] != "/authors search=field cycling MRI per-page=10" {
		t.Fatalf("mode=authors sends the keyword to the name search: %v", p)
	}

	s = newOpenAlexWorksServer(t)
	if _, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "Dana Reyes", Max: 10, Fields: map[string]string{"mode": "works"}}); err != nil || ret.Unit != "works" {
		t.Fatalf("mode=works: unit=%q err=%v", ret.Unit, err)
	}
	for _, p := range s.paths() {
		if strings.Contains(p, "search=") {
			t.Fatalf("mode=works never calls the name search: %v", s.paths())
		}
	}
	if s.paths()[0] != "/works cursor=* per-page=50 filter=title_and_abstract.search:Dana Reyes" {
		t.Fatalf("mode=works: %v", s.paths())
	}

	// a lookup by name from another source's draft is the author branch
	// whatever the name looks like — a lowercase name is still a name here
	s = newOpenAlexWorksServer(t)
	hits, err := s.adapter().LookupCandidate(context.Background(), CandidateDraft{SourceID: "nihreporter", Name: "dana reyes"}, Scope{Query: "dana reyes", Max: 5})
	if err != nil || len(hits) != 3 {
		t.Fatalf("lookup: hits=%d err=%v", len(hits), err)
	}
	if p := s.paths(); len(p) != 1 || p[0] != "/authors search=dana reyes per-page=5" {
		t.Fatalf("lookup is the name search: %v", p)
	}

	// full text and filters reach the request exactly
	s = newOpenAlexWorksServer(t)
	if _, _, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "field cycling MRI", Max: 10,
		Fields: map[string]string{"text": "fulltext", "years": "2018-2026", "type": "article"}}); err != nil {
		t.Fatal(err)
	}
	want := "/works search=field cycling MRI cursor=* per-page=50 filter=from_publication_date:2018-01-01,to_publication_date:2026-12-31,type:article"
	if p := s.paths(); p[0] != want || p[3] != "/works search=field cycling MRI group_by=authorships.author.id per-page=200 filter=from_publication_date:2018-01-01,to_publication_date:2026-12-31,type:article" {
		t.Fatalf("fulltext with filters: %v", p)
	}
}

// Rule 3 — no contact details (D15): the fixtures carry an email in a raw
// affiliation string and one in an author record. Neither reaches a draft.
func TestOpenAlexWorksNeverEmitContactDetails(t *testing.T) {
	s := newOpenAlexWorksServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "field cycling MRI", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Errorf("%s: contact fields set: %+v", d.Name, d.Contact)
		}
		hay := strings.Join(append([]string{d.Name, d.Org, d.Title, d.Note}, d.Links...), " ")
		for _, ev := range d.Evidence {
			hay += " " + ev.Snippet + " " + ev.URLOrFile
		}
		for _, e := range d.Edges {
			hay += " " + e.Basis
		}
		if strings.Contains(hay, "@") || strings.Contains(hay, "example.test") || strings.Contains(hay, "Aberdeen Biomedical Imaging Centre") {
			t.Errorf("%s: an address or a raw affiliation reached the draft: %q", d.Name, hay)
		}
	}
}

// Every request is load-bearing: a failed page, group_by or author batch
// fails the run rather than returning a smaller field as if it were the
// field. Shape failures name the endpoint.
func TestOpenAlexWorksErrorsAreClear(t *testing.T) {
	fastScholarlyRetries(t)
	run := func(t *testing.T, mutate func(s *openAlexWorksServer), want string) {
		t.Helper()
		s := newOpenAlexWorksServer(t)
		mutate(s)
		got, err := s.adapter().Search(context.Background(), Scope{Query: "field cycling MRI", Max: 25})
		if err == nil {
			t.Fatalf("no error; drafts=%d", len(got))
		}
		if !strings.HasPrefix(err.Error(), "openalex:") || !strings.Contains(err.Error(), want) {
			t.Errorf("err=%q want it to name %q", err, want)
		}
		if got != nil {
			t.Errorf("an error still returned drafts: %+v", got)
		}
	}
	run(t, func(s *openAlexWorksServer) { s.pages["CURSOR-2"] = `{"meta":{"count":1}}` }, "no results")
	run(t, func(s *openAlexWorksServer) { s.pages["*"] = `<html>maintenance</html>` }, "malformed")
	run(t, func(s *openAlexWorksServer) { s.group = `{"meta":{"count":1}}` }, "group_by")
	run(t, func(s *openAlexWorksServer) { s.group = `{"group_by": [` }, "malformed")
	run(t, func(s *openAlexWorksServer) { s.batch = `{"meta":{"count":0}}` }, "batch")
	run(t, func(s *openAlexWorksServer) { delete(s.pages, "CURSOR-2") }, "HTTP 400")
}
