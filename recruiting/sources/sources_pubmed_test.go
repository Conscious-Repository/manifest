package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// pubmedServer serves canned esearch and efetch responses from an httptest
// server and records every request it saw. Nothing here leaves the
// process: the adapter's BaseURL and Client both point at the test server.
// There is no esummary route on purpose: a request for one is a 404, so
// the summary-only path cannot quietly come back.
type pubmedServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []*http.Request

	searchStatus int
	searchBody   string
	fetchStatus  int
	fetchBody    string
}

func newPubMedServer(t *testing.T, searchStatus int, searchBody string, fetchStatus int, fetchBody string) *pubmedServer {
	t.Helper()
	s := &pubmedServer{searchStatus: searchStatus, searchBody: searchBody, fetchStatus: fetchStatus, fetchBody: fetchBody}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Clone(context.Background()))
		s.mu.Unlock()
		switch r.URL.Path {
		case "/entrez/eutils/esearch.fcgi":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(s.searchStatus)
			_, _ = w.Write([]byte(s.searchBody))
		case "/entrez/eutils/efetch.fcgi":
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			w.WriteHeader(s.fetchStatus)
			_, _ = w.Write([]byte(s.fetchBody))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// newPubMedFixtureServer answers both endpoints from the checked-in
// fixtures: 162 matching, 5 ids, and the 5 full records (12 people, one
// collective name).
func newPubMedFixtureServer(t *testing.T) *pubmedServer {
	t.Helper()
	return newPubMedServer(t, http.StatusOK, pubmedFixture(t, "pubmed-esearch.json"),
		http.StatusOK, pubmedFixture(t, "pubmed-efetch.xml"))
}

func (s *pubmedServer) adapter() PubMed {
	return PubMed{BaseURL: s.srv.URL, Client: *s.srv.Client()}
}

func (s *pubmedServer) requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.reqs...)
}

func (s *pubmedServer) requestPaths() []string {
	var out []string
	for _, r := range s.requests() {
		out = append(out, r.URL.Path)
	}
	return out
}

func pubmedFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const pubmedEmptySearch = `{"header":{"type":"esearch","version":"0.3"},"esearchresult":{"count":"0","retmax":"0","retstart":"0","idlist":[],"errorlist":{"phrasesnotfound":["nobody"]},"warninglist":{}}}`

