package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"manifest/graph"
	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// Social ties + domain leverage through the real mux: accepting two authors
// of one cited paper writes the coauthorship into network/edges.md (by key,
// then resolved) and mirrors it — with the paper and each authorship — into
// the general graph once BOTH are known; the leverage read then ranks them
// with every component visible, and says so plainly when a topic has no
// expert.

// paperAdapter is a work sweep: two authors on one paper, each carrying the
// other's durable key as a coauthor claim and a shared-institution overlap,
// plus a third author nobody can name again (no key → no claim).
type paperAdapter struct{}

const (
	leverageDOI  = "https://doi.org/10.1000/coil.2026"
	leverageCite = "Coil arrays for low-field MRI, Nature, 2026, 10.1000/coil.2026"
)

func (paperAdapter) ID() string         { return "paper" }
func (paperAdapter) Kind() sources.Kind { return sources.KindScholarly }
func (paperAdapter) Scope() []sources.ScopeField {
	return []sources.ScopeField{{Key: "query", Label: "query", Required: true}}
}
func (paperAdapter) Search(_ context.Context, _ sources.Scope) ([]sources.CandidateDraft, error) {
	when := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	author := func(name, orcid, other string, topics ...string) sources.CandidateDraft {
		d := sources.CandidateDraft{
			SourceID: "paper", ExternalID: orcid, Name: name, Org: "Example Lab",
			Links: []string{"https://orcid.org/" + orcid}, Topics: topics,
			Evidence: []sources.Evidence{{
				SourceID: "paper", URLOrFile: leverageDOI, RetrievedAt: when,
				Snippet: leverageCite + " · author · Example Lab · 3 authors · topics: " + strings.Join(topics, "; "),
				Kind:    sources.EvidencePublication, Trust: sources.TrustMedium,
			}},
		}
		if other != "" {
			d.Edges = []sources.EdgeClaim{
				{From: "ext/orcid/" + other, Type: sources.EdgeCoauthor, SourceID: "paper", Basis: "both authors on " + leverageCite, Confidence: 0.55, Evidence: leverageDOI},
				{From: "ext/orcid/" + other, Type: sources.EdgeSameLab, SourceID: "paper", Basis: "both list Example Lab on " + leverageCite, Confidence: 0.45, Inferred: true, Evidence: leverageDOI},
			}
		}
		return d
	}
	return []sources.CandidateDraft{
		author("Avery Quill", "0000-0001-2345-6789", "0000-0002-0000-0002", "Low-Field MRI", "Coil Design"),
		author("Kim Collab", "0000-0002-0000-0002", "0000-0001-2345-6789", "Low-Field MRI"),
		author("Sam Keyless", "", "", "Optics"),
	}, nil
}
func (paperAdapter) Enrich(_ context.Context, d sources.CandidateDraft) (sources.CandidateDraft, error) {
	return d, nil
}
func (paperAdapter) GraphEdges(_ context.Context, d sources.CandidateDraft) ([]sources.EdgeClaim, error) {
	return d.Edges, nil
}

func leverageGet(t *testing.T, mux http.Handler, query string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/aion/recruiting/leverage"+query, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var out map[string]any
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: %v\n%s", query, err, w.Body.String())
		}
	}
	return w.Code, out
}

