package recruiting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"manifest/graph"
	"manifest/recruiting/sources"
)

// Social ties (ties.go): what a cited paper lets the general graph say
// about who wrote it with whom — and, just as much, what it must NOT say.

const (
	orcidAvery    = "0000-0001-2345-6789"
	orcidKim      = "0000-0002-0000-0002"
	orcidStranger = "0000-0009-9999-9999"
	paperDOI      = "https://doi.org/10.1000/xyz.2024"
	paperCite     = "Low-field coil arrays, Nature, 2024, 10.1000/xyz.2024"
)

// coauthorDraft is one author on the shared paper: their ORCID as a link,
// the paper as publication evidence, and one coauthor claim per far key —
// what an OpenAlex work sweep emits.
func paperAuthorDraft(name, orcid string, far ...string) sources.CandidateDraft {
	d := sources.CandidateDraft{
		SourceID: "openalex", ExternalID: "A-" + orcid, Name: name, Org: "Example Lab",
		Links: []string{"https://orcid.org/" + orcid}, Orcid: "https://orcid.org/" + orcid,
		Topics: []string{"Low-Field MRI"},
		Evidence: []sources.Evidence{{
			SourceID: "openalex", URLOrFile: paperDOI, RetrievedAt: testNow,
			Snippet: paperCite + " · first author · Example Lab · 3 authors", Kind: sources.EvidencePublication, Trust: sources.TrustMedium,
		}},
	}
	for _, f := range far {
		d.Edges = append(d.Edges, sources.EdgeClaim{
			From: "ext/orcid/" + f, Type: sources.EdgeCoauthor, SourceID: "openalex",
			Basis: "both authors on " + paperCite, Confidence: 0.55, Evidence: paperDOI,
		})
	}
	return d
}

func edgeKinds(edges []graph.Edge) map[string]int {
	out := map[string]int{}
	for _, e := range edges {
		out[e.Kind]++
	}
	return out
}

// Two people on one cited paper, both accepted: the second accept mirrors
// the coauthorship into the general graph, canonically ordered, with the
// paper's basis and URL, plus the paper itself and each person's authorship.
// The first accept — when the other author was still a stranger — writes no
// person tie and says which endpoint it refused.
func TestDeriveTiesCoauthorFromCitedPaperNeedsBothKnown(t *testing.T) {
	s, _ := testStore(t)
	avery := paperAuthorDraft("Avery Quill", orcidAvery, orcidKim)
	kim := paperAuthorDraft("Kim Collab", orcidKim, orcidAvery, orcidStranger)

	a, err := s.AcceptDraft(avery, testNow)
	if err != nil {
		t.Fatal(err)
	}
	first := DeriveTies(avery, a.ID, s.LoadEdges().Edges(), s.PersonResolver(), testNow)
	if len(first.Papers) != 1 || first.Papers[0].ID != "doi/10.1000/xyz.2024" || first.Papers[0].Kind != graph.KindPaper || !strings.HasPrefix(first.Papers[0].Title, "Low-field coil arrays") {
		t.Fatalf("the paper is registered by its DOI: %+v", first.Papers)
	}
	if kinds := edgeKinds(first.Edges); kinds[graph.EdgeAuthored] != 1 || kinds[graph.EdgeCoauthor] != 0 {
		t.Fatalf("with kim a stranger, only the authorship is claimed: %+v", first.Edges)
	}
	if len(first.Skipped) != 1 || first.Skipped[0].Endpoint != "ext/orcid/"+orcidKim || first.Skipped[0].Kind != graph.EdgeCoauthor {
		t.Fatalf("the refused tie is named: %+v", first.Skipped)
	}

	k, err := s.AcceptDraft(kim, testNow)
	if err != nil {
		t.Fatal(err)
	}
	second := DeriveTies(kim, k.ID, s.LoadEdges().Edges(), s.PersonResolver(), testNow)
	var tie *graph.Edge
	for i := range second.Edges {
		if second.Edges[i].Kind == graph.EdgeCoauthor {
			tie = &second.Edges[i]
		}
	}
	if tie == nil {
		t.Fatalf("both authors known → a coauthor tie: %+v", second.Edges)
	}
	if tie.From != PersonRef(a.ID) || tie.To != PersonRef(k.ID) {
		t.Fatalf("a symmetric tie is stored smaller-id first: %s → %s", tie.From, tie.To)
	}
	if tie.Inferred || tie.Confidence != "0.55" || tie.Source != "openalex" || tie.Evidence != paperDOI || !strings.Contains(tie.Basis, paperCite) || tie.Observed != "2026-09-02" {
		t.Fatalf("the tie keeps the row's provenance: %+v", tie)
	}
	if err := graph.Validate(*tie, graph.Default()); err != nil {
		t.Fatalf("the platform validator refuses it: %v", err)
	}
	// the stranger on the same paper is refused, by name, and stays an
	// external key in network/edges.md
	if len(second.Skipped) != 1 || second.Skipped[0].Endpoint != "ext/orcid/"+orcidStranger {
		t.Fatalf("never-guess: %+v", second.Skipped)
	}
	strangerOnFile := false
	for _, e := range s.LoadEdges().Edges() {
		if e.From == "ext/orcid/"+orcidStranger && e.To == k.ID && e.Evidence == paperDOI {
			strangerOnFile = true
		}
	}
	if !strangerOnFile {
		t.Fatal("the external-key claim still lands in network/edges.md with its evidence")
	}
	// the authorship edge cites the work
	for _, e := range second.Edges {
		if e.Kind == graph.EdgeAuthored && (e.To.ID != "doi/10.1000/xyz.2024" || e.Evidence != paperDOI || e.Confidence != TieAuthoredConfidence || e.Inferred) {
			t.Fatalf("authored edge: %+v", e)
		}
	}
}