// draftNamed finds one draft by name, failing the test when it is absent.
func draftNamed(t *testing.T, drafts []CandidateDraft, name string) CandidateDraft {
	t.Helper()
	for _, d := range drafts {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no draft named %q among %v", name, draftNames(drafts))
	return CandidateDraft{}
}

func draftNames(drafts []CandidateDraft) []string {
	out := make([]string, 0, len(drafts))
	for _, d := range drafts {
		out = append(out, d.Name)
	}
	return out
}

// PHASE 1 — the fixture is 5 papers carrying 12 person authors and one
// collective name. They fold to 8 people: one per safe key, last authors
// included, the paper the person omitted their ORCID on landing beside the
// papers they gave it, an initials-only byline kept apart from the full
// name at the same university, and no address anywhere.
func TestPubMedAggregatesFixtureIntoPeople(t *testing.T) {
	s := newPubMedFixtureServer(t)
	before := time.Now().Add(-time.Second)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "field-cycling MRI", Max: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Dana M Reyes", "Samuel Okafor", "Priya Natarajan", "Lurie DJ", "Reyes DM", "Lionel M Broche", "P James Ross", "David J Lurie"}
	if names := draftNames(got); strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("people (in order of first mention):\n got %v\nwant %v", names, want)
	}
	if ret.Available == nil || *ret.Available != 162 || ret.Read != 5 || ret.PeopleSeen != 8 || ret.Unit != "papers" {
		t.Errorf("retrieval: available=%v read=%d people=%d unit=%q; want 162 / 5 / 8 / papers", deref(ret.Available), ret.Read, ret.PeopleSeen, ret.Unit)
	}

	// the person with an ORCID: three papers, keyed by the ORCID, the paper
	// without it bridged in by exact name + organization
	reyes := got[0]
	if reyes.SourceID != "pubmed" || reyes.ExternalID != "orcid/0000-0001-2345-6789" || reyes.Org != "University of Aberdeen" ||
		reyes.Orcid != "https://orcid.org/0000-0001-2345-6789" || reyes.Role != "role/mri-engineer" || reyes.Note != "" ||
		reyes.Title != "" || reyes.Location != "" {
		t.Fatalf("Reyes: %+v", reyes)
	}
	if strings.Join(reyes.Links, " ") != "https://pubmed.ncbi.nlm.nih.gov/39000001/ https://pubmed.ncbi.nlm.nih.gov/39000002/ https://pubmed.ncbi.nlm.nih.gov/39000003/ https://orcid.org/0000-0001-2345-6789" {
		t.Errorf("Reyes links: %v", reyes.Links)
	}
	pubs, affs := evidenceOfKind(reyes, EvidencePublication), evidenceOfKind(reyes, EvidenceAffiliation)
	if len(pubs) != 3 || len(affs) != 3 {
		t.Fatalf("Reyes rows: %d publication, %d affiliation: %+v", len(pubs), len(affs), reyes.Evidence)
	}
	ev := pubs[0]
	if ev.SourceID != "pubmed" || ev.URLOrFile != "https://pubmed.ncbi.nlm.nih.gov/39000001/" || !ev.Cited() ||
		ev.RetrievedAt.IsZero() || ev.RetrievedAt.Before(before) || ev.Trust != TrustHigh {
		t.Errorf("first row: %+v", ev)
	}
	for _, part := range []string{"author: Dana M Reyes", "byline: Reyes DM", "position: first of 3",
		"title: Self-supervised reconstruction of diffusion MRI with learned priors.",
		"journal: Magnetic resonance in medicine", "pubdate: 2026 Mar 12", "pmid: 39000001",
		"doi: 10.1002/mrm.00001", "pmcid: PMC9900001",
		"mesh: Diffusion Magnetic Resonance Imaging*; Magnetic Resonance Imaging; Humans"} {
		if !strings.Contains(ev.Snippet, part) {
			t.Errorf("first row lacks %q: %q", part, ev.Snippet)
		}
	}
	// position evidence survives per paper: last author twice, once behind
	// a collective name that is recorded as such and is nobody's draft
	if sn := pubs[1].Snippet; !strings.Contains(sn, "position: last of 3") || !strings.Contains(sn, "pmid: 39000002") ||
		!strings.Contains(sn, "journal: Journal of magnetic resonance imaging : JMRI") || !strings.Contains(sn, "pubdate: 2025 Nov-Dec") || strings.Contains(sn, "doi:") {
		t.Errorf("second row: %q", sn)
	}
	if sn := pubs[2].Snippet; !strings.Contains(sn, "position: last of 2") || !strings.Contains(sn, "collective: Radiology Consortium") ||
		!strings.Contains(sn, "title: Multicenter evaluation of an accelerated pulse sequence at B0 = 0.2 T.") ||
		!strings.Contains(sn, "mesh: Magnetic Resonance Imaging*") {
		t.Errorf("third row: %q", sn)
	}
	if affs[0].Snippet != "affiliation on pmid 39000001 (first author): Department of Radiology, University of Aberdeen, Aberdeen, UK." ||
		affs[0].URLOrFile != "https://pubmed.ncbi.nlm.nih.gov/39000001/" || affs[0].Trust != TrustMedium {
		t.Errorf("affiliation row: %+v", affs[0])
	}
	for _, d := range got {
		if d.Name == "Radiology Consortium" || strings.Contains(d.Name, "Consortium") {
			t.Errorf("a collective name became a draft: %+v", d)
		}
	}

	// the person who gave their ORCID on one paper of three: one row, the
	// ORCID, all three papers (middle, first, sole), the organization from
	// the first mention
	okafor := got[1]
	if okafor.ExternalID != "orcid/0000-0003-4444-5555" || okafor.Orcid != "https://orcid.org/0000-0003-4444-5555" || okafor.Org != "University of Oxford" {
		t.Errorf("Okafor: %+v", okafor)
	}
	if pubs := evidenceOfKind(okafor, EvidencePublication); len(pubs) != 3 ||
		!strings.Contains(pubs[0].Snippet, "position: middle of 3") || !strings.Contains(pubs[1].Snippet, "position: first of 3") ||
		!strings.Contains(pubs[2].Snippet, "position: sole of 1") {
		t.Errorf("Okafor positions: %+v", pubs)
	}
	if affs := evidenceOfKind(okafor, EvidenceAffiliation); len(affs) != 4 ||
		!strings.HasSuffix(affs[1].Snippet, "University of Oxford, Oxford, UK.") ||
		!strings.HasSuffix(affs[3].Snippet, "Oxford University Hospitals NHS Foundation Trust, Oxford, UK.") {
		t.Errorf("Okafor affiliations: %+v", affs)
	}

	// last authors are people too
	if nat := got[2]; nat.ExternalID != "name/priya natarajan/university of aberdeen" || nat.Note != "" ||
		!strings.Contains(nat.Evidence[0].Snippet, "position: last of 3") {
		t.Errorf("Natarajan: %+v", nat)
	}
	if lurie := got[7]; lurie.ExternalID != "name/david j lurie/university of aberdeen" || lurie.Note != "" ||
		!strings.Contains(lurie.Evidence[0].Snippet, "position: last of 4") {
		t.Errorf("David J Lurie: %+v", lurie)
	}

	// every draft: cited, dated, no edge, the parts a card splits on
	for _, d := range got {
		if len(d.Edges) != 0 {
			t.Errorf("%s: no coauthor edge in this phase, got %+v", d.Name, d.Edges)
		}
		if len(d.Evidence) == 0 || d.ExternalID == "" {
			t.Errorf("%s: no evidence or no key: %+v", d.Name, d)
		}
		for _, ev := range d.Evidence {
			if !strings.HasPrefix(ev.URLOrFile, "https://pubmed.ncbi.nlm.nih.gov/") || ev.RetrievedAt.IsZero() {
				t.Errorf("%s: evidence uncited or undated: %+v", d.Name, ev)
			}
			if ev.Kind == EvidencePublication && (!strings.Contains(ev.Snippet, "title: ") || !strings.Contains(ev.Snippet, "pmid: ") ||
				!strings.Contains(ev.Snippet, "position: ") || !strings.Contains(ev.Snippet, "journal: ") || !strings.Contains(ev.Snippet, "pubdate: ")) {
				t.Errorf("%s: snippet lacks a required part: %q", d.Name, ev.Snippet)
			}
		}
	}
	// Search is SearchCounted without the numbers — same drafts
	plain, err := s.adapter().Search(context.Background(), Scope{Query: "field-cycling MRI", Max: 25})
	if err != nil || len(plain) != len(got) {
		t.Errorf("Search: %d drafts, err %v", len(plain), err)
	}
}