func TestRecruitingAcceptDerivesSocialTiesAndLeverageRanksThem(t *testing.T) {
	s, mux, gs, led := testRecruitingPhase3Server(t)
	s.recruitingRuns.Register(paperAdapter{})
	mux = s.Handler()
	run := startRun(t, mux, `{"source":"paper","role":"role/mri-engineer","query":"coil"}`)

	// ---- first accept: the other author is still a stranger
	p1 := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d1", ""))
	avery := *p1.Candidate
	var k1 recruiting.KnowledgeResult
	if err := json.Unmarshal(p1.Raw["knowledge"], &k1); err != nil {
		t.Fatal(err)
	}
	if len(k1.Claims.Papers) != 1 || k1.Claims.Papers[0].ID != "doi/10.1000/coil.2026" {
		t.Fatalf("the paper is registered on the first accept: %+v", k1.Claims.Papers)
	}
	if len(k1.Claims.Skipped) != 2 || !strings.HasPrefix(k1.Claims.Skipped[0].Endpoint, "ext/orcid/0000-0002") {
		t.Fatalf("the refused ties are named in the payload: %+v", k1.Claims.Skipped)
	}
	for _, e := range k1.AddedEdges {
		if e.From.Kind == graph.KindPerson && e.To.Kind == graph.KindPerson {
			t.Fatalf("a tie to a stranger reached the general graph: %+v", e)
		}
	}

	// ---- second accept: both known → the ties land, canonically ordered
	p2 := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d2", ""))
	kim := *p2.Candidate
	var k2 recruiting.KnowledgeResult
	if err := json.Unmarshal(p2.Raw["knowledge"], &k2); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range k2.AddedEdges {
		kinds[e.Kind]++
		if e.Kind == graph.EdgeCoauthor || e.Kind == graph.EdgeSameLab {
			if e.From != recruiting.PersonRef(avery.ID) || e.To != recruiting.PersonRef(kim.ID) || e.Evidence != leverageDOI {
				t.Fatalf("tie shape: %+v", e)
			}
		}
	}
	if kinds[graph.EdgeExpertise] != 1 || kinds[graph.EdgeAuthored] != 1 || kinds[graph.EdgeCoauthor] != 1 || kinds[graph.EdgeSameLab] != 1 {
		t.Fatalf("second accept added: %v", kinds)
	}
	if len(k2.Claims.Skipped) != 0 {
		t.Fatalf("nothing refused when both are known: %+v", k2.Claims.Skipped)
	}
	// the paper was registered once
	if _, ok := gs.LoadEntities().Find(graph.R(graph.KindPaper, "doi/10.1000/coil.2026")); !ok {
		t.Fatal("paper entity missing")
	}
	papers := 0
	for _, e := range gs.LoadEntities().Entities() {
		if e.Kind == graph.KindPaper {
			papers++
		}
	}
	if papers != 1 {
		t.Fatalf("papers registered: %d", papers)
	}
	// network/edges.md holds the resolved rows with the paper as evidence
	rows := s.recruiting.LoadEdges().Edges()
	if len(rows) != 2 {
		t.Fatalf("network rows: %+v", rows)
	}
	for _, r := range rows {
		if r.From != kim.ID && r.To != kim.ID || r.From != avery.ID && r.To != avery.ID || r.Evidence != leverageDOI {
			t.Fatalf("network row: %+v", r)
		}
	}
	// provenance: every derived claim rides on its from-endpoint — the
	// expertise and authorship under Kim, the two ties under Avery (the
	// canonical, smaller-id end of a symmetric claim) — and the accept
	// event counts the ties
	count := func(id string) map[string]int {
		n := map[string]int{}
		for _, k := range ledgerKinds(t, led, graph.KindPerson, id) {
			n[k]++
		}
		return n
	}
	if n := count(kim.ID); n["graph.edge.derived"] != 2 || n["graph.entity.added"] != 1 {
		t.Fatalf("ledger under %s: %v", kim.ID, n)
	}
	// avery's own accept: 2 expertise + 1 authored; kim's accept: 2 ties
	if n := count(avery.ID); n["graph.edge.derived"] != 5 {
		t.Fatalf("ledger under %s: %v", avery.ID, n)
	}
	if h, _ := led.History(graph.KindPerson, kim.ID); h.Entries[len(h.Entries)-1].Meta["ties"] != float64(2) || h.Entries[len(h.Entries)-1].Meta["papers"] != float64(1) {
		t.Fatalf("accept event meta: %+v", h.Entries[len(h.Entries)-1].Meta)
	}
	if k := ledgerKinds(t, led, graph.KindPaper, "doi/10.1000/coil.2026"); len(k) != 1 || k[0] != "graph.entity.added" {
		t.Errorf("paper registration event: %v", k)
	}

	// ---- the leverage read
	before := graphSnapshot(t, s.recruiting.Path("../../.."))
	code, r := leverageGet(t, mux, "?topic=low-field%20mri")
	if code != http.StatusOK {
		t.Fatalf("leverage: %d", code)
	}
	if r["known"] != true || r["experts"] != float64(2) || r["configured"] != true || r["topicId"] != "low field mri" {
		t.Fatalf("header: %+v", r)
	}
	people := r["people"].([]any)
	if len(people) != 2 {
		t.Fatalf("people: %+v", people)
	}
	// equal knowledge, equal reach (one coauthor + one same_lab tie to each
	// other, counted once each across the two files): tie broken by id
	for i, want := range []string{avery.ID, kim.ID} {
		m := people[i].(map[string]any)
		if m["id"] != want || m["role"] != "expert" || m["knowledge"] != 0.6 || m["connectivity"] != 0.55 || m["leverage"] != 0.93 || m["tieCount"] != float64(1) {
			t.Fatalf("person %d: %+v", i, m)
		}
		if m["name"] == "" || m["name"] == want {
			t.Fatalf("named, not just an id: %+v", m["name"])
		}
		ex := m["expertise"].(map[string]any)
		if ex["confidence"] != recruiting.KnowledgeConfidence || !strings.Contains(ex["basis"].(string), leverageDOI) {
			t.Fatalf("the knowledge component shows its works: %+v", ex)
		}
		ties := m["ties"].([]any)
		if len(ties) != 1 {
			t.Fatalf("one tie listed (the strongest edge to that person): %+v", ties)
		}
		tie := ties[0].(map[string]any)
		if tie["kind"] != "coauthor" || tie["known"] != true || tie["contribution"] != 0.55 || tie["evidence"] != leverageDOI || tie["basis"] == "" {
			t.Fatalf("the tie shows its edge: %+v", tie)
		}
	}
	if r["formula"] == "" {
		t.Fatal("the score is explained on every reply")
	}

	// ---- honest when nobody knows the topic; Sam is still a draft, so
	// optics has no expert — and the reply says so, and suggests nothing
	// unrelated
	code, r = leverageGet(t, mux, "?topic=optics")
	if code != http.StatusOK || r["known"] != false || r["experts"] != float64(0) || len(r["people"].([]any)) != 0 || !strings.Contains(r["note"].(string), "no expertise edge") {
		t.Fatalf("no expert: %d %+v", code, r)
	}
	code, r = leverageGet(t, mux, "?topic=mri%20coil")
	if code != http.StatusOK || r["known"] != false {
		t.Fatalf("near miss is not a match: %+v", r)
	}
	if sug := r["suggestions"].([]any); len(sug) != 2 || sug[0] != "coil design" || sug[1] != "low field mri" {
		t.Fatalf("the topics that share a word are offered: %+v", sug)
	}
	if code, _ := leverageGet(t, mux, ""); code != http.StatusBadRequest {
		t.Fatalf("no topic: %d", code)
	}
	// a read is a read
	after := graphSnapshot(t, s.recruiting.Path("../../.."))
	if len(before) != len(after) {
		t.Fatalf("the leverage read changed the file set: %d → %d", len(before), len(after))
	}
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("the leverage read rewrote %s", name)
		}
	}
}

// Without a graph store there is no knowledge overlay to rank: the reply
// is configured=false and empty, never an error.
func TestRecruitingLeverageWithoutGraphStoreIsEmpty(t *testing.T) {
	s, mux := testRecruitingSourcesServer(t)
	code, r := leverageGet(t, mux, "?topic=low-field%20mri")
	if code != http.StatusOK || r["configured"] != false || r["known"] != false {
		t.Fatalf("%d %+v", code, r)
	}
	_ = s
}