// A name is not an endpoint. A claim whose far end is a display name, an
// unknown external key, or nothing at all yields no tie; and the resolver
// itself never answers to a name.
func TestDeriveTiesNeverGuessesFromANameAlone(t *testing.T) {
	s, _ := testStore(t)
	d := paperAuthorDraft("Avery Quill", orcidAvery)
	d.Edges = []sources.EdgeClaim{
		{From: "Kim Collab", Type: sources.EdgeCoauthor, SourceID: "pubmed", Basis: "listed together on a paper", Confidence: 0.55},
		{From: "ext/orcid/" + orcidStranger, Type: sources.EdgeSameLab, SourceID: "openalex", Basis: "same institution", Confidence: 0.45, Inferred: true},
	}
	c, err := s.AcceptDraft(d, testNow)
	if err != nil {
		t.Fatal(err)
	}
	// even with a record NAMED Kim Collab on the board
	if _, err := s.AddCandidate(QuickAdd{Text: "Kim Collab", Name: "Kim Collab"}, testNow); err != nil {
		t.Fatal(err)
	}
	resolve := s.PersonResolver()
	if _, ok := resolve("Kim Collab"); ok {
		t.Fatal("a display name resolved to a person")
	}
	if id, ok := resolve(c.ID); !ok || id != c.ID {
		t.Fatalf("a record id resolves to itself: %q %v", id, ok)
	}
	if id, ok := resolve("ext/orcid/" + orcidAvery); !ok || id != c.ID {
		t.Fatalf("a matched external key resolves to its record: %q %v", id, ok)
	}
	if _, ok := resolve("ext/orcid/" + orcidStranger); ok {
		t.Fatal("a stranger's ORCID resolved")
	}
	ties := DeriveTies(d, c.ID, s.LoadEdges().Edges(), resolve, testNow)
	if kinds := edgeKinds(ties.Edges); kinds[graph.EdgeCoauthor] != 0 || kinds[graph.EdgeSameLab] != 0 {
		t.Fatalf("a tie was invented: %+v", ties.Edges)
	}
	if len(ties.Skipped) != 2 {
		t.Fatalf("both refusals are named: %+v", ties.Skipped)
	}
	// and nothing at all without a candidate or a resolver
	if got := DeriveTies(d, "", nil, nil, testNow); len(got.Edges) != 0 || len(got.Papers) != 0 {
		t.Fatalf("no candidate, no claims: %+v", got)
	}
	if got := DeriveTies(d, c.ID, s.LoadEdges().Edges(), nil, testNow); len(got.Edges) != 1 || got.Edges[0].Kind != graph.EdgeAuthored {
		t.Fatalf("no resolver → nobody is known → authorship only: %+v", got.Edges)
	}
}