func evidenceOfKind(d CandidateDraft, kind string) []Evidence {
	var out []Evidence
	for _, ev := range d.Evidence {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// Initials are not a name. "Reyes DM" at the University of Aberdeen is a
// separate, ambiguous, source-local row from "Dana M Reyes" at the
// University of Aberdeen — and "Lurie DJ" from "David J Lurie" — however
// likely the owner finds the match. A full name with no affiliation is
// ambiguous too. The note says so.
func TestPubMedInitialsOnlyStayAmbiguousAndApart(t *testing.T) {
	s := newPubMedFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "field-cycling MRI", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	reyesDM := draftNamed(t, got, "Reyes DM")
	if reyesDM.ExternalID != "byline/reyes dm/university of aberdeen" || reyesDM.Orcid != "" || reyesDM.Org != "University of Aberdeen" ||
		!strings.HasPrefix(reyesDM.Note, "identity: ambiguous") || !strings.Contains(reyesDM.Note, "initials only (Reyes DM)") {
		t.Errorf("Reyes DM: %+v", reyesDM)
	}
	if len(reyesDM.Evidence) != 2 || !strings.Contains(reyesDM.Evidence[0].Snippet, "author: Reyes DM · position: first of 4") ||
		strings.Contains(reyesDM.Evidence[0].Snippet, "byline:") {
		t.Errorf("Reyes DM rows: %+v", reyesDM.Evidence)
	}
	if dana := draftNamed(t, got, "Dana M Reyes"); len(evidenceOfKind(dana, EvidencePublication)) != 3 {
		t.Errorf("the initials-only paper leaked onto the full name: %+v", dana.Evidence)
	}
	lurieDJ := draftNamed(t, got, "Lurie DJ")
	david := draftNamed(t, got, "David J Lurie")
	if lurieDJ.ExternalID != "byline/lurie dj/university of aberdeen" || david.ExternalID != "name/david j lurie/university of aberdeen" ||
		lurieDJ.Note == "" || david.Note != "" || len(lurieDJ.Evidence) != 2 || len(david.Evidence) != 2 {
		t.Errorf("Lurie DJ %+v\nDavid J Lurie %+v", lurieDJ, david)
	}
	ross := draftNamed(t, got, "P James Ross")
	if ross.ExternalID != "name/p james ross/" || ross.Org != "" || !strings.Contains(ross.Note, "no affiliation") || len(ross.Evidence) != 1 {
		t.Errorf("Ross: %+v", ross)
	}
}

// The request itself: esearch with the PAPER budget as retmax, then efetch
// in XML for exactly those ids — GET, the polite User-Agent, no key — and
// never an esummary.
func TestPubMedRequestShape(t *testing.T) {
	s := newPubMedFixtureServer(t)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "diffusion MRI[Title]", Max: 10}); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests=%d want 2 (esearch + efetch): %v", len(reqs), s.requestPaths())
	}
	q := reqs[0].URL.Query()
	if reqs[0].URL.Path != "/entrez/eutils/esearch.fcgi" || q.Get("db") != "pubmed" ||
		q.Get("term") != "diffusion MRI[Title]" || q.Get("retmax") != "100" || q.Get("retmode") != "json" {
		t.Errorf("esearch: %s %v", reqs[0].URL.Path, q)
	}
	q = reqs[1].URL.Query()
	if reqs[1].URL.Path != "/entrez/eutils/efetch.fcgi" || q.Get("db") != "pubmed" ||
		q.Get("id") != "39000001,39000002,39000003,39000004,39000005" || q.Get("retmode") != "xml" || q.Get("rettype") != "" {
		t.Errorf("efetch: %s %v", reqs[1].URL.Path, q)
	}
	if a := reqs[1].Header.Get("Accept"); !strings.Contains(a, "xml") {
		t.Errorf("efetch Accept: %q", a)
	}
	for _, r := range reqs {
		if r.Method != http.MethodGet {
			t.Errorf("%s %s: not a GET", r.Method, r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); ua != PubMedUserAgent || !strings.HasPrefix(ua, "manifest-aion-recruiting/") {
			t.Errorf("%s User-Agent: %q", r.URL.Path, ua)
		}
		if r.Header.Get("Authorization") != "" || r.URL.Query().Get("api_key") != "" {
			t.Errorf("%s sent credentials", r.URL.Path)
		}
	}
}

// Two numbers, kept apart: the PAPER budget is what is asked for and read;
// Scope.Max is how many PEOPLE are shown. Cutting one does not move the
// other, and the counts say what the cut hid.
func TestPubMedPaperBudgetAndDisplayCapAreSeparate(t *testing.T) {
	// a display cap of 3 over the full budget: 5 read, 8 seen, 3 shown
	s := newPubMedFixtureServer(t)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "mri", Max: 3})
	if err != nil {
		t.Fatal(err)
	}
	if names := draftNames(got); strings.Join(names, "|") != "Dana M Reyes|Samuel Okafor|Priya Natarajan" {
		t.Errorf("capped drafts: %v", names)
	}
	if ret.Read != 5 || ret.PeopleSeen != 8 || ret.Available == nil || *ret.Available != 162 {
		t.Errorf("cap hid the field: %+v", ret)
	}
	if rm := s.requests()[0].URL.Query().Get("retmax"); rm != "100" {
		t.Errorf("the people cap became the page size: retmax=%q", rm)
	}

	// a paper budget of 2 over a generous cap: retmax 2, two ids fetched,
	// two read (the server sent all five), the people on those two
	s = newPubMedFixtureServer(t)
	got, ret, err = s.adapter().SearchCounted(context.Background(), Scope{Query: "mri", Max: 25, Fields: map[string]string{"papers": "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if names := draftNames(got); strings.Join(names, "|") != "Dana M Reyes|Samuel Okafor|Priya Natarajan|Lurie DJ" {
		t.Errorf("budget-2 drafts: %v", names)
	}
	if ret.Read != 2 || ret.PeopleSeen != 4 {
		t.Errorf("budget 2: %+v", ret)
	}
	reqs := s.requests()
	if rm := reqs[0].URL.Query().Get("retmax"); rm != "2" {
		t.Errorf("retmax=%q want 2", rm)
	}
	if id := reqs[1].URL.Query().Get("id"); id != "39000001,39000002" {
		t.Errorf("efetch id=%q want the first two PMIDs", id)
	}
	// Reyes on budget 2 has both papers but ORCID only from the first
	if reyes := got[0]; reyes.ExternalID != "orcid/0000-0001-2345-6789" || len(evidenceOfKind(reyes, EvidencePublication)) != 2 {
		t.Errorf("Reyes on budget 2: %+v", reyes)
	}

	// no budget → the default; an absurd budget → the ceiling; a bad one
	// is refused before any request; the cap never grows the budget
	for papers, want := range map[string]string{"": "100", "0": "100", "-3": "100", "100000": "500", "7": "7"} {
		s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusOK, ``)
		if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 100, Fields: map[string]string{"papers": papers}}); err != nil {
			t.Fatal(err)
		}
		if rm := s.requests()[0].URL.Query().Get("retmax"); rm != want {
			t.Errorf("papers %q → retmax=%q want %s", papers, rm, want)
		}
	}
	s = newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusOK, ``)
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Fields: map[string]string{"papers": "lots"}}); err == nil ||
		!strings.Contains(err.Error(), "papers") {
		t.Errorf("a non-numeric budget: err=%v", err)
	}
	if n := len(s.requests()); n != 0 {
		t.Errorf("a refused budget still made %d request(s)", n)
	}
	// the people cap itself still has its own default and ceiling
	for max, want := range map[int]int{0: 25, -1: 25, 100000: 100, 4: 4} {
		search, fetch := pubmedField(120, "120")
		s := newPubMedServer(t, http.StatusOK, search, http.StatusOK, fetch)
		got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "mri", Max: max, Fields: map[string]string{"papers": "120"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want || ret.PeopleSeen != 119 || ret.Read != 120 {
			t.Errorf("max %d: %d drafts, %+v; want %d shown of 119 seen, 120 read", max, len(got), ret, want)
		}
	}
}

// PrepareScope writes the EFFECTIVE budget back into the scope, so a run
// that named none still records "papers: 100" instead of an absence that
// silently meant the default — and refuses what Search would refuse.
func TestPubMedPrepareScopeRecordsBudget(t *testing.T) {
	p := PubMed{}
	got, err := p.PrepareScope(Scope{Query: "mri", Max: 8})
	if err != nil || got.Fields["papers"] != "100" || got.Query != "mri" || got.Max != 8 {
		t.Errorf("no budget: %+v err=%v", got, err)
	}
	got, err = p.PrepareScope(Scope{Query: "mri", Fields: map[string]string{"papers": "9999", "other": "kept"}})
	if err != nil || got.Fields["papers"] != "500" || got.Fields["other"] != "kept" {
		t.Errorf("ceiling: %+v err=%v", got, err)
	}
	if _, err := p.PrepareScope(Scope{Query: "mri", Fields: map[string]string{"papers": "many"}}); err == nil || !strings.Contains(err.Error(), "papers") {
		t.Errorf("bad budget: err=%v", err)
	}
	if _, err := p.PrepareScope(Scope{Fields: map[string]string{"papers": "10"}}); err == nil || !strings.Contains(err.Error(), "query") {
		t.Errorf("no query: err=%v", err)
	}
	if fields := p.Scope(); len(fields) != 4 || fields[3].Key != "papers" || fields[3].Required || !strings.Contains(fields[3].Placeholder, "500") {
		t.Errorf("scope fields: %+v", fields)
	}
}