// Applying the same accept twice adds nothing; a pair claimed twice keeps
// the stronger claim — stated over inferred, then higher confidence — and
// a low-confidence overlap never displaces a cited coauthorship.
func TestDeriveTiesIdempotentAndConflictsResolveDeterministically(t *testing.T) {
	s, _ := testStore(t)
	gs, vault := testGraphStore(t)
	a, err := s.AcceptDraft(paperAuthorDraft("Avery Quill", orcidAvery, orcidKim), testNow)
	if err != nil {
		t.Fatal(err)
	}
	kim := paperAuthorDraft("Kim Collab", orcidKim, orcidAvery)
	k, err := s.AcceptDraft(kim, testNow)
	if err != nil {
		t.Fatal(err)
	}
	claims := DeriveKnowledge(kim, k.ID, "", testNow).WithTies(DeriveTies(kim, k.ID, s.LoadEdges().Edges(), s.PersonResolver(), testNow))
	first, err := ApplyKnowledge(gs, claims)
	if err != nil {
		t.Fatal(err)
	}
	// person + topic + paper; expertise + authored + coauthor
	if len(first.AddedEntities) != 3 || len(first.AddedEdges) != 3 {
		t.Fatalf("first apply: %d entities %d edges", len(first.AddedEntities), len(first.AddedEdges))
	}
	before := snapshot(t, vault)
	second, err := ApplyKnowledge(gs, claims)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.AddedEntities) != 0 || len(second.AddedEdges) != 0 {
		t.Fatalf("replay added %d/%d", len(second.AddedEntities), len(second.AddedEdges))
	}
	for p, b := range before {
		if after := snapshot(t, vault); after[p] != b {
			t.Fatalf("replay rewrote %s", p)
		}
	}
	// the same tie derived from the OTHER side is the same key — nothing new
	fromAvery := DeriveTies(paperAuthorDraft("Avery Quill", orcidAvery, orcidKim), a.ID, s.LoadEdges().Edges(), s.PersonResolver(), testNow)
	third, err := ApplyKnowledge(gs, KnowledgeClaims{Person: graph.Entity{ID: a.ID, Kind: graph.KindPerson}}.WithTies(fromAvery))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range third.AddedEdges {
		if e.Kind == graph.EdgeCoauthor {
			t.Fatalf("the pair was claimed a second time from the other side: %+v", e)
		}
	}

	// conflicts, over hand-built rows (the store itself never writes two
	// rows with one key): stated beats inferred whatever the number; among
	// equals the higher confidence wins
	rows := []Edge{
		{From: a.ID, To: k.ID, Kind: "coauthor", Basis: "derived overlap", Confidence: "0.80", Inferred: true, Source: "scan"},
		{From: k.ID, To: a.ID, Kind: "coauthor", Basis: "both authors on the paper", Confidence: "0.55", Inferred: false, Source: "openalex"},
		{From: a.ID, To: k.ID, Kind: "same_lab", Basis: "weaker", Confidence: "0.45", Inferred: true, Source: "openalex"},
		{From: k.ID, To: a.ID, Kind: "same_lab", Basis: "stronger", Confidence: "0.50", Inferred: true, Source: "openalex"},
	}
	got := DeriveTies(sources.CandidateDraft{}, k.ID, rows, s.PersonResolver(), testNow)
	if len(got.Edges) != 2 {
		t.Fatalf("one claim per key: %+v", got.Edges)
	}
	for _, e := range got.Edges {
		switch e.Kind {
		case "coauthor":
			if e.Inferred || e.Confidence != "0.55" {
				t.Fatalf("an inferred 0.80 displaced the cited claim: %+v", e)
			}
		case "same_lab":
			if e.Confidence != "0.50" || e.Basis != "stronger" {
				t.Fatalf("higher confidence wins: %+v", e)
			}
		}
		if e.From != PersonRef(a.ID) || e.To != PersonRef(k.ID) {
			t.Fatalf("canonical order regardless of the row's direction: %+v", e)
		}
	}
	// deterministic: the same rows in reverse order give the same answer
	rev := []Edge{rows[3], rows[2], rows[1], rows[0]}
	again := DeriveTies(sources.CandidateDraft{}, k.ID, rev, s.PersonResolver(), testNow)
	if fmt.Sprint(again.Edges) != fmt.Sprint(got.Edges) {
		t.Fatalf("order-dependent result:\n%+v\n%+v", got.Edges, again.Edges)
	}
}