// A budget above the batch size is fetched in batches — 120 papers is
// three efetches of at most 50 ids, in search order — and read as one
// field.
func TestPubMedFetchesInBatches(t *testing.T) {
	search, fetch := pubmedField(120, "300")
	s := newPubMedServer(t, http.StatusOK, search, http.StatusOK, fetch)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "mri", Max: 100, Fields: map[string]string{"papers": "120"}})
	if err != nil {
		t.Fatal(err)
	}
	paths := s.requestPaths()
	if len(paths) != 4 || paths[0] != "/entrez/eutils/esearch.fcgi" || paths[1] != "/entrez/eutils/efetch.fcgi" || paths[3] != "/entrez/eutils/efetch.fcgi" {
		t.Fatalf("requests: %v", paths)
	}
	sizes := []int{}
	for _, r := range s.requests()[1:] {
		sizes = append(sizes, len(strings.Split(r.URL.Query().Get("id"), ",")))
	}
	if fmt.Sprint(sizes) != "[50 50 20]" {
		t.Errorf("batch sizes: %v", sizes)
	}
	if first := strings.Split(s.requests()[1].URL.Query().Get("id"), ",")[0]; first != "39000001" {
		t.Errorf("first batch starts at %s, not the top of the search", first)
	}
	if len(got) != 100 || ret.Read != 120 || ret.PeopleSeen != 119 || *ret.Available != 300 {
		t.Errorf("drafts=%d retrieval=%+v; want 100 shown, 120 read, 119 seen, 300 available", len(got), ret)
	}
	if got[0].Name != "Person 1 Author" || len(evidenceOfKind(got[0], EvidencePublication)) != 2 {
		t.Errorf("the repeated person did not aggregate: %+v", got[0])
	}
}

// An empty query is refused before any request is made.
func TestPubMedRefusesEmptyQuery(t *testing.T) {
	s := newPubMedFixtureServer(t)
	for _, q := range []string{"", "   ", "\t\n"} {
		if _, err := s.adapter().Search(context.Background(), Scope{Query: q, Max: 5}); err == nil ||
			!strings.Contains(err.Error(), "query") {
			t.Errorf("query %q: err=%v", q, err)
		}
	}
	if n := len(s.requests()); n != 0 {
		t.Errorf("an empty query still made %d request(s)", n)
	}
}

// A search that finds nothing is an empty slice, not an error — and makes
// no fetch request.
func TestPubMedNoResultsIsEmpty(t *testing.T) {
	s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusInternalServerError, `boom`)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "nobody", Max: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("drafts=%#v want an empty, non-nil slice", got)
	}
	if paths := s.requestPaths(); len(paths) != 1 || paths[0] != "/entrez/eutils/esearch.fcgi" {
		t.Errorf("an empty search still fetched records: %v", paths)
	}
}