// The calendar / notes derivations (same_meeting, co_mentioned) are
// untouched by the tie projection: they stay on the network read as they
// were, are never written to the general graph (not in its vocabulary), and
// network/edges.md is byte-identical after the general-graph write.
func TestDeriveTiesPreservesDerivedNetworkEdges(t *testing.T) {
	s, vault := testStore(t)
	gs, _ := testGraphStore(t)
	a, _ := s.AcceptDraft(paperAuthorDraft("Avery Quill", orcidAvery, orcidKim), testNow)
	kim := paperAuthorDraft("Kim Collab", orcidKim, orcidAvery)
	k, err := s.AcceptDraft(kim, testNow)
	if err != nil {
		t.Fatal(err)
	}
	s.UseDerivedEdges(func() []Edge {
		return []Edge{
			{From: a.ID, To: k.ID, Kind: "same_meeting", Basis: "both on the 2026-08-01 call", Confidence: "0.70", Inferred: true, Source: "calendar"},
			{From: a.ID, To: k.ID, Kind: "co_mentioned", Basis: "log/2026-08-02.md names both", Confidence: "0.40", Inferred: true, Source: "notes"},
		}
	})
	edgesFile := s.Path("network/edges.md")
	fileBefore, _ := os.ReadFile(edgesFile)
	netBefore := s.NetworkEdges()

	ties := DeriveTies(kim, k.ID, netBefore, s.PersonResolver(), testNow)
	kinds := edgeKinds(ties.Edges)
	if kinds["same_meeting"] != 0 || kinds["co_mentioned"] != 0 || kinds[graph.EdgeCoauthor] != 1 {
		t.Fatalf("only the platform-vocabulary ties are mirrored: %+v", kinds)
	}
	refused := map[string]bool{}
	for _, sk := range ties.Skipped {
		refused[sk.Kind] = true
	}
	if !refused["same_meeting"] || !refused["co_mentioned"] {
		t.Fatalf("the derived kinds are refused by name, not dropped silently: %+v", ties.Skipped)
	}
	if _, err := ApplyKnowledge(gs, DeriveKnowledge(kim, k.ID, "", testNow).WithTies(ties)); err != nil {
		t.Fatal(err)
	}
	fileAfter, _ := os.ReadFile(edgesFile)
	if string(fileBefore) != string(fileAfter) {
		t.Fatal("a general-graph write touched network/edges.md")
	}
	if _, err := os.Stat(vault + "/system/aion/recruiting/network/edges.md"); err != nil {
		t.Fatal(err)
	}
	netAfter := s.NetworkEdges()
	if len(netAfter) != len(netBefore) {
		t.Fatalf("the network read changed: %d → %d", len(netBefore), len(netAfter))
	}
	derived := 0
	for _, e := range netAfter {
		if e.Derived {
			derived++
		}
	}
	if derived != 2 {
		t.Fatalf("the log-derived edges are still there: %d", derived)
	}
	for _, e := range gs.LoadEdges().Edges() {
		if e.Kind == "same_meeting" || e.Kind == "co_mentioned" {
			t.Fatalf("a derived edge was serialized into the general graph: %+v", e)
		}
	}
}