// Server and shape failures on either call each produce an error that says
// what happened, and never a partial draft list.
func TestPubMedErrorsAreClear(t *testing.T) {
	fastScholarlyRetries(t)
	okSearch := pubmedFixture(t, "pubmed-esearch.json")
	okFetch := pubmedFixture(t, "pubmed-efetch.xml")
	cases := map[string]struct {
		searchStatus, fetchStatus int
		searchBody, fetchBody     string
		want                      string
		requests                  int
	}{
		"search http 500":     {500, 200, `<html>Internal Server Error</html>`, okFetch, "HTTP 500", 1},
		"search http 429":     {429, 200, `{"error":"API rate limit exceeded"}`, okFetch, "HTTP 429", scholarlyAttempts},
		"search malformed":    {200, 200, `{"esearchresult":{"idlist":["1",`, okFetch, "malformed", 1},
		"search not json":     {200, 200, `<html>maintenance</html>`, okFetch, "malformed", 1},
		"search no result":    {200, 200, `{"header":{"type":"esearch"}}`, okFetch, "no esearchresult", 1},
		"search error field":  {200, 200, `{"esearchresult":{"ERROR":"Unable to obtain query #1"}}`, okFetch, "Unable to obtain", 1},
		"search top error":    {200, 200, `{"error":"error forwarding request"}`, okFetch, "error forwarding", 1},
		"search no idlist":    {200, 200, `{"esearchresult":{"count":"3"}}`, okFetch, "no idlist", 1},
		"search null idlist":  {200, 200, `{"esearchresult":{"count":"3","idlist":null}}`, okFetch, "no idlist", 1},
		"fetch http 500":      {200, 500, okSearch, `<html>upstream exploded</html>`, "HTTP 500", 2},
		"fetch http 503":      {200, 503, okSearch, `busy`, "HTTP 503", 1 + scholarlyAttempts},
		"fetch malformed":     {200, 200, okSearch, `<PubmedArticleSet><PubmedArticle><MedlineCitation><PMID>1</PMID>`, "malformed", 2},
		"fetch not xml":       {200, 200, okSearch, `{"error":"Invalid uid"}`, "malformed", 2},
		"fetch html":          {200, 200, okSearch, `<html><body>maintenance</body></html>`, "not a PubmedArticleSet", 2},
		"fetch ncbi error":    {200, 200, okSearch, `<?xml version="1.0"?><eFetchResult><ERROR>Empty id list - nothing todo</ERROR></eFetchResult>`, "Empty id list", 2},
		"fetch wrong db":      {200, 200, okSearch, `<?xml version="1.0"?><GBSet></GBSet>`, "not a PubmedArticleSet", 2},
		"fetch empty body":    {200, 200, okSearch, ``, "malformed", 2},
		"fetch empty article": {200, 200, okSearch, `<PubmedArticleSet/>`, "", 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newPubMedServer(t, c.searchStatus, c.searchBody, c.fetchStatus, c.fetchBody)
			got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
			if c.want == "" {
				// an empty set is an honest zero: read nothing, no error
				if err != nil || len(got) != 0 {
					t.Fatalf("drafts=%+v err=%v", got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error; drafts=%+v", got)
			}
			if !strings.HasPrefix(err.Error(), "pubmed:") || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err=%q want it to name %q", err, c.want)
			}
			if got != nil {
				t.Errorf("an error still returned drafts: %+v", got)
			}
			if n := len(s.requests()); n != c.requests {
				t.Errorf("requests=%d want %d: %v", n, c.requests, s.requestPaths())
			}
		})
	}

	// a record with no PMID is not read; one with no title, or no person
	// on it, is read and yields nobody; a PMID the fetch omits is not read
	s := newPubMedServer(t, http.StatusOK, okSearch, http.StatusOK, `<PubmedArticleSet>
		<PubmedArticle><MedlineCitation><Article><ArticleTitle>No id.</ArticleTitle><AuthorList><Author><LastName>Ghost</LastName><ForeName>Anna</ForeName></Author></AuthorList></Article></MedlineCitation></PubmedArticle>
		<PubmedArticle><MedlineCitation><PMID>39000002</PMID><Article><ArticleTitle></ArticleTitle><AuthorList><Author><LastName>Okafor</LastName><ForeName>Samuel</ForeName></Author></AuthorList></Article></MedlineCitation></PubmedArticle>
		<PubmedArticle><MedlineCitation><PMID>39000003</PMID><Article><ArticleTitle>Editorial.</ArticleTitle><AuthorList><Author><CollectiveName>Editors</CollectiveName></Author></AuthorList></Article></MedlineCitation></PubmedArticle>
		<PubmedArticle><MedlineCitation><PMID>39000004</PMID><Article><Journal><Title>Radiology</Title><JournalIssue><PubDate><Year>2026</Year></PubDate></JournalIssue></Journal><ArticleTitle>A paper.</ArticleTitle><AuthorList><Author><LastName>Natarajan</LastName><ForeName>Priya</ForeName><Initials>P</Initials></Author></AuthorList></Article></MedlineCitation></PubmedArticle>
		</PubmedArticleSet>`)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "mri", Max: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Priya Natarajan" || got[0].ExternalID != "name/priya natarajan/" {
		t.Errorf("drafts=%+v want only the one usable paper", got)
	}
	if ret.Read != 3 || ret.PeopleSeen != 1 {
		t.Errorf("retrieval: %+v; want 3 read (no-title and collective-only count, no-PMID and missing do not), 1 seen", ret)
	}
}

// A transport failure — the network is down, the host is wrong — is an
// error too, and the default base URL is never reached from a test.
func TestPubMedTransportFailureIsAnError(t *testing.T) {
	s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusOK, ``)
	s.srv.Close()
	if _, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5}); err == nil ||
		!strings.HasPrefix(err.Error(), "pubmed:") {
		t.Errorf("closed server: err=%v", err)
	}
	if got := (PubMed{}).Scope(); len(got) != 4 || got[0].Key != "role" || got[1].Key != "query" || !got[1].Required || got[2].Key != "max" || got[3].Key != "papers" {
		t.Errorf("scope fields must be role/query/max/papers with query required: %+v", got)
	}
	if (PubMed{}).ID() != "pubmed" || (PubMed{}).Kind() != KindScholarly {
		t.Error("id/kind")
	}
}