// A PubMed draft resolved through OpenAlex's work object carries the paper's
// coauthors — by durable key — onto the draft, and a second lookup adds
// nothing.
func TestPubMedLookupCarriesCoauthorClaims(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/entrez/eutils/esearch.fcgi":
			fmt.Fprint(w, `{"esearchresult":{"idlist":["39000001"]}}`)
		case "/entrez/eutils/efetch.fcgi":
			fmt.Fprint(w, `<PubmedArticleSet><PubmedArticle><MedlineCitation><PMID>39000001</PMID><Article><ArticleTitle>Diffusion MRI reconstruction.</ArticleTitle><AuthorList>
				<Author><LastName>Yu</LastName><ForeName>G</ForeName><Initials>G</Initials></Author>
				<Author><LastName>Park</LastName><ForeName>S</ForeName><Initials>S</Initials></Author>
				</AuthorList></Article></MedlineCitation><PubmedData><ArticleIdList><ArticleId IdType="doi">10.1000/dmri.2025</ArticleId></ArticleIdList></PubmedData></PubmedArticle></PubmedArticleSet>`)
		case "/works/pmid:39000001":
			fmt.Fprint(w, `{"id":"https://openalex.org/W1234","doi":"https://doi.org/10.1000/dmri.2025","title":"Diffusion MRI reconstruction.","authorships":[
				{"author_position":"first","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A1234","display_name":"Guang Yu"},"institutions":[{"id":"https://openalex.org/I1","display_name":"Example University"}]},
				{"author_position":"last","raw_author_name":"Park S","author":{"id":"https://openalex.org/A5678","display_name":"Sun Park","orcid":"https://orcid.org/0000-0003-0000-0003"},"institutions":[{"id":"https://openalex.org/I1","display_name":"Example University"}]},
				{"author_position":"middle","raw_author_name":"Nobody","author":{"display_name":"No Key"}}]}`)
		case "/authors/A1234":
			fmt.Fprint(w, `{"id":"https://openalex.org/A1234","display_name":"Guang Yu","last_known_institution":{"display_name":"Example University"},"topics":[{"display_name":"Diffusion MRI"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rs, store, _ := testRunStore(t, nil)
	rs.Register(sources.PubMed{BaseURL: srv.URL, Client: *srv.Client()})
	rs.Register(sources.OpenAlex{BaseURL: srv.URL, Client: *srv.Client()})
	run := mustRun(t, rs, RunRequest{Source: "pubmed", Query: "diffusion MRI"})
	if len(run.Drafts[0].Draft.Edges) != 0 {
		t.Fatal("PubMed alone names nobody by key")
	}
	run, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Edges != 2 {
		t.Fatalf("one coauthor + one same_lab claim ride the resolved paper: %+v", res)
	}
	d := run.Drafts[0].Draft
	for _, e := range d.Edges {
		if e.From != "ext/orcid/0000-0003-0000-0003" || e.Evidence != "https://doi.org/10.1000/dmri.2025" || e.To != "" {
			t.Fatalf("claim shape: %+v", e)
		}
		if strings.Contains(e.Basis, "No Key") {
			t.Fatalf("a keyless author is never an endpoint: %+v", e)
		}
	}
	_, again, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil || again.Edges != 0 {
		t.Fatalf("a second lookup re-claims nothing: %+v %v", again, err)
	}
	// accepted: the claims land in network/edges.md by external key, and the
	// paper reached through PubMed and through OpenAlex is ONE node (the DOI)
	_, c, err := rs.Accept(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	rows := store.LoadEdges().Edges()
	if len(rows) != 2 || rows[0].To != c.ID || rows[0].Evidence != "https://doi.org/10.1000/dmri.2025" {
		t.Fatalf("network rows: %+v", rows)
	}
	ties := DeriveTies(d, c.ID, rows, store.PersonResolver(), testNow)
	if len(ties.Papers) != 1 || ties.Papers[0].ID != "doi/10.1000/dmri.2025" || !strings.HasPrefix(ties.Papers[0].Title, "Diffusion MRI reconstruction") {
		t.Fatalf("one paper by DOI: %+v", ties.Papers)
	}
	if len(ties.Skipped) != 2 {
		t.Fatalf("the stranger is refused for both kinds: %+v", ties.Skipped)
	}
}

func TestPaperRef(t *testing.T) {
	for in, want := range map[string]string{
		"https://doi.org/10.1038/s41586-020-2649-2": "doi/10.1038/s41586-020-2649-2",
		"http://doi.org/10.1000/ABC.Def":            "doi/10.1000/abc.def",
		"https://openalex.org/W3035965352":          "ext/openalex/W3035965352",
		"https://pubmed.ncbi.nlm.nih.gov/39000001/": "ext/pubmed/39000001",
		"https://openalex.org/A123":                 "",
		"https://lab.example/people/avery":          "",
		"":                                          "",
	} {
		got, ok := PaperRef(in)
		if (want == "") == ok || got.ID != want || (ok && got.Kind != graph.KindPaper) {
			t.Errorf("PaperRef(%q) = %+v %v, want %q", in, got, ok, want)
		}
	}
}