// Rule 3 — no contact details (D15). The fixture prints addresses the way
// live records do — "Electronic address: …" and "E-mail: …" inside
// affiliations, a bare address after the country, one in an abstract — and
// none may reach a draft anywhere: not Contact, not Org, not a link, not a
// note, not a snippet. The affiliation itself survives, verbatim minus the
// address.
func TestPubMedNeverEmitsContactDetails(t *testing.T) {
	s := newPubMedFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("drafts=%d", len(got))
	}
	affiliations := 0
	for _, d := range got {
		if len(d.Contact) != 0 {
			t.Errorf("%s: contact fields set: %+v", d.Name, d.Contact)
		}
		texts := append([]string{d.Name, d.ExternalID, d.Note, d.Title, d.Org, d.Location, d.Orcid}, d.Links...)
		for _, ev := range d.Evidence {
			texts = append(texts, ev.Snippet, ev.URLOrFile)
			if ev.Kind == EvidenceContactPublished {
				t.Errorf("%s: a contact evidence row was emitted: %+v", d.Name, ev)
			}
			if ev.Kind == EvidenceAffiliation {
				affiliations++
			}
		}
		for _, s := range texts {
			if containsAddress(s) || strings.Contains(s, "mailto:") || strings.Contains(s, "example.test") ||
				strings.Contains(strings.ToLower(s), "electronic address") || strings.Contains(strings.ToLower(s), "e-mail") {
				t.Errorf("%s: an address reached the draft: %q", d.Name, s)
			}
		}
	}
	if affiliations == 0 {
		t.Error("stripping the address stripped the affiliations too")
	}
	for name, want := range map[string]string{
		"Dana M Reyes":  "affiliation on pmid 39000001 (first author): Department of Radiology, University of Aberdeen, Aberdeen, UK.",
		"Samuel Okafor": "affiliation on pmid 39000002 (first author): University of Oxford, Oxford, UK.",
		"David J Lurie": "affiliation on pmid 39000004 (last author): University of Aberdeen, Aberdeen, UK.",
	} {
		found := false
		for _, ev := range evidenceOfKind(draftNamed(t, got, name), EvidenceAffiliation) {
			found = found || ev.Snippet == want
		}
		if !found {
			t.Errorf("%s: no affiliation row %q among %+v", name, want, evidenceOfKind(draftNamed(t, got, name), EvidenceAffiliation))
		}
	}
	// an author entry whose name is itself an address is not an author,
	// and an affiliation that is only an address is no affiliation
	s = newPubMedServer(t, http.StatusOK, `{"esearchresult":{"idlist":["1"]}}`, http.StatusOK,
		`<PubmedArticleSet><PubmedArticle><MedlineCitation><PMID>1</PMID><Article><ArticleTitle>T.</ArticleTitle><AuthorList>
		<Author><LastName>dana@example.test</LastName><ForeName>Dana</ForeName></Author>
		<Author><LastName>Reyes</LastName><ForeName>Dana M</ForeName><Initials>DM</Initials><AffiliationInfo><Affiliation>dana@example.test</Affiliation></AffiliationInfo></Author>
		</AuthorList></Article></MedlineCitation></PubmedArticle></PubmedArticleSet>`)
	got, err = s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 5})
	if err != nil || len(got) != 1 || got[0].Name != "Dana M Reyes" || got[0].Org != "" || len(got[0].Evidence) != 1 {
		t.Errorf("drafts=%+v err=%v", got, err)
	}
}

// RETRIEVAL — the denominators (sourcing-effectiveness plan Phase 0). A run
// capped at 8 that says "8 fetched" has said nothing about the field; these
// tests pin that the adapter reports what PubMed said matched, what it
// actually read, and how many people it saw — and that a count the source
// did not give is nil, never zero.

// pubmedField builds an esearch/efetch pair for n one-author papers whose
// authors are all distinct except that the last paper repeats the first's —
// so n papers read fold into n-1 people, and the two numbers are told
// apart. Everyone is at one university, so the name is what differs.
func pubmedField(n int, count string) (search, fetch string) {
	ids := make([]string, 0, n)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><PubmedArticleSet>`)
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("3900%04d", i)
		ids = append(ids, id)
		fore := fmt.Sprintf("Person %d", i)
		if i == n {
			fore = "Person 1"
		}
		fmt.Fprintf(&b, `<PubmedArticle><MedlineCitation><PMID>%s</PMID><Article><Journal><Title>J Test</Title><JournalIssue><PubDate><Year>2025</Year><Month>Jan</Month></PubDate></JournalIssue></Journal><ArticleTitle>Paper %d</ArticleTitle><AuthorList><Author><LastName>Author</LastName><ForeName>%s</ForeName><AffiliationInfo><Affiliation>Test University, Testville.</Affiliation></AffiliationInfo></Author></AuthorList></Article></MedlineCitation></PubmedArticle>`, id, i, fore)
	}
	b.WriteString(`</PubmedArticleSet>`)
	s, _ := json.Marshal(map[string]any{"esearchresult": map[string]any{"count": count, "retmax": fmt.Sprint(n), "retstart": "0", "idlist": ids}})
	return string(s), b.String()
}

func TestPubMedRetrievalTellsDraftsFromAvailablePapers(t *testing.T) {
	search, fetch := pubmedField(32, "32")
	s := newPubMedServer(t, http.StatusOK, search, http.StatusOK, fetch)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "field-cycling MRI", Max: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 31 {
		t.Fatalf("31 people → %d drafts", len(got))
	}
	if ret.Available == nil || *ret.Available != 32 || ret.Read != 32 || ret.PeopleSeen != 31 || ret.Unit != "papers" {
		t.Errorf("retrieval: available=%v read=%d people=%d unit=%q; want 32 / 32 / 31 / papers", deref(ret.Available), ret.Read, ret.PeopleSeen, ret.Unit)
	}

	// the count is the FIELD, not the page: 162 matching, 32 read — the
	// number that was invisible behind "8 fetched"
	search, fetch = pubmedField(32, "162")
	s = newPubMedServer(t, http.StatusOK, search, http.StatusOK, fetch)
	got, ret, err = s.adapter().SearchCounted(context.Background(), Scope{Query: "field-cycling MRI", Max: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 31 || ret.Available == nil || *ret.Available != 162 || ret.Read != 32 {
		t.Errorf("162 matching: drafts=%d available=%v read=%d", len(got), deref(ret.Available), ret.Read)
	}

	// Search is SearchCounted without the numbers — same drafts
	plain, err := s.adapter().Search(context.Background(), Scope{Query: "field-cycling MRI", Max: 100})
	if err != nil || len(plain) != len(got) {
		t.Errorf("Search: %d drafts, err %v", len(plain), err)
	}
}

func TestPubMedRetrievalHonestZeroAndUnknown(t *testing.T) {
	// the source said zero → zero, known
	s := newPubMedServer(t, http.StatusOK, pubmedEmptySearch, http.StatusOK, ``)
	got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "nobody", Max: 5})
	if err != nil || len(got) != 0 {
		t.Fatalf("empty: %d drafts, err %v", len(got), err)
	}
	if ret.Available == nil || *ret.Available != 0 || ret.Read != 0 || ret.PeopleSeen != 0 {
		t.Errorf("a source that said zero: %+v", ret)
	}

	// the source did not say (no count, or not a number) → nil, never zero
	for _, count := range []string{"", "lots"} {
		search, fetch := pubmedField(3, count)
		if count == "" {
			search = strings.Replace(search, `"count":"",`, "", 1)
		}
		s := newPubMedServer(t, http.StatusOK, search, http.StatusOK, fetch)
		got, ret, err := s.adapter().SearchCounted(context.Background(), Scope{Query: "mri", Max: 5})
		if err != nil || len(got) != 2 {
			t.Fatalf("count %q: %d drafts, err %v", count, len(got), err)
		}
		if ret.Available != nil || ret.Read != 3 || ret.PeopleSeen != 2 {
			t.Errorf("count %q: available=%v read=%d people=%d; want nil / 3 / 2", count, deref(ret.Available), ret.Read, ret.PeopleSeen)
		}
	}
}

// deref prints a known count or nil, for messages.
func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// Enrich is a no-op and makes no request: the bounded record fetch already
// happened inside Search.
func TestPubMedEnrichChangesNothing(t *testing.T) {
	s := newPubMedFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("drafts=%+v err=%v", got, err)
	}
	before := len(s.requests())
	enriched, err := s.adapter().Enrich(context.Background(), got[0])
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Name != got[0].Name || len(enriched.Evidence) != len(got[0].Evidence) || len(enriched.Links) != len(got[0].Links) {
		t.Errorf("Enrich changed the draft:\n%+v\n%+v", got[0], enriched)
	}
	if edges, err := s.adapter().GraphEdges(context.Background(), got[0]); err != nil || len(edges) != 0 {
		t.Errorf("edges=%+v err=%v", edges, err)
	}
	if n := len(s.requests()); n != before {
		t.Errorf("Enrich/GraphEdges made a request: %d → %d", before, n)
	}
}

// COMPATIBILITY — the first-author projection. Before Phase 1 the adapter
// emitted one draft per paper for its first person author, as the byline.
// That is no longer what a run produces, but every such byline is still
// present, on the paper it headed, at position first: the old answer is a
// strict subset of the new one, read from the same fixture.
func TestPubMedFirstAuthorProjectionIsASubset(t *testing.T) {
	s := newPubMedFixtureServer(t)
	got, err := s.adapter().Search(context.Background(), Scope{Query: "mri", Max: 25})
	if err != nil {
		t.Fatal(err)
	}
	// first person author per paper, as the old adapter would have keyed
	// it, with the position the byline now records. The old adapter skipped
	// the collective name heading 39000003 and called Reyes its first
	// author; the byline says "Radiology Consortium, Reyes DM", so Reyes is
	// last of 2 there and the collective is recorded — the honest reading.
	oldMode := map[string][2]string{"39000001": {"Reyes DM", "first"}, "39000002": {"Okafor S", "first"}, "39000003": {"Reyes DM", "last"}, "39000004": {"Reyes DM", "first"}, "39000005": {"Okafor S", "sole"}}
	for pmid, want := range oldMode {
		found := false
		for _, d := range got {
			for _, pr := range pubmedPrintedOn(d, PubMedArticleURL+pmid+"/") {
				if pr.Byline == want[0] || pr.Name == want[0] {
					found = found || pr.Position == want[1]
				}
			}
		}
		if !found {
			t.Errorf("pmid %s: the old first-author draft %q is not present at position %s", pmid, want[0], want[1])
		}
	}
	// and the printed forms read back exactly, per paper
	reyes := draftNamed(t, got, "Dana M Reyes")
	pr := pubmedPrintedOn(reyes, PubMedArticleURL+"39000002/")
	if len(pr) != 1 || pr[0].Name != "Dana M Reyes" || pr[0].Byline != "Reyes DM" || pr[0].Position != "last" {
		t.Errorf("printed on 39000002: %+v", pr)
	}
	if all := pubmedPrintedOn(reyes, ""); len(all) != 3 {
		t.Errorf("printed on every paper: %+v", all)
	}
}
